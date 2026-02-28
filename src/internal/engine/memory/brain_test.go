package memory

import (
	"context"
	"miri-main/src/internal/config"
	"os"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type mockChat struct {
	response string
	err      error
}

func (m *mockChat) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage(m.response, nil), m.err
}

func (m *mockChat) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func TestBrain_Compact(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "miri-brain-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		StorageDir: tmpDir,
		Miri: config.MiriConfig{
			Brain: config.BrainConfig{
				Embeddings: config.EmbeddingConfig{
					UseNativeEmbeddings: true,
				},
			},
		},
	}

	vm, err := NewVectorMemory(cfg, "test_brain_collection")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Add some facts
	_ = vm.Add(ctx, "Fact 1", map[string]string{"type": "fact", "confidence": "0.9", "access_count": "1", "created_at": time.Now().Format(time.RFC3339)})
	_ = vm.Add(ctx, "Fact 2", map[string]string{"type": "fact", "confidence": "0.4", "access_count": "0", "created_at": time.Now().Format(time.RFC3339)})                           // Should be cleaned up (low confidence)
	_ = vm.Add(ctx, "Fact 3", map[string]string{"type": "fact", "confidence": "0.8", "access_count": "0", "created_at": time.Now().Add(-40 * 24 * time.Hour).Format(time.RFC3339)}) // Should be cleaned up (old, never accessed)

	// Add some summaries
	_ = vm.Add(ctx, "Summary 1", map[string]string{"type": "summary", "access_count": "0", "created_at": time.Now().Format(time.RFC3339)})
	_ = vm.Add(ctx, "Summary 2", map[string]string{"type": "summary", "access_count": "0", "created_at": time.Now().Format(time.RFC3339)})

	chat := &mockChat{
		response: `[]`, // No duplicates found by default
	}

	// Create templates directory for tests before NewBrain
	_ = os.MkdirAll("templates/brain", 0755)
	_ = os.WriteFile("templates/brain/promote_facts.prompt", []byte(`[{"fact": "Fact 1", "category": "general", "confidence": 0.9}]`), 0644)
	defer os.RemoveAll("templates")

	brain := NewBrain(chat, vm, 1000, tmpDir)

	err = brain.Compact(ctx)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}

	// Verify cleanup
	all, _ := vm.ListAll(ctx)
	foundFact1 := 0
	foundFact2 := false
	foundFact3 := false

	for _, item := range all {
		if item.Content == "Fact 1" {
			foundFact1++
		}
		if item.Content == "Fact 2" {
			foundFact2 = true
		}
		if item.Content == "Fact 3" {
			foundFact3 = true
		}
	}

	if foundFact1 != 1 {
		t.Errorf("Fact 1 should be present exactly once, found %d (deduplication failed or it was deleted)", foundFact1)
	}
	if foundFact2 {
		t.Error("Fact 2 should have been cleaned up (low confidence)")
	}
	if foundFact3 {
		t.Error("Fact 3 should have been cleaned up (old, never accessed)")
	}
}

func TestBrain_InteractionThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "miri-brain-test-threshold-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		StorageDir: tmpDir,
		Miri: config.MiriConfig{
			Brain: config.BrainConfig{
				Embeddings: config.EmbeddingConfig{
					UseNativeEmbeddings: true,
				},
			},
		},
	}

	vm, err := NewVectorMemory(cfg, "test_brain_threshold")
	if err != nil {
		t.Fatal(err)
	}

	chat := &mockChat{response: "[]"}
	brain := NewBrain(chat, vm, 1000, tmpDir)

	// Interaction 1 to 99 should not trigger compaction
	for i := 1; i < 100; i++ {
		_, _ = brain.Retrieve(context.Background(), "query")
	}

	brain.mu.Lock()
	count := brain.interactionCount
	brain.mu.Unlock()
	if count != 99 {
		t.Errorf("Expected interaction count 99, got %d", count)
	}

	// Interaction 100 should trigger compaction
	_, _ = brain.Retrieve(context.Background(), "query")

	// Compaction is async, wait a bit or check if interactionCount was reset
	// Since compaction resets count to 0
	time.Sleep(100 * time.Millisecond)

	brain.mu.Lock()
	count = brain.interactionCount
	brain.mu.Unlock()

	if count != 0 {
		t.Errorf("Expected interaction count to be reset to 0 after compaction, got %d", count)
	}
}

func TestBrain_ContextUsageThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "miri-brain-test-context-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		StorageDir: tmpDir,
		Miri: config.MiriConfig{
			Brain: config.BrainConfig{
				Embeddings: config.EmbeddingConfig{
					UseNativeEmbeddings: true,
				},
			},
		},
	}

	vm, err := NewVectorMemory(cfg, "test_brain_context")
	if err != nil {
		t.Fatal(err)
	}

	chat := &mockChat{response: "[]"}
	// Window 1000, 60% = 600
	brain := NewBrain(chat, vm, 1000, tmpDir)

	// Usage 500 should not trigger compaction
	brain.UpdateContextUsage(context.Background(), 500)
	time.Sleep(100 * time.Millisecond)

	brain.mu.Lock()
	brain.interactionCount = 10
	brain.mu.Unlock()

	brain.UpdateContextUsage(context.Background(), 500)
	time.Sleep(100 * time.Millisecond)

	brain.mu.Lock()
	if brain.interactionCount != 10 {
		t.Errorf("Expected interaction count 10, got %d (compaction should not have triggered)", brain.interactionCount)
	}
	brain.mu.Unlock()

	// Usage 600 should trigger compaction
	brain.UpdateContextUsage(context.Background(), 600)
	time.Sleep(200 * time.Millisecond)

	brain.mu.Lock()
	if brain.interactionCount != 0 {
		t.Errorf("Expected interaction count 0, got %d (compaction should have triggered and reset it)", brain.interactionCount)
	}
	brain.mu.Unlock()
}
