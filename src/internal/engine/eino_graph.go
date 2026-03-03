package engine

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/engine/memory"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// buildGraph compiles the Eino chain (retriever → agent → brain) and stores it in e.compiledGraph.
func (e *EinoEngine) buildGraph() error {
	sanitizer := memory.NewMemorySanitizer(e.sanitizeString)

	chain := compose.NewChain[*graphInput, *graphOutput]()

	// 1. Retriever node
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, input *graphInput) (*graphInput, error) {

		// Inject agent prompt + topology injection guidance
		if e.brain != nil {
			prompt, err := e.brain.GetPrompt("agent.prompt")
			if err == nil && prompt != "" {
				if injection, ierr := e.brain.GetPrompt("topology_injection.prompt"); ierr == nil && injection != "" {
					prompt += "\n\n" + injection
				}
				input.Messages = append([]*schema.Message{schema.SystemMessage(prompt)}, input.Messages...)
				if e.debug {
					slog.Info("EinoEngine Debug: Agent prompt injected")
				}
			}
		}

		// Inject retrieved memory
		if e.brain != nil && input.Prompt != "" {
			docs, err := e.brain.RetrieveDocuments(ctx, input.SessionID, input.Prompt)
			if err == nil && len(docs) > 0 {
				// Post-retrieval sanitization
				sanitizedDocs, err := sanitizer.Transform(ctx, docs)
				if err == nil {
					docs = sanitizedDocs
				}

				var sb strings.Builder
				for _, doc := range docs {
					sb.WriteString(doc.Content)
					if !strings.HasSuffix(doc.Content, "\n") {
						sb.WriteString("\n")
					}
				}

				memories := sb.String()
				if memories != "" {
					input.Messages = append(input.Messages, schema.SystemMessage(memories))
					if e.debug {
						slog.Info("EinoEngine Debug: Brain memory injected (sanitized)")
					}
				}
			}
		}

		return input, nil
	}), compose.WithNodeName("retriever"))

	// 2. Agent node
	agentLambda, err := compose.AnyLambda[*graphInput, *graphOutput, any](
		func(ctx context.Context, input *graphInput, opts ...any) (*graphOutput, error) {
			return e.agentInvoke(ctx, input)
		},
		func(ctx context.Context, input *graphInput, opts ...any) (*schema.StreamReader[*graphOutput], error) {
			return e.agentStream(ctx, input)
		},
		nil, nil,
	)
	if err != nil {
		return err
	}
	chain.AppendLambda(agentLambda, compose.WithNodeName("agent"))

	// 3. Brain node (Post-processing)
	chain.AppendLambda(compose.InvokableLambda(func(ctx context.Context, output *graphOutput) (*graphOutput, error) {
		if e.brain == nil {
			return output, nil
		}

		// Add assistant response to brain buffer
		if output.LastMessage != nil {
			e.brain.AddToBuffer(output.SessionID, output.LastMessage)
		} else {
			e.brain.AddToBuffer(output.SessionID, schema.AssistantMessage(output.Answer, nil))
		}

		// Trigger maintenance if context usage is high
		if output.Usage.TotalTokens > 0 {
			e.brain.UpdateContextUsage(ctx, output.Usage.TotalTokens)
		}

		return output, nil
	}), compose.WithNodeName("brain"))

	compiled, err := chain.Compile(context.Background(),
		compose.WithCheckPointStore(e.checkPointStore),
		compose.WithMaxRunSteps(e.maxSteps+5),
	)
	if err != nil {
		return fmt.Errorf("failed to compile graph: %w", err)
	}
	e.compiledGraph = compiled
	return nil
}
