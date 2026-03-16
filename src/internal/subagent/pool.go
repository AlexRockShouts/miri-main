package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"sync"
	"time"
)

// EngineResponder is the minimal interface the pool needs from an engine.
type EngineResponder interface {
	Startup(ctx context.Context)
	Shutdown(ctx context.Context)
	Respond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (string, *llm.Usage, error)
}

// FactInjector allows injecting facts into a parent engine's memory.
type FactInjector interface {
	InjectFact(ctx context.Context, content string, metadata map[string]string) error
}

// EngineFactory creates a new EngineResponder for a sub-agent run.
type EngineFactory func(model string) (EngineResponder, error)

// Pool manages the lifecycle of dynamic sub-agent runs.
type Pool struct {
	mu      sync.RWMutex
	cancels map[string]context.CancelFunc

	factory    EngineFactory
	sessionMgr *session.SessionManager
	storage    *storage.Storage
	parentEng  FactInjector // orchestrator engine for fact injection (optional)
}

// NewPool creates a Pool.
func NewPool(factory EngineFactory, sm *session.SessionManager, st *storage.Storage, parentEng FactInjector) *Pool {
	return &Pool{
		cancels:    make(map[string]context.CancelFunc),
		factory:    factory,
		sessionMgr: sm,
		storage:    st,
		parentEng:  parentEng,
	}
}

// Spawn persists a new run record and starts the sub-agent goroutine.
// The run ID must be set by the caller before calling Spawn.
func (p *Pool) Spawn(ctx context.Context, run *storage.SubAgentRun) error {
	run.Status = "pending"
	run.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := p.storage.SaveSubAgentRun(run); err != nil {
		return fmt.Errorf("persist sub-agent run: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.cancels[run.ID] = cancel
	p.mu.Unlock()

	go p.execute(runCtx, run)
	return nil
}

// Cancel stops a running sub-agent by ID.
func (p *Pool) Cancel(id string) error {
	p.mu.Lock()
	cancel, ok := p.cancels[id]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("sub-agent run %s not found or already finished", id)
	}
	cancel()
	return nil
}

func (p *Pool) execute(ctx context.Context, run *storage.SubAgentRun) {
	defer func() {
		p.mu.Lock()
		delete(p.cancels, run.ID)
		p.mu.Unlock()
	}()

	run.Status = "running"
	run.StartedAt = time.Now().UTC().Format(time.RFC3339)
	_ = p.storage.SaveSubAgentRun(run)
	_ = p.storage.AppendSubAgentTranscript(run.ID, "user", run.Goal)

	eng, err := p.factory(run.Model)
	if err != nil {
		p.fail(run, fmt.Errorf("create engine: %w", err))
		return
	}
	eng.Startup(ctx)
	defer eng.Shutdown(context.Background())

	sessionID := "subagent-" + run.ID
	sess := p.sessionMgr.GetOrCreate(sessionID)

	sysCtx := fmt.Sprintf("You are a %s sub-agent. Solve the given goal autonomously.", run.Role)
	resp, usage, err := eng.Respond(ctx, sess, run.Goal, sysCtx)

	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)

	if err != nil {
		if ctx.Err() != nil {
			run.Status = "canceled"
		} else {
			run.Status = "failed"
			run.Error = err.Error()
		}
		_ = p.storage.SaveSubAgentRun(run)
		return
	}

	if usage != nil {
		run.PromptTokens = uint64(math.Max(0, float64(usage.PromptTokens)))
		run.OutputTokens = uint64(math.Max(0, float64(usage.CompletionTokens)))
		run.TotalCost = usage.TotalCost
	}
	run.Status = "done"
	run.Output = resp
	_ = p.storage.AppendSubAgentTranscript(run.ID, "assistant", resp)
	_ = p.storage.SaveSubAgentRun(run)

	p.injectFact(run)
}

func (p *Pool) fail(run *storage.SubAgentRun, err error) {
	run.Status = "failed"
	run.Error = err.Error()
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	_ = p.storage.SaveSubAgentRun(run)
	slog.Error("sub-agent run failed", "id", run.ID, "role", run.Role, "error", err)
}

func (p *Pool) injectFact(run *storage.SubAgentRun) {
	if p.parentEng == nil || run.Output == "" {
		return
	}
	content := fmt.Sprintf("[SubAgent:%s] Goal: %s\nResult: %s", run.Role, run.Goal, run.Output)
	metadata := map[string]string{
		"type":        "subagent_result",
		"subagent_id": run.ID,
		"role":        run.Role,
		"session":     run.ParentSession,
		"created_at":  run.FinishedAt,
	}
	if err := p.parentEng.InjectFact(context.Background(), content, metadata); err != nil {
		slog.Warn("failed to inject sub-agent fact into brain", "id", run.ID, "error", err)
	}
}
