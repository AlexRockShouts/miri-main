package subagents

import (
	"context"
	"log/slog"
	"miri-main/src/internal/engine/tools"
	"path/filepath"

	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

import (
	"github.com/google/uuid"
	"miri-main/src/internal/storage"
)

// BuildSubAgentTools creates Researcher, Coder, and Reviewer sub-agents using Eino ADK
// and wraps each as a tool callable by the orchestrator LLM.
func BuildSubAgentTools(ctx context.Context, chatModel model.BaseChatModel, storageDir string, st *storage.Storage) []einotool.BaseTool {
	generatedDir := filepath.Join(storageDir, "generated")

	researcherTools := []einotool.BaseTool{
		&tools.SearchToolWrapper{},
		&tools.FetchToolWrapper{},
		&tools.GrokipediaToolWrapper{},
	}

	coderTools := []einotool.BaseTool{
		tools.NewCmdTool(generatedDir),
		tools.NewFileManagerTool(storageDir, nil),
		&tools.SearchToolWrapper{},
		&tools.FetchToolWrapper{},
	}

	reviewerTools := []einotool.BaseTool{
		&tools.SearchToolWrapper{},
		&tools.FetchToolWrapper{},
	}

	researcher, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "Researcher",
		Description: "Searches the web, fetches pages, and produces a structured summary of findings on a given topic. Use for fact-finding, research, and summarization of external information.",
		Model:       chatModel,
		Instruction: `You are a research specialist. Given a topic or question, search the web, fetch relevant pages, and produce a structured summary of findings. Focus on accuracy and cite your sources.`,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: buildToolsNodeConfig(researcherTools),
		},
	})
	if err != nil {
		slog.Warn("failed to create Researcher sub-agent", "error", err)
		return nil
	}

	coder, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "Coder",
		Description: "Writes, runs, and debugs code to solve programming tasks. Use for code generation, execution, and debugging.",
		Model:       chatModel,
		Instruction: `You are a software engineering specialist. Given a programming task, write clean, tested code and execute it to verify correctness. Use the file manager and cmd tools to write and run code.`,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: buildToolsNodeConfig(coderTools),
		},
	})
	if err != nil {
		slog.Warn("failed to create Coder sub-agent", "error", err)
		return nil
	}

	reviewer, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "Reviewer",
		Description: "Critiques, quality-checks, and reviews work. Use for code review, fact-checking, or quality assurance of any output.",
		Model:       chatModel,
		Instruction: `You are a quality assurance specialist. Given work to review, analyze it thoroughly for correctness, completeness, security, and best practices. Provide structured, actionable feedback.`,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: buildToolsNodeConfig(reviewerTools),
		},
	})
	if err != nil {
		slog.Warn("failed to create Reviewer sub-agent", "error", err)
		return nil
	}

	innerResearcher := adk.NewAgentTool(ctx, researcher)
	researcherTool := &LoggingWrapper{
		tool:    innerResearcher,
		storage: st,
		role:    "researcher",
		timeout: 10 * time.Minute,
	}
	innerCoder := adk.NewAgentTool(ctx, coder)
	coderTool := &LoggingWrapper{
		tool:    innerCoder,
		storage: st,
		role:    "coder",
		timeout: 10 * time.Minute,
	}
	innerReviewer := adk.NewAgentTool(ctx, reviewer)
	reviewerTool := &LoggingWrapper{
		tool:    innerReviewer,
		storage: st,
		role:    "reviewer",
		timeout: 10 * time.Minute,
	}

	return []einotool.BaseTool{researcherTool, coderTool, reviewerTool}
}

func buildToolsNodeConfig(agentTools []einotool.BaseTool) compose.ToolsNodeConfig {
	return compose.ToolsNodeConfig{Tools: agentTools}
}

type LoggingWrapper struct {
	tool    einotool.BaseTool
	storage *storage.Storage
	role    string
	timeout time.Duration
}

func (w *LoggingWrapper) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return w.tool.Info(ctx)
}

func (w *LoggingWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	type sessionIDKey struct{}
	parentSessionI := ctx.Value(sessionIDKey{})
	var parentSession string
	if ps, ok := parentSessionI.(string); ok {
		parentSession = ps
	}
	id := uuid.New().String()
	run := &storage.SubAgentRun{
		ID:            id,
		ParentSession: parentSession,
		Role:          w.role,
		Goal:          argumentsInJSON,
	}
	run.Status = "pending"
	run.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := w.storage.SaveSubAgentRun(run); err != nil {
		return "", fmt.Errorf("save pending run: %w", err)
	}
	if err := w.storage.AppendSubAgentTranscript(id, "user", argumentsInJSON); err != nil {
		slog.Warn("failed to append user transcript", "run_id", id, "error", err)
	}
	subctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	invoker, ok := w.tool.(einotool.InvokableTool)
	if !ok {
		return "", fmt.Errorf("tool %T does not implement InvokableTool", w.tool)
	}
	resp, err := invoker.InvokableRun(subctx, argumentsInJSON, opts...)
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
	return run.Output, nil
}
