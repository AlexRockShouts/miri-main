package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetProjectRoot(t *testing.T) {
	root := GetProjectRoot()
	if root == "" {
		t.Fatal("GetProjectRoot returned empty string")
	}

	// Verify it contains go.mod
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("GetProjectRoot() = %v; go.mod not found: %v", root, err)
	}

	// Test with MIRI_ROOT env var
	tempDir, err := os.MkdirTemp("", "miri_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	os.Setenv("MIRI_ROOT", tempDir)
	defer os.Unsetenv("MIRI_ROOT")

	root = GetProjectRoot()
	absTemp, _ := filepath.Abs(tempDir)
	if root != absTemp {
		t.Errorf("Expected root %v (from MIRI_ROOT), got %v", absTemp, root)
	}
}
