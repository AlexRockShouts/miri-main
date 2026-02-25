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

	gw.cronMgr = cron.NewCronManager(gw.Storage, func(sessionID, prompt string) (string, error) {
		return gw.PrimaryAgent.DelegatePrompt(sessionID, prompt)
	})
	gw.cronMgr.Start()

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
			sessionID := gw.CreateNewSession(device)
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
			sessionID := gw.CreateNewSession("irc:" + target)
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

func (gw *Gateway) CreateNewSession(clientID string) string {
	return gw.SessionMgr.CreateNewSession(clientID)
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
	for _, sub := range gw.SubAgents {
		sub.Config = newCfg
		sub.InitEngine()
	}
}

func (gw *Gateway) SaveHumanInfo(info *storage.HumanInfo) error {
	return gw.Storage.SaveHumanInfo(info)
}

func (gw *Gateway) ListHumanInfo() ([]*storage.HumanInfo, error) {
	return gw.Storage.ListHumanInfo()
}

func (gw *Gateway) StartEngine(ctx context.Context) {
	if gw.engine != nil {
		gw.engine.Start(ctx)
	}
}

func (gw *Gateway) GetSession(id string) *session.Session {
	return gw.SessionMgr.GetSession(id)
}

func (gw *Gateway) ListSkills() []any {
	return gw.PrimaryAgent.ListSkills()
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
