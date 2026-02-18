package agent

import (
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"strings"
)

type Agent struct {
	Config     *config.Config
	SessionMgr *session.SessionManager `json:"-"`
	Storage    *storage.Storage        `json:"-"`
	Parent     *Agent                  `json:"-"`
}

func NewAgent(cfg *config.Config, sm *session.SessionManager, st *storage.Storage) *Agent {
	return &Agent{
		Config:     cfg,
		SessionMgr: sm,
		Storage:    st,
	}
}

func (a *Agent) Chat(modelStr string, messages []llm.Message) (string, error) {
	return llm.ChatCompletion(a.Config, modelStr, messages)
}

func (a *Agent) PrimaryModel() string {
	if a.Config.Agents.Defaults.Model.Primary != "" {
		return a.Config.Agents.Defaults.Model.Primary
	}
	return "xai/grok-beta"
}

func (a *Agent) DelegatePrompt(sessionID string, prompt string) (string, error) {
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

	session := a.SessionMgr.GetOrCreate(sessionID)
	if err := session.SetSoulIfEmpty(a.Storage); err != nil {
		return "", fmt.Errorf("load soul for session %s: %w", sessionID, err)
	}
	soul := session.GetSoul()

	messages := []llm.Message{
		{Role: "system", Content: soul + humanContext},
		{Role: "user", Content: prompt},
	}

	models := []string{a.PrimaryModel()}
	for _, fb := range a.Config.Agents.Defaults.Model.Fallbacks {
		models = append(models, fb)
	}

	var lastErr error
	for _, model := range models {
		slog.Debug("attempting LLM", "model", model, "session", sessionID)
		response, err := a.Chat(model, messages)
		if err == nil {
			slog.Info("LLM success", "model", model, "session", sessionID)
			a.SessionMgr.AddMessage(sessionID, prompt, response)

			if strings.Contains(strings.ToLower(prompt), "write to memory") {
				a.Storage.AppendToMemory(fmt.Sprintf("Session %s: %s", sessionID, response))
			}
			return response, nil
		}
		lastErr = fmt.Errorf("model %s failed: %w", model, err)
		slog.Warn("LLM failed", "model", model, "session", sessionID, "error", err)
	}
	return "", fmt.Errorf("all models failed: primary=%s fallbacks=%v: %w", a.PrimaryModel(), a.Config.Agents.Defaults.Model.Fallbacks, lastErr)
}
