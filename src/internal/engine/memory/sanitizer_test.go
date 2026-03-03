package memory

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMemorySanitizer_Transform(t *testing.T) {
	// Simple mock sanitization function that redacts "SECRET"
	mockSanitize := func(s string) string {
		return "REDACTED"
	}

	sanitizer := NewMemorySanitizer(mockSanitize)

	docs := []*schema.Document{
		{
			Content: "This is a SECRET message.",
		},
		{
			Content: "Another message without the word.",
		},
	}

	transformed, err := sanitizer.Transform(context.Background(), docs)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(transformed) != 2 {
		t.Fatalf("Expected 2 documents, got %d", len(transformed))
	}

	for _, doc := range transformed {
		if doc.Content != "REDACTED" {
			t.Errorf("Document content not sanitized: %s", doc.Content)
		}
	}
}

func TestMemorySanitizer_NilFunc(t *testing.T) {
	sanitizer := NewMemorySanitizer(nil)

	docs := []*schema.Document{
		{
			Content: "Not sanitized.",
		},
	}

	transformed, err := sanitizer.Transform(context.Background(), docs)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if transformed[0].Content != "Not sanitized." {
		t.Errorf("Expected unchanged content, got: %s", transformed[0].Content)
	}
}
