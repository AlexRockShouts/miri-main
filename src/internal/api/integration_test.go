package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"miri-main/src/internal/config"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/tasks"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func setupTestServer(t *testing.T) (*Server, string) {
	tmpDir, err := os.MkdirTemp("", "miri-test-*")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		StorageDir: tmpDir,
		Server: config.ServerConfig{
			Addr:      ":8080",
			Key:       "test-server-key",
			AdminUser: "admin",
			AdminPass: "admin-password",
		},
		Models: config.ModelsConfig{
			Providers: map[string]config.ProviderConfig{
				"mock": {
					BaseURL: "http://localhost:9999",
					APIKey:  "mock-key",
					API:     "openai",
				},
			},
		},
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Model: config.ModelSelection{
					Primary: "mock/test-model",
				},
			},
		},
	}

	st, err := storage.New(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	gw := gateway.New(cfg, st)
	return NewServer(gw), tmpDir
}

func adminAuth(user, pass string) string {
	auth := user + ":" + pass
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}

func TestAPI_Prompt(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	reqBody := promptRequest{Prompt: "hello"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/prompt", bytes.NewReader(body))
	req.Header.Set("X-Server-Key", "test-server-key")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Errorf("POST prompt: expected 500 (LLM fail), got %d. Body: %s", resp.Code, resp.Body.String())
	}
}

func TestAPI_PromptStream(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	req := httptest.NewRequest("GET", "/api/v1/prompt/stream?prompt=hello", nil)
	req.Header.Set("X-Server-Key", "test-server-key")
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK && resp.Code != http.StatusInternalServerError {
		t.Errorf("GET prompt stream: expected 200 or 500, got %d", resp.Code)
	}
}

func TestAPI_AdminHealth(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	req := httptest.NewRequest("GET", "/api/admin/v1/health", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp := httptest.NewRecorder()

	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.Code)
	}
}

func TestAPI_AdminConfig(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	req := httptest.NewRequest("GET", "/api/admin/v1/config", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("GET config: expected 200, got %d", resp.Code)
	}

	newCfg := s.Gateway.Config
	newCfg.Agents.Debug = true
	body, _ := json.Marshal(newCfg)
	req = httptest.NewRequest("POST", "/api/admin/v1/config", bytes.NewReader(body))
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("POST config: expected 200, got %d", resp.Code)
	}
}

func TestAPI_AdminHuman(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	human := map[string]string{
		"content": "# Test Human\nThis is a test human.",
	}
	body, _ := json.Marshal(human)
	req := httptest.NewRequest("POST", "/api/admin/v1/human", bytes.NewReader(body))
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("POST human: expected 200, got %d", resp.Code)
	}

	req = httptest.NewRequest("GET", "/api/admin/v1/human", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("GET human: expected 200, got %d", resp.Code)
	}

	var got map[string]string
	json.Unmarshal(resp.Body.Bytes(), &got)
	if got["content"] != human["content"] {
		t.Errorf("Expected content %q, got %q", human["content"], got["content"])
	}
}

func TestAPI_V1InteractionStatus(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	reqBody := interactionRequest{Action: "status"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/interaction", bytes.NewReader(body))
	req.Header.Set("X-Server-Key", "test-server-key")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Interaction status: expected 200, got %d", resp.Code)
	}
}

