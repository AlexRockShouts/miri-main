package agent

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/system"
	"strings"
)

type Agent struct {
	Config      *config.Config
	SessionMgr  *session.SessionManager `json:"-"`
	Storage     *storage.Storage        `json:"-"`
	Parent      *Agent                  `json:"-"`
	Eng         engine.Engine           `json:"-"`
	taskGateway any
}

func NewAgent(cfg *config.Config, sm *session.SessionManager, st *storage.Storage) *Agent {
	a := &Agent{
		Config:     cfg,
		SessionMgr: sm,
		Storage:    st,
	}

	a.InitEngine()

	return a
}

func (a *Agent) InitEngine() {
	provider, model := a.splitModel(a.PrimaryModel())
	react, err := engine.NewEinoEngine(a.Config, a.Storage, provider, model)
	if err != nil {
		slog.Error("failed to initialize Eino engine", "error", err)
	} else {
		a.Eng = react
		if a.taskGateway != nil {
			a.Eng.SetTaskGateway(a.taskGateway)
		}
	}
}

func (a *Agent) SetTaskGateway(gw any) {
	a.taskGateway = gw
	if a.Eng != nil {
		a.Eng.SetTaskGateway(gw)
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
	// Gather context from indexed human info
	humanInfos, err := a.Storage.ListHumanInfo()
	if err != nil {
		return "", fmt.Errorf("list human info: %w", err)
	}
	var contextBuilder strings.Builder
	if len(humanInfos) > 0 {
		contextBuilder.WriteString("\nInformation about my human:\n")
		for _, info := range humanInfos {
			contextBuilder.WriteString(fmt.Sprintf("- %s: %v. Notes: %s\\n", info.ID, info.Data, info.Notes))
		}
	}
	humanContext := contextBuilder.String()

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
			a.Eng.ClearHistory(sessionID)
			a.Eng.CompactMemory(ctx)
		}
		session.Clear()
		return "Session renewed. Current history cleared.", nil
	}

	if err := session.SetSoulIfEmpty(a.Storage); err != nil {
		return "", fmt.Errorf("load soul for session %s: %w", sessionID, err)
	}

	// Wrap context with dynamic options
	engineCtx := engine.WithOptions(ctx, opts)

	// If a specific model is requested, we might need to re-initialize it or use a temporary one
	eng := a.Eng
	if opts.Model != "" {
		provider, model := a.splitModel(a.PrimaryModel())
		provider, model = a.splitModel(opts.Model)
		eino, err := engine.NewEinoEngine(a.Config, a.Storage, provider, model)
		if err == nil {
			eng = eino
		} else {
			slog.Error("failed to initialize dynamic Eino engine", "error", err)
		}
	}

	resp, usage, err := eng.Respond(engineCtx, session, prompt, humanContext+sysContext)
	if err != nil {
		return "", err
	}
	if usage != nil {
		session.AddTokens(uint64(usage.PromptTokens), uint64(usage.CompletionTokens), usage.TotalCost)
	}

	lowerPrompt := strings.ToLower(prompt)
	if strings.Contains(lowerPrompt, "write to memory") || strings.Contains(lowerPrompt, "save a fact to memory") {
		if err := a.Storage.AppendToMemory(fmt.Sprintf("Session %s: %s", sessionID, resp)); err != nil {
			slog.Error("failed to append to memory", "error", err)
		}
	}
	return resp, nil
}

func (a *Agent) DelegatePromptStreamWithOptions(ctx context.Context, sessionID string, prompt string, opts engine.Options) (<-chan string, error) {
	// Gather context from indexed human info
	humanInfos, err := a.Storage.ListHumanInfo()
	if err != nil {
		return nil, fmt.Errorf("list human info: %w", err)
	}
	var contextBuilder strings.Builder
	if len(humanInfos) > 0 {
		contextBuilder.WriteString("\nInformation about my human:\n")
		for _, info := range humanInfos {
			contextBuilder.WriteString(fmt.Sprintf("- %s: %v. Notes: %s\\n", info.ID, info.Data, info.Notes))
		}
	}
	humanContext := contextBuilder.String()

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
			a.Eng.ClearHistory(sessionID)
		}
		session.Clear()

		proxy := make(chan string, 1)
		proxy <- "Session renewed. Current history cleared."
		close(proxy)
		return proxy, nil
	}

	if err := session.SetSoulIfEmpty(a.Storage); err != nil {
		return nil, fmt.Errorf("load soul for session %s: %w", sessionID, err)
	}

	// Wrap context with dynamic options
	engineCtx := engine.WithOptions(ctx, opts)

	eng := a.Eng
	if opts.Model != "" {
		provider, model := a.splitModel(a.PrimaryModel())
		provider, model = a.splitModel(opts.Model)
		eino, err := engine.NewEinoEngine(a.Config, a.Storage, provider, model)
		if err == nil {
			eng = eino
		} else {
			slog.Error("failed to initialize dynamic Eino engine (stream)", "error", err)
		}
	}

	stream, err := eng.StreamRespond(engineCtx, session, prompt, humanContext+sysContext)
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
					session.AddTokens(uint64(p), uint64(c), cost)
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
		resp := fullResp.String()
		if resp != "" {
			lowerPrompt := strings.ToLower(prompt)
			if strings.Contains(lowerPrompt, "write to memory") || strings.Contains(lowerPrompt, "save a fact to memory") {
				if err := a.Storage.AppendToMemory(fmt.Sprintf("Session %s: %s", sessionID, resp)); err != nil {
					slog.Error("failed to append to memory (stream)", "error", err)
				}
			}
		}
	}()

	return proxy, nil
}

func (a *Agent) ListSkills() []any {
	return a.Eng.ListSkills()
}

func (a *Agent) ListSkillCommands(ctx context.Context) ([]engine.SkillCommand, error) {
	return a.Eng.ListSkillCommands(ctx)
}

func (a *Agent) ListRemoteSkills(ctx context.Context) (any, error) {
	return a.Eng.ListRemoteSkills(ctx)
}

func (a *Agent) InstallSkill(ctx context.Context, name string) (string, error) {
	return a.Eng.InstallSkill(ctx, name)
}

func (a *Agent) RemoveSkill(name string) error {
	return a.Eng.RemoveSkill(name)
}

func (a *Agent) GetSkill(name string) (any, error) {
	return a.Eng.GetSkill(name)
}

func (a *Agent) Shutdown(ctx context.Context) {
	if a.Eng != nil {
		a.Eng.Shutdown(ctx)
	}
}

func (a *Agent) CompactMemory(ctx context.Context) {
	if a.Eng != nil {
		a.Eng.CompactMemory(ctx)
	}
}
