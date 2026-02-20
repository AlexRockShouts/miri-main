package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"io"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miri-main/src/internal/config"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/storage"
)

type Model struct {
	viewport        viewport.Model
	list            list.Model
	tabIndex        int
	gw              *gateway.Gateway
	ctx             context.Context
	cancel          context.CancelFunc
	engineCtx       context.Context
	engineCancel    context.CancelFunc
	serverStatus    string
	configContent   string
	installStatus   string
	mode            string
	selectedChannel string
	enrolling       bool
	channelStatus   string
	prevTabIndex    int
}

type item string

func (i item) FilterValue() string { return string(i) }

type itemDelegate struct{}

func (d itemDelegate) Height() int { return 1 }

func (d itemDelegate) Spacing() int { return 0 }

func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}
	text := string(i)
	var st lipgloss.Style
	if index == m.Index() {
		st = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).PaddingLeft(2)
	} else {
		st = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).PaddingLeft(2)
	}
	fmt.Fprint(w, st.Render(text))
}

func initialModel(ctx context.Context, cancel context.CancelFunc) Model {
	m := Model{
		ctx:             ctx,
		cancel:          cancel,
		gw:              nil,
		engineCtx:       nil,
		engineCancel:    nil,
		serverStatus:    "Gateway stopped",
		installStatus:   "Ready",
		mode:            "main",
		selectedChannel: "",
		enrolling:       false,
		channelStatus:   "",
		prevTabIndex:    0,
	}
	m.viewport = viewport.New(100, 20)
	m.viewport.Width = 100
	m.viewport.Height = 20
	m.viewport.SetContent("Welcome to Miri TUI! 🚀\\n\\n↑↓: select segment | q: quit. Server: s/t/r | Config: r reload | etc.")

	items := []list.Item{
		item("Channels"),
		item("Server"),
	}
	delegate := itemDelegate{}
	m.list = list.New(items, delegate, 80, 6)
	m.list.Title = "Main Segments"
	m.list.SetShowHelp(false)
	m.list.Select(0)

	m.tabIndex = 0
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)

	cmds := []tea.Cmd{cmd}

	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	m.tabIndex = int(m.list.Index())

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width == 0 || msg.Height == 0 {
			return m, nil
		}
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 24
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(8)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			m.cancel()
			return m, tea.Quit
			// ↑↓ navigate list
		case "s":
			if m.tabIndex == 1 && m.gw == nil {
				if err := m.initGateway(); err != nil {
					m.serverStatus = "Start failed: " + err.Error()
				} else {
					m.serverStatus = "Gateway running"
				}
				m.viewport.SetContent(tabView(m))
			}
		case "t":
			if m.tabIndex == 1 {
				m.stopGateway()
				m.viewport.SetContent(tabView(m))
			}
		case "r":
			if m.tabIndex == 2 {
				m.refreshConfig()
				m.viewport.SetContent(tabView(m))
			} else if m.tabIndex == 1 {
				m.restartGateway()
				m.viewport.SetContent(tabView(m))
			}
		case "e":
			if m.tabIndex == 0 {
				if m.gw == nil {
					m.viewport.SetContent("Start gateway first (Server tab → s)")
				} else {
					m.gw.ChannelEnroll("whatsapp", m.engineCtx)
					m.viewport.SetContent("Enrolling whatsapp...\\nQR code printed to stdout!\\nScan it with WhatsApp app.\\nPress 'S' to refresh status.")
				}
				return m, nil
			}
		case "d":
			if m.tabIndex == 0 && m.gw != nil {
				whatsappDir := filepath.Join(m.gw.Config.StorageDir, "whatsapp")
				dbPath := filepath.Join(whatsappDir, "whatsapp.db")
				err := os.Remove(dbPath)
				if err != nil && !os.IsNotExist(err) {
					m.viewport.SetContent(fmt.Sprintf("Reset failed: %v", err))
				} else {
					m.viewport.SetContent("Whatsapp reset: DB removed.\\nEnroll again to start fresh.")
				}
				return m, nil
			}
		case "S":
			if m.tabIndex == 0 && m.gw != nil {
				status := m.gw.ChannelStatus("whatsapp")
				data, _ := json.MarshalIndent(status, "", "  ")
				m.viewport.SetContent(fmt.Sprintf("Whatsapp Status:\n%s", string(data)))
				return m, nil
			}

		case "enter":
			if m.tabIndex == 0 {
				if m.gw == nil {
					m.viewport.SetContent("Start gateway first (Server tab → s key)")
				} else {
					m.gw.ChannelEnroll("whatsapp", m.engineCtx)
					m.viewport.SetContent("Enrolling whatsapp...\nQR code printed to stdout!\nScan with WhatsApp app.\nPress S to check status.")
				}
				return m, nil
			} else if m.tabIndex == 1 {
				m.viewport.SetContent(tabView(m))
				return m, nil
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) initGateway() error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}
	st, err := storage.New(cfg.StorageDir)
	if err != nil {
		return err
	}
	soulPath := filepath.Join(cfg.StorageDir, "soul.txt")
	if _, err := os.Stat(soulPath); os.IsNotExist(err) {
		templatePath := filepath.Join(".", "templates", "soul.txt")
		if templateData, err := os.ReadFile(templatePath); err == nil {
			os.WriteFile(soulPath, templateData, 0644)
		}
	}
	m.gw = gateway.New(cfg, st)
	m.engineCtx, m.engineCancel = context.WithCancel(context.Background())
	m.gw.StartEngine(m.engineCtx)
	m.refreshConfig()
	return nil
}

