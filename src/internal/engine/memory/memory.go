package memory

import (
	"context"
)

type SearchResult struct {
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
	Distance float32           `json:"distance"`
}

type Document struct {
	ID       string            `json:"id,omitempty"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
}

type MemorySystem interface {
	// Add adds a new memory entry.
	Add(ctx context.Context, content string, metadata map[string]string) error
	// Search searches for relevant memories based on a query and optional metadata filter.
	Search(ctx context.Context, query string, limit int, filter map[string]string) ([]SearchResult, error)
	// ListAll returns all documents in the collection (use with caution).
	ListAll(ctx context.Context) ([]SearchResult, error)
	// GetByID retrieves a document by ID.
	GetByID(ctx context.Context, id string) (*SearchResult, error)
	// Delete deletes a document by ID.
	Delete(ctx context.Context, id string) error
	// Update updates an existing memory entry.
	Update(ctx context.Context, id string, content string, metadata map[string]string) error
	// Count returns the number of documents in the collection.
	Count(ctx context.Context) (int, error)
	// BulkAdd adds multiple documents.
	BulkAdd(ctx context.Context, docs []Document) error
	// ExportJSON exports all documents as JSON array of Document.
	ExportJSON(ctx context.Context) ([]byte, error)
	// ImportJSON imports documents from JSON array of Document.
	ImportJSON(ctx context.Context, data []byte) error
	// Close closes the memory system.
	Close() error
}
