package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"miri-main/src/internal/engine/memory"
	"miri-main/src/internal/engine/skills"
	"miri-main/src/internal/engine/subagents"
	"miri-main/src/internal/engine/tools"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/storage"
	"path/filepath"
	"regexp"
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

// EinoEngine implements a ReAct agent using Eino components (ChatModel + ToolsNode) with a manual control loop.
type EinoEngine struct {
	chat            model.BaseChatModel
	tools           *compose.ToolsNode
	maxSteps        int
	debug           bool
	checkPointStore *FileCheckPointStore
	contextWindow   int
	storageBaseDir  string
	storage         *storage.Storage
	compiledGraph   compose.Runnable[*graphInput, *graphOutput]
	skillLoader     *skills.SkillLoader
	taskGateway     tools.TaskGateway
	memorySystem    memory.MemorySystem
	brain           *memory.Brain
	subAgentTools   map[string]tool.InvokableTool
	modelCost       config.ModelCost

	sensitiveStrings []string
	xaiIDRegex       *regexp.Regexp
	uuidRegex        *regexp.Regexp
	emailRegex       *regexp.Regexp
	hexKeyRegex      *regexp.Regexp
}

type graphInput struct {
	SessionID string
	Messages  []*schema.Message
	Prompt    string
	CallOpts  []model.Option
	// VerboseCh receives tagged verbose events during the agent loop:
	// [Thought: ...], [Tool: name(args)], [ToolResult: name → result]
	// It is optional; nil means no verbose output.
	VerboseCh chan<- string
}

type graphOutput struct {
	SessionID   string
	Answer      string
	Messages    []*schema.Message
	Usage       llm.Usage
	LastMessage *schema.Message
}

