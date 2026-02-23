package curlinstall

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("echo hello world"))
	}))
	defer ts.Close()

	rc, err := InstallStream(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("InstallStream failed: %v", err)
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !strings.Contains(string(content), "hello world") {
		t.Errorf("Expected content to contain 'hello world', got %q", string(content))
	}
}
