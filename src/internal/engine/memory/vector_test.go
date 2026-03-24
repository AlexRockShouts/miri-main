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

func TestVectorMemory_NewMethods(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "miri-vector-test-new-*")
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

	vm, err := NewVectorMemory(cfg, "test_new")
	if err != nil {
		t.Fatalf("NewVectorMemory: %v", err)
	}
	defer vm.Close()

	ctx := context.Background()

	// Count initial
	count, err := vm.Count(ctx)
	if err != nil {
		t.Fatalf("Count initial: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 got %d", count)
	}

	// Add with ID
	meta1 := map[string]string{"id": "known1", "type": "test"}
	err = vm.Add(ctx, "content1", meta1)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if c, _ := vm.Count(ctx); c != 1 {
		t.Errorf("expected 1 got %d", c)
	}

	// GetByID
	res, err := vm.GetByID(ctx, "known1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if res == nil || res.Content != "content1" || res.Metadata["id"] != "known1" {
		t.Errorf("GetByID mismatch: %+v", res)
	}

	// BulkAdd
	docs := []Document{
		{Content: "bulk1", Metadata: map[string]string{"type": "bulk"}},
		{Content: "bulk2"},
	}
	err = vm.BulkAdd(ctx, docs)
	if err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}
	if c, _ := vm.Count(ctx); c != 3 {
		t.Errorf("expected 3 got %d", c)
	}

	// Export
	data, err := vm.ExportJSON(ctx)
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	// Import to new VM
	vm2, err := NewVectorMemory(cfg, "test_import")
	if err != nil {
		t.Fatalf("NewVectorMemory2: %v", err)
	}
	defer vm2.Close()

	err = vm2.ImportJSON(ctx, data)
	if err != nil {
		t.Fatalf("ImportJSON: %v", err)
	}
	if c2, _ := vm2.Count(ctx); c2 != 3 {
		t.Errorf("expected 3 imported got %d", c2)
	}
}
