package subagents

import (
	"context"
	"log/slog"
	"miri-main/src/internal/engine/tools"
	"path/filepath"

	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

import (
	"github.com/google/uuid"
	"miri-main/src/internal/storage"
)

// BuildSubAgentTools creates Researcher, Coder, and Reviewer sub-agents using Eino ADK
// and wraps each as a tool callable by the orchestrator LLM.

func getInstruction(role string) string {
	promptPath := filepath.Join("templates", "subagents", role+".prompt")
	data, err := os.ReadFile(promptPath)
	if err != nil {
		slog.Warn("failed to load subagent instruction prompt", "role", role, "path", promptPath, "error", err)
		return ""
	}
	return strings.TrimSpace(string(data))
}

func BuildSubAgentTools(ctx context.Context, chatModel model.BaseChatModel, storageDir string, st *storage.Storage) []einotool.BaseTool {
	sandboxDir := storageDir

	researcherTools := []einotool.BaseTool{
		&tools.SearchToolWrapper{},
		&tools.FetchToolWrapper{},
		&tools.GrokipediaToolWrapper{},
	}

	coderTools := []einotool.BaseTool{
		tools.NewCmdTool(sandboxDir),
		tools.NewFileManagerTool(storageDir, nil),
		&tools.SearchToolWrapper{},
		&tools.FetchToolWrapper{},
	}

	reviewerTools := []einotool.BaseTool{
		&tools.SearchToolWrapper{},
		&tools.FetchToolWrapper{},
	}

	innerResearcher := &SubAgentTool{
		name: "Researcher",
		description: `Searches the web, fetches pages, and produces a structured summary of findings on a given topic. Use for fact-finding, research, and summarization of external information.

 IMPORTANT: Only invoke the Researcher tool if the user explicitly gives a prompt to spin-off a Researcher sub-agent run, such as 'run researcher on [topic]' or 'spin up researcher for [query]'. Do not call this tool proactively or automatically.`,
		instruction: getInstruction("researcher"),
		model:       chatModel,
		tools:       researcherTools,
	}
	researcherTool := &LoggingWrapper{
		tool:    innerResearcher,
		storage: st,
		role:    "researcher",
		timeout: 10 * time.Minute,
	}
	innerCoder := &SubAgentTool{
		name: "Coder",
		description: `Writes, runs, and debugs code to solve programming tasks. Use for code generation, execution, and debugging.

 IMPORTANT: Only invoke the Coder tool if the user explicitly gives a prompt to spin-off a Coder sub-agent run, such as 'run coder on [task]' or similar. Do not call this tool proactively or automatically.`,
		instruction: getInstruction("coder"),
		model:       chatModel,
		tools:       coderTools,
	}
	coderTool := &LoggingWrapper{
		tool:    innerCoder,
		storage: st,
		role:    "coder",
		timeout: 10 * time.Minute,
	}
	innerReviewer := &SubAgentTool{
		name: "Reviewer",
		description: `Critiques, quality-checks, and reviews work. Use for code review, fact-checking, or quality assurance of any output.

 IMPORTANT: Only invoke the Reviewer tool if the user explicitly gives a prompt to spin-off a Reviewer sub-agent run, such as 'run reviewer on [work]' or similar. Do not call this tool proactively or automatically.`,
		instruction: getInstruction("reviewer"),
		model:       chatModel,
		tools:       reviewerTools,
	}
	reviewerTool := &LoggingWrapper{
		tool:    innerReviewer,
		storage: st,
		role:    "reviewer",
		timeout: 10 * time.Minute,
	}

	return []einotool.BaseTool{researcherTool, coderTool, reviewerTool}
}

type LoggingWrapper struct {
	tool    einotool.BaseTool
	storage *storage.Storage
	role    string
	timeout time.Duration
}

type SubAgentTool struct {
	name        string
	description string
	instruction string
	model       model.BaseChatModel
	tools       []einotool.BaseTool
}

func (s *SubAgentTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: s.name,
		Desc: s.description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "The query or goal for the sub-agent",
				Required: true,
			},
		}),
	}, nil
}

