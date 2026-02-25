package engine

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// retrieveMemory loads content from memory.md, user.md, and facts.json
// and formats them into a system message to be injected into the conversation.
func (e *EinoEngine) retrieveMemory(ctx context.Context) (*schema.Message, bool) {
	if e.storageBaseDir == "" && e.storage == nil {
		return nil, false
	}

	var memoryMD, userMD, factsJSON string
	if e.storage != nil {
		memoryMD, _ = e.storage.ReadMemory()
		// user.md and facts.json are still read from storageBaseDir for now,
		// or we could add methods to Storage for them too.
		userMD = readFileIfExists(filepath.Join(e.storageBaseDir, "user.md"))
		factsJSON = readFileIfExists(filepath.Join(e.storageBaseDir, "facts.json"))
	} else {
		memoryMD = readFileIfExists(filepath.Join(e.storageBaseDir, "memory.md"))
		userMD = readFileIfExists(filepath.Join(e.storageBaseDir, "user.md"))
		factsJSON = readFileIfExists(filepath.Join(e.storageBaseDir, "facts.json"))
	}

	if memoryMD == "" && userMD == "" && factsJSON == "" {
		return nil, false
	}

	var sb strings.Builder
	sb.WriteString("### Retrieved Long-term Memory ###\n")

	if userMD != "" {
		sb.WriteString("\n#### User Profile/Preferences (user.md):\n")
		sb.WriteString(userMD)
		sb.WriteString("\n")
	}

	if factsJSON != "" {
		sb.WriteString("\n#### Key Facts/Entities (facts.json):\n")
		sb.WriteString(factsJSON)
		sb.WriteString("\n")
	}

	if memoryMD != "" {
		sb.WriteString("\n#### Historical Context/Decisions (memory.md):\n")
		// If memory.md is too large, we might want to truncate it here,
		// but for now we include it as requested.
		sb.WriteString(memoryMD)
		sb.WriteString("\n")
	}

	content := sb.String()
	// Using a System Message as requested to provide the model with context.
	return schema.SystemMessage(content), true
}

// injectRetrievedMemoryWithStatus adds the retrieved memory message to the message list and returns true if injected.
func (e *EinoEngine) injectRetrievedMemoryWithStatus(ctx context.Context, msgs []*schema.Message) ([]*schema.Message, bool) {
	memMsg, ok := e.retrieveMemory(ctx)
	if !ok {
		return msgs, false
	}

	// We insert it after the initial system prompt (soul + humanContext) if it exists.
	if len(msgs) > 0 && msgs[0].Role == schema.System {
		newMsgs := make([]*schema.Message, 0, len(msgs)+1)
		newMsgs = append(newMsgs, msgs[0])
		newMsgs = append(newMsgs, memMsg)
		newMsgs = append(newMsgs, msgs[1:]...)
		return newMsgs, true
	}

	// Fallback: prepend
	return append([]*schema.Message{memMsg}, msgs...), true
}

// injectRetrievedMemory adds the retrieved memory message to the message list.
func (e *EinoEngine) injectRetrievedMemory(ctx context.Context, msgs []*schema.Message) []*schema.Message {
	newMsgs, _ := e.injectRetrievedMemoryWithStatus(ctx, msgs)
	return newMsgs
}
