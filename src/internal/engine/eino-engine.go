package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine/tools"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// newSlogHandler creates a callback handler that logs component execution to slog.
func newSlogHandler(out chan<- string) callbacks.Handler {
	return callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			slog.Info("Eino Component Start",
				"name", info.Name,
				"type", info.Type,
				"component", info.Component)
			if out != nil && info.Component == components.ComponentOfChatModel {
				out <- fmt.Sprintf("[Thought: %s started]\n", info.Name)
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			slog.Info("Eino Component End",
				"name", info.Name,
				"type", info.Type,
				"component", info.Component)
			if out != nil && info.Component == compose.ComponentOfToolsNode {
				out <- fmt.Sprintf("[Tools: %s finished]\n", info.Name)
			}
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			slog.Error("Eino Component Error",
				"name", info.Name,
				"type", info.Type,
				"component", info.Component,
				"error", err)
			if out != nil {
				out <- fmt.Sprintf("[Error in %s: %v]\n", info.Name, err)
			}
			return ctx
		}).
		Build()
}

// Memory implements an Eino-compatible memory store using our session.Session.
type Memory struct {
	session *session.Session
}

func (m *Memory) Get(ctx context.Context, _ any) ([]*schema.Message, error) {
	var msgs []*schema.Message
	for _, msg := range m.session.Messages {
		if strings.TrimSpace(msg.Prompt) != "" {
			msgs = append(msgs, schema.UserMessage(msg.Prompt))
		}
		if strings.TrimSpace(msg.Response) != "" {
			msgs = append(msgs, schema.AssistantMessage(msg.Response, nil))
		}
	}
	return msgs, nil
}

func (m *Memory) Save(ctx context.Context, msgs []*schema.Message) error {
	// Not used since our session management handles final persistence.
	// But it's good to have it here for future graph use.
	return nil
}

// EinoEngine implements a ReAct agent using Eino components (ChatModel + ToolsNode) with a manual control loop.
type EinoEngine struct {
	chat            model.BaseChatModel
	tools           *compose.ToolsNode
	maxSteps        int
	debug           bool
	checkPointStore *FileCheckPointStore
	contextWindow   int
	storageBaseDir  string
}

func NewEinoEngine(cfg *config.Config, providerName, modelName string) (*EinoEngine, error) {
	prov, ok := cfg.Models.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}

	cm, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
		BaseURL: prov.BaseURL,
		APIKey:  prov.APIKey,
		Model:   modelName,
		Timeout: 300 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	var chatModel model.BaseChatModel = cm

	// determine context window for selected model
	ctxWindow := 0
	fullName := providerName + "/" + modelName
	for _, m := range prov.Models {
		if m.ID == fullName || m.Name == fullName || m.Name == modelName {
			if m.ContextWindow > 0 {
				ctxWindow = m.ContextWindow
			}
			break
		}
	}

	// Define tools
	searchTool := &tools.SearchToolWrapper{}
	fetchTool := &tools.FetchToolWrapper{}
	cmdTool := &tools.CmdToolWrapper{}
	goInstallTool := &tools.GoInstallToolWrapper{}
	curlInstallTool := &tools.CurlInstallToolWrapper{}

	toolsNode, err := compose.NewToolNode(context.Background(), &compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{searchTool, fetchTool, cmdTool, goInstallTool, curlInstallTool},
	})
	if err != nil {
		return nil, err
	}

	// Bind tools to model
	toolInfos := []*schema.ToolInfo{
		searchTool.GetInfo(),
		fetchTool.GetInfo(),
		cmdTool.GetInfo(),
		goInstallTool.GetInfo(),
		curlInstallTool.GetInfo(),
	}

	// Prefer the safer ToolCalling API
	if tc, err2 := cm.WithTools(toolInfos); err2 == nil {
		chatModel = tc
	} else if err := cm.BindTools(toolInfos); err != nil {
		return nil, err
	}

	cpStore, err := NewFileCheckPointStore(cfg.StorageDir)
	if err != nil {
		slog.Warn("failed to initialize checkpoint store", "error", err)
	}

	// Return engine with model and tools node; we'll drive ReAct in code
	return &EinoEngine{
		chat:            chatModel,
		tools:           toolsNode,
		maxSteps:        12,
		debug:           cfg.Agents.Debug,
		checkPointStore: cpStore,
		contextWindow:   ctxWindow,
		storageBaseDir:  filepath.Join(cfg.StorageDir, "memory"),
	}, nil
}

