package engine

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/schema"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// FileCheckPointStore implements compose.CheckPointStore by saving data to the filesystem.
type FileCheckPointStore struct {
	baseDir string
	mu      sync.RWMutex
}

func NewFileCheckPointStore(baseDir string) (*FileCheckPointStore, error) {
	dir := filepath.Join(baseDir, "checkpoints")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &FileCheckPointStore{baseDir: dir}, nil
}

func (s *FileCheckPointStore) Get(ctx context.Context, checkPointID string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, checkPointID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	return data, true, nil
}

func (s *FileCheckPointStore) Set(ctx context.Context, checkPointID string, checkPoint []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, checkPointID+".json")
	return os.WriteFile(path, checkPoint, 0644)
}

func (s *FileCheckPointStore) Delete(ctx context.Context, checkPointID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, checkPointID+".json")
	err := os.Remove(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		slog.Warn("failed to delete checkpoint", "path", path, "err", err)
		return err
	}
	return nil
}

// engineState represents the resumable state of the EinoEngine ReAct loop.
type engineState struct {
	Messages []*schema.Message `json:"messages"`
	Step     int               `json:"step"`
}
