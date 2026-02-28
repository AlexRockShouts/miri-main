package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"miri-main/src/internal/config"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCost        float64 `json:"total_cost,omitempty"`
}

type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

func ChatCompletion(cfg *config.Config, modelStr string, messages []Message) (string, *Usage, error) {
	if cfg.Models.Mode != "merge" {
		return "", nil, fmt.Errorf("unsupported models.mode: %s", cfg.Models.Mode)
	}

	parts := strings.SplitN(modelStr, "/", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid model format %q, expected provider/model", modelStr)
	}

	provider, model := parts[0], parts[1]
	prov, ok := cfg.Models.Providers[provider]
	if !ok {
		return "", nil, fmt.Errorf("provider %q not configured", provider)
	}

	if prov.API != "openai-completions" {
		return "", nil, fmt.Errorf("unsupported provider API %q", prov.API)
	}

	url := strings.TrimRight(prov.BaseURL, "/") + "/chat/completions"

	reqBody := ChatCompletionRequest{
		Model:    model,
		Messages: messages,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if prov.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+prov.APIKey)
	}

	client := &http.Client{
		Timeout: 300 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("%s API error: %d - %s", provider, resp.StatusCode, string(body))
	}

	var completionResp ChatCompletionResponse
	if err := json.Unmarshal(body, &completionResp); err != nil {
		return "", nil, err
	}

	if len(completionResp.Choices) == 0 {
		return "", nil, fmt.Errorf("no choices returned from %s API", provider)
	}

	return completionResp.Choices[0].Message.Content, completionResp.Usage, nil
}
