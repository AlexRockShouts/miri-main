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
	err = os.WriteFile(st.baseDir+"/memory.md", []byte(template), 0644)
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
