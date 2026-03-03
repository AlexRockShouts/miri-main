package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"miri-main/src/internal/engine/memory/mole_syn"

	"github.com/cloudwego/eino/schema"
)

func (b *Brain) ExtractFacts(ctx context.Context, messages []*schema.Message) error {
	if b.factMemory == nil {
		return nil
	}

	prompt, err := b.GetPrompt("extract.prompt")
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

	sanitized := b.sanitize([]*schema.Message{schema.UserMessage(fullPrompt)})
	resp, err := b.chat.Generate(ctx, sanitized)
	if err != nil {
		slog.Error("Generate facts failed", "error", err, "prompt", sanitized[0].Content)
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

		// Check for duplicates before adding
		if exists, existingContent := b.checkFactDuplicate(ctx, f.Fact); exists {
			slog.Debug("Fact already exists, skipping extraction", "fact", f.Fact, "existing", existingContent)
			continue
		}

		metadata := b.prepareMetadata(map[string]string{
			"type":        "fact",
			"category":    f.Category,
			"source_turn": f.SourceTurn,
			"confidence":  fmt.Sprintf("%.2f", f.Confidence),
		})
		_ = b.factMemory.Add(ctx, f.Fact, metadata)
		slog.Info("Extracted and stored fact", "fact", f.Fact, "category", f.Category)
	}

	return nil
}

func (b *Brain) Reflect(ctx context.Context, messages []*schema.Message) error {
	if b.summaryMemory == nil {
		return nil
	}

	prompt, err := b.GetPrompt("reflection.prompt")
	if err != nil {
		return fmt.Errorf("read reflection prompt: %w", err)
	}

	var conv strings.Builder
	for _, m := range messages {
		role := string(m.Role)
		conv.WriteString(fmt.Sprintf("%s: %s\n", role, m.Content))
	}

	fullPrompt := strings.Replace(string(prompt), "{context + your_previous_output}", conv.String(), 1)

	sanitized := b.sanitize([]*schema.Message{schema.UserMessage(fullPrompt)})
	resp, err := b.chat.Generate(ctx, sanitized)
	if err != nil {
		slog.Error("Generate reflection failed", "error", err, "prompt", sanitized[0].Content)
		return fmt.Errorf("generate reflection: %w", err)
	}

	metadata := b.prepareMetadata(map[string]string{
		"type": "reflection",
	})
	_ = b.summaryMemory.Add(ctx, resp.Content, metadata)
	slog.Info("Stored self-reflection")

	return nil
}

func (b *Brain) Summarize(ctx context.Context, messages []*schema.Message) error {
	if b.summaryMemory == nil {
		return nil
	}

	prompt, err := b.GetPrompt("compact.prompt")
	if err != nil {
		return fmt.Errorf("read compact prompt: %w", err)
	}

	var conv strings.Builder
	for _, m := range messages {
		if m.Role != schema.User {
			continue // Summarize user requests and intent only
		}
		role := string(m.Role)
		conv.WriteString(fmt.Sprintf("%s: %s\n", role, m.Content))
	}

	fullPrompt := strings.Replace(string(prompt), "{full_or_recent_conversation_text}", conv.String(), 1)

	sanitized := b.sanitize([]*schema.Message{schema.UserMessage(fullPrompt)})
	resp, err := b.chat.Generate(ctx, sanitized)
	if err != nil {
		slog.Error("Generate summary failed", "error", err, "prompt", sanitized[0].Content)
		return fmt.Errorf("generate summary: %w", err)
	}

	metadata := b.prepareMetadata(map[string]string{
		"type": "summary",
	})
	_ = b.summaryMemory.Add(ctx, resp.Content, metadata)
	slog.Info("Stored conversation summary")

	return nil
}

func (b *Brain) analyzeTopology(ctx context.Context, trace string) (*mole_syn.TopologyAnalysis, error) {
	prompt, err := b.GetPrompt("topology_extraction.prompt")
	if err != nil {
		return nil, fmt.Errorf("read topology extraction prompt: %w", err)
	}

	fullPrompt := strings.Replace(prompt, "{agent_cot_trace + final_answer}", trace, 1)

	sanitized := b.sanitize([]*schema.Message{
		schema.UserMessage(fullPrompt),
	})
	resp, err := b.chat.Generate(ctx, sanitized)
	if err != nil {
		slog.Error("Generate topology analysis failed", "error", err, "prompt", sanitized[0].Content)
		return nil, err
	}

	content := resp.Content
	// Extract JSON if it's wrapped in triple backticks
	if start := strings.Index(content, "{"); start != -1 {
		if end := strings.LastIndex(content, "}"); end != -1 && end > start {
			content = content[start : end+1]
		}
	}

	var analysis mole_syn.TopologyAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w\nRaw output:\n%s", err, content)
	}

	return &analysis, nil
}
