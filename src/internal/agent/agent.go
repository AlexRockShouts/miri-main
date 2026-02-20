package agent

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"strings"
)

type Agent struct {
	Config     *config.Config
	SessionMgr *session.SessionManager `json:"-"`
	Storage    *storage.Storage        `json:"-"`
	Parent     *Agent                  `json:"-"`
	Eng        engine.Engine           `json:"-"`
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
	// Initialize engine based on configuration
	engineKind := strings.ToLower(a.Config.Agents.Defaults.Engine)
	if engineKind == "eino" {
		provider, model := a.splitModel(a.PrimaryModel())
		react, err := engine.NewEinoEngine(a.Config, provider, model)
		if err != nil {
			slog.Warn("failed to initialize Eino engine", "error", err)
		} else {
			a.Eng = react
		}
	} else {
		a.Eng = engine.NewBasicEngine(a.Config)
	}

	if a.Eng == nil { // fallback to basic engine if Eino failed or was not selected
		a.Eng = engine.NewBasicEngine(a.Config)
	}
}

func (a *Agent) splitModel(modelStr string) (string, string) {
	parts := strings.SplitN(modelStr, "/", 2)
	if len(parts) != 2 {
		return "xai", "grok-beta" // Default fallback
	}
	return parts[0], parts[1]
}

func (a *Agent) PrimaryModel() string {
	if a.Config.Agents.Defaults.Model.Primary != "" {
		return a.Config.Agents.Defaults.Model.Primary
	}
	return "xai/grok-beta"
}

func (a *Agent) DelegatePrompt(sessionID string, prompt string) (string, error) {
	return a.DelegatePromptWithOptions(context.Background(), sessionID, prompt, engine.Options{})
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

	if sessionID == "" {
		sessionID = "default"
	}
	session := a.SessionMgr.GetOrCreate(sessionID)
	if err := session.SetSoulIfEmpty(a.Storage); err != nil {
		return "", fmt.Errorf("load soul for session %s: %w", sessionID, err)
	}

	// Wrap context with dynamic options
	engineCtx := engine.WithOptions(ctx, opts)

	resp, usage, err := a.Eng.Respond(engineCtx, session, prompt, humanContext)
	if err != nil {
		return "", err
	}
	if usage != nil {
		session.AddTokens(uint64(usage.PromptTokens + usage.CompletionTokens))
	}
	a.SessionMgr.AddMessage(sessionID, prompt, resp)
	if strings.Contains(strings.ToLower(prompt), "write to memory") {
		a.Storage.AppendToMemory(fmt.Sprintf("Session %s: %s", sessionID, resp))
	}
	return resp, nil
}
