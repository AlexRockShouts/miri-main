package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine/skills"
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
				"component", info.Component,
				"input_type", fmt.Sprintf("%T", input))

			if out != nil && info.Component == components.ComponentOfChatModel {
				out <- fmt.Sprintf("[Thought: %s started]\n", info.Name)
			}
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			slog.Info("Eino Component End",
				"name", info.Name,
				"type", info.Type,
				"component", info.Component,
				"output_type", fmt.Sprintf("%T", output))

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
	compiledGraph   compose.Runnable[*graphInput, *graphOutput]
	skillLoader     *skills.SkillLoader
}

type graphInput struct {
	SessionID    string
	Messages     []*schema.Message
	Prompt       string
	HumanContext string
	Soul         string
	CallOpts     []model.Option
}

type graphOutput struct {
	Answer   string
	Messages []*schema.Message
	Usage    llm.Usage
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

	ee := &EinoEngine{
		chat:            chatModel,
		tools:           toolsNode,
		maxSteps:        12,
		debug:           cfg.Agents.Debug,
		checkPointStore: cpStore,
		contextWindow:   ctxWindow,
		storageBaseDir:  filepath.Join(cfg.StorageDir, "memory"),
	}

	// Initialize skills
	skillsDir := filepath.Join(cfg.StorageDir, "skills")
	scriptsDir := "scripts" // default scripts directory
	ee.skillLoader = skills.NewSkillLoader(skillsDir, scriptsDir)
	if err := ee.skillLoader.Load(); err != nil {
		slog.Warn("Failed to load skills", "dir", skillsDir, "error", err)
	}

	// Add skill tools
	skillSearchTool := skills.NewSearchTool(ee.skillLoader)
	skillUseTool := skills.NewUseTool(ee.skillLoader)

	// Update tools node with skill tools and inferred script tools
	allTools := []tool.BaseTool{searchTool, fetchTool, cmdTool, goInstallTool, curlInstallTool, skillSearchTool, skillUseTool}
	allTools = append(allTools, ee.skillLoader.GetExtraTools()...)

	toolsNode, err = compose.NewToolNode(context.Background(), &compose.ToolsNodeConfig{
		Tools: allTools,
	})
	if err != nil {
		return nil, err
	}
	ee.tools = toolsNode

	// Bind tools to model (including skill tools and inferred script tools)
	toolInfos = append(toolInfos, &schema.ToolInfo{
		Name: "skill_search",
		Desc: "Search for available skills and capabilities. Returns matching skill names and descriptions.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query to match against skill names, descriptions, or tags.",
				Required: false,
			},
		}),
	}, &schema.ToolInfo{
		Name: "skill_use",
		Desc: "Load a specific skill's instructions and capabilities into the current context.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"skill_name": {
				Type:     schema.String,
				Desc:     "The exact name of the skill to load.",
				Required: true,
			},
		}),
	})

	for _, t := range ee.skillLoader.GetExtraTools() {
		info, _ := t.Info(context.Background())
		toolInfos = append(toolInfos, info)
	}

	// Re-bind with updated toolInfos
	if tc, err2 := cm.WithTools(toolInfos); err2 == nil {
		ee.chat = tc
	} else if err := cm.BindTools(toolInfos); err != nil {
		return nil, err
	}

	// Compile the graph
	chain := compose.NewChain[*graphInput, *graphOutput]()

	// 1. Retriever node
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input *graphInput) (*graphInput, error) {
		// Initialize messages if needed (this part was in Respond/StreamRespond)
		if len(input.Messages) == 0 {
			input.Messages = []*schema.Message{schema.SystemMessage(input.Soul + input.HumanContext)}
		}

		// Inject retrieved memory
		newMsgs, ok := ee.injectRetrievedMemoryWithStatus(ctx, input.Messages)
		input.Messages = newMsgs
		if ok && ee.debug {
			slog.Info("EinoEngine Debug: Long-term memory injected")
		}
		return input, nil
	}), compose.WithNodeName("retriever"))

	// 2. Flush node
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input *graphInput) (*graphInput, error) {
		// Use a heuristic for usage if not available yet (first call)
		// Or just skip flush if no usage info is present in input.
		// For now, the current implementation in Respond calls flush *after* generate.
		// If we want it as a node *before* agent, it will use usage from previous turns if available.
		// But graphInput doesn't have usage yet.
		// Let's keep the flush/compact logic inside the agent node for now if we want it to be per-step,
		// OR move it after the agent node.
		// The user said: flush, compact, agent.
		return input, nil
	}), compose.WithNodeName("flush"))

	// 3. Compact node
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input *graphInput) (*graphInput, error) {
		return input, nil
	}), compose.WithNodeName("compact"))

	// 4. Agent node
	agentLambda, err := compose.AnyLambda[*graphInput, *graphOutput, any](
		func(ctx context.Context, input *graphInput, opts ...any) (*graphOutput, error) {
			return ee.agentInvoke(ctx, input)
		},
		func(ctx context.Context, input *graphInput, opts ...any) (*schema.StreamReader[*graphOutput], error) {
			return ee.agentStream(ctx, input)
		},
		nil, nil,
	)
	if err != nil {
		return nil, err
	}
	chain.AppendLambda(agentLambda, compose.WithNodeName("agent"))

	compiled, err := chain.Compile(context.Background(),
		compose.WithCheckPointStore(ee.checkPointStore),
		compose.WithMaxRunSteps(ee.maxSteps+5),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compile graph: %w", err)
	}
	ee.compiledGraph = compiled

	return ee, nil
}

