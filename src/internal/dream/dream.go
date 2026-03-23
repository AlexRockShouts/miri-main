package dream

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/llm"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryWriter is the minimal interface required to persist dream results.
type MemoryWriter interface {
	AppendToMemory(text string) error
}

// Result holds the outcome of a single simulated CoT path.
type Result struct {
	Path  int     `json:"path"`
	Plan  string  `json:"plan"`
	Score float64 `json:"score"`
}

// Report is the full output of a dream simulation run.
type Report struct {
	Goal      string        `json:"goal"`
	Paths     int           `json:"paths"`
	Duration  time.Duration `json:"duration_ms"`
	BestPlan  string        `json:"best_plan"`
	BestScore float64       `json:"best_score"`
	AllPlans  []Result      `json:"all_plans"`
}

// Simulator runs offline chain-of-thought simulations for a given goal.
type Simulator struct {
	cfg     *config.Config
	storage MemoryWriter
}

// New creates a new Simulator with the given config and optional memory writer.
func New(cfg *config.Config, storage MemoryWriter) *Simulator {
	return &Simulator{cfg: cfg, storage: storage}
}

// Run executes n parallel CoT simulations, scores each path, persists the best
// plan to memory, and returns a full report.
func (s *Simulator) Run(ctx context.Context, goal string, n int) (*Report, error) {
	if n <= 0 {
		n = 10
	}
	if n > 1000 {
		n = 1000
	}

	modelStr := primaryModel(s.cfg)
	start := time.Now()

	slog.Info("dream simulation started", "goal", goal, "paths", n)

	results := make([]Result, n)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // max 8 concurrent LLM calls

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			plan, score, err := s.simulatePath(ctx, modelStr, goal, idx)
			if err != nil {
				slog.Warn("dream path failed", "path", idx, "error", err)
				results[idx] = Result{Path: idx, Plan: "", Score: -1}
				return
			}
			results[idx] = Result{Path: idx, Plan: plan, Score: score}
		}(i)
	}
	wg.Wait()

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	best := results[0]
	duration := time.Since(start)

	slog.Info("dream simulation complete",
		"goal", goal,
		"paths", n,
		"best_score", best.Score,
		"duration", duration,
	)

	// Persist best plan to memory
	if best.Plan != "" && s.storage != nil {
		entry := fmt.Sprintf("\n\n## Dream Plan [%s]\n**Goal:** %s\n**Score:** %.2f\n**Simulated paths:** %d\n\n%s\n",
			time.Now().Format(time.RFC3339), goal, best.Score, n, best.Plan)
		if err := s.storage.AppendToMemory(entry); err != nil {
			slog.Warn("dream: failed to persist best plan to memory", "error", err)
		}
	}

	return &Report{
		Goal:      goal,
		Paths:     n,
		Duration:  duration,
		BestPlan:  best.Plan,
		BestScore: best.Score,
		AllPlans:  results,
	}, nil
}

// simulatePath runs a single CoT simulation and returns the plan text and a quality score.
func (s *Simulator) simulatePath(ctx context.Context, modelStr, goal string, idx int) (string, float64, error) {
	messages := []llm.Message{
		{
			Role: "system",
			Content: `You are an expert strategic planner running an offline simulation.
Your task is to reason step-by-step (chain-of-thought) and produce an optimized, actionable plan.
Think deeply, consider edge cases, and output a structured plan with clear steps.
Be creative but grounded. Vary your approach from other simulations.`,
		},
		{
			Role: "user",
			Content: fmt.Sprintf(`Simulation path #%d.

Goal: %s

Think through this carefully using chain-of-thought reasoning. Explore a unique angle or strategy.
Then output a structured plan with:
1. Key insight / approach angle
2. Step-by-step action plan (numbered)
3. Risk mitigations
4. Success metrics

Begin your reasoning now:`, idx+1, goal),
		},
	}

	resp, _, err := llm.ChatCompletion(s.cfg, modelStr, messages)
	if err != nil {
		return "", 0, err
	}

	score := scorePlan(resp)
	return resp, score, nil
}

// scorePlan heuristically scores a plan based on structural richness and reasoning depth.
func scorePlan(plan string) float64 {
	if plan == "" {
		return 0
	}

	score := 0.0
	lower := strings.ToLower(plan)

	// Length reward (more detailed = better, up to a point)
	words := len(strings.Fields(plan))
	switch {
	case words > 500:
		score += 3.0
	case words > 200:
		score += 2.0
	case words > 100:
		score += 1.0
	}

	// Structural markers
	for _, marker := range []string{"step", "1.", "2.", "3.", "risk", "metric", "success", "action", "insight", "strategy"} {
		if strings.Contains(lower, marker) {
			score += 0.5
		}
	}

	// Reasoning depth indicators
	for _, indicator := range []string{"because", "therefore", "however", "consider", "alternatively", "tradeoff", "mitigation"} {
		if strings.Contains(lower, indicator) {
			score += 0.3
		}
	}

	// Penalize very short or empty responses
	if words < 50 {
		score -= 2.0
	}

	return score
}

// primaryModel returns the configured primary model string.
func primaryModel(cfg *config.Config) string {
	if cfg.Agents.Defaults.Model.Primary != "" {
		return cfg.Agents.Defaults.Model.Primary
	}
	return "xai/grok-4-1-fast-reasoning"
}
