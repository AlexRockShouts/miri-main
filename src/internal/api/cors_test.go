package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"miri-main/src/internal/config"
	"miri-main/src/internal/gateway"
)

func TestOptionsAuthorized(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Server.Key = "test-key"
	gw := &gateway.Gateway{Config: cfg}
	s := NewServer(gw)

	// Test OPTIONS /api/v1/prompt
	req, _ := http.NewRequest("OPTIONS", "/api/v1/prompt", nil)
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code == http.StatusNoContent {
		t.Log("Confirmed: OPTIONS request returned 204 No Content")
	} else {
		t.Errorf("Expected 204 No Content for OPTIONS request, got %d", resp.Code)
	}

	// Verify CORS headers
	if resp.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin: *, got %s", resp.Header().Get("Access-Control-Allow-Origin"))
	}

	// Test POST /api/v1/prompt (should still be 401 without key)
	req2, _ := http.NewRequest("POST", "/api/v1/prompt", nil)
	resp2 := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp2, req2)
	if resp2.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for POST request without key, got %d", resp2.Code)
	}

	// Test POST /api/v1/prompt (should be OK with key - though we need a mock gateway to fully test)
	req3, _ := http.NewRequest("POST", "/api/v1/prompt", nil)
	req3.Header.Set("X-Server-Key", "test-key")
	resp3 := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp3, req3)
	// It might fail with 500 or 400 because gateway is not fully mocked, but it shouldn't be 401
	if resp3.Code == http.StatusUnauthorized {
		t.Errorf("Expected NOT 401 Unauthorized for POST request with key, got %d", resp3.Code)
	}

	// Test admin endpoint (should still be 401 without basic auth)
	req4, _ := http.NewRequest("GET", "/api/admin/v1/config", nil)
	resp4 := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp4, req4)
	if resp4.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for admin endpoint without basic auth, got %d", resp4.Code)
	}
}
