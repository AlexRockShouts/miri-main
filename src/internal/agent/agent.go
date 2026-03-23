package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/engine/skills"
	"miri-main/src/internal/engine/tools"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/system"
	"strings"
	"sync"
	"time"
)

type Agent struct {
	Config     *config.Config
	SessionMgr *session.SessionManager `json:"-"`
	Storage    *storage.Storage        `json:"-"`
	Parent     *Agent                  `json:"-"`
	Eng        engine.Engine           `json:"-"`
}

func NewAgent(cfg *config.Config, sm *session.SessionManager, st *storage.Storage, gw tools.TaskGateway) *Agent {
	a := &Agent{
		Config:     cfg,
		SessionMgr: sm,
		Storage:    st,
	}

	a.InitEngine(gw)

	return a
}

func (a *Agent) InitEngine(gw tools.TaskGateway) {
	provider, model := a.splitModel(a.PrimaryModel())
	react, err := engine.NewEinoEngine(a.Config, a.Storage, provider, model, gw)
	if err != nil {
		slog.Error("failed to initialize Eino engine", "error", err)
	} else {
		a.Eng = react
		a.Eng.Startup(context.Background())
	}
}

func (a *Agent) splitModel(modelStr string) (string, string) {
	parts := strings.SplitN(modelStr, "/", 2)
	if len(parts) != 2 {
		return "xai", "grok-4-1-fast-reasoning" // Default fallback
	}
	return parts[0], parts[1]
}

func (a *Agent) PrimaryModel() string {
	if a.Config.Agents.Defaults.Model.Primary != "" {
		return a.Config.Agents.Defaults.Model.Primary
	}
	return "xai/grok-4-1-fast-reasoning"
}

func (a *Agent) DelegatePrompt(sessionID string, prompt string) (string, error) {
	return a.DelegatePromptWithOptions(context.Background(), sessionID, prompt, engine.Options{})
}

func (a *Agent) DelegatePromptStream(sessionID string, prompt string) (<-chan string, error) {
	return a.DelegatePromptStreamWithOptions(context.Background(), sessionID, prompt, engine.Options{})
}

func (a *Agent) DelegatePromptWithOptions(ctx context.Context, sessionID string, prompt string, opts engine.Options) (string, error) {

	// Gather system context
	sysContext := fmt.Sprintf("\nSystem information:\n- %s\n", system.GetInfo())

	if sessionID == "" || sessionID == session.DefaultSessionID {
		sessionID = session.DefaultSessionID
	}
	session := a.SessionMgr.GetOrCreate(sessionID)

	// Intercept /new command
	if strings.TrimSpace(prompt) == "/new" {
		slog.Info("Session renewal triggered", "session_id", sessionID)
		if a.Eng != nil {
			go a.Eng.CompactMemory(ctx, sessionID)
			a.Eng.ClearHistory(sessionID)
		}
		session.Clear()
		return "Session renewed. Current history cleared.", nil
	}

	// Wrap context with dynamic options
	engineCtx := engine.WithOptions(ctx, opts)

	// If a specific model is requested, we might need to re-initialize it or use a temporary one
	eng := a.Eng
	if opts.Model != "" {
		provider, model := a.splitModel(a.PrimaryModel())
		provider, model = a.splitModel(opts.Model)
		eino, err := engine.NewEinoEngine(a.Config, a.Storage, provider, model, nil)
		if err == nil {
			eng = eino
		} else {
			slog.Error("failed to initialize dynamic Eino engine", "error", err)
		}
	}

	resp, usage, err := eng.Respond(engineCtx, session, prompt, sysContext)
	if err != nil {
		return "", err
	}
	if usage != nil {
		pt := math.Max(0, float64(usage.PromptTokens))
		ct := math.Max(0, float64(usage.CompletionTokens))
		session.AddTokens(uint64(pt), uint64(ct), usage.TotalCost)
	}

	return resp, nil
}

