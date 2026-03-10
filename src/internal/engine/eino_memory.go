package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"time"

	"miri-main/src/internal/engine/memory"
	"miri-main/src/internal/engine/memory/mole_syn"
	"miri-main/src/internal/llm"
	"miri-main/src/internal/session"
)

// Respond builds a conversation including system prompt, history and current user prompt.
func (e *EinoEngine) Respond(ctx context.Context, sess *session.Session, promptStr string, humanContext string) (string, *llm.Usage, error) {
	slog.Info("EinoEngine Respond", "session_id", sess.ID, "prompt_len", len(promptStr))

	type sessionIDKey struct{}
	ctx = context.WithValue(ctx, sessionIDKey{}, sess.ID)

	// Sanitize prompt to remove potentially sensitive data before adding to buffer
	promptStr = e.sanitizeString(promptStr)

	// Add user prompt to brain buffer as it arrives
	if e.brain != nil {
		e.brain.AddToBuffer(sess.ID, schema.UserMessage(promptStr))
	}

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

	// Load history from brain
	var history []*schema.Message
	if e.brain != nil {
		history = e.brain.GetBuffer(sess.ID)
	}

	// Filter out empty messages just in case, though brain buffer should be clean
	var cleanHistory []*schema.Message
	for _, m := range history {
		if strings.TrimSpace(m.Content) != "" {
			cleanHistory = append(cleanHistory, m)
		}
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

	input := &graphInput{
		SessionID: sess.ID,
		Messages:  cleanHistory,
		Prompt:    promptStr,
		CallOpts:  callOpts,
	}

	subctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	output, err := e.compiledGraph.Invoke(subctx, input, compose.WithCheckPointID(sess.ID))
	if err != nil {
		// Check for persistent 503 error
		if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "Service Unavailable") {
			return "I'm sorry, but the language model is currently overloaded (503 Service Unavailable). Please try again in a moment.", nil, err
		}

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

	// Update brain with context usage to trigger maintenance if needed
	if e.brain != nil {
		e.brain.UpdateContextUsage(ctx, output.Usage.TotalTokens)
	}

	// Clear checkpoint on success
	if e.checkPointStore != nil {
		_ = e.checkPointStore.Delete(ctx, sess.ID)
	}

	finalResp = output.Answer
	return output.Answer, &output.Usage, nil
}

func (e *EinoEngine) StreamRespond(ctx context.Context, sess *session.Session, promptStr string, humanContext string) (<-chan string, error) {
	slog.Info("EinoEngine StreamRespond", "session_id", sess.ID, "prompt_len", len(promptStr))

	// Sanitize prompt to remove potentially sensitive data before adding to buffer
	promptStr = e.sanitizeString(promptStr)

	// Add user prompt to brain buffer as it arrives
	if e.brain != nil {
		e.brain.AddToBuffer(sess.ID, schema.UserMessage(promptStr))
	}

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

		// Load history from brain
		var history []*schema.Message
		if e.brain != nil {
			history = e.brain.GetBuffer(sess.ID)
		}

		// Filter out empty messages
		var cleanHistory []*schema.Message
		for _, m := range history {
			if strings.TrimSpace(m.Content) != "" {
				cleanHistory = append(cleanHistory, m)
			}
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

		verboseCh := make(chan string, 64)
		go func() {
			for ev := range verboseCh {
				out <- ev
			}
		}()
		input := &graphInput{
			SessionID: sess.ID,
			Messages:  cleanHistory,
			Prompt:    promptStr,
			CallOpts:  callOpts,
			VerboseCh: verboseCh,
		}

		subctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		stream, err := e.compiledGraph.Stream(subctx, input, compose.WithCheckPointID(sess.ID))
		if err != nil {
			close(verboseCh)
			callbacks.OnError(ctx, err)
			// Check for persistent 503 error
			if strings.Contains(err.Error(), "503") || strings.Contains(err.Error(), "Service Unavailable") {
				out <- "I'm sorry, but the language model is currently overloaded (503 Service Unavailable). Please try again in a moment."
			} else {
				out <- fmt.Sprintf("[Error: %v]\n", err)
			}
			return
		}

		var lastOutput *graphOutput
		for {
			chunk, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				close(verboseCh)
				callbacks.OnError(ctx, err)
				out <- fmt.Sprintf("[Stream Error: %v]\n", err)
				return
			}
			lastOutput = chunk
		}
		close(verboseCh)
		if lastOutput != nil {
			out <- lastOutput.Answer
			callbacks.OnEnd(ctx, lastOutput.Answer)
			// Clear checkpoint on success
			if e.checkPointStore != nil {
				_ = e.checkPointStore.Delete(ctx, sess.ID)
			}
			// Send usage as a special metadata chunk
			out <- fmt.Sprintf("[Usage: %d prompt, %d completion, %d total tokens, %.6f cost]",
				lastOutput.Usage.PromptTokens,
				lastOutput.Usage.CompletionTokens,
				lastOutput.Usage.TotalTokens,
				lastOutput.Usage.TotalCost)
		}
	}()

	return out, nil
}

