package storage

import (
	"os"
	"strings"
	"testing"
)

func TestAppendToMemory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "miri-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	st, err := New(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Initial write with template to simulate server bootstrap
	template := `# Memory / CLAUDE.md / Project Memory
Last updated: 2026-02-25

## 8. Memory Log / Decisions Changelog / What We Learned

## 9. Optional: Quick Reference / Cheat Sheet
`
	err = os.WriteFile(st.baseDir+"/soul.md", []byte(template), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Initial append
	err = st.AppendToMemory("Initial fact.")
	if err != nil {
		t.Errorf("Initial append failed: %v", err)
	}

	// 3. Append second fact
	err = st.AppendToMemory("Second fact.")
	if err != nil {
		t.Errorf("Second append failed: %v", err)
	}

	// 4. Read memory
	mem, err := st.ReadMemory()
	if err != nil {
		t.Errorf("ReadMemory failed: %v", err)
	}

	// 5. Verify sections
	if !strings.Contains(mem, "## 8. Memory Log") {
		t.Error("Memory should contain Section 8 header")
	}
	if !strings.Contains(mem, "Initial fact.") {
		t.Error("Memory should contain Initial fact.")
	}
	if !strings.Contains(mem, "Second fact.") {
		t.Error("Memory should contain Second fact.")
	}
	if !strings.Contains(mem, "## 9. Optional: Quick Reference / Cheat Sheet") {
		t.Error("Memory should contain Section 9 header")
	}

	t.Logf("Memory content:\n%s", mem)
}

func TestBootstrapSoul(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "miri-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	st, err := New(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	templatePath := tempDir + "/template_soul.md"
	templateContent := "template soul content"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatal(err)
	}

	// First bootstrap: should succeed and return true
	bootstrapped, err := st.BootstrapSoul(templatePath)
	if err != nil {
		t.Fatalf("First bootstrap failed: %v", err)
	}
	if !bootstrapped {
		t.Error("First bootstrap should have returned true")
	}

	// Verify content
	content, err := st.GetSoul()
	if err != nil {
		t.Fatalf("GetSoul failed: %v", err)
	}
	if content != templateContent {
		t.Errorf("Expected content %q, got %q", templateContent, content)
	}

	// Second bootstrap: should succeed and return false
	bootstrapped, err = st.BootstrapSoul(templatePath)
	if err != nil {
		t.Fatalf("Second bootstrap failed: %v", err)
	}
	if bootstrapped {
		t.Error("Second bootstrap should have returned false")
	}
}

func TestBootstrapHuman(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "miri-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	st, err := New(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	templatePath := tempDir + "/template_human.md"
	templateContent := "template human content"
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatal(err)
	}

	// First bootstrap: should succeed and return true
	bootstrapped, err := st.BootstrapHuman(templatePath)
	if err != nil {
		t.Fatalf("First bootstrap failed: %v", err)
	}
	if !bootstrapped {
		t.Error("First bootstrap should have returned true")
	}

	// Verify content
	content, err := st.GetHuman()
	if err != nil {
		t.Fatalf("GetHuman failed: %v", err)
	}
	if content != templateContent {
		t.Errorf("Expected content %q, got %q", templateContent, content)
	}

	// Second bootstrap: should succeed and return false
	bootstrapped, err = st.BootstrapHuman(templatePath)
	if err != nil {
		t.Fatalf("Second bootstrap failed: %v", err)
	}
	if bootstrapped {
		t.Error("Second bootstrap should have returned false")
	}
}

func TestSaveGetHuman(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "miri-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	st, err := New(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	content := "Updated human content"
	err = st.SaveHuman(content)
	if err != nil {
		t.Fatalf("SaveHuman failed: %v", err)
	}

	got, err := st.GetHuman()
	if err != nil {
		t.Fatalf("GetHuman failed: %v", err)
	}
	if got != content {
		t.Errorf("Expected content %q, got %q", content, got)
	}
}

func TestGetBrainPromptInjection(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "miri-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	st, err := New(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	brainDir := tempDir + "/brain"
	if err := os.MkdirAll(brainDir, 0755); err != nil {
		t.Fatal(err)
	}

	topologyContent := "TOPOLOGY INSTRUCTIONS"
	if err := os.WriteFile(brainDir+"/topology_injection.prompt", []byte(topologyContent), 0644); err != nil {
		t.Fatal(err)
	}

	agentContent := "AGENT INSTRUCTIONS"
	if err := os.WriteFile(brainDir+"/agent.prompt", []byte(agentContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Test case 1: Request agent.prompt, should have topology prepended
	p1, err := st.GetBrainPrompt("agent.prompt")
	if err != nil {
		t.Fatalf("GetBrainPrompt failed: %v", err)
	}
	expected1 := topologyContent + "\n\n" + agentContent
	if p1 != expected1 {
		t.Errorf("Expected %q, got %q", expected1, p1)
	}

	// Test case 2: Request topology_injection.prompt directly, should NOT be doubled
	p2, err := st.GetBrainPrompt("topology_injection.prompt")
	if err != nil {
		t.Fatalf("GetBrainPrompt failed: %v", err)
	}
	if p2 != topologyContent {
		t.Errorf("Expected %q, got %q (self-injection detected)", topologyContent, p2)
	}
}
