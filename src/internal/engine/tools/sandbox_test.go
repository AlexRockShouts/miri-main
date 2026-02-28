package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCmdTool_Sandbox(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "miri-cmd-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	genDir := filepath.Join(tmpDir, ".generated")
	tool := NewCmdTool(genDir)

	ctx := context.Background()

	// 1. Test relative file creation (should go to .generated)
	args := `{"command": "echo 'hello' > relative.txt"}`
	_, err = tool.InvokableRun(ctx, args)
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(genDir, "relative.txt")); err != nil {
		t.Errorf("relative.txt not found in .generated: %v", err)
	}

	// 2. Test file creation in parent (should be moved to .generated)
	// We use absolute path to parent to be sure
	parentDir := tmpDir
	args = `{"command": "echo 'parent' > ` + filepath.Join(parentDir, "outside.txt") + `"}`
	_, err = tool.InvokableRun(ctx, args)
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(parentDir, "outside.txt")); err == nil {
		t.Errorf("outside.txt still found in parent directory, should have been moved")
	}

	if _, err := os.Stat(filepath.Join(genDir, "outside.txt")); err != nil {
		t.Errorf("outside.txt not found in .generated: %v", err)
	}

	// 3. Test whitelist (soul.txt should NOT be moved)
	soulFile := filepath.Join(parentDir, "soul.txt")
	os.WriteFile(soulFile, []byte("soul"), 0644)

	args = `{"command": "ls"}` // Just trigger a run
	tool.InvokableRun(ctx, args)

	if _, err := os.Stat(soulFile); err != nil {
		t.Errorf("soul.txt was moved but it is whitelisted")
	}
}

func TestFileManagerTool_ListDefault(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "miri-fm-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	genDir := filepath.Join(tmpDir, ".generated")
	os.MkdirAll(genDir, 0755)
	os.WriteFile(filepath.Join(genDir, "gen.txt"), []byte("gen"), 0644)

	tool := NewFileManagerTool(tmpDir, nil)

	ctx := context.Background()

	// Test default list (should list .generated)
	args := `{"action": "list"}`
	res, err := tool.InvokableRun(ctx, args)
	if err != nil {
		t.Fatalf("FileManager list failed: %v", err)
	}

	if res != "gen.txt" {
		t.Errorf("Expected gen.txt, got %q", res)
	}
}
