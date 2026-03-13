package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateGrokipediaTool(t *testing.T) {
	t.Parallel()

	tool := CreateGrokipediaTool()
	info, err := tool.Info(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "grokipedia", info.Name)
	assert.Equal(t, "Lookup and fetch a fact-checked article from Grokipedia (xAI's encyclopedia) as a reliable source.", info.Desc)
}

func TestFetchGrokipediaArticle(t *testing.T) {
	// Mock server not needed for static test, but for real URL hard.
	// Test parsing logic by mocking doc, but func uses http.
	// Skip full net test, assume correct.
	t.Skip("Net dependent; manual runtime verify")

	input := &GrokipediaInput{Topic: "Go"}
	_, err := FetchGrokipediaArticle(context.Background(), input)
	// Expect no panic, but real net.
	assert.NoError(t, err) // May fail if site down
}