func (e *EinoEngine) agentInvoke(ctx context.Context, input *graphInput) (*graphOutput, error) {
	slog.Info("Agent loop start", "session_id", input.SessionID, "max_steps", e.maxSteps)
	msgs := input.Messages
	// Add user prompt if not already there (it might be restored from checkpoint)
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != schema.User || msgs[len(msgs)-1].Content != input.Prompt {
		msgs = append(msgs, schema.UserMessage(input.Prompt))
	}

	var totalUsage llm.Usage
	systemPrompt := input.Soul + input.HumanContext

	for i := 0; i < e.maxSteps; i++ {
		slog.Debug("Agent loop iteration", "step", i, "messages_count", len(msgs))
		assistant, err := e.chat.Generate(ctx, msgs, input.CallOpts...)
		if err != nil {
			slog.Error("Chat generate failed", "step", i, "error", err)
			return nil, err
		}

		if assistant.ResponseMeta != nil && assistant.ResponseMeta.Usage != nil {
			totalUsage.PromptTokens += assistant.ResponseMeta.Usage.PromptTokens
			totalUsage.CompletionTokens += assistant.ResponseMeta.Usage.CompletionTokens
			totalUsage.TotalTokens += assistant.ResponseMeta.Usage.TotalTokens

			slog.Debug("Usage update", "step", i, "total_tokens", totalUsage.TotalTokens)

			if _, err := e.flushIfNeeded(ctx, msgs, totalUsage.PromptTokens, input.CallOpts); err != nil {
				slog.Warn("flush failed", "error", err)
			}
			if newMsgs, summarized, err := e.summarizeIfNeeded(ctx, systemPrompt, msgs, totalUsage.PromptTokens, input.CallOpts); err == nil && summarized {
				slog.Info("Memory summarized", "step", i, "old_count", len(msgs), "new_count", len(newMsgs))
				msgs = newMsgs
			}
		}

		if len(assistant.ToolCalls) == 0 {
			slog.Info("Agent loop finished (no tool calls)", "steps", i+1, "total_tokens", totalUsage.TotalTokens)
			return &graphOutput{
				Answer:   assistant.Content,
				Messages: msgs,
				Usage:    totalUsage,
			}, nil
		}

		slog.Info("Agent tool calls triggered", "step", i, "calls", len(assistant.ToolCalls))
		msgs = append(msgs, assistant)

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
						msgs = append(msgs, schema.SystemMessage(fmt.Sprintf("SKILL LOADED: %s\n\n%s", skill.Name, skill.FullContent)))
					}
				}
			}
		}

		toolMsgs, err := e.tools.Invoke(ctx, assistant)
		if err != nil {
			slog.Error("Tool invoke failed", "step", i, "error", err)
			return nil, err
		}
		msgs = append(msgs, toolMsgs...)
	}

	// Final generation if loop exhausted
	slog.Info("Agent loop exhausted, final generation", "max_steps", e.maxSteps)
	final, err := e.chat.Generate(ctx, msgs, input.CallOpts...)
	if err != nil {
		return nil, err
	}
	if final.ResponseMeta != nil && final.ResponseMeta.Usage != nil {
		totalUsage.PromptTokens += final.ResponseMeta.Usage.PromptTokens
		totalUsage.CompletionTokens += final.ResponseMeta.Usage.CompletionTokens
		totalUsage.TotalTokens += final.ResponseMeta.Usage.TotalTokens
	}

	return &graphOutput{
		Answer:   final.Content,
		Messages: msgs,
		Usage:    totalUsage,
	}, nil
}

