package tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type FetchToolWrapper struct{}

func (f *FetchToolWrapper) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "web_fetch",
		Desc: "Fetch the content of a web page.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"url": {
				Type:     schema.String,
				Desc:     "The URL to fetch",
				Required: true,
			},
		}),
	}
}

func (f *FetchToolWrapper) Info(_ context.Context) (*schema.ToolInfo, error) {
	return f.GetInfo(), nil
}

func (f *FetchToolWrapper) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		slog.Error("failed to unmarshal fetch arguments", "error", err, "arguments", argumentsInJSON)
		return "", err
	}
	_, _, body, err := fetchURL(ctx, args.URL, 0)
	if err != nil {
		slog.Error("failed to fetch URL", "url", args.URL, "error", err)
		return "", err
	}
	const maxOutput = 8192
	if len(body) > maxOutput {
		body = body[:maxOutput] + "\n... (content truncated)"
	}
	return body, nil
}

func fetchURL(ctx context.Context, urlStr string, maxBytes int) (statusCode int, headers http.Header, body string, err error) {
	if maxBytes == 0 {
		maxBytes = 1024 * 1024 // 1MB default
	}
	dialer := &net.Dialer{Timeout: 60 * time.Second}
	transport := &http.Transport{DialContext: dialer.DialContext}
	client := &http.Client{Transport: transport, Timeout: 180 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return 0, nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	bodyBytes := make([]byte, maxBytes)
	n, readErr := resp.Body.Read(bodyBytes)
	if readErr != nil && readErr != io.EOF {
		return 0, nil, "", readErr
	}
	bodyStr := string(bodyBytes[:n])
	if n == maxBytes {
		bodyStr += " [truncated]"
	}
	return resp.StatusCode, resp.Header.Clone(), bodyStr, nil
}
