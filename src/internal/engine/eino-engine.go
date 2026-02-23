package engine

import (
	"context"
	"fmt"
	"io"
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
	compiledGraph   compose.Runnable[*graphInput, *graphOutput]
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
	msgs := input.Messages
	// Add user prompt if not already there (it might be restored from checkpoint)
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != schema.User || msgs[len(msgs)-1].Content != input.Prompt {
		msgs = append(msgs, schema.UserMessage(input.Prompt))
	}

	var totalUsage llm.Usage
	systemPrompt := input.Soul + input.HumanContext

	for i := 0; i < e.maxSteps; i++ {
		assistant, err := e.chat.Generate(ctx, msgs, input.CallOpts...)
		if err != nil {
			return nil, err
		}

		if assistant.ResponseMeta != nil && assistant.ResponseMeta.Usage != nil {
			totalUsage.PromptTokens += assistant.ResponseMeta.Usage.PromptTokens
			totalUsage.CompletionTokens += assistant.ResponseMeta.Usage.CompletionTokens
			totalUsage.TotalTokens += assistant.ResponseMeta.Usage.TotalTokens

			if _, err := e.flushIfNeeded(ctx, msgs, totalUsage.PromptTokens, input.CallOpts); err != nil {
				slog.Warn("flush failed", "error", err)
			}
			if newMsgs, summarized, err := e.summarizeIfNeeded(ctx, systemPrompt, msgs, totalUsage.PromptTokens, input.CallOpts); err == nil && summarized {
				msgs = newMsgs
			}
		}

		if len(assistant.ToolCalls) == 0 {
			return &graphOutput{
				Answer:   assistant.Content,
				Messages: msgs,
				Usage:    totalUsage,
			}, nil
		}

		msgs = append(msgs, assistant)
		toolMsgs, err := e.tools.Invoke(ctx, assistant)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, toolMsgs...)
	}

	// Final generation if loop exhausted
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
		finalErr = err
		return "", nil, err
	}

	// Clear checkpoint on success
	if e.checkPointStore != nil {
		_ = e.checkPointStore.Delete(ctx, sess.ID)
	}

	finalResp = output.Answer
	return output.Answer, &output.Usage, nil
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