func (s *SubAgentTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	toolMap := make(map[string]einotool.InvokableTool)
	for _, t := range s.tools {
		info, err := t.Info(ctx)
		if err == nil {
			toolMap[info.Name] = t.(einotool.InvokableTool)
		}
	}

	messages := []*schema.Message{
		schema.SystemMessage(s.instruction),
		schema.UserMessage(args.Query),
	}

	const maxSteps = 50
	for step := 0; step < maxSteps; step++ {
		assistantResp, err := s.model.Generate(ctx, messages)
		if err != nil {
			return fmt.Sprintf("Generation error at step %d: %v", step, err), err
		}

		content := strings.TrimSpace(assistantResp.Content)
		if content == "" {
			assistantResp.Content = "..."
		}

		messages = append(messages, schema.AssistantMessage(assistantResp.Content, assistantResp.ToolCalls))

		if len(assistantResp.ToolCalls) == 0 {
			return assistantResp.Content, nil
		}

		for _, tc := range assistantResp.ToolCalls {
			tool, ok := toolMap[tc.Function.Name]
			if !ok {
				toolRes := fmt.Sprintf("Unknown tool: %s", tc.Function.Name)
				messages = append(messages, schema.ToolMessage(toolRes, tc.ID))
				continue
			}

			toolRes, err := tool.InvokableRun(ctx, tc.Function.Arguments)
			if err != nil {
				toolRes = fmt.Sprintf("Tool %s error: %v", tc.Function.Name, err)
			}
			messages = append(messages, schema.ToolMessage(toolRes, tc.ID))
		}
	}
	return "Max steps reached without final answer", nil
}

type ToolArgs struct {
	Query string `json:"query"`
}

func (w *LoggingWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return w.tool.Info(ctx)
}

func (w *LoggingWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	const parentSessionKey = "parent_subagent_session"
	var parentSession string
	if ps, ok := ctx.Value(parentSessionKey).(string); ok {
		parentSession = ps
	}
	id := uuid.New().String()
	var args ToolArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid tool arguments JSON: %w", err)
	}
	run := &storage.SubAgentRun{
		ID:            id,
		ParentSession: parentSession,
		Role:          w.role,
		Goal:          args.Query,
	}
	run.Status = "pending"
	run.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := w.storage.SaveSubAgentRun(run); err != nil {
		return "", fmt.Errorf("save pending run: %w", err)
	}
	if err := w.storage.AppendSubAgentTranscript(id, "user", args.Query); err != nil {
		slog.Warn("failed to append user transcript", "run_id", id, "error", err)
	}
	go func(opts ...einotool.Option) {
		execCtx, execCancel := context.WithTimeout(context.Background(), w.timeout)
		defer execCancel()
		invoker, ok := w.tool.(einotool.InvokableTool)
		if !ok {
			finishTime := time.Now().UTC().Format(time.RFC3339)
			run.Status = "failed"
			run.FinishedAt = finishTime
			run.Error = fmt.Sprintf("tool %T does not implement InvokableTool", w.tool)
			run.Output = fmt.Sprintf("The %s sub-agent failed to start: tool not invokable", w.role)
			if trErr := w.storage.AppendSubAgentTranscript(id, "assistant", run.Output); trErr != nil {
				slog.Warn("failed to append assistant transcript", "run_id", id, "error", trErr)
			}
			_ = w.storage.SaveSubAgentRun(run)
			return
		}
		resp, err := invoker.InvokableRun(execCtx, argumentsInJSON, opts...)
		finishTime := time.Now().UTC().Format(time.RFC3339)
		run.FinishedAt = finishTime
		if err != nil {
			run.Status = "failed"
			run.Error = err.Error()
			firstLine := strings.SplitN(err.Error(), "\n", 2)[0]
			summary := fmt.Sprintf("The %s sub-agent failed: %s", w.role, firstLine)
			run.Output = summary
			if trErr := w.storage.AppendSubAgentTranscript(id, "assistant", summary); trErr != nil {
				slog.Warn("failed to append assistant transcript", "run_id", id, "error", trErr)
			}
		} else {
			run.Status = "done"
			run.Output = resp
			if trErr := w.storage.AppendSubAgentTranscript(id, "assistant", resp); trErr != nil {
				slog.Warn("failed to append assistant transcript", "run_id", id, "error", trErr)
			}
		}
		if saveErr := w.storage.SaveSubAgentRun(run); saveErr != nil {
			slog.Error("failed to save final subagent run", "run_id", id, "error", saveErr)
		}
	}()
	return fmt.Sprintf("Spun off %s sub-agent run (ID: %s) asynchronously to handle: %q", w.role, id, args.Query), nil
}
