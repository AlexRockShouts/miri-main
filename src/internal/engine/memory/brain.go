package memory

import (
	"context"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine/memory/mole_syn"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/system"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type Brain struct {
	chat              model.BaseChatModel
	factMemory        MemorySystem
	summaryMemory     MemorySystem
	stepsMemory       MemorySystem
	buffer            map[string][]*schema.Message
	mu                sync.RWMutex
	interactionCount  int
	lastTopologyScore int
	lastDeepBondRatio float32
	lastContextUsage  int
	contextWindow     int
	lastMaintenance   time.Time
	storage           *storage.Storage
	Graph             *mole_syn.MemoryGraph
	sanitizeMsgs      func([]*schema.Message) []*schema.Message
	retrieval         config.RetrievalConfig
}

func NewBrain(chat model.BaseChatModel, factMs, summaryMs, stepsMs MemorySystem, contextWindow int, st *storage.Storage, retrieval config.RetrievalConfig, maxNodesPerSession int) *Brain {
	var ms mole_syn.MemorySystem
	if stepsMs != nil {
		ms = &memorySystemWrapper{ms: stepsMs}
	}
	mg := mole_syn.New(chat, st, ms, maxNodesPerSession)
	b := &Brain{
		chat:             chat,
		factMemory:       factMs,
		summaryMemory:    summaryMs,
		stepsMemory:      stepsMs,
		buffer:           make(map[string][]*schema.Message),
		interactionCount: 0,
		contextWindow:    contextWindow,
		storage:          st,
		Graph:            mg,
		retrieval:        retrieval,
	}
	_ = b.syncPrompts()
	return b
}

type memorySystemWrapper struct {
	ms MemorySystem
}

func (w *memorySystemWrapper) Add(ctx context.Context, content string, metadata map[string]string) error {
	return w.ms.Add(ctx, content, metadata)
}

func (w *memorySystemWrapper) Delete(ctx context.Context, id string) error {
	return w.ms.Delete(ctx, id)
}

func (w *memorySystemWrapper) ListAll(ctx context.Context) ([]mole_syn.SearchResult, error) {
	res, err := w.ms.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mole_syn.SearchResult, len(res))
	for i, r := range res {
		out[i] = mole_syn.SearchResult{
			Content:  r.Content,
			Metadata: r.Metadata,
		}
	}
	return out, nil
}

func (b *Brain) SetSanitizeFunc(f func([]*schema.Message) []*schema.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sanitizeMsgs = f
}

func (b *Brain) sanitize(msgs []*schema.Message) []*schema.Message {
	b.mu.RLock()
	f := b.sanitizeMsgs
	b.mu.RUnlock()

	if f != nil {
		return f(msgs)
	}
	return msgs
}

func (b *Brain) syncPrompts() error {
	templatesDir := filepath.Join(system.GetProjectRoot(), "templates", "brain")
	if err := b.storage.SyncBrainPrompts(templatesDir); err != nil {
		slog.Warn("Failed to synchronize brain prompts from templates, falling back to storage", "error", err)
		return nil
	}

	slog.Info("Brain prompts synchronized")
	return nil
}

func (b *Brain) GetPrompt(name string) (string, error) {
	return b.storage.GetBrainPrompt(name)
}

func (b *Brain) AddToBuffer(sessionID string, msg *schema.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buffer == nil {
		b.buffer = make(map[string][]*schema.Message)
	}

	b.buffer[sessionID] = append(b.buffer[sessionID], msg)
	b.interactionCount++
	count := b.interactionCount

	// Short-term buffer: Keep last N turns (e.g., 100 messages = ~10-50 turns)
	const maxBuffer = 100
	if len(b.buffer[sessionID]) > maxBuffer {
		b.buffer[sessionID] = b.buffer[sessionID][len(b.buffer[sessionID])-maxBuffer:]
	}

	// Trigger maintenance every 100 writes
	if count > 0 && count%100 == 0 {
		go b.TriggerMaintenance(TriggerInteraction)
	}
}

func (b *Brain) ClearBuffer(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.buffer, sessionID)
}

func (b *Brain) GetBuffer(sessionID string) []*schema.Message {
	b.mu.RLock()
	defer b.mu.RUnlock()

	msgs := b.buffer[sessionID]
	if msgs == nil {
		return []*schema.Message{}
	}

	// Return a copy to avoid data races
	res := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		if m != nil && strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) == 0 {
			continue
		}
		res = append(res, m)
	}
	return res
}

func (b *Brain) checkFactDuplicate(ctx context.Context, fact string) (bool, string) {
	if b.factMemory == nil {
		return false, ""
	}
	// Check if fact already exists using vector search
	existing, err := b.factMemory.Search(ctx, fact, 1, map[string]string{"type": "fact"})
	if err != nil {
		slog.Warn("checkFactDuplicate search failed", "error", err)
		return false, ""
	}
	if len(existing) > 0 {
		// 0.15 is a strict threshold for semantic similarity (1.0 - CosineSimilarity)
		// 0.0 is identical, 1.0 is orthogonal.
		if existing[0].Distance < 0.15 {
			slog.Debug("checkFactDuplicate found identical or near-identical fact", "fact", fact, "existing", existing[0].Content, "distance", existing[0].Distance)
			return true, existing[0].Content
		}
	}
	return false, ""
}

func (b *Brain) AddReasoningTrace(ctx context.Context, sessionID, trace string) error {
	analysis, err := b.analyzeTopology(ctx, trace)
	if err != nil {
		return err
	}
	return b.Graph.AddStepsFromAnalysis(sessionID, analysis)
}
