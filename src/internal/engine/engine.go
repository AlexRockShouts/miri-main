package engine

import (
	"context"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
)

type Options struct {
	Model       string   `json:"model,omitempty"`
	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
}

type optionsKey struct{}

func WithOptions(ctx context.Context, opts Options) context.Context {
	return context.WithValue(ctx, optionsKey{}, opts)
}

func FromContext(ctx context.Context) (Options, bool) {
	opts, ok := ctx.Value(optionsKey{}).(Options)
	return opts, ok
}

// Engine is a pluggable response engine used by the Agent.
// Implementations can be simple chat completion engines or tool-augmented ReAct agents.
//
// Implementations should not mutate session state; the caller (Agent)
// is responsible for persisting messages and accounting tokens.
// Return the textual response and, when available, usage information.
// If usage is not available, return nil for usage.
type Engine interface {
	Respond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (string, *llm.Usage, error)
	StreamRespond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (<-chan string, error)
	ListSkills() []any
	ListSkillCommands(ctx context.Context) ([]SkillCommand, error)
	ListRemoteSkills(ctx context.Context) (any, error)
	InstallSkill(ctx context.Context, name string) (string, error)
	RemoveSkill(name string) error
	GetSkill(name string) (any, error)
	SetTaskGateway(gw any)
	ClearHistory(sessionID string)
	CompactMemory(ctx context.Context)
	Shutdown(ctx context.Context)
}

type SkillCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
