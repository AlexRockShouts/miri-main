package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/agent"
	"miri-main/src/internal/channels"
	"miri-main/src/internal/config"
	"miri-main/src/internal/cron"
	"miri-main/src/internal/engine"
	"miri-main/src/internal/session"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/tasks"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type Gateway struct {
	Config       *config.Config
	Storage      *storage.Storage
	SessionMgr   *session.SessionManager
	PrimaryAgent *agent.Agent
	SubAgents    []*agent.Agent
	Channels     map[string]channels.Channel
	cronMgr      *cron.CronManager
	engine       *engine.Loop

	taskReportHandler func(sessionID, taskName, taskID, message string)
	reportMu          sync.RWMutex
}

func New(cfg *config.Config, st *storage.Storage) *Gateway {
	gw := &Gateway{
		Config:     cfg,
		Storage:    st,
		SessionMgr: session.NewSessionManager(),
		Channels:   make(map[string]channels.Channel),
	}
	gw.PrimaryAgent = agent.NewAgent(cfg, gw.SessionMgr, gw.Storage)

	numSub := gw.Config.Agents.SubAgents
	gw.SubAgents = make([]*agent.Agent, numSub)
	for i := range gw.SubAgents {
		gw.SubAgents[i] = agent.NewAgent(gw.Config, gw.SessionMgr, gw.Storage)
	}
	for i := range gw.SubAgents {
		gw.SubAgents[i].Parent = gw.PrimaryAgent
	}

	gw.cronMgr = cron.NewCronManager(gw.Storage, func(ctx context.Context, sessionID, prompt string, opts engine.Options) (string, error) {
		return gw.PrimaryAgent.DelegatePromptWithOptions(ctx, sessionID, prompt, opts)
	}, func(t *tasks.Task, response string) {
		if t.Silent {
			slog.Info("task execution silent, skipping reports", "task_id", t.ID, "task_name", t.Name)
			return
		}

		// Report to session (WS chat clients)
		reportSession := t.ReportSession
		if reportSession == "" {
			reportSession = session.DefaultSessionID
		}

		gw.reportMu.RLock()
		handler := gw.taskReportHandler
		gw.reportMu.RUnlock()

		if handler != nil {
			handler(reportSession, t.Name, t.ID, response)
		} else {
			slog.Info("no task report handler set for session", "task_id", t.ID, "session_id", reportSession)
		}

		// Report to active channels
		for _, target := range t.ReportChannels {
			parts := strings.SplitN(target, ":", 2)
			if len(parts) != 2 {
				continue
			}
			channel, device := parts[0], parts[1]
			if err := gw.ChannelSend(channel, device, response); err != nil {
				slog.Error("failed to report task result to channel", "task_id", t.ID, "channel", channel, "device", device, "error", err)
			}
		}
	})
	gw.cronMgr.Start()

	// Add scheduled maintenance: every 12 hours (0:00 and 12:00)
	if _, err := gw.cronMgr.AddFunc("0 0 0,12 * * *", func() {
		slog.Info("Starting scheduled brain maintenance")
		gw.PrimaryAgent.TriggerMaintenance(context.Background())
	}); err != nil {
		slog.Error("Failed to schedule brain maintenance", "error", err)
	}

	gw.PrimaryAgent.SetTaskGateway(gw)
	for _, sub := range gw.SubAgents {
		sub.SetTaskGateway(gw)
	}

	if gw.Config.Channels.Whatsapp.Enabled {
		ch := channels.NewWhatsapp(gw.Config.StorageDir, gw.Config.Channels.Whatsapp.Allowlist, gw.Config.Channels.Whatsapp.Blocklist)
		if ch != nil {
			gw.Channels["whatsapp"] = ch
			slog.Info("whatsapp channel initialized")
		} else {
			slog.Warn("failed to initialize whatsapp channel")
		}
	}

	if gw.Config.Channels.IRC.Enabled {
		ch := channels.NewIRC(gw.Config.Channels.IRC)
		if ch != nil {
			gw.Channels["irc"] = ch
			slog.Info("irc channel initialized")
		} else {
			slog.Warn("failed to initialize irc channel")
		}
	}

	gw.engine = engine.New()

	if w, ok := gw.Channels["whatsapp"].(*channels.Whatsapp); ok {
		w.SetMessageHandler(func(device, msg string) {
			sessionID := session.DefaultSessionID
			resp, err := gw.PrimaryAgent.DelegatePrompt(sessionID, msg)
			if err != nil {
				slog.Error("failed to handle incoming whatsapp msg", "device", device, "error", err)
				return
			}
			if err := gw.ChannelSend("whatsapp", device, resp); err != nil {
				slog.Error("failed to send auto-response", "device", device, "error", err)
			}
		})
		gw.engine.Register(w.Poll)
	}

	if i, ok := gw.Channels["irc"].(*channels.IRC); ok {
		i.SetMessageHandler(func(target, msg string) {
			// For IRC, we use the target (channel or nick) as the session ID prefix or client ID
			sessionID := session.DefaultSessionID
			resp, err := gw.PrimaryAgent.DelegatePrompt(sessionID, msg)
			if err != nil {
				slog.Error("failed to handle incoming irc msg", "target", target, "error", err)
				return
			}
			if err := gw.ChannelSend("irc", target, resp); err != nil {
				slog.Error("failed to send irc response", "target", target, "error", err)
			}
		})
		gw.engine.Register(func() {
			if err := i.Run(); err != nil {
				slog.Error("IRC run error", "error", err)
			}
		})
	}

	return gw
}