func (e *EinoEngine) ClearHistory(sessionID string) {
	if e.brain != nil {
		e.brain.ClearBuffer(sessionID)
	}
}

func (e *EinoEngine) GetHistory(sessionID string) []session.Message {
	if e.brain == nil {
		return nil
	}
	msgs := e.brain.GetBuffer(sessionID)
	res := make([]session.Message, 0, len(msgs)/2)
	for i := 0; i < len(msgs); i++ {
		if msgs[i].Role == schema.User {
			m := session.Message{Prompt: msgs[i].Content}
			if i+1 < len(msgs) && msgs[i+1].Role == schema.Assistant {
				m.Response = msgs[i+1].Content
				i++
			}
			res = append(res, m)
		} else if msgs[i].Role == schema.Assistant {
			res = append(res, session.Message{Response: msgs[i].Content})
		}
	}
	return res
}

func (e *EinoEngine) GetBrainFacts(ctx context.Context) ([]memory.SearchResult, error) {
	if e.brain == nil {
		return nil, nil
	}
	return e.brain.GetFacts(ctx)
}

func (e *EinoEngine) GetBrainSummaries(ctx context.Context) ([]memory.SearchResult, error) {
	if e.brain == nil {
		return nil, nil
	}
	return e.brain.GetSummaries(ctx)
}

func (e *EinoEngine) InjectFact(ctx context.Context, content string, metadata map[string]string) error {
	if e.brain == nil {
		return nil
	}
	return e.brain.StoreFact(ctx, content, metadata)
}

func (e *EinoEngine) GetBrainTopology(ctx context.Context, sessionID string) (*mole_syn.TopologyData, error) {
	if e.brain == nil {
		return nil, nil
	}
	return e.brain.GetTopology(ctx, sessionID)
}

func (e *EinoEngine) Shutdown(ctx context.Context) {
	if e.brain != nil {
		slog.Info("Triggering final brain maintenance before shutdown")
		e.brain.TriggerMaintenance(memory.TriggerShutdown)
	}
}

func (e *EinoEngine) CompactMemory(ctx context.Context, sessionID string) {
	if e.brain != nil {
		slog.Info("Triggering brain maintenance for new session", "session_id", sessionID)
		human, _ := e.storage.GetHuman()
		soul, _ := e.storage.GetSoul()
		_ = e.brain.IngestMetadata(ctx, human, soul)

		// Synchronous maintenance to ensure buffer is processed before clearing
		e.brain.TriggerMaintenance(memory.TriggerNewSession)
	}
}

func (e *EinoEngine) TriggerMaintenance(ctx context.Context) {
	if e.brain != nil {
		slog.Info("Triggering scheduled brain maintenance")
		go func() {
			human, _ := e.storage.GetHuman()
			soul, _ := e.storage.GetSoul()
			_ = e.brain.IngestMetadata(ctx, human, soul)
			e.brain.TriggerMaintenance(memory.TriggerScheduled)
		}()
	}
}

func (e *EinoEngine) Startup(ctx context.Context) {
	if e.brain != nil {
		slog.Info("Running startup brain ingestion and maintenance")
		go func() {
			human, _ := e.storage.GetHuman()
			soul, _ := e.storage.GetSoul()
			_ = e.brain.IngestMetadata(ctx, human, soul)
		}()
	}
}
