package storage

import (
	"encoding/json"
	"io"
	"miri-main/src/internal/tasks"
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
	skillsDir := filepath.Join(baseDir, "skills")
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		if err := os.MkdirAll(skillsDir, 0755); err != nil {
			return nil, err
		}
	}
	genDir := filepath.Join(baseDir, "generated")
	if _, err := os.Stat(genDir); os.IsNotExist(err) {
		if err := os.MkdirAll(genDir, 0755); err != nil {
			return nil, err
		}
	}
	return &Storage{baseDir: baseDir}, nil
}

func (s *Storage) CopySkills(srcDir string) error {
	destDir := filepath.Join(s.baseDir, "skills")
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return nil // Source doesn't exist, nothing to copy
	}

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		destPath := filepath.Join(destDir, relPath)
		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		// Don't overwrite if exists
		if _, err := os.Stat(destPath); err == nil {
			return nil
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		destFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, srcFile)
		if err != nil {
			return err
		}
		return os.Chmod(destPath, info.Mode())
	})
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

func (s *Storage) ReadMemory() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "memory.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (s *Storage) AppendToMemory(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, "memory.md")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if os.IsNotExist(err) || len(data) == 0 {
		return os.WriteFile(path, []byte(content+"\n"), 0644)
	}

	// If file exists, try to insert into section 8
	strData := string(data)
	section8Header := "## 8. Memory Log / Decisions Changelog / What We Learned"
	section9Header := "## 9. Optional: Quick Reference / Cheat Sheet"

	if strings.Contains(strData, section8Header) && strings.Contains(strData, section9Header) {
		parts := strings.Split(strData, section9Header)
		if len(parts) >= 2 {
			newContent := parts[0] + content + "\n\n" + section9Header + parts[1]
			return os.WriteFile(path, []byte(newContent), 0644)
		}
	}

	// Fallback to append if headers not found
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(content + "\n")
	return err
}

func (s *Storage) SaveTask(t *tasks.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	taskDir := filepath.Join(s.baseDir, "tasks")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		return err
	}

	path := filepath.Join(taskDir, t.ID+".json")
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *Storage) LoadTask(id string) (*tasks.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "tasks", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t tasks.Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Storage) ListTasks() ([]*tasks.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	taskDir := filepath.Join(s.baseDir, "tasks")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var res []*tasks.Task
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			path := filepath.Join(taskDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var t tasks.Task
			if err := json.Unmarshal(data, &t); err == nil {
				res = append(res, &t)
			}
		}
	}
	return res, nil
}

func (s *Storage) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, "tasks", id+".json")
	return os.Remove(path)
}
