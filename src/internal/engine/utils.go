package engine

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func ensureDir(dir string) error { return os.MkdirAll(dir, 0o755) }

func readFileIfExists(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("engine: read file failed", "file", path, "error", err)
		}
		return ""
	}
	return string(b)
}

func appendLine(path, line string) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if _, err := w.WriteString(line); err != nil {
		return err
	}
	if !strings.HasSuffix(line, "\n") {
		if _, err := w.WriteString("\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}

var jsonFenceRe = regexp.MustCompile("(?s)```json\\s*(.*?)\\s*```")

func extractJSONBlocks(s string) []string {
	blocks := jsonFenceRe.FindAllStringSubmatch(s, -1)
	res := make([]string, 0, len(blocks))
	for _, m := range blocks {
		if len(m) > 1 {
			res = append(res, strings.TrimSpace(m[1]))
		}
	}
	return res
}
