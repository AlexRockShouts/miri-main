package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ChromeMCPTool leverages Chrome 146+ native MCP support for web automation.
type ChromeMCPTool struct {
	basePort   int
	httpClient *http.Client
}

func NewChromeMCPTool() *ChromeMCPTool {
	return &ChromeMCPTool{
		basePort:   9222, // Default Chrome remote debugging port
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ChromeMCPTool) GetInfo() *schema.ToolInfo {
	return &schema.ToolInfo{
		Name: "chrome_browser",
		Desc: "Native Google Chrome automation via MCP (Model Context Protocol). Supports navigation, snapshots, and interaction. Requires Chrome 146+ with --remote-debugging-port=9222.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "Action to perform: navigate, snapshot, click, type, scroll",
				Required: true,
			},
			"url": {
				Type:     schema.String,
				Desc:     "URL for 'navigate' action",
				Required: false,
			},
			"selector": {
				Type:     schema.String,
				Desc:     "CSS selector or accessibility ID for click/type",
				Required: false,
			},
			"text": {
				Type:     schema.String,
				Desc:     "Text content for 'type' action",
				Required: false,
			},
		}),
	}
}

func (c *ChromeMCPTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return c.GetInfo(), nil
}

func (c *ChromeMCPTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (resStr string, err error) {
	var args struct {
		Action   string `json:"action"`
		URL      string `json:"url"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}

	slog.Info("chrome_mcp executing", "action", args.Action, "url", args.URL)

	// In a real implementation, this would talk to Chrome's MCP endpoint.
	// Chrome 146+ exposes MCP over the debugging port.
	// For this placeholder, we simulate a successful response or call an actual local endpoint if available.

	mcpEndpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", c.basePort)

	// Example payload for Chrome native MCP
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "browser/" + args.Action,
		"params":  args,
		"id":      time.Now().UnixNano(),
	})

	req, err := http.NewRequestWithContext(ctx, "POST", mcpEndpoint, strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Miri-AI-Agent/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Fallback for demo/env where Chrome might not be running with MCP yet
		return fmt.Sprintf(`{"status":"error","message":"failed to connect to Chrome MCP at %s: %v. Ensure Chrome 146+ is running with --remote-debugging-port=%d"}`, mcpEndpoint, err, c.basePort), nil
	}
	defer resp.Body.Close()

	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	b, _ := json.Marshal(result)
	return string(b), nil
}