// Respond builds a conversation including system prompt, history and current user prompt.
func (e *EinoEngine) Respond(ctx context.Context, sess *session.Session, promptStr string, humanContext string) (string, *llm.Usage, error) {
	// Initialize callbacks with slog handler
	ctx = callbacks.InitCallbacks(ctx, &callbacks.RunInfo{
		Name: "EinoEngine",
	}, newSlogHandler(nil))

	// Trigger start callback for the engine itself
	ctx = callbacks.OnStart(ctx, promptStr)
	var finalResp string
	var finalErr error
	defer func() {
		if finalErr != nil {
			callbacks.OnError(ctx, finalErr)
		} else {
			callbacks.OnEnd(ctx, finalResp)
		}
	}()

	// Build initial message list
	systemPrompt := sess.GetSoul() + humanContext
	msgs := []*schema.Message{schema.SystemMessage(systemPrompt)}

	if e.debug {
		slog.Info("EinoEngine Debug: System Prompt and Human Context initialized", "systemPrompt", systemPrompt)
	}

	// Use our new Memory to load history
	mem := &Memory{session: sess}
	history, err := mem.Get(ctx, nil)
	if err == nil {
		msgs = append(msgs, history...)
		if e.debug {
			slog.Info("EinoEngine Debug: History loaded", "historyCount", len(history))
		}
	} else if e.debug {
		slog.Error("EinoEngine Debug: Failed to load history", "error", err)
	}

	msgs = append(msgs, schema.UserMessage(promptStr))
	if e.debug {
		slog.Info("EinoEngine Debug: User prompt added", "prompt", promptStr)
	}

	// Collect dynamic options from context
	var callOpts []model.Option
	if opts, ok := FromContext(ctx); ok {
		if opts.Model != "" {
			callOpts = append(callOpts, model.WithModel(opts.Model))
		}
		if opts.Temperature != nil {
			callOpts = append(callOpts, model.WithTemperature(*opts.Temperature))
		}
		if opts.MaxTokens != nil {
			callOpts = append(callOpts, model.WithMaxTokens(*opts.MaxTokens))
		}
	}

	var totalUsage llm.Usage
	startStep := 0

	// Try to restore from checkpoint
	if e.checkPointStore != nil {
		if data, ok, err := e.checkPointStore.Get(ctx, sess.ID); err == nil && ok {
			var state engineState
			if err := json.Unmarshal(data, &state); err == nil {
				msgs = state.Messages
				startStep = state.Step
				if e.debug {
					slog.Info("EinoEngine Debug: Restored from checkpoint", "session", sess.ID, "step", startStep)
				}
			}
		}
	}

	for i := startStep; i < e.maxSteps; i++ {
		if e.debug {
			slog.Info("EinoEngine Debug: Step started", "step", i)
		}
		// Let model think/respond
		assistant, err := e.chat.Generate(ctx, msgs, callOpts...)
		if err != nil {
			finalErr = err
			if e.debug {
				slog.Error("EinoEngine Debug: Model generation failed", "step", i, "error", err)
			}
			return "", &totalUsage, err
		}

		if assistant.ResponseMeta != nil && assistant.ResponseMeta.Usage != nil {
			totalUsage.PromptTokens += assistant.ResponseMeta.Usage.PromptTokens
			totalUsage.CompletionTokens += assistant.ResponseMeta.Usage.CompletionTokens
			totalUsage.TotalTokens += assistant.ResponseMeta.Usage.TotalTokens
			// Check for pre-compaction flush need
			if flushed, err := e.flushIfNeeded(ctx, msgs, totalUsage.PromptTokens, callOpts); err != nil {
				slog.Warn("flush failed", "error", err)
			} else if flushed && e.debug {
				slog.Info("EinoEngine Debug: Performed memory flush due to high context usage")
			}
			// Check for hard-threshold summary/compaction
			if newMsgs, summarized, err := e.summarizeIfNeeded(ctx, systemPrompt, msgs, totalUsage.PromptTokens, callOpts); err != nil {
				slog.Warn("summary failed", "error", err)
			} else if summarized {
				msgs = newMsgs
				if e.debug {
					slog.Info("EinoEngine Debug: Performed conversation summary compaction (kept last 12 raw messages)")
				}
			}
		}

		if e.debug {
			slog.Info("EinoEngine Debug: Model responded", "step", i, "content", assistant.Content, "toolCalls", len(assistant.ToolCalls))
		}
		// If model doesn't call tools, we are done
		if len(assistant.ToolCalls) == 0 {
			// Clear checkpoint on success
			if e.checkPointStore != nil {
				_ = e.checkPointStore.Delete(ctx, sess.ID)
			}
			finalResp = assistant.Content
			return assistant.Content, &totalUsage, nil
		}
		// Append assistant tool call message
		msgs = append(msgs, assistant)
		// Execute tools
		toolMsgs, err := e.tools.Invoke(ctx, assistant)
		if err != nil {
			finalErr = err
			if e.debug {
				slog.Error("EinoEngine Debug: Tool invocation failed", "step", i, "error", err)
			}
			return "", &totalUsage, err
		}
		if e.debug {
			for idx, tm := range toolMsgs {
				slog.Info("EinoEngine Debug: Tool result", "step", i, "index", idx, "name", tm.ToolName, "id", tm.ToolCallID, "result", tm.Content)
			}
		}
		// Feed tool results back into the conversation
		msgs = append(msgs, toolMsgs...)

		// Save checkpoint
		if e.checkPointStore != nil {
			state := engineState{
				Messages: msgs,
				Step:     i + 1,
			}
			if data, err := json.Marshal(state); err == nil {
				_ = e.checkPointStore.Set(ctx, sess.ID, data)
			}
		}
	}

	// Safety: if loop exhausted, return the latest assistant content without tools
	if e.debug {
		slog.Info("EinoEngine Debug: Max steps reached, final generation")
	}
	final, err := e.chat.Generate(ctx, msgs, callOpts...)
	if err != nil {
		finalErr = err
		if e.debug {
			slog.Error("EinoEngine Debug: Final model generation failed", "error", err)
		}
		return "", &totalUsage, err
	}

	if final.ResponseMeta != nil && final.ResponseMeta.Usage != nil {
		totalUsage.PromptTokens += final.ResponseMeta.Usage.PromptTokens
		totalUsage.CompletionTokens += final.ResponseMeta.Usage.CompletionTokens
		totalUsage.TotalTokens += final.ResponseMeta.Usage.TotalTokens
	}

	finalResp = final.Content

	// Clear checkpoint on success
	if e.checkPointStore != nil {
		_ = e.checkPointStore.Delete(ctx, sess.ID)
	}

	return final.Content, &totalUsage, nil
}