func TestAPI_AdminSessions(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	s.Gateway.SessionMgr.GetOrCreate(session.DefaultSessionID)
	s.Gateway.AddTokens(session.DefaultSessionID, 100, 50, 0.0015)

	req := httptest.NewRequest("GET", "/api/admin/v1/sessions", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("List sessions: expected 200, got %d", resp.Code)
	}

	req = httptest.NewRequest("GET", "/api/admin/v1/sessions/"+session.DefaultSessionID, nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Get session: expected 200, got %d", resp.Code)
	}

	req = httptest.NewRequest("GET", "/api/admin/v1/sessions/"+session.DefaultSessionID+"/stats", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Get session stats: expected 200, got %d", resp.Code)
	}

	var stats map[string]any
	json.Unmarshal(resp.Body.Bytes(), &stats)
	if stats["total_tokens"].(float64) != 150 {
		t.Errorf("Expected 150 total tokens, got %v", stats["total_tokens"])
	}
	if stats["total_cost"].(float64) != 0.0015 {
		t.Errorf("Expected 0.0015 total cost, got %v", stats["total_cost"])
	}

	req = httptest.NewRequest("GET", "/api/admin/v1/sessions/"+session.DefaultSessionID+"/history", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Get session history: expected 200, got %d. Body: %s", resp.Code, resp.Body.String())
	}
}

func TestAPI_AdminSkills(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	skillFile := filepath.Join(tmpDir, "skills", "test-skill.md")
	os.MkdirAll(filepath.Join(tmpDir, "skills"), 0755)
	os.WriteFile(skillFile, []byte("---\nname: test-skill\n---\nbody"), 0644)

	req := httptest.NewRequest("GET", "/api/admin/v1/skills", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("List skills: expected 200, got %d", resp.Code)
	}

	req = httptest.NewRequest("GET", "/api/admin/v1/skills/test-skill", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Get skill: expected 200, got %d", resp.Code)
	}

	req = httptest.NewRequest("DELETE", "/api/admin/v1/skills/test-skill", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Delete skill: expected 200, got %d", resp.Code)
	}
}

func TestAPI_AdminTasks(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	task := &tasks.Task{
		ID:             "task123",
		Name:           "Test Task",
		CronExpression: "0 0 * * * *",
		Prompt:         "test prompt",
	}
	s.Gateway.AddTask(task)

	req := httptest.NewRequest("GET", "/api/admin/v1/tasks", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("List tasks: expected 200, got %d", resp.Code)
	}

	req = httptest.NewRequest("GET", "/api/admin/v1/tasks/task123", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Get task: expected 200, got %d", resp.Code)
	}

	// Test task with Silent flag
	silentTask := &tasks.Task{
		ID:             "silent123",
		Name:           "Silent Task",
		CronExpression: "0 0 * * * *",
		Prompt:         "silent prompt",
		Silent:         true,
	}
	s.Gateway.AddTask(silentTask)

	req = httptest.NewRequest("GET", "/api/admin/v1/tasks/silent123", nil)
	req.Header.Set("Authorization", adminAuth("admin", "admin-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("Get silent task: expected 200, got %d", resp.Code)
	}

	var fetchedTask tasks.Task
	json.Unmarshal(resp.Body.Bytes(), &fetchedTask)
	if !fetchedTask.Silent {
		t.Errorf("expected task to be silent")
	}
}

func TestAPI_Files(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	// Files are now accessible within storageDir
	genDir := filepath.Join(tmpDir, "generated")
	os.MkdirAll(genDir, 0755)

	// Test file in a subfolder
	testFile := filepath.Join(genDir, "test.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	// Should be accessible via /api/v1/files/generated/test.txt
	req := httptest.NewRequest("GET", "/api/v1/files/generated/test.txt", nil)
	req.Header.Set("X-Server-Key", "test-server-key")
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("GET file in generated folder: expected 200, got %d", resp.Code)
	}

	// Test file directly in storageDir
	rootFile := filepath.Join(tmpDir, "root.txt")
	os.WriteFile(rootFile, []byte("root content"), 0644)

	req = httptest.NewRequest("GET", "/api/v1/files/root.txt", nil)
	req.Header.Set("X-Server-Key", "test-server-key")
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Errorf("GET file in root storage: expected 200, got %d", resp.Code)
	}

	// Test security: path traversal
	req = httptest.NewRequest("GET", "/api/v1/files/../some_outside_file.txt", nil)
	req.Header.Set("X-Server-Key", "test-server-key")
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)

	if resp.Code == http.StatusOK {
		t.Errorf("GET file outside storage: expected non-200, got %d", resp.Code)
	}
}

