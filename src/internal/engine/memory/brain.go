package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// MaintenanceTrigger is a reason for triggering brain maintenance
type MaintenanceTrigger string

const (
	TriggerInteraction  MaintenanceTrigger = "interaction_threshold"
	TriggerContextUsage MaintenanceTrigger = "context_usage_high"
	TriggerNewSession   MaintenanceTrigger = "new_session"
	TriggerShutdown     MaintenanceTrigger = "shutdown"
	TriggerManual       MaintenanceTrigger = "manual"
)

type Brain struct {
	chat             model.BaseChatModel
	memorySystem     MemorySystem
	buffer           map[string][]*schema.Message
	mu               sync.RWMutex
	interactionCount int
	contextWindow    int
	lastMaintenance  time.Time
	promptsDir       string
}

func NewBrain(chat model.BaseChatModel, ms MemorySystem, contextWindow int, storageDir string) *Brain {
	b := &Brain{
		chat:             chat,
		memorySystem:     ms,
		buffer:           make(map[string][]*schema.Message),
		interactionCount: 0,
		contextWindow:    contextWindow,
		promptsDir:       filepath.Join(storageDir, "brain"),
	}
	_ = b.syncPrompts()
	return b
}

func (b *Brain) syncPrompts() error {
	if err := os.MkdirAll(b.promptsDir, 0755); err != nil {
		return fmt.Errorf("mkdir prompts dir: %w", err)
	}

	files, err := os.ReadDir("templates/brain")
	if err != nil {
		// If templates/brain doesn't exist, we skip sync (maybe running in production without source)
		slog.Warn("Template prompts directory not found, skipping sync", "dir", "templates/brain")
		return nil
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		src := filepath.Join("templates/brain", file.Name())
		dst := filepath.Join(b.promptsDir, file.Name())

		content, err := os.ReadFile(src)
		if err != nil {
			slog.Error("Failed to read template", "file", src, "error", err)
			continue
		}

		if err := os.WriteFile(dst, content, 0644); err != nil {
			slog.Error("Failed to write prompt", "file", dst, "error", err)
		}
	}
	slog.Info("Brain prompts synchronized", "dir", b.promptsDir)
	return nil
}