func NewEinoEngine(cfg *config.Config, st *storage.Storage, providerName, modelName string, taskGateway tools.TaskGateway) (*EinoEngine, error) {
	prov, ok := cfg.Models.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", providerName)
	}

	cm, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
		BaseURL: prov.BaseURL,
		APIKey:  prov.APIKey,
		Model:   modelName,
		Timeout: 30 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	var chatModel model.BaseChatModel = cm

	// determine context window and cost for selected model
	ctxWindow := 0
	var modelCost config.ModelCost
	fullName := providerName + "/" + modelName
	for _, m := range prov.Models {
		if m.ID == fullName || m.Name == fullName || m.Name == modelName {
			if m.ContextWindow > 0 {
				ctxWindow = m.ContextWindow
			}
			modelCost = m.Cost
			break
		}
	}

	// Initialize Vector Memory
	factsVM, err := memory.NewVectorMemory(cfg, "miri_facts")
	if err != nil {
		slog.Warn("failed to initialize facts vector memory", "error", err)
	}

	summariesVM, err := memory.NewVectorMemory(cfg, "miri_summaries")
	if err != nil {
		slog.Warn("failed to initialize summaries vector memory", "error", err)
	}

	stepsVM, err := memory.NewVectorMemory(cfg, "miri_steps")
	if err != nil {
		slog.Warn("failed to initialize steps vector memory", "error", err)
	}

	// Define tools
	searchTool := &tools.SearchToolWrapper{}
	fetchTool := &tools.FetchToolWrapper{}
	// pruned: grokipediaTool := tools.CreateGrokipediaTool() // redundant with search/fetch
	uploadsDir := filepath.Join(cfg.StorageDir, "uploads")
	cmdTool := tools.NewCmdTool(uploadsDir)
	fileManagerTool := tools.NewFileManagerTool(cfg.StorageDir, nil) // Will be properly set if gateway is available
	retrievePasswordTool := tools.NewRetrievePasswordTool(cfg.Miri.KeePass.DBPath, cfg.Miri.KeePass.Password)
	storePasswordTool := tools.NewStorePasswordTool(cfg.Miri.KeePass.DBPath, cfg.Miri.KeePass.Password)

	cpStore, err := NewFileCheckPointStore(cfg.StorageDir)
	if err != nil {
		slog.Warn("failed to initialize checkpoint store", "error", err)
	}

	ee := &EinoEngine{
		chat:             chatModel,
		maxSteps:         12,
		debug:            cfg.Agents.Debug,
		checkPointStore:  cpStore,
		contextWindow:    ctxWindow,
		storageBaseDir:   cfg.StorageDir,
		storage:          st,
		memorySystem:     factsVM,
		brain:            memory.NewBrain(chatModel, factsVM, summariesVM, stepsVM, ctxWindow, st, cfg.Miri.Brain.Retrieval, cfg.Miri.Brain.MaxNodesPerSession),
		modelCost:        modelCost,
		taskGateway:      taskGateway,
		sensitiveStrings: []string{prov.APIKey},
		xaiIDRegex:       regexp.MustCompile(`(?i)(Team|API key ID):? [0-9a-f-]{36}`),
		uuidRegex:        regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`),
		emailRegex:       regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`),
		hexKeyRegex:      regexp.MustCompile(`(?i)\b[0-9a-f]{32,}\b`),
	}

	if ee.brain != nil {
		ee.brain.SetSanitizeFunc(ee.sanitizeMessages)
	}

	// Also add other provider API keys to sensitive strings
	for _, p := range cfg.Models.Providers {
		if p.APIKey != "" {
			ee.sensitiveStrings = append(ee.sensitiveStrings, p.APIKey)
		}
	}

	// Tools requiring ee
	chromeMCPTool := tools.NewChromeMCPTool()
	cotGraphTool := NewCotGraphTool()
	localInstallTool := NewLocalInstallTool(ee)
	topologyTool := NewTopologyTool()

	skillRemoveTool := tools.NewSkillRemoveTool(cfg, func() {
		if ee.skillLoader != nil {
			_ = ee.skillLoader.Load()
		}
	})

	skillListTool := &tools.SkillRemoteListToolWrapper{}
	skillInstallTool := tools.NewSkillInstallTool(cfg, func() {
		if ee.skillLoader != nil {
			_ = ee.skillLoader.Load()
		}
	})

	// Add skill tools
	skillsDir := filepath.Join(cfg.StorageDir, "skills")
	scriptsDir := "scripts" // default scripts directory
	ee.skillLoader = skills.NewSkillLoader(skillsDir, scriptsDir)
	if err := ee.skillLoader.Load(); err != nil {
		slog.Warn("Failed to load skills", "dir", skillsDir, "error", err)
	}

	skillUseTool := skills.NewUseTool(ee.skillLoader)

	// Update tools node with all tools
	allTools := []tool.BaseTool{searchTool, fetchTool /* pruned: grokipediaTool (redundant with search/fetch) */, cmdTool, skillRemoveTool, skillListTool, skillInstallTool, skillUseTool, fileManagerTool, retrievePasswordTool, storePasswordTool, chromeMCPTool, cotGraphTool, localInstallTool, topologyTool}
	allTools = append(allTools, ee.skillLoader.GetExtraTools()...)

	// Add Eino ADK sub-agent tools (Researcher, Coder, Reviewer)
	adkTools := subagents.BuildSubAgentTools(context.Background(), chatModel, filepath.Join(ee.storageBaseDir, "uploads"), ee.storage)
	allTools = append(allTools, adkTools...)

	// Extract sub-agent invokers for /agent slash command
	ee.subAgentTools = make(map[string]tool.InvokableTool)
	for _, t := range adkTools {
		if info, err := t.Info(context.Background()); err == nil {
			if invoker, ok := t.(tool.InvokableTool); ok {
				ee.subAgentTools[info.Name] = invoker
			}
		}
	}

	toolsNode, err := compose.NewToolNode(context.Background(), &compose.ToolsNodeConfig{
		Tools: allTools,
	})
	if err != nil {
		return nil, err
	}
	ee.tools = toolsNode

	// Bind tools to model
	toolInfos := []*schema.ToolInfo{
		searchTool.GetInfo(),
		fetchTool.GetInfo(),
		cmdTool.GetInfo(),
		skillRemoveTool.GetInfo(),
		// pruned grokipediaTool - redundant info retrieval tool (search/fetch suffice)
		fileManagerTool.GetInfo(),
		retrievePasswordTool.GetInfo(),
		storePasswordTool.GetInfo(),
		chromeMCPTool.GetInfo(),
	}

	if info, err := skillUseTool.Info(context.Background()); err == nil {
		toolInfos = append(toolInfos, info)
	}

	// Task Manager Tool Info
	taskMgrTool := tools.NewTaskManagerTool(nil, "")
	toolInfos = append(toolInfos, taskMgrTool.GetInfo())

	// ADK sub-agent tool infos
	for _, t := range allTools {
		if info, err2 := t.Info(context.Background()); err2 == nil {
			// avoid duplicates already added above
			var already bool
			for _, existing := range toolInfos {
				if existing.Name == info.Name {
					already = true
					break
				}
			}
			if !already {
				toolInfos = append(toolInfos, info)
			}
		}
	}

	for _, t := range ee.skillLoader.GetExtraTools() {
		info, _ := t.Info(context.Background())
		toolInfos = append(toolInfos, info)
	}

	// Prefer the safer ToolCalling API
	if tc, err2 := cm.WithTools(toolInfos); err2 == nil {
		ee.chat = tc
	} else if err := cm.BindTools(toolInfos); err != nil {
		return nil, err
	}

	// Initialize sub-agent tools
	ctx := context.Background()
	agentTools := subagents.BuildSubAgentTools(ctx, ee.chat, filepath.Join(ee.storageBaseDir, "uploads"), ee.storage)
	ee.subAgentTools = make(map[string]tool.InvokableTool, 3)
	for _, baseTool := range agentTools {
		info, err := baseTool.Info(ctx)
		if err != nil {
			slog.Warn("failed to get subagent tool info", "error", err)
			continue
		}
		toolRole := strings.ToLower(info.Name)
		if toolRole == "researcher" || toolRole == "coder" || toolRole == "reviewer" {
			ee.subAgentTools[toolRole] = baseTool.(tool.InvokableTool)
		}
	}

	if err := ee.buildGraph(); err != nil {
		return nil, err
	}

	return ee, nil
}

