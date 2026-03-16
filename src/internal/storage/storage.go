package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"miri-main/src/internal/tasks"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SubAgentRun is the persisted record of a sub-agent execution.
// Defined here (not in the subagent package) to avoid import cycles.
type SubAgentRun struct {
	ID            string  `json:"id"`
	ParentSession string  `json:"parent_session"`
	Role          string  `json:"role"`
	Goal          string  `json:"goal"`
	Model         string  `json:"model,omitempty"`
	Status        string  `json:"status"`
	Output        string  `json:"output,omitempty"`
	Error         string  `json:"error,omitempty"`
	PromptTokens  uint64  `json:"prompt_tokens"`
	OutputTokens  uint64  `json:"output_tokens"`
	TotalCost     float64 `json:"total_cost"`
	CreatedAt     string  `json:"created_at"`
	StartedAt     string  `json:"started_at,omitempty"`
	FinishedAt    string  `json:"finished_at,omitempty"`
}

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

	type entry struct {
		rel   string
		mode  os.FileMode
		isDir bool
	}
	var entries []entry

	if err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{
			rel:   rel,
			mode:  info.Mode(),
			isDir: info.IsDir(),
		})
		return nil
	}); err != nil {
		return err
	}

	for _, e := range entries {
		destPath := filepath.Join(destDir, e.rel)
		if e.isDir {
			if err := os.MkdirAll(destPath, e.mode); err != nil {
				return err
			}
			continue
		}
		// Don't overwrite if exists
		if _, err := os.Stat(destPath); err == nil {
			continue
		}
		srcPath := filepath.Join(srcDir, e.rel)
		srcFile, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		destFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer destFile.Close()
		if _, err := io.Copy(destFile, srcFile); err != nil {
			return err
		}
		if err := os.Chmod(destPath, e.mode); err != nil {
			return err
		}
	}
	return nil
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
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to delete task", "path", path, "err", err)
	}
	return err
}

func (s *Storage) SyncBrainPrompts(srcDir string) error {
	return s.SyncPrompts(srcDir, "brain")
}

func (s *Storage) SyncSubAgentPrompts(srcDir string) error {
	return s.SyncPrompts(srcDir, "subagents")
}

func (s *Storage) SyncPrompts(srcDir, subDir string) error {
	destDir := filepath.Join(s.baseDir, subDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create prompts directory %s: %w", subDir, err)
	}

	files, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source prompts from %s: %w", srcDir, err)
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

func (s *Storage) subAgentRunsDir() string {
	return filepath.Join(s.baseDir, "subagent_runs")
}

// SaveSubAgentRun persists a sub-agent run record as JSON.
func (s *Storage) SaveSubAgentRun(run *SubAgentRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.subAgentRunsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, run.ID+".json"), data, 0644)
}

// LoadSubAgentRun loads a single run record by ID.
func (s *Storage) LoadSubAgentRun(id string) (*SubAgentRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	safeID := filepath.Base(id)
	if safeID != id {
		return nil, fmt.Errorf("invalid run ID %q", id)
	}
	data, err := os.ReadFile(filepath.Join(s.subAgentRunsDir(), safeID+".json"))
	if err != nil {
		return nil, err
	}
	var run SubAgentRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListSubAgentRuns returns all persisted runs. If parentSession is non-empty,
// only runs belonging to that session are returned.
func (s *Storage) ListSubAgentRuns(parentSession string) ([]*SubAgentRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.subAgentRunsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var runs []*SubAgentRun
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r SubAgentRun
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		if parentSession == "" || r.ParentSession == parentSession {
			runs = append(runs, &r)
		}
	}
	return runs, nil
}

// AppendSubAgentTranscript appends a single JSON-encoded message line to the
// run's transcript file (JSONL format).
func (s *Storage) AppendSubAgentTranscript(runID string, role, content string) error {
	dir := s.subAgentRunsDir()
	safeID := filepath.Base(runID)
	if safeID != runID {
		return fmt.Errorf("invalid run ID %q", runID)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, safeID+".transcript")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	data := map[string]string{
		"role":    role,
		"content": content,
	}
	line, _ := json.Marshal(data)
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

// LoadSubAgentTranscript reads all transcript lines for a run.
func (s *Storage) LoadSubAgentTranscript(runID string) ([]map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	safeRunID := filepath.Base(runID)
	if safeRunID != runID {
		return nil, fmt.Errorf("invalid run ID %q", runID)
	}
	data, err := os.ReadFile(filepath.Join(s.subAgentRunsDir(), safeRunID+".transcript"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var msgs []map[string]string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			continue
		}
		var m map[string]string
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
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
