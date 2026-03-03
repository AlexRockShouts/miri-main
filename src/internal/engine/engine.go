package engine

import (
	"context"
	"miri-main/src/internal/engine/memory"
	"miri-main/src/internal/engine/memory/mole_syn"
	"miri-main/src/internal/engine/skills"
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

// Responder handles prompt execution — the core of what the agent loop needs.
type Responder interface {
	Respond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (string, *llm.Usage, error)
	StreamRespond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (<-chan string, error)
}

// SkillManager handles skill lifecycle operations.
type SkillManager interface {
	ListSkills() []*skills.Skill
	ListSkillCommands(ctx context.Context) ([]SkillCommand, error)
	ListRemoteSkills(ctx context.Context) ([]string, error)
	InstallSkill(ctx context.Context, name string) (string, error)
	RemoveSkill(name string) error
	GetSkill(name string) (*skills.Skill, error)
}

// MemoryManager handles memory and history operations.
type MemoryManager interface {
	ClearHistory(sessionID string)
	GetHistory(sessionID string) []session.Message
	CompactMemory(ctx context.Context, sessionID string)
	TriggerMaintenance(ctx context.Context)
	GetBrainFacts(ctx context.Context) ([]memory.SearchResult, error)
	GetBrainSummaries(ctx context.Context) ([]memory.SearchResult, error)
	GetBrainTopology(ctx context.Context, sessionID string) (*mole_syn.TopologyData, error)
}

// Lifecycle manages engine startup and shutdown.
type Lifecycle interface {
	Startup(ctx context.Context)
	Shutdown(ctx context.Context)
}

// Engine is a pluggable response engine used by the Agent.
// It composes all sub-interfaces. Implementations can be simple chat completion
// engines or tool-augmented ReAct agents.
type Engine interface {
	Responder
	SkillManager
	MemoryManager
	Lifecycle
}

type SkillCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
