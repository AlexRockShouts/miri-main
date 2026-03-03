package mole_syn

import (
	"context"
	"miri-main/src/internal/storage"
	"os"
	"sync"
	"testing"
)

// Mock MemorySystem for testing
type mockMS struct {
	mu   sync.RWMutex
	data []SearchResult
}

func (m *mockMS) Add(ctx context.Context, content string, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append(m.data, SearchResult{Content: content, Metadata: metadata})
	return nil
}

func (m *mockMS) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.data {
		if r.Metadata["id"] == id {
			m.data = append(m.data[:i], m.data[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockMS) ListAll(ctx context.Context) ([]SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.data, nil
}

func TestMemoryGraph_AddStep(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mole_syn-test-*")
	defer os.RemoveAll(tmpDir)

	st, _ := storage.New(tmpDir)
	ms := &mockMS{}
	mg := New(nil, st, ms, 0)
	sessionID := "test-session"

	id1, err := mg.AddStep(sessionID, "Step 1", "")
	if err != nil {
		t.Fatalf("AddStep failed: %v", err)
	}
	if id1 == "" {
		t.Fatal("id1 is empty")
	}

	id2, err := mg.AddStep(sessionID, "Step 2", id1)
	if err != nil {
		t.Fatalf("AddStep failed: %v", err)
	}
	if id2 == "" {
		t.Fatal("id2 is empty")
	}

	content, ok := mg.GetNodeContent(id1)
	if !ok {
		t.Error("id1 not found")
	}
	if content != "Step 1" {
		t.Errorf("expected Step 1, got %s", content)
	}

	content, ok = mg.GetNodeContent(id2)
	if !ok {
		t.Error("id2 not found")
	}
	if content != "Step 2" {
		t.Errorf("expected Step 2, got %s", content)
	}
}

func TestMemoryGraph_AddStepsFromAnalysis(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mole_syn-test-*")
	defer os.RemoveAll(tmpDir)

	st, _ := storage.New(tmpDir)
	ms := &mockMS{}
	mg := New(nil, st, ms, 0)
	sessionID := "test-session"

	analysis := &TopologyAnalysis{
		Steps: []struct {
			ID      int    `json:"id"`
			Content string `json:"content"`
		}{
			{ID: 1, Content: "First"},
			{ID: 2, Content: "Second"},
		},
		Bonds: []struct {
			From        int    `json:"from"`
			To          int    `json:"to"`
			Type        string `json:"type"`
			Explanation string `json:"explanation"`
		}{
			{From: 1, To: 2, Type: "Deep"},
		},
	}

	err := mg.AddStepsFromAnalysis(sessionID, analysis)
	if err != nil {
		t.Fatalf("AddStepsFromAnalysis failed: %v", err)
	}

	lastID := mg.lastNode[sessionID]
	if lastID == "" {
		t.Fatal("lastID is empty")
	}

	content, ok := mg.GetNodeContent(lastID)
	if !ok {
		t.Error("lastID not found")
	}
	if content != "Second" {
		t.Errorf("expected Second, got %s", content)
	}
}

func TestMemoryGraph_Persistence(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mole_syn-test-persist-*")
	defer os.RemoveAll(tmpDir)

	st, _ := storage.New(tmpDir)
	ms := &mockMS{}
	mg := New(nil, st, ms, 0)
	sessionID := "test-session"

	id1, _ := mg.AddStep(sessionID, "Step 1", "")
	_, _ = mg.AddStep(sessionID, "Step 2", id1)

	// Create new instance loading from same mockMS
	mg2 := New(nil, st, ms, 0)

	if mg2.lastNode[sessionID] == "" {
		t.Fatal("lastNode not loaded")
	}

	content, ok := mg2.GetNodeContent(mg2.lastNode[sessionID])
	if !ok {
		t.Error("node content not loaded")
	}
	if content != "Step 2" {
		t.Errorf("expected Step 2, got %s", content)
	}

	// Verify mole_syn structure was rebuilt
	preds, _ := mg2.g.PredecessorMap()
	if len(preds[mg2.lastNode[sessionID]]) == 0 {
		t.Error("mole_syn edges not rebuilt")
	}
}

func TestMemoryGraph_GetStrongPath(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mole_syn-strong-path-*")
	defer os.RemoveAll(tmpDir)

	st, _ := storage.New(tmpDir)
	ms := &mockMS{}
	mg := New(nil, st, ms, 0)
	sessionID := "session-1"

	analysis := &TopologyAnalysis{
		Steps: []struct {
			ID      int    `json:"id"`
			Content string `json:"content"`
		}{
			{ID: 1, Content: "Root"},
			{ID: 2, Content: "Deep1"},
			{ID: 3, Content: "Explore1"},
			{ID: 4, Content: "Reflect1"},
			{ID: 5, Content: "Explore2"},
			{ID: 6, Content: "Explore3"},
		},
		Bonds: []struct {
			From        int    `json:"from"`
			To          int    `json:"to"`
			Type        string `json:"type"`
			Explanation string `json:"explanation"`
		}{
			{From: 1, To: 2, Type: "D"},
			{From: 1, To: 3, Type: "E"},
			{From: 2, To: 4, Type: "R"},
			{From: 2, To: 5, Type: "E"},
			{From: 2, To: 6, Type: "E"},
		},
	}

	_ = mg.AddStepsFromAnalysis(sessionID, analysis)

	// Test depth 2
	path2 := mg.GetStrongPath(sessionID, 5)
	var path2Contents []string
	for _, id := range path2 {
		if content, ok := mg.GetNodeContent(id); ok {
			path2Contents = append(path2Contents, content)
		}
	}

	// For session-1, the last node should be Explore3 (id 6).
	// Path should be Root (1) -> Deep1 (2) -> Explore3 (6)
	expected2 := []string{"Root", "Deep1", "Explore3"}
	if len(path2Contents) != len(expected2) {
		t.Errorf("expected path length %d, got %d: %v", len(expected2), len(path2Contents), path2Contents)
	}
	for i, v := range expected2 {
		if i < len(path2Contents) && path2Contents[i] != v {
			t.Errorf("expected at index %d: %s, got %s", i, v, path2Contents[i])
		}
	}

	// Test depth limiting
	pathLimited := mg.GetStrongPath(sessionID, 1)
	if len(pathLimited) != 1 {
		t.Errorf("expected path length 1, got %d", len(pathLimited))
	}
}

func TestMemoryGraph_Pruning(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mole_syn-prune-*")
	defer os.RemoveAll(tmpDir)

	st, _ := storage.New(tmpDir)
	ms := &mockMS{}
	const maxNodes = 3
	mg := New(nil, st, ms, maxNodes)
	sessionID := "prune-session"

	// Add 5 steps — should be capped at maxNodes after each add.
	var ids []string
	prev := ""
	for i := range 5 {
		id, err := mg.AddStep(sessionID, "step", prev)
		if err != nil {
			t.Fatalf("AddStep %d failed: %v", i, err)
		}
		ids = append(ids, id)
		prev = id
	}

	// Count nodes for this session.
	mg.mu.RLock()
	count := 0
	for _, node := range mg.vertexData {
		if s, _ := node.Meta["session"].(string); s == sessionID {
			count++
		}
	}
	mg.mu.RUnlock()

	if count != maxNodes {
		t.Errorf("expected %d nodes after pruning, got %d", maxNodes, count)
	}

	// The last node (most recent) must still be present.
	lastID := ids[len(ids)-1]
	if _, ok := mg.GetNodeContent(lastID); !ok {
		t.Errorf("most recent node %s was pruned but should be kept", lastID)
	}

	// The first two nodes (oldest) must have been pruned.
	for _, id := range ids[:2] {
		if _, ok := mg.GetNodeContent(id); ok {
			t.Errorf("old node %s should have been pruned but still exists", id)
		}
	}
}

func containsAll(mg *MemoryGraph, path []string, items ...string) bool {
	contents := make(map[string]bool)
	for _, id := range path {
		if content, ok := mg.GetNodeContent(id); ok {
			contents[content] = true
		}
	}
	for _, item := range items {
		if !contents[item] {
			return false
		}
	}
	return true
}

func containsAny(mg *MemoryGraph, path []string, items ...string) bool {
	contents := make(map[string]bool)
	for _, id := range path {
		if content, ok := mg.GetNodeContent(id); ok {
			contents[content] = true
		}
	}
	for _, item := range items {
		if contents[item] {
			return true
		}
	}
	return false
}
