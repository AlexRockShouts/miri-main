package grokipedia

import (
	"context"
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	ctx := context.Background()
	query := "Go programming language"
	result, err := Search(ctx, query)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if !strings.Contains(result, "Go") {
		t.Errorf("Result should contain 'Go', got: %s", result)
	}

	if !strings.Contains(result, "Grokipedia") {
		t.Errorf("Result should mention 'Grokipedia', got: %s", result)
	}

	t.Logf("Result: %s", result)
}
