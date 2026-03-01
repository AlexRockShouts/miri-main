package tasks

import (
	"time"
)

type Task struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CronExpression string    `json:"cron_expression"`
	Prompt         string    `json:"prompt"`
	Active         bool      `json:"active"`
	NeededSkills   []string  `json:"needed_skills,omitempty"`
	LastRun        time.Time `json:"last_run,omitempty"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
	ReportSession  string    `json:"report_session,omitempty"`  // If set, report to this session (e.g., from which it was created)
	ReportChannels []string  `json:"report_channels,omitempty"` // If set, report to these channels (e.g., "whatsapp:device_id", "irc:#channel")
	Silent         bool      `json:"silent,omitempty"`          // If true, do not report results to WS or channels
}