func (gw *Gateway) ChannelStatus(channel string) map[string]any {
	if ch, ok := gw.Channels[channel]; ok {
		return ch.Status()
	}
	return map[string]any{"error": fmt.Sprintf("channel %q not found", channel)}
}

func (gw *Gateway) ChannelEnroll(channel string, ctx context.Context) error {
	if ch, ok := gw.Channels[channel]; ok {
		return ch.Enroll(ctx)
	}
	return fmt.Errorf("channel %q not found", channel)
}

func (gw *Gateway) ChannelListDevices(channel string) ([]string, error) {
	if ch, ok := gw.Channels[channel]; ok {
		return ch.ListDevices(context.Background())
	}
	return nil, fmt.Errorf("channel %q not found", channel)
}

func (gw *Gateway) ChannelSend(channel, device, msg string) error {
	if ch, ok := gw.Channels[channel]; ok {
		return ch.Send(context.Background(), device, msg)
	}
	return fmt.Errorf("channel %q not found", channel)
}

func (gw *Gateway) ChannelSendFile(channel, device, filePath, caption string) error {
	if ch, ok := gw.Channels[channel]; ok {
		return ch.SendFile(context.Background(), device, filePath, caption)
	}
	return fmt.Errorf("channel %q not found", channel)
}

func (gw *Gateway) ChannelChat(channel, device, prompt string) (string, error) {
	resp, err := gw.PrimaryAgent.DelegatePrompt(device, prompt)
	if err != nil {
		return "", err
	}
	if err := gw.ChannelSend(channel, device, resp); err != nil {
		slog.Error("failed to send channel chat response", "channel", channel, "device", device, "error", err)
	}
	return resp, nil
}

func (gw *Gateway) CreateNewSession() string {
	sessionID := gw.SessionMgr.CreateNewSession()
	if gw.PrimaryAgent != nil {
		if gw.PrimaryAgent.Eng != nil {
			gw.PrimaryAgent.Eng.ClearHistory(sessionID)
		}
		gw.PrimaryAgent.CompactMemory(context.Background())
	}
	return sessionID
}

func (gw *Gateway) ListSessions() []string {
	return gw.SessionMgr.ListIDs()
}

func (gw *Gateway) NumSubAgents() int {
	return len(gw.SubAgents)
}

func (gw *Gateway) UpdateConfig(newCfg *config.Config) {
	gw.Config = newCfg
	gw.PrimaryAgent.Config = newCfg
	gw.PrimaryAgent.InitEngine()
	gw.PrimaryAgent.SetTaskGateway(gw)
	for _, sub := range gw.SubAgents {
		sub.Config = newCfg
		sub.InitEngine()
		sub.SetTaskGateway(gw)
	}
}

func (gw *Gateway) SaveHuman(content string) error {
	return gw.Storage.SaveHuman(content)
}

func (gw *Gateway) GetHuman() (string, error) {
	return gw.Storage.GetHuman()
}

func (gw *Gateway) StartEngine(ctx context.Context) {
	if gw.engine != nil {
		gw.engine.Start(ctx)
	}
	// Trigger startup brain maintenance
	if gw.PrimaryAgent != nil {
		slog.Info("Triggering startup brain maintenance")
		gw.PrimaryAgent.TriggerMaintenance(ctx)
	}
}

func (gw *Gateway) GetSession(id string) *session.Session {
	return gw.SessionMgr.GetSession(id)
}

func (gw *Gateway) AddTokens(id string, prompt, output uint64, cost float64) {
	gw.SessionMgr.AddTokens(id, prompt, output, cost)
}

func (gw *Gateway) ListSkills() []any {
	return gw.PrimaryAgent.ListSkills()
}

func (gw *Gateway) ListSkillCommands(ctx context.Context) ([]engine.SkillCommand, error) {
	return gw.PrimaryAgent.ListSkillCommands(ctx)
}

func (gw *Gateway) ListRemoteSkills(ctx context.Context) (any, error) {
	return gw.PrimaryAgent.ListRemoteSkills(ctx)
}

func (gw *Gateway) InstallSkill(ctx context.Context, name string) (string, error) {
	return gw.PrimaryAgent.InstallSkill(ctx, name)
}

func (gw *Gateway) RemoveSkill(name string) error {
	return gw.PrimaryAgent.RemoveSkill(name)
}

func (gw *Gateway) GetSkill(name string) (any, error) {
	return gw.PrimaryAgent.GetSkill(name)
}

func (gw *Gateway) AddTask(t *tasks.Task) error {
	if t.ID == "" {
		t.ID = uuid.New().String()[:8]
	}
	if err := gw.Storage.SaveTask(t); err != nil {
		return err
	}
	return gw.cronMgr.AddTask(t)
}

func (gw *Gateway) DeleteTask(id string) error {
	if err := gw.Storage.DeleteTask(id); err != nil {
		return err
	}
	gw.cronMgr.RemoveTask(id)
	return nil
}

func (gw *Gateway) ListTasks() ([]*tasks.Task, error) {
	return gw.Storage.ListTasks()
}

func (gw *Gateway) GetTask(id string) (*tasks.Task, error) {
	return gw.Storage.LoadTask(id)
}

func (gw *Gateway) SetTaskReportHandler(h func(sessionID, taskName, taskID, message string)) {
	gw.reportMu.Lock()
	defer gw.reportMu.Unlock()
	gw.taskReportHandler = h
}
