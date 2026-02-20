package engine

import (
	"context"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
)

// Engine is a pluggable response engine used by the Agent.
// Implementations can be simple chat completion engines or tool-augmented ReAct agents.
//
// Implementations should not mutate session state; the caller (Agent)
// is responsible for persisting messages and accounting tokens.
// Return the textual response and, when available, usage information.
// If usage is not available, return nil for usage.
type Engine interface {
	Respond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (string, *llm.Usage, error)
}