func TestAPI_AuthFailures(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	req := httptest.NewRequest("POST", "/api/v1/interaction", bytes.NewReader([]byte("{}")))
	resp := httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for missing server key, got %d", resp.Code)
	}

	req = httptest.NewRequest("GET", "/api/admin/v1/health", nil)
	req.Header.Set("Authorization", adminAuth("admin", "wrong-password"))
	resp = httptest.NewRecorder()
	s.Engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for wrong admin password, got %d", resp.Code)
	}
}

func TestAPI_WebSocket(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(s.Engine)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	q := u.Query()
	q.Set("token", "test-server-key")
	u.RawQuery = q.Encode()

	dialer := websocket.DefaultDialer
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("WebSocket expected 101, got %d", resp.StatusCode)
	}

	// Test task reporting
	done := make(chan bool)
	go func() {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("failed to read from websocket: %v", err)
			done <- false
			return
		}
		if msg["source"] != "task" {
			t.Errorf("expected source task, got %v", msg["source"])
		}
		if msg["response"] != "hello from task" {
			t.Errorf("expected hello from task, got %v", msg["response"])
		}
		if msg["task_name"] != "Test Task" {
			t.Errorf("expected task_name Test Task, got %v", msg["task_name"])
		}
		if msg["task_id"] != "task123" {
			t.Errorf("expected task_id task123, got %v", msg["task_id"])
		}
		if msg["session_id"] != session.DefaultSessionID {
			t.Errorf("expected session_id %s, got %v", session.DefaultSessionID, msg["session_id"])
		}
		done <- true
	}()

	// Simulate task report
	s.handleTaskReport(session.DefaultSessionID, "Test Task", "task123", "hello from task")

	select {
	case success := <-done:
		if !success {
			t.Error("task report test failed")
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for task report via websocket")
	}
}

func TestAPI_TaskDefaultReporting(t *testing.T) {
	s, tmpDir := setupTestServer(t)
	defer os.RemoveAll(tmpDir)

	server := httptest.NewServer(s.Engine)
	defer server.Close()

	u, _ := url.Parse(server.URL)
	u.Scheme = "ws"
	u.Path = "/ws"
	q := u.Query()
	q.Set("token", "test-server-key")
	u.RawQuery = q.Encode()

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	// Test task reporting to default
	done := make(chan bool)
	go func() {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("failed to read from websocket: %v", err)
			done <- false
			return
		}
		if msg["source"] != "task" {
			t.Errorf("expected source task, got %v", msg["source"])
		}
		if msg["session_id"] != session.DefaultSessionID {
			t.Errorf("expected session_id %s, got %v", session.DefaultSessionID, msg["session_id"])
		}
		done <- true
	}()

	// Simulate task report with empty session ID through the Gateway/CronManager path
	// But since we are testing the Server's handleTaskReport which is what Gateway calls,
	// and we updated Gateway to pass session.DefaultSessionID if empty.
	// Wait, Server.handleTaskReport doesn't do the fallback, Gateway does.
	// In the real app: CronManager -> reportFn (in Gateway) -> taskReportHandler (in Server) -> WS broadcast.

	// So I should test it by calling the Gateway's reportFn if I can, or just call handleTaskReport with empty session
	// if I want to test the Server's behavior, but I already moved the logic to Gateway.

	// Let's check Gateway's setTaskReportHandler again.
	// It's in Gateway.New.

	// Actually, I can just call the server's handler which is what's registered.
	// If I pass "" to s.handleTaskReport, it currently doesn't fallback to session.DefaultSessionID.
	// Should I also put the fallback in Server?

	s.handleTaskReport("", "Default Task", "def123", "hello from default task")

	select {
	case success := <-done:
		if !success {
			t.Errorf("WebSocket test failed")
		}
	case <-time.After(2 * time.Second):
		t.Errorf("timeout waiting for websocket message")
	}
}
