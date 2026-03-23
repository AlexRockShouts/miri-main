package memory

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"math"
	"miri-main/src/internal/config"
	"miri-main/src/internal/system"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/philippgille/chromem-go"
)

type VectorMemory struct {
	db         *chromem.DB
	collection *chromem.Collection
	mu         sync.Mutex
}

//go:embed static_qwen3_embedding_0.6b_pca384.msgpack
var embeddedEmbeddings []byte

func NewVectorMemory(cfg *config.Config, collectionName string) (*VectorMemory, error) {
	storageDir := cfg.StorageDir
	dbPath := filepath.Join(storageDir, "vector_db")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create vector db directory: %w", err)
	}
	// Initialize chromem-go DB with persistence
	// Use NewPersistentDB for automatic loading/saving
	db, err := chromem.NewPersistentDB(dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create persistent db: %w", err)
	}
	slog.Info("initialized vector database", "path", dbPath)

	var embedFunc chromem.EmbeddingFunc
	if cfg.Miri.Brain.Embeddings.UseNativeEmbeddings {
		embedder, err := LoadStaticEmbedderFromBytes(embeddedEmbeddings)
		if err != nil {
			slog.Warn("failed to load static embedder, using zero-vector fallback", "error", err)
			embedFunc = func(ctx context.Context, text string) ([]float32, error) {
				return make([]float32, 384), nil
			}
		} else {
			embedFunc = embedder.Embed
		}
	} else {
		// Use external embedding API
		embType := cfg.Miri.Brain.Embeddings.Model.Type
		apiKey := cfg.Miri.Brain.Embeddings.Model.APIKey

		switch strings.ToLower(embType) {
		case "openai":
			// Default OpenAI model
			embedFunc = chromem.NewEmbeddingFuncOpenAI(apiKey, chromem.EmbeddingModelOpenAI3Small)
		case "mistral":
			embedFunc = chromem.NewEmbeddingFuncMistral(apiKey)
		case "cohere":
			embedFunc = chromem.NewEmbeddingFuncCohere(apiKey, chromem.EmbeddingModelCohereEnglishV3)
		case "ollama":
			// Default Ollama model and localhost URL
			embedFunc = chromem.NewEmbeddingFuncOllama("nomic-embed-text", "")
		case "jina":
			embedFunc = chromem.NewEmbeddingFuncJina(apiKey, chromem.EmbeddingModelJina2BaseEN)
		case "mixedbread":
			embedFunc = chromem.NewEmbeddingFuncMixedbread(apiKey, chromem.EmbeddingModelMixedbreadLargeV1)
		case "localai":
			embedFunc = chromem.NewEmbeddingFuncLocalAI("bert-cpp-minilm-v6")
		case "openai-compatible":
			url := cfg.Miri.Brain.Embeddings.Model.URL
			model := cfg.Miri.Brain.Embeddings.Model.Model
			embedFunc = chromem.NewEmbeddingFuncOpenAICompat(apiKey, url, model, nil)
		default:
			// Fallback to OpenAI
			embedFunc = chromem.NewEmbeddingFuncOpenAI(apiKey, chromem.EmbeddingModelOpenAI3Small)
		}
	}

	col, err := db.GetOrCreateCollection(collectionName, nil, embedFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create collection: %w", err)
	}
	slog.Info("using vector collection", "name", collectionName, "count", col.Count())

	// Test if data retrievable (detect embedder incompatibility/mismatch)
	ctx := context.Background()
	testCount := col.Count()
	testResults, testErr := col.Query(ctx, " ", testCount, nil, nil)
	if testErr == nil && len(testResults) == 0 && testCount > 0 {
		slog.Warn("Test query returned no results despite count > 0; possible embedding incompatibility. Clearing collection.", "collection", collectionName, "count", testCount)
		if clearErr := col.Delete(ctx, nil, nil); clearErr != nil {
			slog.Error("Failed to clear incompatible collection", "collection", collectionName, "error", clearErr)
		} else {
			slog.Info("Cleared incompatible collection for compatibility", "collection", collectionName)
		}
	} else if testErr != nil {
		slog.Warn("Test query failed", "collection", collectionName, "error", testErr)
	}

	system.LogMemoryUsage("vector_memory_init")

	return &VectorMemory{
		db:         db,
		collection: col,
	}, nil
}

