package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const flushThreshold = 0.65

func buildFlushPrompt(templatePath, memoryMD, userMD, factsJSON string, msgs []*schema.Message) (string, error) {
	b, err := os.ReadFile(templatePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		b = []byte("Internal Memory Flush:\n- read existing memory.md / user.md / facts.json first\n- extract new or updated facts, preferences, entities, open tasks\n- always append, never overwrite \n- structure memory document as headings + bullet-points and JSON blocks\n- output: \"written: memory.md (+rows), facts.json\"")
	}
	var hist strings.Builder
	count := 0
	for i := len(msgs) - 1; i >= 0 && count < 20; i-- {
		m := msgs[i]
		role := string(m.Role)
		if role == "" {
			role = "message"
		}
		hist.WriteString(fmt.Sprintf("%s: %s\n\n", role, m.Content))
		count++
	}
	prompt := strings.Builder{}
	prompt.WriteString(string(b))
	prompt.WriteString("\n\nExisting memory.md:\n\n")
	prompt.WriteString(memoryMD)
	prompt.WriteString("\n\nExisting user.md:\n\n")
	prompt.WriteString(userMD)
	prompt.WriteString("\n\nExisting facts.json (NDJSON):\n\n")
	prompt.WriteString(factsJSON)
	prompt.WriteString("\n\nRecent conversation excerpt (latest first):\n\n")
	prompt.WriteString(hist.String())
	prompt.WriteString("\n\nPlease output:\n- Append-ready markdown for memory.md (headings + bullet points).\n- Optional ```json fenced blocks``` with facts to append to facts.json (one object per block).\n")
	return prompt.String(), nil
}

func (e *EinoEngine) flushIfNeeded(ctx context.Context, msgs []*schema.Message, usagePromptTokens int, callOpts []model.Option) (bool, error) {
	if e.contextWindow <= 0 {
		return false, nil
	}
	limit := int(float64(e.contextWindow) * flushThreshold)
	if usagePromptTokens < limit {
		return false, nil
	}

	slog.Info("Triggering memory flush", "usage", usagePromptTokens, "limit", limit)
	// prepare paths
	base := e.storageBaseDir
	if base == "" {
		base = filepath.Join(os.TempDir(), "miri-memory")
	}
	if err := ensureDir(base); err != nil {
		return false, err
	}
	memoryPath := filepath.Join(base, "memory.md")
	userPath := filepath.Join(base, "user.md")
	factsPath := filepath.Join(base, "facts.json")
	memoryMD := readFileIfExists(memoryPath)
	userMD := readFileIfExists(userPath)
	factsJSON := readFileIfExists(factsPath)
	prompt, err := buildFlushPrompt("templates/flush.md", memoryMD, userMD, factsJSON, msgs)
	if err != nil {
		return false, err
	}
	sys := "You are a precise memory compaction assistant. Follow the template strictly and produce append-only content."
	flushMsgs := []*schema.Message{schema.SystemMessage(sys), schema.UserMessage(prompt)}
	resp, err := e.chat.Generate(ctx, flushMsgs, callOpts...)
	if err != nil {
		return false, err
	}
	out := strings.TrimSpace(resp.Content)
	if out == "" {
		return false, nil
	}
	if err := appendLine(memoryPath, "\n\n# Flush @ "+time.Now().UTC().Format(time.RFC3339)+"\n\n"+out+"\n"); err != nil {
		slog.Error("Failed to append to memory.md", "error", err)
		return false, err
	}
	jsonCount := 0
	for _, blk := range extractJSONBlocks(out) {
		if err := appendLine(factsPath, blk); err != nil {
			slog.Error("Failed to append to facts.json", "error", err)
		} else {
			jsonCount++
		}
	}
	slog.Info("Memory flush completed", "json_blocks", jsonCount)
	return true, nil
}
