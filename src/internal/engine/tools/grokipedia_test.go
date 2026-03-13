package tools

import (
	"context"
	"testing"
	"time"

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
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := &GrokipediaInput{Topic: "Van der Waals force"}
	output, err := FetchGrokipediaArticle(ctx, input)
	assert.NoError(t, err)
	assert.Empty(t, output.Error)
	assert.Equal(t, "Van der Waals force", output.Title)
	assert.NotEmpty(t, output.Content)
	assert.True(t, len(output.Citations) > 0)
}
