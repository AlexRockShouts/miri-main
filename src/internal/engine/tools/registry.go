package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type ToolFunc func(context.Context, map[string]any) (any, error)

var (
	dynamicMu    sync.RWMutex
	dynamicTools = make(map[string]ToolFunc)
)

func RegisterDynamicTool(name string, fn ToolFunc) {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	dynamicTools[name] = fn
}

func LoadDynamicTools(dir string) error {
	d := filepath.Join(dir, "tools")
	entries, err := os.ReadDir(d)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(d, e.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		type ToolJSON struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
			Fn          string                 `json:"fn"`
		}
		var tj ToolJSON
		if err := json.Unmarshal(data, &tj); err != nil {
			continue
		}
		// Safety validation (med priority)
		if len(tj.Fn) > 1024 || strings.ContainsAny(tj.Fn, ";|&`><$()[]{}") {
			slog.Warn("skipped unsafe dynamic tool", slog.String("name", tj.Name), slog.String("reason", "invalid fn chars or too long"))
			continue
		}
		fn := func(ctx context.Context, args map[string]any) (any, error) {
			// Stub: template + sandbox exec later
			return fmt.Sprintf("dynamic tool %s executed fn: %s args: %v", tj.Name, tj.Fn, args), nil
		}
		RegisterDynamicTool(tj.Name, fn)
	}
	return nil
}