func (a *Agent) DelegatePromptStreamWithOptions(ctx context.Context, sessionID string, prompt string, opts engine.Options) (<-chan string, error) {

	// Gather system context
	sysContext := fmt.Sprintf("\nSystem information:\n- %s\n", system.GetInfo())

	if sessionID == "" || sessionID == session.DefaultSessionID {
		sessionID = session.DefaultSessionID
	}
	session := a.SessionMgr.GetOrCreate(sessionID)

	// Intercept /new command
	if strings.TrimSpace(prompt) == "/new" {
		slog.Info("Session renewal triggered (stream)", "session_id", sessionID)
		if a.Eng != nil {
			go a.Eng.CompactMemory(ctx, sessionID)
			a.Eng.ClearHistory(sessionID)
		}
		session.Clear()

		proxy := make(chan string, 1)
		proxy <- "Session renewed. Current history cleared."
		close(proxy)
		return proxy, nil
	}

	// Wrap context with dynamic options
	engineCtx := engine.WithOptions(ctx, opts)

	eng := a.Eng
	if opts.Model != "" {
		provider, model := a.splitModel(a.PrimaryModel())
		provider, model = a.splitModel(opts.Model)
		eino, err := engine.NewEinoEngine(a.Config, a.Storage, provider, model, nil)
		if err == nil {
			eng = eino
		} else {
			slog.Error("failed to initialize dynamic Eino engine (stream)", "error", err)
		}
	}

	stream, err := eng.StreamRespond(engineCtx, session, prompt, sysContext)
	if err != nil {
		return nil, err
	}

	// Create a proxy channel to intercept the final response for persistence
	proxy := make(chan string, 100)
	go func() {
		defer close(proxy)
		var fullResp strings.Builder
		for chunk := range stream {
			// In our current implementation, we send thoughts in brackets [Thought: ...]
			// Only chunks NOT in brackets are part of the final response to be persisted.
			// Actually, EinoEngine's Respond returns the final answer.
			// Our StreamRespond sends thoughts and then the final answer.
			if strings.HasPrefix(chunk, "[Usage: ") {
				var p, c, t int
				var cost float64
				_, err := fmt.Sscanf(chunk, "[Usage: %d prompt, %d completion, %d total tokens, %f cost]", &p, &c, &t, &cost)
				if err == nil {
					pt := math.Max(0, float64(p))
					ct := math.Max(0, float64(c))
					session.AddTokens(uint64(pt), uint64(ct), cost)
				}
				// Don't pass usage chunk to client if you want it to be hidden
				continue
			}

			// Only exclude specific meta-tags from the persisted history.
			isMeta := strings.HasPrefix(chunk, "[Thought:") ||
				strings.HasPrefix(chunk, "[Usage:") ||
				strings.HasPrefix(chunk, "[Tools:") ||
				strings.HasPrefix(chunk, "[Error:") ||
				strings.HasPrefix(chunk, "[Panic:") ||
				strings.HasPrefix(chunk, "[Stream Error:")

			if !isMeta {
				fullResp.WriteString(chunk)
			}
			proxy <- chunk
		}
	}()

	return proxy, nil
}

func (a *Agent) ListSkills() []*skills.Skill {
	return a.Eng.ListSkills()
}

func (a *Agent) ListSkillCommands(ctx context.Context) ([]engine.SkillCommand, error) {
	return a.Eng.ListSkillCommands(ctx)
}

func (a *Agent) ListRemoteSkills(ctx context.Context) ([]string, error) {
	return a.Eng.ListRemoteSkills(ctx)
}

func (a *Agent) InstallSkill(ctx context.Context, name string) (string, error) {
	return a.Eng.InstallSkill(ctx, name)
}

func (a *Agent) RemoveSkill(name string) error {
	return a.Eng.RemoveSkill(name)
}

func (a *Agent) GetSkill(name string) (*skills.Skill, error) {
	return a.Eng.GetSkill(name)
}

func (a *Agent) Shutdown(ctx context.Context) {
	if a.Eng != nil {
		a.Eng.Shutdown(ctx)
	}
}

func (a *Agent) CompactMemory(ctx context.Context, sessionID string) {
	if a.Eng != nil {
		a.Eng.CompactMemory(ctx, sessionID)
	}
}

func (a *Agent) TriggerMaintenance(ctx context.Context) {
	if a.Eng != nil {
		a.Eng.TriggerMaintenance(ctx)
	}
}

func (a *Agent) SpawnSubAgent(ctx context.Context, role, query string) (string, error) {
	return a.Eng.SpawnSubAgent(ctx, role, query)
}

func (a *Agent) waitForSubAgent(ctx context.Context, id string) (*storage.SubAgentRun, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			run, err := a.Storage.LoadSubAgentRun(id)
			if err != nil {
				continue
			}
			if run.Status == "completed" {
				return run, nil
			}
			if run.Status == "failed" {
				if run.Error != "" {
					return nil, errors.New(run.Error)
				}
				return nil, fmt.Errorf("subagent %s failed", id)
			}
			if run.Status == "cancelled" {
				return nil, fmt.Errorf("subagent %s cancelled", id)
			}
		}
	}
}

func (a *Agent) DelegateResearcher(ctx context.Context, query string) (string, error) {
	id, err := a.SpawnSubAgent(ctx, "researcher", query)
	if err != nil {
		return "", err
	}
	run, err := a.waitForSubAgent(ctx, id)
	if err != nil {
		return "", err
	}
	return run.Output, nil
}

func (a *Agent) DelegateCoder(ctx context.Context, query string) (string, error) {
	id, err := a.SpawnSubAgent(ctx, "coder", query)
	if err != nil {
		return "", err
	}
	run, err := a.waitForSubAgent(ctx, id)
	if err != nil {
		return "", err
	}
	return run.Output, nil
}

func (a *Agent) DelegateParallelResearchCoder(ctx context.Context, researchQuery, codeQuery string) (string, string, error) {
	type Result struct {
		Out string
		Err error
	}
	resCh := make(chan Result, 1)
	codeCh := make(chan Result, 1)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		out, err := a.DelegateResearcher(ctx, researchQuery)
		resCh <- Result{Out: out, Err: err}
	}()

	go func() {
		defer wg.Done()
		out, err := a.DelegateCoder(ctx, codeQuery)
		codeCh <- Result{Out: out, Err: err}
	}()

	wg.Wait()
	close(resCh)
	close(codeCh)

	res := <-resCh
	if res.Err != nil {
		return "", "", res.Err
	}
	code := <-codeCh
	if code.Err != nil {
		return "", "", code.Err
	}
	return res.Out, code.Out, nil
}
