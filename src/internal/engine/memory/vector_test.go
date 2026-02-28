package memory

import (
	"context"
	"miri-main/src/internal/config"
	"os"
	"testing"
)

func TestVectorMemory_SearchFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "miri-vector-test-*")
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

	vm, err := NewVectorMemory(cfg, "test_collection")
	if err != nil {
		t.Fatalf("Failed to create VectorMemory: %v", err)
	}

	ctx := context.Background()

	_ = vm.Add(ctx, "User likes coffee", map[string]string{"type": "fact", "category": "preference"})
	_ = vm.Add(ctx, "The weather is sunny", map[string]string{"type": "observation"})

	// Search without filter
	results, err := vm.Search(ctx, "coffee", 5, nil)
	if err != nil {
		t.Errorf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("Expected results, got 0")
	}

	// Search with filter
	results, err = vm.Search(ctx, "coffee", 5, map[string]string{"type": "fact"})
	if err != nil {
		t.Errorf("Search with filter failed: %v", err)
	}
	for _, r := range results {
		if r.Metadata["type"] != "fact" {
			t.Errorf("Expected type fact, got %s", r.Metadata["type"])
		}
	}

	// Search with non-matching filter
	results, err = vm.Search(ctx, "coffee", 5, map[string]string{"type": "non-existent"})
	if err != nil {
		t.Errorf("Search with non-matching filter failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results for non-matching filter, got %d", len(results))
	}
}
