package mole_syn

import (
	"miri-main/src/internal/storage"
	"os"
	"testing"
)

func TestMemoryGraph_AddStep(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mole_syn-test-*")
	defer os.RemoveAll(tmpDir)

	st, _ := storage.New(tmpDir)
	mg := New(nil, st)
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
	mg := New(nil, st)
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
	mg := New(nil, st)
	sessionID := "test-session"

	id1, _ := mg.AddStep(sessionID, "Step 1", "")
	_, _ = mg.AddStep(sessionID, "Step 2", id1)

	// Create new instance loading from same dir
	mg2 := New(nil, st)

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
