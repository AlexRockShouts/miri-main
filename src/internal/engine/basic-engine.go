package engine

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
)

// BasicEngine implements a straightforward chat-completion flow
// using the configured provider/model with fallbacks.
// The previous inline logic from Agent has been refactored here.
type BasicEngine struct {
	cfg *config.Config
}

func NewBasicEngine(cfg *config.Config) *BasicEngine {
	return &BasicEngine{cfg: cfg}
}

func (b *BasicEngine) PrimaryModel() string {
	if b.cfg.Agents.Defaults.Model.Primary != "" {
		return b.cfg.Agents.Defaults.Model.Primary
	}
	return "xai/grok-beta"
}

func (b *BasicEngine) Respond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (string, *llm.Usage, error) {
	soul := sess.GetSoul()
	messages := []llm.Message{
		{Role: "system", Content: soul + humanContext},
		{Role: "user", Content: prompt},
	}

	models := []string{b.PrimaryModel()}
	// If dynamic model is provided in context, use it as the first choice
	if opts, ok := FromContext(ctx); ok && opts.Model != "" {
		models = []string{opts.Model}
	}

	for _, fb := range b.cfg.Agents.Defaults.Model.Fallbacks {
		models = append(models, fb)
	}

	var lastErr error
	for _, model := range models {
		slog.Debug("attempting LLM", "model", model, "session", sess.ID)
		response, usage, err := llm.ChatCompletion(b.cfg, model, messages)
		if err == nil {
			slog.Info("LLM success", "model", model, "session", sess.ID)
			return response, usage, nil
		}
		lastErr = fmt.Errorf("model %s failed: %w", model, err)
		slog.Warn("LLM failed", "model", model, "session", sess.ID, "error", err)
	}
	return "", nil, fmt.Errorf("all models failed: primary=%s fallbacks=%v: %w", b.PrimaryModel(), b.cfg.Agents.Defaults.Model.Fallbacks, lastErr)
}

func (b *BasicEngine) StreamRespond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (<-chan string, error) {
	ch := make(chan string, 2)
	go func() {
		defer close(ch)
		resp, usage, err := b.Respond(ctx, sess, prompt, humanContext)
		if err != nil {
			ch <- fmt.Sprintf("Error: %v", err)
			return
		}
		ch <- resp
		if usage != nil {
			ch <- fmt.Sprintf("[Usage: %d prompt, %d completion, %d total tokens]", usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
		}
	}()
	return ch, nil
}
