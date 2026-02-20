package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"miri-main/src/internal/config"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
	"miri-main/src/internal/tools/webfetch"
	"miri-main/src/internal/tools/websearch"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

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
func (e *EinoEngine) Respond(ctx context.Context, sess *session.Session, prompt string, humanContext string) (string, *llm.Usage, error) {
	// Build initial message list
	msgs := []*schema.Message{schema.SystemMessage(sess.GetSoul() + humanContext)}
	for _, m := range sess.Messages {
		if strings.TrimSpace(m.Prompt) != "" {
			msgs = append(msgs, schema.UserMessage(m.Prompt))
		}
		if strings.TrimSpace(m.Response) != "" {
			msgs = append(msgs, schema.AssistantMessage(m.Response, nil))
		}
	}
	msgs = append(msgs, schema.UserMessage(prompt))

	for i := 0; i < e.maxSteps; i++ {
		// Let model think/respond
		assistant, err := e.chat.Generate(ctx, msgs)
		if err != nil {
			return "", nil, err
		}
		// If model doesn't call tools, we are done
		if len(assistant.ToolCalls) == 0 {
			return assistant.Content, nil, nil
		}
		// Append assistant tool call message
		msgs = append(msgs, assistant)
		// Execute tools
		toolMsgs, err := e.tools.Invoke(ctx, assistant)
		if err != nil {
			return "", nil, err
		}
		// Feed tool results back into the conversation
		msgs = append(msgs, toolMsgs...)
	}

	// Safety: if loop exhausted, return the latest assistant content without tools
	final, err := e.chat.Generate(ctx, msgs)
	if err != nil {
		return "", nil, err
	}
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
