package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

// MaintenanceTrigger is a reason for triggering brain maintenance
type MaintenanceTrigger string

const (
	TriggerInteraction  MaintenanceTrigger = "interaction_threshold"
	TriggerContextUsage MaintenanceTrigger = "context_usage_high"
	TriggerNewSession   MaintenanceTrigger = "new_session"
	TriggerStartup      MaintenanceTrigger = "startup"
	TriggerShutdown     MaintenanceTrigger = "shutdown"
	TriggerManual       MaintenanceTrigger = "manual"
	TriggerScheduled    MaintenanceTrigger = "scheduled"
)

func (b *Brain) TriggerMaintenance(trigger MaintenanceTrigger) {
	slog.Info("Brain maintenance triggered", "reason", trigger)

	// withTimeout runs fn with its own deadline so a slow operation cannot
	// starve the operations that follow it.
	withTimeout := func(d time.Duration, name string, fn func(context.Context) error) {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		if err := fn(ctx); err != nil {
			slog.Error("Maintenance operation failed", "op", name, "error", err)
		}
	}

	// 1. Process extraction/reflection/summarization if we have a buffer
	b.mu.RLock()
	sessionIDs := make([]string, 0, len(b.buffer))
	for sid := range b.buffer {
		sessionIDs = append(sessionIDs, sid)
	}
	b.mu.RUnlock()

	for _, sid := range sessionIDs {
		msgs := b.GetBuffer(sid)
		if len(msgs) == 0 {
			continue
		}
		slog.Debug("Running extraction tasks for session", "session_id", sid)

		withTimeout(2*time.Minute, "ExtractFacts", func(ctx context.Context) error {
			return b.ExtractFacts(ctx, msgs)
		})

		withTimeout(1*time.Minute, "Reflect", func(ctx context.Context) error {
			return b.Reflect(ctx, msgs)
		})

		// Topology analysis
		withTimeout(2*time.Minute, "analyzeTopology", func(ctx context.Context) error {
			var sb strings.Builder
			for _, m := range msgs {
				role := string(m.Role)
				content := m.Content
				if m.ReasoningContent != "" {
					content = fmt.Sprintf("<thought>\n%s\n</thought>\n%s", m.ReasoningContent, content)
				}
				if len(m.ToolCalls) > 0 {
					tcBytes, _ := json.Marshal(m.ToolCalls)
					content += fmt.Sprintf("\n[Tool Calls: %s]", string(tcBytes))
				}
				if m.Role == schema.Tool {
					content = fmt.Sprintf("[Tool ID: %s] %s", m.ToolCallID, content)
				}
				sb.WriteString(fmt.Sprintf("%s: %s\n", role, content))
			}
			analysis, err := b.analyzeTopology(ctx, sb.String())
			if err != nil {
				return err
			}
			b.mu.Lock()
			b.lastTopologyScore = analysis.TopologyScore
			b.lastDeepBondRatio = float32(analysis.BondDistribution.D)
			b.mu.Unlock()
			if b.Graph != nil {
				_ = b.Graph.AddStepsFromAnalysis(sid, analysis)
				slog.Info("Updated memory graph with topology analysis", "session_id", sid, "steps", len(analysis.Steps), "score", analysis.TopologyScore)
			}
			return nil
		})

		withTimeout(2*time.Minute, "Summarize", func(ctx context.Context) error {
			if err := b.Summarize(ctx, msgs); err != nil {
				return err
			}
			// Clear buffer after successful summarization to reduce context usage
			b.ClearBuffer(sid)
			slog.Info("Cleared message buffer after summarization", "session_id", sid)
			return nil
		})
	}

	// 2. Run compaction — given it may process many facts/summaries, allow more time
	withTimeout(5*time.Minute, "Compact", func(ctx context.Context) error {
		return b.Compact(ctx)
	})

	b.mu.Lock()
	b.lastMaintenance = time.Now()
	b.mu.Unlock()
}

func (b *Brain) Compact(ctx context.Context) error {
	if b.factMemory == nil || b.summaryMemory == nil {
		return nil
	}

	slog.Info("Starting brain memory compaction")

	b.mu.Lock()
	b.interactionCount = 0
	b.mu.Unlock()

	// 1. Fetch memories from both collections
	facts, err := b.factMemory.ListAll(ctx)
	if err != nil {
		slog.Error("list all facts", "error", err)
	}

	summaries, err := b.summaryMemory.ListAll(ctx)
	if err != nil {
		slog.Error("list all summaries", "error", err)
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

		// 3b. Deduplicate consolidated summaries against each other
		freshSummaries, err := b.summaryMemory.ListAll(ctx)
		if err != nil {
			slog.Error("list all summaries after consolidation", "error", err)
			freshSummaries = summaries
		}
		if len(freshSummaries) > 1 {
			if err := b.deduplicateSummaries(ctx, freshSummaries); err != nil {
				slog.Error("Summary deduplication failed", "error", err)
			}
		}
	}

	// 4. Cleanup old/low-confidence items
	all := append(facts, summaries...)
	if err := b.cleanup(ctx, all); err != nil {
		slog.Error("Memory cleanup failed", "error", err)
	}

	// 5. Promote facts from summaries
	if len(summaries) > 0 {
		if err := b.promoteFacts(ctx, summaries); err != nil {
			slog.Error("Fact promotion failed", "error", err)
		}
	}

	// 6. Deduplicate facts (re-fetch to include any facts promoted in step 5)
	freshFacts, err := b.factMemory.ListAll(ctx)
	if err != nil {
		slog.Error("list all facts after promotion", "error", err)
		freshFacts = facts
	}
	if len(freshFacts) > 0 {
		if err := b.deduplicateFacts(ctx, freshFacts); err != nil {
			slog.Error("Fact deduplication failed", "error", err)
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
					_ = b.factMemory.Delete(ctx, id)
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
					if item.Metadata["type"] == "fact" {
						_ = b.factMemory.Delete(ctx, id)
					} else {
						_ = b.summaryMemory.Delete(ctx, id)
					}
					continue
				}
			}
		}
	}
	return nil
}

