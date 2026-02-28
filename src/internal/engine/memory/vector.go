package memory

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
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
			slog.Error("failed to load static embedder", "error", err)
			os.Exit(1)
		}
		embedFunc = embedder.Embed
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
		searchResults = append(searchResults, SearchResult{
			Content:  r.Content,
			Metadata: r.Metadata,
			Distance: r.Similarity,
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
		res := SearchResult{
			Content:  r.Content,
			Metadata: r.Metadata,
			Distance: r.Similarity,
		}
		// Include ID in metadata for deletion/update
		if res.Metadata == nil {
			res.Metadata = make(map[string]string)
		}
		res.Metadata["id"] = r.ID
		searchResults = append(searchResults, res)
	}
	return searchResults, nil
}

func (v *VectorMemory) Delete(ctx context.Context, id string) error {
	return v.collection.Delete(ctx, nil, nil, id)
}

func (v *VectorMemory) Close() error {
	// Persistent DB handles its own state
	return nil
}
