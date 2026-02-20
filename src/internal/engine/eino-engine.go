package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
	"miri-main/src/internal/tools/webfetch"
	"miri-main/src/internal/tools/websearch"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// newSlogHandler creates a callback handler that logs component execution to slog.
func newSlogHandler() callbacks.Handler {
	return callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			slog.Info("Eino Component Start",
				"name", info.Name,
				"type", info.Type,
				"component", info.Component)
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			slog.Info("Eino Component End",
				"name", info.Name,
				"type", info.Type,
				"component", info.Component)
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			slog.Error("Eino Component Error",
				"name", info.Name,
				"type", info.Type,
				"component", info.Component,
				"error", err)
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
	chat     model.BaseChatModel
	tools    *compose.ToolsNode
	maxSteps int
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
	})
	if err != nil {
		return nil, err
	}

	var chatModel model.BaseChatModel = cm

	// Define tools
	searchTool := &searchToolWrapper{}
	fetchTool := &fetchToolWrapper{}

	toolsNode, err := compose.NewToolNode(context.Background(), &compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{searchTool, fetchTool},
	})
	if err != nil {
		return nil, err
	}

	// Bind tools to model
	toolInfos := []*schema.ToolInfo{
		searchTool.getInfo(),
		fetchTool.getInfo(),
	}

	// Prefer the safer ToolCalling API
	if tc, err2 := cm.WithTools(toolInfos); err2 == nil {
		chatModel = tc
	} else if err := cm.BindTools(toolInfos); err != nil {
		return nil, err
	}

	// Return engine with model and tools node; we'll drive ReAct in code
	return &EinoEngine{chat: chatModel, tools: toolsNode, maxSteps: 6}, nil
}

// Respond builds a conversation including system prompt, history and current user prompt.
func (e *EinoEngine) Respond(ctx context.Context, sess *session.Session, promptStr string, humanContext string) (string, *llm.Usage, error) {
	// Initialize callbacks with slog handler
	ctx = callbacks.InitCallbacks(ctx, &callbacks.RunInfo{
		Name: "EinoEngine",
	}, newSlogHandler())

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

	// Use our new Memory to load history
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

	for i := 0; i < e.maxSteps; i++ {
		// Let model think/respond
		assistant, err := e.chat.Generate(ctx, msgs, callOpts...)
		if err != nil {
			finalErr = err
			return "", nil, err
		}
		// If model doesn't call tools, we are done
		if len(assistant.ToolCalls) == 0 {
			finalResp = assistant.Content
			return assistant.Content, nil, nil
		}
		// Append assistant tool call message
		msgs = append(msgs, assistant)
		// Execute tools
		toolMsgs, err := e.tools.Invoke(ctx, assistant)
		if err != nil {
			finalErr = err
			return "", nil, err
		}
		// Feed tool results back into the conversation
		msgs = append(msgs, toolMsgs...)
	}

	// Safety: if loop exhausted, return the latest assistant content without tools
	final, err := e.chat.Generate(ctx, msgs, callOpts...)
	if err != nil {
		finalErr = err
		return "", nil, err
	}
	finalResp = final.Content
	return final.Content, nil, nil
}

// Tool Wrappers

type searchToolWrapper struct{}

func (s *searchToolWrapper) getInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "web_search",
		Desc: "Search the web for current information.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The search query",
				Required: true,
			},
		}),
	}
}

func (s *searchToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return s.getInfo(), nil
}

func (s *searchToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	results, err := websearch.Search(ctx, args.Query)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(results)
	return string(b), nil
}

type fetchToolWrapper struct{}

func (f *fetchToolWrapper) getInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "web_fetch",
		Desc: "Fetch the content of a web page.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "The URL to fetch",
				Required: true,
			},
		}),
	}
}

func (f *fetchToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return f.getInfo(), nil
}

func (f *fetchToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	_, _, body, err := webfetch.Fetch(ctx, args.URL, 0)
	if err != nil {
		return "", err
	}
	return body, nil
}
