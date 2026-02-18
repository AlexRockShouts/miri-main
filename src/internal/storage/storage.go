package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Storage struct {
	baseDir string
	mu      sync.RWMutex
}

type HumanInfo struct {
	ID    string            `json:"id"`
	Data  map[string]string `json:"data"`
	Notes string            `json:"notes"`
}

type CronTxtJob struct {
	Spec   string
	Prompt string
}

func New(baseDir string) (*Storage, error) {
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			return nil, err
		}
	}
	humanDir := filepath.Join(baseDir, "human_info")
	if _, err := os.Stat(humanDir); os.IsNotExist(err) {
		if err := os.MkdirAll(humanDir, 0755); err != nil {
			return nil, err
		}
	}
	return &Storage{baseDir: baseDir}, nil
}

func (s *Storage) SaveHumanInfo(info *HumanInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, "human_info", info.ID+".json")
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *Storage) GetHumanInfo(id string) (*HumanInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "human_info", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var info HumanInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (s *Storage) ListHumanInfo() ([]*HumanInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	humanDir := filepath.Join(s.baseDir, "human_info")
	files, err := os.ReadDir(humanDir)
	if err != nil {
		return nil, err
	}

	var results []*HumanInfo
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".json" {
			info, err := s.GetHumanInfo(f.Name()[:len(f.Name())-5])
			if err == nil {
				results = append(results, info)
			}
		}
	}
	return results, nil
}

func (s *Storage) SaveState(name string, state interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, name+".json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *Storage) LoadState(name string, state interface{}) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, state)
}

func (s *Storage) LoadCronTxt() ([]CronTxtJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "cron.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var jobs []CronTxtJob
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		spec := strings.Join(fields[:6], " ")
		prompt := strings.Join(fields[6:], " ")
		jobs = append(jobs, CronTxtJob{Spec: spec, Prompt: prompt})
	}
	return jobs, nil
}

func (s *Storage) GetSoul() (string, error) {
	path := filepath.Join(s.baseDir, "soul.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s *Storage) AppendToMemory(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, "memory.txt")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(content + "\n")
	return err
}
