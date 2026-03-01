package storage

import (
	"encoding/json"
	"fmt"
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

func (s *Storage) GetBaseDir() string {
	return s.baseDir
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

// BootstrapSoul ensures that soul.md exists in the storage directory,
// copying it from the provided template path if it doesn't.
// Returns true if the file was bootstrapped, false if it already existed.
func (s *Storage) BootstrapSoul(templatePath string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	soulPath := filepath.Join(s.baseDir, "soul.md")
	if _, err := os.Stat(soulPath); err == nil {
		return false, nil // Already exists
	}

	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return false, fmt.Errorf("failed to read template soul.md: %w", err)
	}

	if err := os.WriteFile(soulPath, templateData, 0644); err != nil {
		return false, fmt.Errorf("failed to bootstrap soul.md: %w", err)
	}
	return true, nil
}

// BootstrapHuman ensures that human.md exists in the storage directory,
// copying it from the provided template path if it doesn't.
// Returns true if the file was bootstrapped, false if it already existed.
func (s *Storage) BootstrapHuman(templatePath string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	humanPath := filepath.Join(s.baseDir, "human.md")
	if _, err := os.Stat(humanPath); err == nil {
		return false, nil // Already exists
	}

	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return false, fmt.Errorf("failed to read template human.md: %w", err)
	}

	if err := os.WriteFile(humanPath, templateData, 0644); err != nil {
		return false, fmt.Errorf("failed to bootstrap human.md: %w", err)
	}
	return true, nil
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

func (s *Storage) SaveHuman(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, "human.md")
	return os.WriteFile(path, []byte(content), 0644)
}

func (s *Storage) GetHuman() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "human.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (s *Storage) GetSoul() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.baseDir, "soul.md")
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
	return s.GetSoul()
}

func (s *Storage) AppendToMemory(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.baseDir, "soul.md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString("\n" + text + "\n"); err != nil {
		return err
	}
	return nil
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

func (s *Storage) SyncBrainPrompts(srcDir string) error {
	destDir := filepath.Join(s.baseDir, "brain")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create brain prompts directory: %w", err)
	}

	files, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source prompts: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		srcPath := filepath.Join(srcDir, file.Name())
		destPath := filepath.Join(destDir, file.Name())

		content, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}

		if err := os.WriteFile(destPath, content, 0644); err != nil {
			continue
		}
	}
	return nil
}

func (s *Storage) GetBrainPrompt(name string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	promptsDir := filepath.Join(s.baseDir, "brain")
	promptPath := filepath.Join(promptsDir, name)

	content, err := os.ReadFile(promptPath)
	if err != nil {
		return "", err
	}

	prompt := string(content)

	// Inject topology_injection.prompt if it exists
	injectionPath := filepath.Join(promptsDir, "topology_injection.prompt")
	if injection, err := os.ReadFile(injectionPath); err == nil {
		if name != "topology_injection.prompt" {
			return string(injection) + "\n\n" + prompt, nil
		}
	}

	return prompt, nil
}
