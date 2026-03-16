package system

import (
	"os"
	"path/filepath"
)

// GetProjectRoot returns the absolute path to the project root.
// It checks for MIRI_ROOT environment variable, then looks for go.mod
// by traversing up from the current working directory.
func GetProjectRoot() string {
	if root := os.Getenv("MIRI_ROOT"); root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			return abs
		}
		return root
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return cwd
}
