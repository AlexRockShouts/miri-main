package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// summarizeThreshold is a hard threshold for triggering summary/compaction
// when prompt tokens usage reaches ~88% of the model context window.
const summarizeThreshold = 0.88

// buildSummaryPrompt builds a prompt to summarize older conversation history using an optional template file.
// It leaves the most recent N messages raw (not part of the summary).
func buildSummaryPrompt(templatePath string, olderMsgs []*schema.Message) (string, error) {
	b, err := os.ReadFile(templatePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		b = []byte(
			"Conversation Summary/Compaction:\n" +
				"- You will receive older conversation messages. Create a structured summary in JSON.\n" +
				"- Capture: topics, decisions, action_items, open_questions, tools_used, entities, constraints, time_range.\n" +
				"- Be faithful, concise, and avoid speculation.\n" +
				"- Output strictly as a single fenced JSON block:```json { ... } ``` without prose.\n",
		)
	}

	var hist strings.Builder
	for i, m := range olderMsgs {
		role := string(m.Role)
		if role == "" {
			role = "message"
		}
		fmt.Fprintf(&hist, "%03d %s: %s\n\n", i, role, m.Content)
	}

	prompt := strings.Builder{}
	prompt.WriteString(string(b))
	prompt.WriteString("\n\nOlder conversation messages (chronological):\n\n")
	prompt.WriteString(hist.String())
	prompt.WriteString("\n\nRemember: Output ONLY one ```json fenced block``` with the summary object.\n")
	return prompt.String(), nil
}

// summarizeIfNeeded triggers when usagePromptTokens exceeds summarizeThreshold of the context window.
// It summarizes older messages (excluding the last keepN) into a structured JSON block and rewrites history:
// system + summary(system) + last keepN raw messages.
func (e *EinoEngine) summarizeIfNeeded(ctx context.Context, systemPrompt string, msgs []*schema.Message, usagePromptTokens int, callOpts []model.Option) ([]*schema.Message, bool, error) {
	if e.contextWindow <= 0 {
		return msgs, false, nil
	}
	limit := int(float64(e.contextWindow) * summarizeThreshold)
	if usagePromptTokens < limit {
		return msgs, false, nil
	}

	slog.Info("Triggering conversation summary", "usage", usagePromptTokens, "limit", limit)

	// Identify messages excluding the leading system message
	if len(msgs) == 0 {
		return msgs, false, nil
	}
	startIdx := 0
	if msgs[0].Role == schema.System {
		startIdx = 1
	}
	keepN := 12
	if len(msgs)-startIdx <= keepN {
		return msgs, false, nil
	}
	older := make([]*schema.Message, len(msgs[startIdx:len(msgs)-keepN]))
	copy(older, msgs[startIdx:len(msgs)-keepN])
	recent := make([]*schema.Message, len(msgs[len(msgs)-keepN:]))
	copy(recent, msgs[len(msgs)-keepN:])

	// Build summary prompt
	prompt, err := buildSummaryPrompt("templates/summary.md", older)
	if err != nil {
		return msgs, false, err
	}
	sys := "You are a rigorous conversation summarizer. Produce strictly structured JSON as instructed."
	sumMsgs := []*schema.Message{schema.SystemMessage(sys), schema.UserMessage(prompt)}
	resp, err := e.chat.Generate(ctx, sumMsgs, callOpts...)
	if err != nil {
		return msgs, false, err
	}
	out := strings.TrimSpace(resp.Content)
	if out == "" {
		return msgs, false, nil
	}

	// Prefer extracting the JSON content inside fences (if any)
	jsonBlocks := extractJSONBlocks(out)
	summary := out
	if len(jsonBlocks) > 0 {
		summary = "```json\n" + jsonBlocks[0] + "\n```"
	}

	// Rebuild messages: system + summary(system) + recent raw
	newMsgs := make([]*schema.Message, 0, 2+len(recent))
	newMsgs = append(newMsgs, schema.SystemMessage(systemPrompt))
	newMsgs = append(newMsgs, schema.SystemMessage("Older conversation summary (structured):\n"+summary))
	newMsgs = append(newMsgs, recent...)
	return newMsgs, true, nil
}