func (e *EinoEngine) CalculateCost(promptTokens, outputTokens int) float64 {
	// Cost is typically per 1M tokens.
	return (float64(promptTokens) * e.modelCost.Input / 1000000.0) + (float64(outputTokens) * e.modelCost.Output / 1000000.0)
}

func (e *EinoEngine) sanitizeString(s string) string {
	if s == "" {
		return ""
	}

	// Redact XAI specific IDs (Team, API key ID)
	if e.xaiIDRegex != nil {
		s = e.xaiIDRegex.ReplaceAllString(s, "$1: [REDACTED]")
	}

	// Redact Emails
	if e.emailRegex != nil {
		s = e.emailRegex.ReplaceAllString(s, "[REDACTED_EMAIL]")
	}

	// Redact long hex strings (common for keys/tokens)
	if e.hexKeyRegex != nil {
		s = e.hexKeyRegex.ReplaceAllString(s, "[REDACTED_KEY]")
	}

	// Redact sensitive strings (API keys)
	for _, ss := range e.sensitiveStrings {
		if ss != "" && len(ss) > 8 { // Only redact if long enough to be a key
			s = strings.ReplaceAll(s, ss, "[REDACTED_SENSITIVE]")
		}
	}

	return s
}

func (e *EinoEngine) sanitizeMessages(msgs []*schema.Message) []*schema.Message {
	if msgs == nil {
		return nil
	}
	res := make([]*schema.Message, len(msgs))
	for i, m := range msgs {
		res[i] = &schema.Message{
			Role:                     m.Role,
			Content:                  e.sanitizeString(m.Content),
			MultiContent:             m.MultiContent,
			UserInputMultiContent:    m.UserInputMultiContent,
			AssistantGenMultiContent: m.AssistantGenMultiContent,
			Name:                     m.Name,
			ToolCalls:                m.ToolCalls,
			ToolCallID:               m.ToolCallID,
			ToolName:                 m.ToolName,
			ResponseMeta:             m.ResponseMeta,
			ReasoningContent:         e.sanitizeString(m.ReasoningContent),
			Extra:                    m.Extra,
		}
		// Sanitize tool call arguments if present
		if len(m.ToolCalls) > 0 {
			newTCs := make([]schema.ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				newTCs[j] = tc
				newTCs[j].Function.Arguments = e.sanitizeString(tc.Function.Arguments)
			}
			res[i].ToolCalls = newTCs
		}
	}
	return res
}

func (e *EinoEngine) SpawnSubAgent(ctx context.Context, role, query string) (string, error) {
	role = strings.ToLower(role)
	switch role {
	case "researcher", "coder", "reviewer":
	default:
		return "", fmt.Errorf("unsupported sub-agent role %q", role)
	}
	invokerI, ok := e.subAgentTools[role]
	if !ok {
		return "", fmt.Errorf("no tool for %q", role)
	}
	invoker, ok := invokerI.(tool.InvokableTool)
	if !ok {
		return "", fmt.Errorf("tool not invokable")
	}
	type toolArgs struct {
		Query string `json:"query"`
	}
	args := toolArgs{Query: query}
	js, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	resp, err := invoker.InvokableRun(ctx, string(js))
	if err != nil {
		return "", fmt.Errorf("invoke: %w", err)
	}
	re := regexp.MustCompile(`ID:\s*(\S+)`)
	matches := re.FindStringSubmatch(resp)
	if len(matches) != 2 {
		return "", fmt.Errorf("parse ID from %q", resp)
	}
	return matches[1], nil
}
