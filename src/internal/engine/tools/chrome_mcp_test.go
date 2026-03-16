package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChromeMCPTool_Info(t *testing.T) {
	c := NewChromeMCPTool()
	info := c.GetInfo()
	assert.Equal(t, "chrome_browser", info.Name)
	assert.Contains(t, info.Desc, "Native Google Chrome automation via MCP")

	// Check parameters via JSONSchema conversion
	js, err := info.ParamsOneOf.ToJSONSchema()
	assert.NoError(t, err)
	assert.NotNil(t, js)

	// Verify "action" is in required fields
	found := false
	for _, req := range js.Required {
		if req == "action" {
			found = true
			break
		}
	}
	assert.True(t, found, "action should be a required parameter")
}

func TestChromeMCPTool_InvokableRun(t *testing.T) {
	// Mock Chrome MCP endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/mcp", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var payload map[string]any
		err := json.NewDecoder(r.Body).Decode(&payload)
		assert.NoError(t, err)
		assert.Equal(t, "browser/navigate", payload["method"])

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"status":"ok","url":"https://example.com"},"id":1}`)
	}))
	defer server.Close()

	// Parse port from mock server URL
	port := 9222
	fmt.Sscanf(server.URL, "http://127.0.0.1:%d", &port)

	c := NewChromeMCPTool()
	// Override port for testing
	c.basePort = port

	args := `{"action":"navigate","url":"https://example.com"}`
	res, err := c.InvokableRun(context.Background(), args)

	assert.NoError(t, err)
	assert.Contains(t, res, `"status":"ok"`)
	assert.Contains(t, res, `"url":"https://example.com"`)
}

func TestChromeMCPTool_InvokableRun_Error(t *testing.T) {
	// No server running on this port
	c := NewChromeMCPTool()
	c.basePort = 9999

	args := `{"action":"navigate","url":"https://example.com"}`
	res, err := c.InvokableRun(context.Background(), args)

	assert.NoError(t, err)
	assert.Contains(t, res, `"status":"error"`)
	assert.Contains(t, res, "failed to connect to Chrome MCP")
}
