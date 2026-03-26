package memory

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"miri-main/src/internal/config"
	"miri-main/src/internal/system"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/philippgille/chromem-go"
)

type VectorMemory struct {
	db         *chromem.DB
	collection *chromem.Collection
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
	id := uuid.New().String()
	// If ID is provided in metadata, use it
	if providedID, ok := metadata["id"]; ok {
		id = providedID
	}
	metadata["id"] = id

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
		meta := maps.Clone(r.Metadata)
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
		meta := maps.Clone(r.Metadata)
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
	filter := map[string]string{"id": id}
	results, err := v.Search(ctx, " ", 1, filter)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

func (v *VectorMemory) Delete(ctx context.Context, id string) error {
	return v.collection.Delete(ctx, nil, nil, id)
}

func (v *VectorMemory) Update(ctx context.Context, id string, content string, metadata map[string]string) error {
	metadata["id"] = id

	// If no content provided, fetch the existing document to preserve its content
	// (chromem-go requires either content or embedding to be set).
	if content == "" {
		existing, err := v.GetByID(ctx, id)
		if err != nil {
			slog.Error("failed to fetch existing document for metadata-only update", "id", id, "error", err)
			return err
		}
		if existing != nil {
			content = existing.Content
		}
	}

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

func (v *VectorMemory) Count(ctx context.Context) (int, error) {
	return v.collection.Count(), nil
}

func (v *VectorMemory) BulkAdd(ctx context.Context, docs []Document) error {
	for _, doc := range docs {
		id := doc.ID
		if id == "" {
			id = uuid.New().String()
		}
		cdoc := chromem.Document{
			ID:       id,
			Content:  doc.Content,
			Metadata: doc.Metadata,
		}
		if err := v.collection.AddDocument(ctx, cdoc); err != nil {
			slog.Error("failed to add document in bulk", "id", id, "error", err)
			return err
		}
	}
	return nil
}

func (v *VectorMemory) ExportJSON(ctx context.Context) ([]byte, error) {
	results, err := v.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	docs := make([]Document, 0, len(results))
	for _, r := range results {
		meta := maps.Clone(r.Metadata)
		docs = append(docs, Document{
			ID:       meta["id"],
			Content:  r.Content,
			Metadata: meta,
		})
	}
	return json.Marshal(docs)
}

func (v *VectorMemory) ImportJSON(ctx context.Context, data []byte) error {
	var docs []Document
	if err := json.Unmarshal(data, &docs); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}
	return v.BulkAdd(ctx, docs)
}

func (v *VectorMemory) Close() error {
	// Persistent DB handles its own state
	return nil
}
