package memory

import (
	"context"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
)

// SanitizeFunc is a function that sanitizes a string.
type SanitizeFunc func(string) string

// MemorySanitizer implements the document.Transformer interface to clean retrieved documents.
type MemorySanitizer struct {
	sanitize SanitizeFunc
}

// NewMemorySanitizer creates a new MemorySanitizer with the given sanitization function.
func NewMemorySanitizer(fn SanitizeFunc) *MemorySanitizer {
	return &MemorySanitizer{sanitize: fn}
}

// Transform applies the sanitization function to each document's content.
func (s *MemorySanitizer) Transform(ctx context.Context, src []*schema.Document, opts ...document.TransformerOption) ([]*schema.Document, error) {
	if s.sanitize == nil {
		return src, nil
	}

	for _, doc := range src {
		doc.Content = s.sanitize(doc.Content)
	}

	return src, nil
}
