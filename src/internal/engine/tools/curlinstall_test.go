package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCurlInstallTruncation(t *testing.T) {
	// Create a large response
	largeOutput := strings.Repeat("A", 5000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("echo " + largeOutput))
	}))
	defer ts.Close()

	wrapper := &CurlInstallToolWrapper{}
	args := map[string]string{"url": ts.URL}
	argsJSON, _ := json.Marshal(args)

	respJSON, err := wrapper.InvokableRun(context.Background(), string(argsJSON))
	if err != nil {
		t.Fatalf("InvokableRun failed: %v", err)
	}

	var res struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal([]byte(respJSON), &res); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(res.Stdout) > 4096+100 { // Allow some buffer for the truncation message
		t.Errorf("Stdout not truncated enough: len=%d", len(res.Stdout))
	}

	if !strings.Contains(res.Stdout, "(stdout truncated)") {
		t.Errorf("Stdout does not contain truncation message")
	}
}
