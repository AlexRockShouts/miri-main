// static_embedder_qwen3.go
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"miri-main/src/internal/system"
	"os"
	"strings"
	"sync"

	"github.com/vmihailenco/msgpack/v5"
)

// StaticEmbedder provides the chromem.EmbeddingFunc implementation
type StaticEmbedder struct {
	mu         sync.RWMutex
	embeddings map[string][]float32
	dim        int
	unknown    []float32 // zero vector fallback
}

// LoadStaticEmbedderFromBytes loads from embedded or any []byte
func LoadStaticEmbedderFromBytes(data []byte) (*StaticEmbedder, error) {
	var loaded struct {
		Dim        int                  `msgpack:"dim"`
		Embeddings map[string][]float64 `msgpack:"embeddings"`
	}

	if err := msgpack.Unmarshal(data, &loaded); err != nil {
		return nil, fmt.Errorf("msgpack unmarshal failed: %w", err)
	}

	// Convert float64 to float32 to save memory in-RAM
	embeddings32 := make(map[string][]float32, len(loaded.Embeddings))
	for k, v := range loaded.Embeddings {
		v32 := make([]float32, len(v))
		for i, f := range v {
			v32[i] = float32(f)
		}
		embeddings32[k] = v32
	}

	unknown := make([]float32, loaded.Dim)

	slog.Info("loaded static embedder from embedded data", "tokens", len(embeddings32), "dim", loaded.Dim)

	system.LogMemoryUsage("static_embedder_load_bytes")

	return &StaticEmbedder{
		embeddings: embeddings32,
		dim:        loaded.Dim,
		unknown:    unknown,
	}, nil
}

// LoadStaticEmbedderMsgPack loads the msgpack file
func LoadStaticEmbedderMsgPack(path string) (*StaticEmbedder, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var loaded struct {
		Dim        int                  `msgpack:"dim"`
		Embeddings map[string][]float64 `msgpack:"embeddings"`
	}
	if err := msgpack.Unmarshal(data, &loaded); err != nil {
		return nil, fmt.Errorf("msgpack unmarshal: %w", err)
	}

	// Convert float64 to float32 to save memory in-RAM
	embeddings32 := make(map[string][]float32, len(loaded.Embeddings))
	for k, v := range loaded.Embeddings {
		v32 := make([]float32, len(v))
		for i, f := range v {
			v32[i] = float32(f)
		}
		embeddings32[k] = v32
	}

	unknown := make([]float32, loaded.Dim)

	slog.Info("loaded static embedder from file", "path", path, "tokens", len(embeddings32), "dim", loaded.Dim)

	system.LogMemoryUsage("static_embedder_load_file")

	return &StaticEmbedder{
		embeddings: embeddings32,
		dim:        loaded.Dim,
		unknown:    unknown,
	}, nil
}

// Embed implements chromem.EmbeddingFunc exactly as required by v0.7.0
func (e *StaticEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	tokens := fastTokenize(text)
	if len(tokens) == 0 {
		zero := make([]float32, e.dim)
		return zero, nil
	}

	sum := make([]float32, e.dim)
	count := 0

	for _, tok := range tokens {
		vec, ok := e.embeddings[tok]
		if !ok {
			vec = e.unknown
		}
		for j := range sum {
			sum[j] += vec[j]
		}
		count++
	}

	if count > 1 {
		fc := float32(count)
		for j := range sum {
			sum[j] /= fc
		}
	}

	// Optional: L2 normalization (strongly recommended for cosine similarity)
	// chromem-go expects normalized vectors for correct ANN/cosine search
	var norm float32
	for _, v := range sum {
		norm += v * v
	}
	if norm > 0 {
		norm = float32(math.Sqrt(float64(norm)))
		for j := range sum {
			sum[j] /= norm
		}
	}

	return sum, nil
}

func fastTokenize(text string) []string {
	return strings.Fields(strings.ToLower(text))
}