func (e *EinoEngine) StreamRespond(ctx context.Context, sess *session.Session, promptStr string, humanContext string) (<-chan string, error) {
	out := make(chan string, 100)

	// Initialize callbacks with our streaming handler
	ctx = callbacks.InitCallbacks(ctx, &callbacks.RunInfo{
		Name: "EinoEngine",
	}, newSlogHandler(out))

	// Trigger start callback for the engine itself
	ctx = callbacks.OnStart(ctx, promptStr)

	go func() {
		defer close(out)
		defer func() {
			if r := recover(); r != nil {
				out <- fmt.Sprintf("[Panic: %v]\n", r)
			}
		}()

		// Build initial message list
		systemPrompt := sess.GetSoul() + humanContext
		msgs := []*schema.Message{schema.SystemMessage(systemPrompt)}

		// Use our Memory to load history
		mem := &Memory{session: sess}
		history, err := mem.Get(ctx, nil)
		if err == nil {
			msgs = append(msgs, history...)
		}

		msgs = append(msgs, schema.UserMessage(promptStr))

		// Collect dynamic options from context
		var callOpts []model.Option
		if opts, ok := FromContext(ctx); ok {
			if opts.Model != "" {
				callOpts = append(callOpts, model.WithModel(opts.Model))
			}
			if opts.Temperature != nil {
				callOpts = append(callOpts, model.WithTemperature(*opts.Temperature))
			}
			if opts.MaxTokens != nil {
				callOpts = append(callOpts, model.WithMaxTokens(*opts.MaxTokens))
			}
		}

		var totalUsage llm.Usage
		startStep := 0

		// Try to restore from checkpoint
		if e.checkPointStore != nil {
			if data, ok, err := e.checkPointStore.Get(ctx, sess.ID); err == nil && ok {
				var state engineState
				if err := json.Unmarshal(data, &state); err == nil {
					msgs = state.Messages
					startStep = state.Step
				}
			}
		}

		for i := startStep; i < e.maxSteps; i++ {
			// Let model think/respond
			assistant, err := e.chat.Generate(ctx, msgs, callOpts...)
			if err != nil {
				callbacks.OnError(ctx, err)
				out <- fmt.Sprintf("[Error: %v]\n", err)
				return
			}

			if assistant.ResponseMeta != nil && assistant.ResponseMeta.Usage != nil {
				totalUsage.PromptTokens += assistant.ResponseMeta.Usage.PromptTokens
				totalUsage.CompletionTokens += assistant.ResponseMeta.Usage.CompletionTokens
				totalUsage.TotalTokens += assistant.ResponseMeta.Usage.TotalTokens
				// Check for pre-compaction flush need
				if flushed, err := e.flushIfNeeded(ctx, msgs, totalUsage.PromptTokens, callOpts); err != nil {
					out <- fmt.Sprintf("[Flush Error: %v]", err)
				} else if flushed {
					out <- "[Flush: memory compacted and appended]"
				}
				// Check for hard-threshold summary/compaction
				if newMsgs, summarized, err := e.summarizeIfNeeded(ctx, systemPrompt, msgs, totalUsage.PromptTokens, callOpts); err != nil {
					out <- fmt.Sprintf("[Summary Error: %v]", err)
				} else if summarized {
					msgs = newMsgs
					out <- "[Summary: history compacted; older messages replaced with structured summary, last 12 kept]"
				}
			}

			// If model doesn't call tools, we are done
			if len(assistant.ToolCalls) == 0 {
				// Clear checkpoint on success
				if e.checkPointStore != nil {
					_ = e.checkPointStore.Delete(ctx, sess.ID)
				}
				out <- assistant.Content
				callbacks.OnEnd(ctx, assistant.Content)
				// Send usage as a special metadata chunk
				out <- fmt.Sprintf("[Usage: %d prompt, %d completion, %d total tokens]", totalUsage.PromptTokens, totalUsage.CompletionTokens, totalUsage.TotalTokens)
				return
			}

			// Report tool calls being made
			for _, tc := range assistant.ToolCalls {
				out <- fmt.Sprintf("[Calling Tool: %s with %s]\n", tc.Function.Name, tc.Function.Arguments)
			}

			// Append assistant tool call message
			msgs = append(msgs, assistant)

			// Execute tools
			toolMsgs, err := e.tools.Invoke(ctx, assistant)
			if err != nil {
				callbacks.OnError(ctx, err)
				out <- fmt.Sprintf("[Tool Invocation Error: %v]\n", err)
				return
			}

			// Feed tool results back into the conversation
			msgs = append(msgs, toolMsgs...)

			// Save checkpoint
			if e.checkPointStore != nil {
				state := engineState{
					Messages: msgs,
					Step:     i + 1,
				}
				if data, err := json.Marshal(state); err == nil {
					_ = e.checkPointStore.Set(ctx, sess.ID, data)
				}
			}
		}

		// Safety: if loop exhausted, return the latest assistant content without tools
		final, err := e.chat.Generate(ctx, msgs, callOpts...)
		if err != nil {
			callbacks.OnError(ctx, err)
			out <- fmt.Sprintf("[Final Generation Error: %v]\n", err)
			return
		}

		if final.ResponseMeta != nil && final.ResponseMeta.Usage != nil {
			totalUsage.PromptTokens += final.ResponseMeta.Usage.PromptTokens
			totalUsage.CompletionTokens += final.ResponseMeta.Usage.CompletionTokens
			totalUsage.TotalTokens += final.ResponseMeta.Usage.TotalTokens
		}

		out <- final.Content
		callbacks.OnEnd(ctx, final.Content)

		// Clear checkpoint on success
		if e.checkPointStore != nil {
			_ = e.checkPointStore.Delete(ctx, sess.ID)
		}

		// Send usage as a special metadata chunk
		out <- fmt.Sprintf("[Usage: %d prompt, %d completion, %d total tokens]", totalUsage.PromptTokens, totalUsage.CompletionTokens, totalUsage.TotalTokens)
	}()

	return out, nil
}
