package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"miri-main/src/internal/engine/tools"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
)

// emitVerbose sends a tagged verbose event to the input's VerboseCh non-blocking.
func emitVerbose(input *graphInput, msg string) {
	if input.VerboseCh == nil {
		return
	}
	select {
	case input.VerboseCh <- msg:
	default:
	}
}

func (e *EinoEngine) agentInvoke(ctx context.Context, input *graphInput) (out *graphOutput, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in agentInvoke", "recover", r)
			err = fmt.Errorf("internal agent error: %v", r)
		}
	}()

	slog.Info("Agent loop start", "session_id", input.SessionID, "max_steps", e.maxSteps)
	msgs := input.Messages

	// Activate skills learn and skill_creator for default session
	if input.SessionID == session.DefaultSessionID {
		activatedSkills := []string{"learn", "skill_creator"}
		for _, sn := range activatedSkills {
			// Check if already in messages to avoid duplicates
			alreadyLoaded := false
			marker := fmt.Sprintf("SKILL LOADED: %s", sn)
			for _, m := range msgs {
				if strings.Contains(m.Content, marker) {
					alreadyLoaded = true
					break
				}
			}
			if !alreadyLoaded {
				if skill, ok := e.skillLoader.GetSkill(sn); ok {
					slog.Info("Auto-activating skill for default session", "skill", sn)
					msgs = append(msgs, schema.SystemMessage(fmt.Sprintf("SKILL LOADED: %s\n\n%s", skill.Name, skill.FullContent)))
				}
			}
		}
	}

	// Add user prompt if not already there (it might be restored from checkpoint)
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != schema.User || msgs[len(msgs)-1].Content != input.Prompt {
		msgs = append(msgs, schema.UserMessage(input.Prompt))
	}

	var totalUsage llm.Usage

	for i := range e.maxSteps {
		slog.Debug("Agent loop iteration", "step", i, "messages_count", len(msgs))

		// Sanitize messages before sending to LLM to avoid safety triggers (e.g. Grok data leakage check)
		sanitizedMsgs := e.sanitizeMessages(msgs)
		assistant, err := e.chat.Generate(ctx, sanitizedMsgs, input.CallOpts...)
		if err != nil {
			// Check for 503 error to retry
			if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "Service Unavailable") {
				slog.Warn("Chat generate failed with 503, retrying once", "step", i)
				time.Sleep(2 * time.Second)
				assistant, err = e.chat.Generate(ctx, sanitizedMsgs, input.CallOpts...)
			}
		}

		if err != nil {
			var sb strings.Builder
			for _, m := range sanitizedMsgs {
				sb.WriteString(fmt.Sprintf("[%s]: %s\n", m.Role, m.Content))
			}
			slog.Error("Chat generate failed", "step", i, "error", err, "prompt", sb.String())
			return nil, err
		}

		// Ensure assistant message has non-empty content
		if strings.TrimSpace(assistant.Content) == "" {
			assistant.Content = "..."
		}

		if assistant.ResponseMeta != nil && assistant.ResponseMeta.Usage != nil {
			totalUsage.PromptTokens += assistant.ResponseMeta.Usage.PromptTokens
			totalUsage.CompletionTokens += assistant.ResponseMeta.Usage.CompletionTokens
			totalUsage.TotalTokens += assistant.ResponseMeta.Usage.TotalTokens
			totalUsage.TotalCost += e.CalculateCost(assistant.ResponseMeta.Usage.PromptTokens, assistant.ResponseMeta.Usage.CompletionTokens)

			slog.Debug("Usage update", "step", i, "total_tokens", totalUsage.TotalTokens, "cost", totalUsage.TotalCost)

			// Update brain usage for maintenance triggering
			if e.brain != nil {
				e.brain.UpdateContextUsage(ctx, assistant.ResponseMeta.Usage.TotalTokens)
			}

			// Parse reasoning trace into Mole-Syn graph
			if e.brain != nil && input.SessionID != "" {
				trace := fmt.Sprintf("Agent step %d:\\n%s", i, assistant.Content)
				if terr := e.brain.AddReasoningTrace(ctx, input.SessionID, trace); terr != nil {
					slog.Warn("Failed to add reasoning to Mole-Syn graph", "session", input.SessionID, "step", i, "error", terr)
				}
			}
		}

		if len(assistant.ToolCalls) == 0 {
			slog.Info("Agent loop finished (no tool calls)", "steps", i+1, "total_tokens", totalUsage.TotalTokens)
			if strings.TrimSpace(assistant.Content) != "" {
				emitVerbose(input, fmt.Sprintf("[Thought: %s]", strings.TrimSpace(assistant.Content)))
			}
			return &graphOutput{
				SessionID:   input.SessionID,
				Answer:      assistant.Content,
				Messages:    msgs,
				Usage:       totalUsage,
				LastMessage: assistant,
			}, nil
		}

		slog.Info("Agent tool calls triggered", "step", i, "calls", len(assistant.ToolCalls))
		if strings.TrimSpace(assistant.Content) != "" {
			emitVerbose(input, fmt.Sprintf("[Thought: %s]", strings.TrimSpace(assistant.Content)))
		}
		for _, tc := range assistant.ToolCalls {
			emitVerbose(input, fmt.Sprintf("[Tool: %s(%s)]", tc.Function.Name, tc.Function.Arguments))
		}
		msgs = append(msgs, assistant)
		if e.brain != nil {
			e.brain.AddToBuffer(input.SessionID, assistant)
		}

		// Handle skill injection
		for _, tc := range assistant.ToolCalls {
			if tc.Function.Name == "skill_use" {
				var args struct {
					SkillName string `json:"skill_name"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
					if skill, ok := e.skillLoader.GetSkill(args.SkillName); ok {
						slog.Info("Injecting skill into context", "skill", skill.Name)
						// Inject skill content as a system message to guide future steps
						// Sanitize skill content as well
						cleanSkillContent := e.sanitizeString(skill.FullContent)
						msgs = append(msgs, schema.SystemMessage(fmt.Sprintf("SKILL LOADED: %s\n\n%s", skill.Name, cleanSkillContent)))
					}
				}
			}
		}

		// Inject Task Manager Tool with current session ID
		if e.taskGateway != nil {
			taskMgrTool := tools.NewTaskManagerTool(e.taskGateway, input.SessionID)

			var toolMsgs []*schema.Message
			var remainingToolCalls []schema.ToolCall

			for _, tc := range assistant.ToolCalls {
				if tc.Function.Name == "task_manager" {
					slog.Info("Executing task_manager tool", "session_id", input.SessionID)
					res, err := taskMgrTool.InvokableRun(ctx, tc.Function.Arguments)
					if err != nil {
						res = fmt.Sprintf("Error: %v", err)
					} else {
						if strings.TrimSpace(res) == "" {
							res = "Task operation completed (no output)"
						}
					}
					// Sanitize tool output before adding to messages and buffer
					res = e.sanitizeString(res)
					emitVerbose(input, fmt.Sprintf("[ToolResult: task_manager → %s]", res))
					toolMsgs = append(toolMsgs, schema.ToolMessage(res, tc.ID))
				} else if tc.Function.Name == "file_manager" {
					slog.Info("Executing file_manager tool")
					fileMgrTool := tools.NewFileManagerTool(e.storageBaseDir, e.taskGateway)
					res, err := fileMgrTool.InvokableRun(ctx, tc.Function.Arguments)
					if err != nil {
						res = fmt.Sprintf("Error: %v", err)
					} else {
						if strings.TrimSpace(res) == "" {
							res = "File operation completed (no output)"
						}
					}
					// Sanitize tool output before adding to messages and buffer
					res = e.sanitizeString(res)
					emitVerbose(input, fmt.Sprintf("[ToolResult: file_manager → %s]", res))
					toolMsgs = append(toolMsgs, schema.ToolMessage(res, tc.ID))
				} else {
					remainingToolCalls = append(remainingToolCalls, tc)
				}
			}

			if len(toolMsgs) > 0 {
				msgs = append(msgs, toolMsgs...)
				if e.brain != nil {
					for _, m := range toolMsgs {
						e.brain.AddToBuffer(input.SessionID, m)
					}
				}
			}

			if len(remainingToolCalls) > 0 {
				// Create a temporary assistant message with only remaining tool calls
				tempAssistant := &schema.Message{
					Role:      schema.Assistant,
					Content:   "",
					ToolCalls: remainingToolCalls,
				}
				moreToolMsgs, err := e.tools.Invoke(ctx, tempAssistant)
				if err != nil {
					slog.Error("Tool invoke failed (remaining)", "step", i, "error", err)
					return nil, err
				}
				// Sanitize tool outputs from e.tools.Invoke
				moreToolMsgs = e.sanitizeMessages(moreToolMsgs)
				for _, m := range moreToolMsgs {
					if strings.TrimSpace(m.Content) == "" {
						m.Content = "Tool execution completed (no output)"
					}
					emitVerbose(input, fmt.Sprintf("[ToolResult: %s]", m.Content))
				}
				msgs = append(msgs, moreToolMsgs...)
				if e.brain != nil {
					for _, m := range moreToolMsgs {
						e.brain.AddToBuffer(input.SessionID, m)
					}
				}
			}
		} else {
			toolMsgs, err := e.tools.Invoke(ctx, assistant)
			if err != nil {
				slog.Error("Tool invoke failed", "step", i, "error", err)
				return nil, err
			}
			// Sanitize tool outputs from e.tools.Invoke
			toolMsgs = e.sanitizeMessages(toolMsgs)
			for _, m := range toolMsgs {
				if strings.TrimSpace(m.Content) == "" {
					m.Content = "Tool execution completed (no output)"
				}
				emitVerbose(input, fmt.Sprintf("[ToolResult: %s]", m.Content))
			}
			msgs = append(msgs, toolMsgs...)
			if e.brain != nil {
				for _, m := range toolMsgs {
					e.brain.AddToBuffer(input.SessionID, m)
				}
			}
		}
	}

	// Final generation if loop exhausted
	slog.Info("Agent loop exhausted, final generation", "max_steps", e.maxSteps)
	final, err := e.chat.Generate(ctx, msgs, input.CallOpts...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(final.Content) == "" {
		final.Content = "..."
	}
	if final.ResponseMeta != nil && final.ResponseMeta.Usage != nil {
		totalUsage.PromptTokens += final.ResponseMeta.Usage.PromptTokens
		totalUsage.CompletionTokens += final.ResponseMeta.Usage.CompletionTokens
		totalUsage.TotalTokens += final.ResponseMeta.Usage.TotalTokens
		totalUsage.TotalCost += e.CalculateCost(final.ResponseMeta.Usage.PromptTokens, final.ResponseMeta.Usage.CompletionTokens)
	}

	return &graphOutput{
		SessionID:   input.SessionID,
		Answer:      final.Content,
		Messages:    msgs,
		Usage:       totalUsage,
		LastMessage: final,
	}, nil
}

func (e *EinoEngine) agentStream(ctx context.Context, input *graphInput) (*schema.StreamReader[*graphOutput], error) {
	sr, sw := schema.Pipe[*graphOutput](1)

	go func() {
		defer sw.Close()
		res, err := e.agentInvoke(ctx, input)
		if err != nil {
			sw.Send(nil, err)
			return
		}
		sw.Send(res, nil)
	}()

	return sr, nil
}
