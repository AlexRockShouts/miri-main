package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/cloudwego/eino/schema"
)

func (b *Brain) prepareMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		metadata = make(map[string]string)
	}

	b.mu.RLock()
	intCount := b.interactionCount
	topScore := b.lastTopologyScore
	b.mu.RUnlock()

	metadata["timestamp"] = time.Now().Format(time.RFC3339)
	metadata["interaction_count"] = strconv.Itoa(intCount)
	metadata["topology_score"] = strconv.Itoa(topScore)

	// Ensure standard fields are present if not already set
	if _, ok := metadata["created_at"]; !ok {
		metadata["created_at"] = time.Now().Format(time.RFC3339)
	}
	if _, ok := metadata["access_count"]; !ok {
		metadata["access_count"] = "0"
	}
	if _, ok := metadata["last_accessed"]; !ok {
		metadata["last_accessed"] = time.Now().Format(time.RFC3339)
	}

	return metadata
}

func (b *Brain) IngestMetadata(ctx context.Context, human, soul string) error {
	if human == "" && soul == "" {
		return nil
	}

	slog.Info("Ingesting persona metadata into brain", "has_human", human != "", "has_soul", soul != "")

	var msgs []*schema.Message
	if soul != "" {
		msgs = append(msgs, &schema.Message{
			Role:    schema.Assistant,
			Content: "My Soul Configuration:\n" + soul,
		})
	}

	if human != "" {
		msgs = append(msgs, &schema.Message{
			Role:    schema.User,
			Content: "Information about my human:\n" + human,
		})
	}

	// 1. Extract facts from this context
	if err := b.ExtractFacts(ctx, msgs); err != nil {
		slog.Error("Failed to extract facts from metadata", "error", err)
	}

	return nil
}

func (b *Brain) UpdateContextUsage(ctx context.Context, usage int) {
	if b.contextWindow <= 0 {
		return
	}

	b.mu.Lock()
	b.lastContextUsage = usage
	b.mu.Unlock()

	percent := float64(usage) / float64(b.contextWindow)
	if percent >= 0.6 {
		slog.Info("Context window usage high, triggering brain maintenance", "usage", usage, "window", b.contextWindow, "percent", fmt.Sprintf("%.2f%%", percent*100))
		go b.TriggerMaintenance(TriggerContextUsage)
	}
}
