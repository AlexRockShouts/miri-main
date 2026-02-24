package channels

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/config"
	"strings"
	"sync"

	"github.com/lrstanley/girc"
	"slices"
)

type IRC struct {
	cfg     config.IRCConfig
	client  *girc.Client
	handler func(channel, message string)
	mu      sync.RWMutex
}

func NewIRC(cfg config.IRCConfig) *IRC {
	gcfg := girc.Config{
		Server: cfg.Host,
		Port:   cfg.Port,
		Nick:   cfg.Nick,
		User:   cfg.User,
		Name:   cfg.Realname,
		SSL:    cfg.TLS,
	}
	if cfg.Password != nil {
		gcfg.ServerPass = *cfg.Password
	}

	client := girc.New(gcfg)

	i := &IRC{
		cfg:    cfg,
		client: client,
	}

	// Basic handlers
	client.Handlers.Add(girc.CONNECTED, func(c *girc.Client, e girc.Event) {
		slog.Info("IRC connected", "server", cfg.Host)
		if cfg.NickServ.Enabled {
			c.Cmd.Message("NickServ", fmt.Sprintf("IDENTIFY %s", cfg.NickServ.Password))
		}
		for _, ch := range cfg.Channels {
			c.Cmd.Join(ch)
		}
	})

	client.Handlers.Add(girc.PRIVMSG, func(c *girc.Client, e girc.Event) {
		target := e.Params[0] // channel or nick
		msg := e.Last()

		// Determine actual response target (if PM to bot, respond to sender)
		respTarget := target
		if !strings.HasPrefix(target, "#") {
			respTarget = e.Source.Name
		}

		// 1) Blocklist: silently ignore
		if slices.Contains(cfg.Blocklist, target) || slices.Contains(cfg.Blocklist, e.Source.Name) {
			return
		}

		// 2) Allowlist: forward to agent if either target or sender is whitelisted
		if slices.Contains(cfg.Allowlist, target) || slices.Contains(cfg.Allowlist, e.Source.Name) {
			i.mu.RLock()
			h := i.handler
			i.mu.RUnlock()
			if h != nil {
				h(respTarget, msg)
			}
			return
		}

		// 3) Otherwise: send default reply
		c.Cmd.Message(respTarget, "thanks for contact, we call you back !")
	})

	return i
}

func (i *IRC) Name() string {
	return "irc"
}

func (i *IRC) Status() map[string]any {
	return map[string]any{
		"connected": i.client.IsConnected(),
		"nick":      i.client.GetNick(),
		"server":    i.cfg.Host,
	}
}

func (i *IRC) Enroll(ctx context.Context) error {
	// IRC doesn't really have an "enroll" step like WhatsApp QR scanning.
	// But we can use this to (re)connect if needed.
	go func() {
		if err := i.client.Connect(); err != nil {
			slog.Error("IRC connect error", "error", err)
		}
	}()
	return nil
}

func (i *IRC) ListDevices(ctx context.Context) ([]string, error) {
	// For IRC, "devices" are channels we are in
	return i.cfg.Channels, nil
}

func (i *IRC) Send(ctx context.Context, target string, msg string) error {
	i.client.Cmd.Message(target, msg)
	return nil
}

func (i *IRC) SetMessageHandler(handler func(target string, message string)) {
	i.mu.Lock()
	i.handler = handler
	i.mu.Unlock()
}

func (i *IRC) Run() error {
	return i.client.Connect()
}
