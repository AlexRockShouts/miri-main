package cron

import (
	"log/slog"
	"miri-main/src/internal/storage"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type CronManager struct {
	st       *storage.Storage
	promptFn func(string, string) (string, error)
	c        *cron.Cron
}

func NewCronManager(st *storage.Storage, promptFn func(string, string) (string, error)) *CronManager {
	return &CronManager{
		st:       st,
		promptFn: promptFn,
		c:        cron.New(cron.WithSeconds()),
	}
}

func (m *CronManager) Start() {
	jobs, err := m.st.LoadCronTxt()
	if err != nil {
		slog.Warn("failed to load cron.txt on startup", "error", err)
		return
	}
	for _, job := range jobs {
		id := uuid.New().String()[:8]
		sessionID := "cron-" + id
		_, err := m.c.AddFunc(job.Spec, func() {
			resp, err := m.promptFn(sessionID, job.Prompt)
			if err != nil {
				slog.Error("cron prompt failed", "job_id", id, "spec", job.Spec[:50], "error", err)
			} else {
				slog.Info("cron prompt success", "job_id", id, "spec", job.Spec[:50], "response", resp[:100])
			}
		})
		if err != nil {
			slog.Error("failed to schedule cron job", "job_id", id, "spec", job.Spec[:50], "error", err)
		}
	}
	go func() {
		m.c.Start()
	}()
}