func (b *Brain) promoteFacts(ctx context.Context, summaries []SearchResult) error {
	slog.Info("Promoting facts from summaries", "count", len(summaries))
	prompt, err := b.GetPrompt("promote_facts.prompt")
	if err != nil {
		return err
	}

	// Process the 3 most recent summaries (tail of the slice, which is insertion order)
	start := max(0, len(summaries)-3)
	for _, s := range summaries[start:] {
		fullPrompt := strings.Replace(string(prompt), "{summary_text}", s.Content, 1)
		sanitized := b.sanitize([]*schema.Message{schema.UserMessage(fullPrompt)})
		resp, err := b.chat.Generate(ctx, sanitized)
		if err != nil {
			slog.Error("Generate promotion facts failed", "error", err, "prompt", sanitized[0].Content)
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
			if exists, existingContent := b.checkFactDuplicate(ctx, p.Fact); exists {
				slog.Debug("Fact already exists, skipping promotion", "fact", p.Fact, "existing", existingContent)
				continue
			}

			metadata := b.prepareMetadata(map[string]string{
				"type":       "fact",
				"category":   p.Category,
				"source":     "summary_promotion",
				"confidence": fmt.Sprintf("%.2f", p.Confidence),
			})
			_ = b.factMemory.Add(ctx, p.Fact, metadata)
		}
	}

	return nil
}

const dedupeChunkSize = 30

func (b *Brain) deduplicateFacts(ctx context.Context, facts []SearchResult) error {
	slog.Info("Deduplicating facts", "count", len(facts))

	prompt, err := b.GetPrompt("deduplicate_facts.prompt")
	if err != nil {
		return err
	}

	for i := 0; i < len(facts); i += dedupeChunkSize {
		end := min(i+dedupeChunkSize, len(facts))
		if err := b.deduplicateFactsBatch(ctx, prompt, facts[i:end]); err != nil {
			slog.Error("Fact deduplication batch failed", "batch_start", i, "error", err)
		}
	}

	return nil
}

func (b *Brain) deduplicateFactsBatch(ctx context.Context, prompt string, facts []SearchResult) error {
	var sb strings.Builder
	for _, f := range facts {
		id := f.Metadata["id"]
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", id, f.Content))
	}

	fullPrompt := strings.Replace(prompt, "{facts_list}", sb.String(), 1)
	sanitized := b.sanitize([]*schema.Message{schema.UserMessage(fullPrompt)})
	chatCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	resp, err := b.chat.Generate(chatCtx, sanitized)
	if err != nil {
		slog.Error("Generate deduplicate facts failed", "error", err, "prompt", sanitized[0].Content)
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
			_ = b.factMemory.Delete(ctx, dupID)
		}
	}

	return nil
}

const dedupeSummaryChunkSize = 20

func (b *Brain) deduplicateSummaries(ctx context.Context, summaries []SearchResult) error {
	slog.Info("Deduplicating summaries", "count", len(summaries))

	prompt, err := b.GetPrompt("deduplicate_summaries.prompt")
	if err != nil {
		return err
	}

	for i := 0; i < len(summaries); i += dedupeSummaryChunkSize {
		end := min(i+dedupeSummaryChunkSize, len(summaries))
		if err := b.deduplicateSummariesBatch(ctx, prompt, summaries[i:end]); err != nil {
			slog.Error("Summary deduplication batch failed", "batch_start", i, "error", err)
		}
	}

	return nil
}

func (b *Brain) deduplicateSummariesBatch(ctx context.Context, prompt string, summaries []SearchResult) error {
	var sb strings.Builder
	for _, s := range summaries {
		id := s.Metadata["id"]
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", id, s.Content))
	}

	fullPrompt := strings.Replace(prompt, "{summaries_list}", sb.String(), 1)
	sanitized := b.sanitize([]*schema.Message{schema.UserMessage(fullPrompt)})
	chatCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	resp, err := b.chat.Generate(chatCtx, sanitized)
	if err != nil {
		slog.Error("Generate deduplicate summaries failed", "error", err, "prompt", sanitized[0].Content)
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
			slog.Info("Deleting duplicate summary", "id", dupID)
			_ = b.summaryMemory.Delete(ctx, dupID)
		}
	}

	return nil
}

func (b *Brain) consolidateSummaries(ctx context.Context, summaries []SearchResult) error {
	slog.Info("Consolidating summaries", "count", len(summaries))

	prompt, err := b.GetPrompt("consolidate_summaries.prompt")
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
		sanitized := b.sanitize([]*schema.Message{schema.UserMessage(fullPrompt)})
		resp, err := b.chat.Generate(ctx, sanitized)
		if err != nil {
			slog.Error("Generate consolidated summaries failed", "error", err, "prompt", sanitized[0].Content)
			continue
		}

		// Add consolidated summary
		metadata := b.prepareMetadata(map[string]string{
			"type":    "summary",
			"subtype": "consolidated",
		})
		_ = b.summaryMemory.Add(ctx, resp.Content, metadata)

		// Delete old summaries
		for _, s := range batch {
			id := s.Metadata["id"]
			if id != "" {
				_ = b.summaryMemory.Delete(ctx, id)
			}
		}
	}

	return nil
}
