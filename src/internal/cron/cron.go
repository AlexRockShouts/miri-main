package cron

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/tasks"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type CronManager struct {
	st       *storage.Storage
	promptFn func(ctx context.Context, sessionID, prompt string, opts engine.Options) (string, error)
	reportFn func(task *tasks.Task, response string)
	c        *cron.Cron
	jobs     map[string]cron.EntryID
	mu       sync.RWMutex
}

func NewCronManager(st *storage.Storage, promptFn func(context.Context, string, string, engine.Options) (string, error), reportFn func(*tasks.Task, string)) *CronManager {
	return &CronManager{
		st:       st,
		promptFn: promptFn,
		reportFn: reportFn,
		c:        cron.New(cron.WithSeconds()),
		jobs:     make(map[string]cron.EntryID),
	}
}

func (m *CronManager) Start() {
	// Load legacy cron.txt jobs
	legacyJobs, err := m.st.LoadCronTxt()
	if err != nil {
		slog.Warn("failed to load cron.txt on startup", "error", err)
	} else {
		for _, job := range legacyJobs {
			id := "legacy-" + uuid.New().String()[:8]
			m.AddLegacyJob(id, job.Spec, job.Prompt)
		}
	}

	// Load dynamic tasks
	dynamicTasks, err := m.st.ListTasks()
	if err != nil {
		slog.Warn("failed to load dynamic tasks on startup", "error", err)
	} else {
		for _, t := range dynamicTasks {
			if t.Active {
				if err := m.AddTask(t); err != nil {
					slog.Error("failed to schedule task on startup", "task_id", t.ID, "error", err)
				}
			}
		}
	}

	go m.c.Start()
}

func (m *CronManager) AddLegacyJob(id, spec, prompt string) {
	sessionID := session.DefaultSessionID
	entryID, err := m.c.AddFunc(spec, func() {
		resp, err := m.promptFn(context.Background(), sessionID, prompt, engine.Options{})
		if err != nil {
			slog.Error("legacy cron prompt failed", "job_id", id, "spec", spec, "error", err)
		} else {
			slog.Info("legacy cron prompt success", "job_id", id, "spec", spec, "response_len", len(resp))
		}
	})
	if err != nil {
		slog.Error("failed to schedule legacy cron job", "job_id", id, "spec", spec, "error", err)
		return
	}
	m.mu.Lock()
	m.jobs[id] = entryID
	m.mu.Unlock()
}

func (m *CronManager) AddTask(t *tasks.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If already exists, remove it first
	if entryID, ok := m.jobs[t.ID]; ok {
		m.c.Remove(entryID)
		delete(m.jobs, t.ID)
	}

	entryID, err := m.c.AddFunc(t.CronExpression, func() {
		m.runTask(t)
	})
	if err != nil {
		return fmt.Errorf("failed to schedule task %s: %w", t.ID, err)
	}

	m.jobs[t.ID] = entryID
	return nil
}

func (m *CronManager) RemoveTask(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entryID, ok := m.jobs[id]; ok {
		m.c.Remove(entryID)
		delete(m.jobs, id)
	}
}

func (m *CronManager) AddFunc(spec string, f func()) (cron.EntryID, error) {
	return m.c.AddFunc(spec, f)
}

func (m *CronManager) runTask(t *tasks.Task) {
	slog.Info("running cron task", "task_id", t.ID, "name", t.Name)

	sessionID := session.DefaultSessionID
	if t.ReportSession != "" {
		sessionID = t.ReportSession
	}

	// We might want to inject needed skills into engine options if the engine supports it
	// For now, EinoEngine loads skills from the skills directory automatically.
	// If the task needs specific skills, they should be installed.

	resp, err := m.promptFn(context.Background(), sessionID, t.Prompt, engine.Options{})
	if err != nil {
		slog.Error("task prompt failed", "task_id", t.ID, "error", err)
		return
	}

	// Update last run
	t.LastRun = time.Now()
	if err := m.st.SaveTask(t); err != nil {
		slog.Warn("failed to update task last_run", "task_id", t.ID, "error", err)
	}

	// Report results
	if m.reportFn != nil {
		m.reportFn(t, resp)
	}
}
