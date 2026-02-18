package webfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchSmallBody(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "value")
		w.WriteHeader(200)
		fmt.Fprint(w, "small body")
	}))
	defer ts.Close()

	ctx := context.Background()
	status, headers, body, err := Fetch(ctx, ts.URL, 100)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if headers.Get("X-Test") != "value" {
		t.Errorf("X-Test = %q, want value", headers.Get("X-Test"))
	}
	if strings.Contains(body, " [truncated]") {
		t.Error("body truncated unexpectedly")
	}
	if body != "small body" {
		t.Errorf("body = %q, want 'small body'", body)
	}
}

func TestFetchLargeTruncated(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		large := strings.Repeat("x", 100)
		io.WriteString(w, large)
	}))
	defer ts.Close()

	status, _, body, err := Fetch(context.Background(), ts.URL, 10)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if !strings.HasSuffix(body, " [truncated]") {
		t.Errorf("body = %q, does not end with [truncated]", body)
	}
	if len(body) != 22 { // 10 chars + " [truncated]" len12 =22
		t.Errorf("len(body) = %d, want 22", len(body))
	}
}

func TestFetch404(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, "not found")
	}))
	defer ts.Close()

	status, _, body, err := Fetch(context.Background(), ts.URL, 100)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
	if body != "not found" {
		t.Errorf("body = %q, want 'not found'", body)
	}
}

func TestFetchInvalidURL(t *testing.T) {
	t.Parallel()

	_, _, _, err := Fetch(context.Background(), "invalid://url", 0)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestFetchMaxBytes0(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, strings.Repeat("a", 2*1024*1024))
	}))
	defer ts.Close()

	status, _, body, err := Fetch(context.Background(), ts.URL, 0)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if len(body) != 1048576 && !strings.HasSuffix(body, " [truncated]") {
		t.Logf("body len %d, suffix %q", len(body), body[len(body)-12:])
		t.Error("expected 1MB truncated")
	}
}

func TestFetchTimeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(200)
	}))
	defer ts.Close()

	_, _, _, err := Fetch(ctx, ts.URL, 1024)
	if err == nil {
		t.Error("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("unexpected error %v", err)
	}
}