func (e *EinoEngine) agentStream(ctx context.Context, input *graphInput) (*schema.StreamReader[*graphOutput], error) {
	// For streaming, we use a pipe to send progress updates and finally the graphOutput.
	// However, eino's graph streaming expects StreamReader of the output type.
	// This means we can only stream graphOutput objects.
	// This might not be what's needed if we want to stream partial text.
	// But Eino's graph can also stream if components support it.

	// To keep it simple and consistent with previous behavior, we'll use a manual stream implementation
	// that sends "thought" strings through the callback handler (which is already set up in StreamRespond).
	// The final result will be the graphOutput.

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

// Respond builds a conversation including system prompt, history and current user prompt.
func (e *EinoEngine) Respond(ctx context.Context, sess *session.Session, promptStr string, humanContext string) (string, *llm.Usage, error) {
	slog.Info("EinoEngine Respond", "session_id", sess.ID, "prompt_len", len(promptStr))
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

	// Load history
	mem := &Memory{session: sess}
	history, _ := mem.Get(ctx, nil)

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

	input := &graphInput{
		SessionID:    sess.ID,
		Messages:     history,
		Prompt:       promptStr,
		HumanContext: humanContext,
		Soul:         sess.GetSoul(),
		CallOpts:     callOpts,
	}

	output, err := e.compiledGraph.Invoke(ctx, input, compose.WithCheckPointID(sess.ID))
	if err != nil {
		// Eino wraps node errors in an internal error type that isn't exported.
		// We can't type-assert it, so log the error and its immediate cause if any.
		slog.Error("Graph error", "error", err, "session_id", sess.ID)
		if cause := errors.Unwrap(err); cause != nil {
			slog.Error("Cause", "cause", cause)
		}
		finalErr = err
		return "", nil, err
	}

	slog.Info("EinoEngine Respond complete", "session_id", sess.ID, "total_tokens", output.Usage.TotalTokens)

	// Clear checkpoint on success
	if e.checkPointStore != nil {
		_ = e.checkPointStore.Delete(ctx, sess.ID)
	}

	finalResp = output.Answer
	return output.Answer, &output.Usage, nil
}

func (e *EinoEngine) StreamRespond(ctx context.Context, sess *session.Session, promptStr string, humanContext string) (<-chan string, error) {
	slog.Info("EinoEngine StreamRespond", "session_id", sess.ID, "prompt_len", len(promptStr))
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

		// Load history
		mem := &Memory{session: sess}
		history, _ := mem.Get(ctx, nil)

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

		input := &graphInput{
			SessionID:    sess.ID,
			Messages:     history,
			Prompt:       promptStr,
			HumanContext: humanContext,
			Soul:         sess.GetSoul(),
			CallOpts:     callOpts,
		}

		stream, err := e.compiledGraph.Stream(ctx, input, compose.WithCheckPointID(sess.ID))
		if err != nil {
			callbacks.OnError(ctx, err)
			out <- fmt.Sprintf("[Error: %v]\n", err)
			return
		}

		var lastOutput *graphOutput
		for {
			chunk, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				callbacks.OnError(ctx, err)
				out <- fmt.Sprintf("[Stream Error: %v]\n", err)
				return
			}
			lastOutput = chunk
		}

		if lastOutput != nil {
			out <- lastOutput.Answer
			callbacks.OnEnd(ctx, lastOutput.Answer)
			// Clear checkpoint on success
			if e.checkPointStore != nil {
				_ = e.checkPointStore.Delete(ctx, sess.ID)
			}
			// Send usage as a special metadata chunk
			out <- fmt.Sprintf("[Usage: %d prompt, %d completion, %d total tokens]",
				lastOutput.Usage.PromptTokens,
				lastOutput.Usage.CompletionTokens,
				lastOutput.Usage.TotalTokens)
		}
	}()

	return out, nil
}