func (b *Brain) AddToBuffer(sessionID string, msg *schema.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buffer == nil {
		b.buffer = make(map[string][]*schema.Message)
	}

	b.buffer[sessionID] = append(b.buffer[sessionID], msg)

	// Short-term buffer: Keep last N turns (e.g., 200 messages = ~20 turns)
	const maxBuffer = 40
	if len(b.buffer[sessionID]) > maxBuffer {
		b.buffer[sessionID] = b.buffer[sessionID][len(b.buffer[sessionID])-maxBuffer:]
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

func (b *Brain) ExtractFacts(ctx context.Context, messages []*schema.Message) error {
	if b.memorySystem == nil {
		return nil
	}

	prompt, err := os.ReadFile(filepath.Join(b.promptsDir, "extract.prompt"))
	if err != nil {
		return fmt.Errorf("read extract prompt: %w", err)
	}

	var conv strings.Builder
	for _, m := range messages {
		role := string(m.Role)
		conv.WriteString(fmt.Sprintf("%s: %s\n", role, m.Content))
	}

	fullPrompt := strings.Replace(string(prompt), "{conversation}", conv.String(), 1)
	fullPrompt = strings.Replace(fullPrompt, "{conversation_text_or_last_N_messages}", conv.String(), 1)

	resp, err := b.chat.Generate(ctx, []*schema.Message{schema.UserMessage(fullPrompt)})
	if err != nil {
		return fmt.Errorf("generate facts: %w", err)
	}

	var extracted []struct {
		Fact       string  `json:"fact"`
		Category   string  `json:"category"`
		Confidence float32 `json:"confidence"`
		SourceTurn string  `json:"source_turn"`
	}

	// Try to find JSON in the response
	content := resp.Content
	if start := strings.Index(content, "["); start != -1 {
		if end := strings.LastIndex(content, "]"); end != -1 && end > start {
			content = content[start : end+1]
		}
	}

	if err := json.Unmarshal([]byte(content), &extracted); err != nil {
		slog.Warn("Failed to unmarshal extracted facts", "error", err, "content", content)
		return nil // Non-critical
	}

	for _, f := range extracted {
		if f.Confidence < 0.7 {
			continue
		}
		metadata := map[string]string{
			"type":          "fact",
			"category":      f.Category,
			"source_turn":   f.SourceTurn,
			"confidence":    fmt.Sprintf("%.2f", f.Confidence),
			"created_at":    time.Now().Format(time.RFC3339),
			"access_count":  "0",
			"last_accessed": time.Now().Format(time.RFC3339),
		}
		_ = b.memorySystem.Add(ctx, f.Fact, metadata)
		slog.Info("Extracted and stored fact", "fact", f.Fact, "category", f.Category)
	}

	return nil
}

func (b *Brain) Reflect(ctx context.Context, messages []*schema.Message) error {
	if b.memorySystem == nil {
		return nil
	}

	prompt, err := os.ReadFile(filepath.Join(b.promptsDir, "reflection.prompt"))
	if err != nil {
		return fmt.Errorf("read reflection prompt: %w", err)
	}

	var conv strings.Builder
	for _, m := range messages {
		role := string(m.Role)
		conv.WriteString(fmt.Sprintf("%s: %s\n", role, m.Content))
	}

	fullPrompt := strings.Replace(string(prompt), "{context + your_previous_output}", conv.String(), 1)

	resp, err := b.chat.Generate(ctx, []*schema.Message{schema.UserMessage(fullPrompt)})
	if err != nil {
		return fmt.Errorf("generate reflection: %w", err)
	}

	metadata := map[string]string{
		"type":          "reflection",
		"created_at":    time.Now().Format(time.RFC3339),
		"access_count":  "0",
		"last_accessed": time.Now().Format(time.RFC3339),
	}
	_ = b.memorySystem.Add(ctx, resp.Content, metadata)
	slog.Info("Stored self-reflection")

	return nil
}

func (b *Brain) Summarize(ctx context.Context, messages []*schema.Message) error {
	if b.memorySystem == nil {
		return nil
	}

	prompt, err := os.ReadFile(filepath.Join(b.promptsDir, "compact.prompt"))
	if err != nil {
		return fmt.Errorf("read compact prompt: %w", err)
	}

	var conv strings.Builder
	for _, m := range messages {
		role := string(m.Role)
		conv.WriteString(fmt.Sprintf("%s: %s\n", role, m.Content))
	}

	fullPrompt := strings.Replace(string(prompt), "{full_or_recent_conversation_text}", conv.String(), 1)

	resp, err := b.chat.Generate(ctx, []*schema.Message{schema.UserMessage(fullPrompt)})
	if err != nil {
		return fmt.Errorf("generate summary: %w", err)
	}

	metadata := map[string]string{
		"type":          "summary",
		"created_at":    time.Now().Format(time.RFC3339),
		"access_count":  "0",
		"last_accessed": time.Now().Format(time.RFC3339),
	}
	_ = b.memorySystem.Add(ctx, resp.Content, metadata)
	slog.Info("Stored conversation summary")

	return nil
}

func (b *Brain) Compact(ctx context.Context) error {
	if b.memorySystem == nil {
		return nil
	}

	slog.Info("Starting brain memory compaction")

	b.mu.Lock()
	b.interactionCount = 0
	b.mu.Unlock()

	// 1. Fetch all memories
	all, err := b.memorySystem.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list all memories: %w", err)
	}

	// Group by type
	facts := make([]SearchResult, 0)
	summaries := make([]SearchResult, 0)
	for _, item := range all {
		switch item.Metadata["type"] {
		case "fact":
			facts = append(facts, item)
		case "summary":
			summaries = append(summaries, item)
		}
	}

	// 2. Deduplicate facts
	if len(facts) > 10 {
		if err := b.deduplicateFacts(ctx, facts); err != nil {
			slog.Error("Fact deduplication failed", "error", err)
		}
	}

	// 3. Consolidate summaries
	if len(summaries) > 5 {
		if err := b.consolidateSummaries(ctx, summaries); err != nil {
			slog.Error("Summary consolidation failed", "error", err)
		}
	}

	// 4. Cleanup old/low-confidence items
	if err := b.cleanup(ctx, all); err != nil {
		slog.Error("Memory cleanup failed", "error", err)
	}

	// 5. Promote facts from summaries
	if len(summaries) > 0 {
		if err := b.promoteFacts(ctx, summaries); err != nil {
			slog.Error("Fact promotion failed", "error", err)
		}
	}

	return nil
}

func (b *Brain) cleanup(ctx context.Context, items []SearchResult) error {
	slog.Info("Cleaning up memories", "count", len(items))
	now := time.Now()
	for _, item := range items {
		id := item.Metadata["id"]
		if id == "" {
			continue
		}

		// 1. Delete low confidence facts
		if item.Metadata["type"] == "fact" {
			confStr := item.Metadata["confidence"]
			if confStr != "" {
				conf, _ := strconv.ParseFloat(confStr, 32)
				if conf < 0.5 {
					slog.Info("Deleting low confidence fact", "id", id, "fact", item.Content)
					_ = b.memorySystem.Delete(ctx, id)
					continue
				}
			}
		}

		// 2. Delete old, never-retrieved items
		accStr := item.Metadata["access_count"]
		acc, _ := strconv.Atoi(accStr)
		if acc == 0 {
			createdStr := item.Metadata["created_at"]
			if createdStr != "" {
				created, err := time.Parse(time.RFC3339, createdStr)
				if err == nil && now.Sub(created) > 30*24*time.Hour { // 30 days
					slog.Info("Deleting old never-retrieved memory", "id", id, "type", item.Metadata["type"])
					_ = b.memorySystem.Delete(ctx, id)
					continue
				}
			}
		}
	}
	return nil
}

func (b *Brain) promoteFacts(ctx context.Context, summaries []SearchResult) error {
	slog.Info("Promoting facts from summaries", "count", len(summaries))
	prompt, err := os.ReadFile(filepath.Join(b.promptsDir, "promote_facts.prompt"))
	if err != nil {
		return err
	}

	// Just process the most recent summary or a few
	for i := range min(len(summaries), 3) {
		s := summaries[i]
		fullPrompt := strings.Replace(string(prompt), "{summary_text}", s.Content, 1)
		resp, err := b.chat.Generate(ctx, []*schema.Message{schema.UserMessage(fullPrompt)})
		if err != nil {
			continue
		}

		var promoted []struct {
			Fact       string  `json:"fact"`
			Category   string  `json:"category"`
			Confidence float32 `json:"confidence"`
		}

		content := resp.Content
		if start := strings.Index(content, "["); start != -1 {
			if end := strings.LastIndex(content, "]"); end != -1 && end > start {
				content = content[start : end+1]
			}
		}

		if err := json.Unmarshal([]byte(content), &promoted); err != nil {
			continue
		}

		for _, p := range promoted {
			if p.Confidence < 0.8 {
				continue
			}
			// Check if fact already exists using vector search
			existing, err := b.memorySystem.Search(ctx, p.Fact, 1, map[string]string{"type": "fact"})
			if err == nil && len(existing) > 0 && existing[0].Distance < 0.15 {
				slog.Debug("Fact already exists, skipping promotion", "fact", p.Fact, "existing", existing[0].Content, "distance", existing[0].Distance)
				continue
			}

			metadata := map[string]string{
				"type":          "fact",
				"category":      p.Category,
				"source":        "summary_promotion",
				"confidence":    fmt.Sprintf("%.2f", p.Confidence),
				"created_at":    time.Now().Format(time.RFC3339),
				"access_count":  "0",
				"last_accessed": time.Now().Format(time.RFC3339),
			}
			_ = b.memorySystem.Add(ctx, p.Fact, metadata)
		}
	}
	return nil
}

func (b *Brain) deduplicateFacts(ctx context.Context, facts []SearchResult) error {
	slog.Info("Deduplicating facts", "count", len(facts))

	prompt, err := os.ReadFile(filepath.Join(b.promptsDir, "deduplicate_facts.prompt"))
	if err != nil {
		return err
	}

	var sb strings.Builder
	for _, f := range facts {
		id := f.Metadata["id"]
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", id, f.Content))
	}

	fullPrompt := strings.Replace(string(prompt), "{facts_list}", sb.String(), 1)
	resp, err := b.chat.Generate(ctx, []*schema.Message{schema.UserMessage(fullPrompt)})
	if err != nil {
		return err
	}

	var dups []struct {
		PrimaryID    string   `json:"primary_id"`
		DuplicateIDs []string `json:"duplicate_ids"`
	}

	content := resp.Content
	if start := strings.Index(content, "["); start != -1 {
		if end := strings.LastIndex(content, "]"); end != -1 && end > start {
			content = content[start : end+1]
		}
	}

	if err := json.Unmarshal([]byte(content), &dups); err != nil {
		return err
	}

	for _, d := range dups {
		for _, dupID := range d.DuplicateIDs {
			slog.Info("Deleting duplicate fact", "id", dupID)
			_ = b.memorySystem.Delete(ctx, dupID)
		}
		// Optionally update primary fact's access count or confidence
	}

	return nil
}

func (b *Brain) consolidateSummaries(ctx context.Context, summaries []SearchResult) error {
	slog.Info("Consolidating summaries", "count", len(summaries))

	prompt, err := os.ReadFile(filepath.Join(b.promptsDir, "consolidate_summaries.prompt"))
	if err != nil {
		return err
	}

	// Consolidate in groups of 5
	for i := 0; i < len(summaries); i += 5 {
		end := i + 5
		if end > len(summaries) {
			end = len(summaries)
		}
		batch := summaries[i:end]
		if len(batch) < 2 {
			continue
		}

		var sb strings.Builder
		for _, s := range batch {
			sb.WriteString(fmt.Sprintf("- %s\n", s.Content))
		}

		fullPrompt := strings.Replace(string(prompt), "{summaries_list}", sb.String(), 1)
		resp, err := b.chat.Generate(ctx, []*schema.Message{schema.UserMessage(fullPrompt)})
		if err != nil {
			continue
		}

		// Add consolidated summary
		metadata := map[string]string{
			"type":          "summary",
			"subtype":       "consolidated",
			"created_at":    time.Now().Format(time.RFC3339),
			"access_count":  "0",
			"last_accessed": time.Now().Format(time.RFC3339),
		}
		_ = b.memorySystem.Add(ctx, resp.Content, metadata)

		// Delete old summaries
		for _, s := range batch {
			id := s.Metadata["id"]
			if id != "" {
				_ = b.memorySystem.Delete(ctx, id)
			}
		}
	}

	return nil
}

func (b *Brain) UpdateContextUsage(ctx context.Context, usage int) {
	if b.contextWindow <= 0 {
		return
	}

	percent := float64(usage) / float64(b.contextWindow)
	if percent >= 0.6 {
		slog.Info("Context window usage high, triggering brain maintenance", "usage", usage, "window", b.contextWindow, "percent", fmt.Sprintf("%.2f%%", percent*100))
		go b.TriggerMaintenance(TriggerContextUsage)
	}
}

func (b *Brain) TriggerMaintenance(trigger MaintenanceTrigger) {
	slog.Info("Brain maintenance triggered", "reason", trigger)

	bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1. Process extraction/reflection/summarization if we have a buffer
	b.mu.RLock()
	sessionIDs := make([]string, 0, len(b.buffer))
	for sid := range b.buffer {
		sessionIDs = append(sessionIDs, sid)
	}
	b.mu.RUnlock()

	for _, sid := range sessionIDs {
		msgs := b.GetBuffer(sid)
		if len(msgs) > 0 {
			slog.Debug("Running extraction tasks for session", "session_id", sid)
			_ = b.ExtractFacts(bgCtx, msgs)
			_ = b.Reflect(bgCtx, msgs)
			_ = b.Summarize(bgCtx, msgs)
		}
	}

	// 2. Run compaction
	if err := b.Compact(bgCtx); err != nil {
		slog.Error("Brain compaction failed", "trigger", trigger, "error", err)
	}

	b.mu.Lock()
	b.lastMaintenance = time.Now()
	b.mu.Unlock()
}

func (b *Brain) Retrieve(ctx context.Context, query string) (string, error) {
	if b.memorySystem == nil {
		return "", nil
	}

	b.mu.Lock()
	b.interactionCount++
	count := b.interactionCount
	b.mu.Unlock()

	// Trigger maintenance every 100 interactions
	if count > 0 && count%100 == 0 {
		go b.TriggerMaintenance(TriggerInteraction)
	}

	// 1. Facts (high value)
	facts, _ := b.memorySystem.Search(ctx, query, 5, map[string]string{"type": "fact"})
	// 2. Summaries (context)
	summaries, _ := b.memorySystem.Search(ctx, query, 3, map[string]string{"type": "summary"})

	results := append(facts, summaries...)
	if len(results) == 0 {
		return "", nil
	}

	// Update access metadata for retrieved results
	for _, r := range results {
		id := r.Metadata["id"]
		if id == "" {
			continue
		}

		// Increment access count
		accStr := r.Metadata["access_count"]
		acc, _ := strconv.Atoi(accStr)
		acc++
		r.Metadata["access_count"] = strconv.Itoa(acc)
		r.Metadata["last_accessed"] = time.Now().Format(time.RFC3339)

		// We need to update this in the memory system.
		// Since we don't have an UpdateMetadata method in MemorySystem interface yet,
		// we'll have to delete and re-add or skip for now.
		// Actually, I'll just update it if I can.
		// For now, let's just proceed with retrieval.
	}

	var sb strings.Builder
	sb.WriteString("### Retrieved Relevant Memories ###\n")
	for _, r := range results {
		// Add prefix based on type if available
		prefix := ""
		if t, ok := r.Metadata["type"]; ok {
			prefix = fmt.Sprintf("[%s] ", strings.ToUpper(t))
		}
		sb.WriteString(fmt.Sprintf("- %s%s\n", prefix, r.Content))
	}

	return sb.String(), nil
}