func (v *VectorMemory) Add(ctx context.Context, content string, metadata map[string]string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	id := uuid.New().String()
	// If ID is provided in metadata, use it
	if providedID, ok := metadata["id"]; ok {
		id = providedID
		delete(metadata, "id")
	}

	doc := chromem.Document{
		ID:       id,
		Content:  content,
		Metadata: metadata,
	}

	err := v.collection.AddDocument(ctx, doc)
	if err != nil {
		slog.Error("failed to add document to vector memory", "error", err)
		return err
	}
	slog.Debug("added document to vector memory", "id", doc.ID, "content_len", len(content))
	return nil
}

func (v *VectorMemory) Search(ctx context.Context, query string, limit int, filter map[string]string) ([]SearchResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	slog.Debug("searching vector memory", "query", query, "limit", limit, "filter", filter)

	// chromem-go fails if limit > collection count
	count := v.collection.Count()
	if count == 0 {
		return nil, nil
	}
	if limit > count {
		limit = count
	}

	results, err := v.collection.Query(ctx, query, limit, filter, nil)
	if err != nil {
		slog.Error("vector memory search failed", "error", err)
		return nil, err
	}

	var searchResults []SearchResult
	for _, r := range results {
		// Include ID in metadata for deletion/update
		meta := r.Metadata
		if meta == nil {
			meta = make(map[string]string)
		}
		meta["id"] = r.ID

		// chromem-go returns cosine similarity:
		// 1.0 is identical, 0.0 is orthogonal, -1.0 is opposite.
		// Our "Distance" conceptually should be (1.0 - Similarity), where 0.0 is identical.
		distance := 1.0 - r.Similarity
		if math.IsNaN(float64(distance)) || math.IsInf(float64(distance), 0) {
			distance = 1.0 // Default to max distance if invalid
		}

		searchResults = append(searchResults, SearchResult{
			Content:  r.Content,
			Metadata: meta,
			Distance: distance,
		})
	}
	if len(searchResults) > 0 {
		slog.Debug("vector memory search complete", "results", len(searchResults), "top_similarity", searchResults[0].Distance)
	} else {
		slog.Debug("vector memory search returned no results")
	}
	return searchResults, nil
}

func (v *VectorMemory) ListAll(ctx context.Context) ([]SearchResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// chromem-go doesn't have a direct ListAll, but we can query with a dummy string or use the underlying store.
	// Querying with a very large limit and a wildcard-like behavior.
	// If query is empty, chromem-go currently fails.
	count := v.collection.Count()
	if count == 0 {
		return nil, nil
	}
	results, err := v.collection.Query(ctx, " ", count, nil, nil)
	if err != nil {
		return nil, err
	}

	var searchResults []SearchResult
	for _, r := range results {
		// Include ID in metadata for deletion/update
		meta := r.Metadata
		if meta == nil {
			meta = make(map[string]string)
		}
		meta["id"] = r.ID

		distance := 1.0 - r.Similarity
		if math.IsNaN(float64(distance)) || math.IsInf(float64(distance), 0) {
			distance = 1.0
		}
		res := SearchResult{
			Content:  r.Content,
			Metadata: meta,
			Distance: distance,
		}
		searchResults = append(searchResults, res)
	}
	return searchResults, nil
}

func (v *VectorMemory) GetByID(ctx context.Context, id string) (*SearchResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// chromem-go doesn't have a GetByID by default, but we can use Query with ID filtering if supported
	// or ListAll and filter manually.
	// For now, we'll list all and filter, or we can use the internal collection store if available.
	// Actually, chromem-go doesn't have a direct "GetByID" API exposed on the collection easily.
	// Let's list all and find it (slow, but fine for small collections like our current state).
	all, err := v.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range all {
		if r.Metadata["id"] == id {
			return &r, nil
		}
	}
	return nil, nil
}

func (v *VectorMemory) Delete(ctx context.Context, id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.collection.Delete(ctx, nil, nil, id)
}

func (v *VectorMemory) Update(ctx context.Context, id string, content string, metadata map[string]string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	// If ID is in metadata, remove it so it's not stored twice (once as ID, once in metadata)
	// but chromem-go actually stores both if we don't.
	// Actually, let's just make sure we use the provided id.
	// We'll clone metadata to avoid side effects if needed, but for now we'll just delete if present.
	delete(metadata, "id")

	// In chromem-go, AddDocument with the same ID will replace the existing one
	doc := chromem.Document{
		ID:       id,
		Content:  content,
		Metadata: metadata,
	}

	err := v.collection.AddDocument(ctx, doc)
	if err != nil {
		slog.Error("failed to update document in vector memory", "id", id, "error", err)
		return err
	}
	slog.Debug("updated document in vector memory", "id", doc.ID)
	return nil
}

func (v *VectorMemory) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	// Persistent DB handles its own state
	return nil
}