func (m *Model) stopGateway() {
	if m.engineCancel != nil {
		m.engineCancel()
	}
	m.gw = nil
	m.engineCtx = nil
	m.engineCancel = nil
	m.serverStatus = "Gateway stopped"
}

func (m *Model) restartGateway() {
	m.stopGateway()
	m.initGateway()
}

func (m *Model) refreshConfig() {
	if m.gw != nil {
		data, _ := json.MarshalIndent(m.gw.Config, "", "  ")
		m.configContent = string(data)
	} else {
		m.configContent = "No gateway"
	}
}

func tabsView(m Model) string {
	tabs := []string{"Channels", "Server", "Config", "Install"}
	var parts []string
	for i, t := range tabs {
		var st lipgloss.Style
		if i == m.tabIndex {
			st = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).Background(lipgloss.Color("237")).Padding(0, 1)
		} else {
			st = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 1)
		}
		parts = append(parts, st.Render(t))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func tabView(m Model) string {
	switch m.tabIndex {
	case 0:
		if m.gw == nil {
			return "Start gateway first (Server tab → s)"
		}
		status := m.gw.ChannelStatus("whatsapp")
		data, _ := json.MarshalIndent(status, "", "  ")
		return fmt.Sprintf(`Available channels:
• whatsapp

Status:
%s

Keys:
• e: enroll (scan QR on stdout)
• d: reset (rm whatsapp.db)
• S: refresh status`, string(data))
	case 1:
		if m.gw == nil {
			return `Server
 Gateway stopped
 s: start gw | t: stop | r: restart`
		}
		model := m.gw.PrimaryAgent.PrimaryModel()
		addr := m.gw.Config.Server.Addr
		chCount := len(m.gw.Channels)
		sessCount := len(m.gw.ListSessions())
		chDetails := ""
		for name := range m.gw.Channels {
			st := m.gw.ChannelStatus(name)
			chDetails += fmt.Sprintf("  %s: connected=%v logged_in=%v\n", name, st["connected"], st["logged_in"])
		}
		return fmt.Sprintf(`Server Properties (Gateway running)

 Address: %s
 Model: %s
 Channels: %d
 Sessions: %d
 Details:
 %s
 s: start (no-op) | t: stop | r: restart | S: refresh status`, addr, model, chCount, sessCount, chDetails)
	case 2:
		return fmt.Sprintf("Config:\\n%s", m.configContent)
	case 3:
		return fmt.Sprintf("Install:\\nStatus: %s\\nb build | i install | s service stub", m.installStatus)
	}
	return "Invalid tab"
}

func helpView() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("242")).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		Height(8).
		Render(`↑↓ select | Enter: default enroll (channels) | q: quit
Channels: e enroll | d reset | S refresh
Server: s start | t stop | r restart | Esc: back if sub`)
}

func (m Model) View() string {
	m.viewport.SetContent(tabView(m))
	return lipgloss.JoinVertical(lipgloss.Top,
		lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).MaxHeight(10).Render(m.list.View()),
		"Miri TUI",
		"\\n",
		m.viewport.View(),
		helpView(),
	)
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "config path")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	p := tea.NewProgram(initialModel(ctx, cancel), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		slog.Error("TUI error", "err", err)
		os.Exit(1)
	}
}
