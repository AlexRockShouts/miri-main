//go:build !test

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miri-main/src/internal/config"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/storage"

	"encoding/json"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

type item string

type itemDelegate struct{}

func (d itemDelegate) Height() int {
	return 1
}

func (d itemDelegate) Spacing() int {
	return 0
}

func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (i item) FilterValue() string { return string(i) }

func (i item) Title() string { return string(i) }

func (i item) Description() string { return "" }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		fmt.Fprint(w, "invalid item")
		return
	}
	str := i.Title()
	help := "(↑↓ select, enter config, esc chat, q quit)"

	if index == m.Index() {
		str = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).Render(str)
	} else {
		str = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(str)
	}

	view := lipgloss.NewStyle().
		Width(40).
		PaddingLeft(2).
		Border(lipgloss.RoundedBorder()).
		Render(str + "\n" + help)
	fmt.Fprint(w, view)
}

type model struct {
	viewport   viewport.Model
	chatList   list.Model
	chatChoice item
	chatErr    error
	gw         *gateway.Gateway
	ctx        context.Context
	cancel     context.CancelFunc

	// channels
	mode        string
	channels    list.Model
	choice      item
	enrolling   bool
	channelsErr error

	tabIndex      int
	serverStatus  string
	configContent string
	installStatus string
	engineCtx     context.Context
	engineCancel  context.CancelFunc
}

func initialModel(ctx context.Context, cancel context.CancelFunc) model {
	m := model{
		ctx:          ctx,
		cancel:       cancel,
		mode:         "chat",
		gw:           nil,
		engineCtx:    nil,
		engineCancel: nil,
		serverStatus: "Gateway stopped",
	}

	m.viewport = viewport.New(100, 20)

	items := []list.Item{item("whatsapp")}
	cl := list.New(items, itemDelegate{}, 50, 10)
	cl.Title = "Channels Management"
	cl.SetShowHelp(false)
	m.channels = cl
	m.viewport.Height = 20
	m.viewport.Width = 100
	m.viewport.Style = lipgloss.NewStyle()
	m.viewport.SetContent(`Welcome to Miri TUI! 🚀

Full Dashboard Overview:

• Tab ←→/h/l | q: quit

0️⃣ CHAT: Enter send, Esc clear, c → channels
1️⃣ CHANNELS: ↑↓ select, Enter enroll, Esc back
2️⃣ SERVER: s start, t stop, r restart, F5 refresh
3️⃣ CONFIG: r reload config
4️⃣ INSTALL: b build, i install /usr/local/bin, s service setup

Start gateway in Server tab first for chat/channels!

Type here to chat →`)

	m.chatList = list.New([]list.Item{item("List Sessions"), item("New Session (testclient)"), item("Send Test Prompt"), item("Gateway Status")}, itemDelegate{}, 50, 10)
	m.chatList.Title = "Chat Actions"
	m.chatList.SetShowHelp(false)
	m.tabIndex = 0
	m.refreshServer()
	m.installStatus = "Ready"
	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width == 0 || msg.Height == 0 {
			return m, nil
		}
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 14
		m.chatList.SetWidth(msg.Width)
		m.chatList.SetHeight(msg.Height - 14)
		if m.mode == "channels" {
			m.channels.SetWidth(msg.Width)
			m.channels.SetHeight(msg.Height - 14)
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode == "channels" {
			switch msg.String() {
			case "q", "Q", "ctrl+c":
				m.cancel()
				return m, tea.Quit
			case "esc":
				m.mode = "chat"
				return m, nil
			case "enter":
				if m.enrolling {
					m.mode = "chat"
					m.enrolling = false
					return m, nil
				}
				chname := m.channels.SelectedItem().(item)
				if m.gw == nil {
					m.channelsErr = fmt.Errorf("Gateway not started. Server tab → s")
					return m, nil
				}
				if err := m.gw.ChannelEnroll(string(chname), m.ctx); err != nil {
					m.channelsErr = err
					return m, nil
				}
				m.choice = chname
				m.enrolling = true
				return m, nil
			}
			var cmd tea.Cmd
			m.channels, cmd = m.channels.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "c", "C":
			m.mode = "channels"
			return m, nil
		case "ctrl+c", "q", "Q":
			m.cancel()
			return m, tea.Quit
		}

		if m.tabIndex == 0 {
			switch msg.String() {
			case "enter":
				m.handleChatAction()
				return m, nil
			case "r", "R":
				m.refreshChat()
				return m, nil
			}

			var cmd tea.Cmd
			m.chatList, cmd = m.chatList.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}

	case tea.MouseMsg:
		// optional
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	if vpCmd != nil {
		cmds = append(cmds, vpCmd)
	}

	if m.mode == "channels" {
		var clCmd tea.Cmd
		m.channels, clCmd = m.channels.Update(msg)
		if clCmd != nil {
			cmds = append(cmds, clCmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func chatView(m model) string {
	if m.chatErr != nil {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Padding(1).Border(lipgloss.RoundedBorder()).Render(fmt.Sprintf("Error: %v\nPress r to refresh", m.chatErr))
	}
	return m.chatList.View()
}

func channelsView(m model) string {
	if m.channelsErr != nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Padding(1).
			Border(lipgloss.RoundedBorder()).
			Render(fmt.Sprintf("Error: %%v\nPress esc to chat", m.channelsErr))
	}
	if m.enrolling {
		ch := string(m.choice)
		return lipgloss.NewStyle().
			Width(40).
			Padding(2).
			Border(lipgloss.RoundedBorder()).
			Foreground(lipgloss.Color("62")).
			Render(fmt.Sprintf("Started enrollment for %%s.\nQR code (if needed) printed above.\nPress esc to chat.", ch))
	}
	return m.channels.View()
}

func (m *model) refreshServer() {
	if m.gw != nil {
		m.serverStatus = "Gateway running"
	} else {
		m.serverStatus = "Gateway stopped"
	}
}

func (m *model) refreshConfig() {
	data, _ := json.MarshalIndent(m.gw.Config, "", "  ")
	m.configContent = string(data)
}

func (m *model) initGateway() error {
	cfg, err := config.Load("")
	if err != nil {
		return err
	}

	s, err := storage.New(cfg.StorageDir)
	if err != nil {
		return err
	}

	soulPath := filepath.Join(cfg.StorageDir, "soul.txt")
	if _, err := os.Stat(soulPath); os.IsNotExist(err) {
		templatePath := filepath.Join(".", "templates", "soul.txt")
		templateData, err := os.ReadFile(templatePath)
		if err != nil {
			slog.Warn("soul template", "error", err)
		} else if err := os.WriteFile(soulPath, templateData, 0644); err != nil {
			slog.Warn("bootstrap soul", "error", err)
		} else {
			slog.Info("bootstrapped soul.txt")
		}
	}

	m.gw = gateway.New(cfg, s)
	m.engineCtx, m.engineCancel = context.WithCancel(context.Background())
	m.gw.StartEngine(m.engineCtx)
	m.refreshConfig()
	return nil
}

func (m *model) stopGateway() {
	if m.engineCancel != nil {
		m.engineCancel()
	}
	m.gw = nil
	m.engineCtx = nil
	m.engineCancel = nil
	m.serverStatus = "Gateway stopped"
}

func (m *model) restartGateway() {
	m.stopGateway()
	if err := m.initGateway(); err != nil {
		m.serverStatus = err.Error()
	} else {
		m.serverStatus = "Gateway restarted"
	}
}

func tabsView(m model) string {
	tabs := []string{"Chat", "Channels", "Server", "Config", "Install"}
	var parts []string
	for i, tab := range tabs {
		st := lipgloss.NewStyle().Padding(0, 1)
		if i == m.tabIndex {
			st = st.Bold(true).Foreground(lipgloss.Color("62")).Background(lipgloss.Color("237"))
		} else {
			st = st.Foreground(lipgloss.Color("240"))
		}
		parts = append(parts, st.Render(tab))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func serverView(m model) string {
	s := lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Render(m.serverStatus)
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render(" s:start t:stop r:restart f5:r refresh")
	return lipgloss.JoinVertical(lipgloss.Top, s, help)
}

func configView(m model) string {
	c := lipgloss.NewStyle().Padding(1).Border(lipgloss.RoundedBorder()).Render(m.configContent)
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render(" r:reload config")
	return lipgloss.JoinVertical(lipgloss.Top, c, help)
}

func installView(m model) string {
	goos := runtime.GOOS
	i := fmt.Sprintf("OS: %s\\n\\nb: make all\\ni: make install\\ns: setup service (may require sudo)\\n\\nStatus: %s", goos, m.installStatus)
	return lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).Render(i)
}

func helpView(m model) string {
	helpText := `
Global:
  ←→/h/l : switch tabs   q/Ctrl+C : quit

Chat (tab 0):     Enter: send   Esc: clear   c: channels
Channels (1):     ↑↓: select    Enter: enroll  Esc: back
Server (2):       s: start  t: stop  r: restart  F5: refresh
Config (3):       r: reload
Install (4):      b: build  i: install  s: service
`
	st := lipgloss.NewStyle().
		Foreground(lipgloss.Color("242")).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		Height(10).
		Render(helpText)
	return st
}

func (m *model) pidFromFile() (int, error) {
	pidPath := filepath.Join(m.gw.Config.StorageDir, "miri.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	pidStr := strings.TrimSpace(string(data))
	return strconv.Atoi(pidStr)
}

func (m *model) startServer() {
	m.serverStatus = "Starting..."
	go func() {
		dir, _ := os.Getwd()
		cmd := exec.Command("sh", "-c", fmt.Sprintf("cd %s &amp;&amp; make server &gt;/dev/null 2&gt;&amp;1 &amp;&amp; disown", dir))
		err := cmd.Start()
		if err != nil {
			m.serverStatus = "Start failed: " + err.Error()
			return
		}
		time.Sleep(2 * time.Second)
		m.refreshServer()
	}()
}

func (m *model) stopServer() {
	pid, err := m.pidFromFile()
	if err != nil || pid <= 0 {
		m.serverStatus = "No PID to stop"
		return
	}
	m.serverStatus = "Stopping..."
	syscall.Kill(pid, syscall.SIGTERM)
	time.Sleep(1 * time.Second)
	m.refreshServer()
}

func (m *model) restartServer() {
	m.stopServer()
	time.Sleep(500 * time.Millisecond)
	m.startServer()
}

func (m *model) reloadConfig() {
	cfg, err := config.Load("")
	if err != nil {
		m.configContent = "Reload failed: " + err.Error()
		return
	}
	m.gw.UpdateConfig(cfg)
	m.refreshConfig()
}

func (m *model) buildAll() {
	m.installStatus = "Building..."
	cmd := exec.Command("make", "all")
	output, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 0 {
		m.installStatus = fmt.Sprintf("Build failed:\n%s", string(output))
	} else {
		m.installStatus = "Build success"
	}
}

func (m *model) installBin() {
	m.installStatus = "Installing..."
	cmd := exec.Command("make", "install")
	output, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 0 {
		m.installStatus = fmt.Sprintf("Install failed:\n%s", string(output))
	} else {
		m.installStatus = "Install success\n/usr/local/bin/miri-server miri-tui"
	}
}

func (m *model) setupService() {
	goos := runtime.GOOS
	m.installStatus = fmt.Sprintf("Setting up service for %s...", goos)
	home := os.Getenv("HOME")
	dir, _ := os.Getwd()
	if goos == "darwin" {
		plistSrc := filepath.Join(dir, "templates", "launchd", "com.miri.agent.plist")
		plistTarget := filepath.Join(home, "Library", "LaunchAgents", "com.miri.agent.plist")
		cmd := exec.Command("cp", plistSrc, plistTarget)
		err := cmd.Run()
		if err != nil {
			m.installStatus = "Copy plist failed: " + err.Error()
			return
		}
		cmd = exec.Command("launchctl", "load", plistTarget)
		err = cmd.Run()
		if err != nil {
			m.installStatus = "launchctl load failed (run manually): " + err.Error()
		} else {
			m.installStatus = "Service setup complete. launchctl list | grep miri"
		}
	} else if goos == "linux" {
		m.installStatus = "Linux service:\n1. sudo cp templates/systemd/miri.service /etc/systemd/system/\n2. sudo systemctl daemon-reload\n3. sudo systemctl enable --now miri"
	} else {
		m.installStatus = fmt.Sprintf("Manual setup for %s", goos)
	}
}

func (m model) View() string {
	tabsRow := tabsView(m)

	title := lipgloss.NewStyle().Bold(true).Padding(0, 1).Render("Miri TUI")

	gap1 := strings.Repeat("\n", 1)

	subview := ""
	switch m.tabIndex {
	case 0:
		subview = chatView(m)
	case 1:
		subview = channelsView(m)
	case 2:
		subview = serverView(m)
	case 3:
		subview = configView(m)
	case 4:
		subview = installView(m)
	default:
		subview = "Invalid tab"
	}

	gap2 := strings.Repeat("\n", 1)

	help := helpView(m)
	gap3 := "\n"

	return lipgloss.JoinVertical(
		lipgloss.Top,
		tabsRow,
		title,
		gap1,
		subview,
		gap2,
		gap3,
		help,
	)
}

func (m *model) refreshChat() {
	m.chatErr = nil
}

func (m *model) handleChatAction() {
	choice := m.chatList.SelectedItem().(item)
	m.chatChoice = choice
	switch string(choice) {
	case "List Sessions":
		if m.gw == nil {
			m.chatErr = fmt.Errorf("Gateway not started")
			return
		}
		sessions := m.gw.ListSessions()
		data, _ := json.MarshalIndent(sessions, "", "  ")
		m.viewport.SetContent(string(data))
	case "New Session (testclient)":
		if m.gw == nil {
			m.chatErr = fmt.Errorf("Gateway not started")
			return
		}
		id := m.gw.CreateNewSession("testclient")
		m.viewport.SetContent(fmt.Sprintf("New session: %s", id))
	case "Send Test Prompt":
		if m.gw == nil {
			m.chatErr = fmt.Errorf("Gateway not started")
			return
		}
		resp, err := m.gw.PrimaryAgent.DelegatePrompt("tui", "Test from menu")
		if err != nil {
			m.chatErr = err
		} else {
			m.viewport.SetContent(resp)
		}
	case "Gateway Status":
		if m.gw == nil {
			m.viewport.SetContent("Gateway stopped")
		} else {
			status := fmt.Sprintf("Running\nChannels: %d\nSessions: %d", len(m.gw.Channels), len(m.gw.ListSessions()))
			m.viewport.SetContent(status)
		}
	}
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("shutdown signal")
		cancel()
	}()

	m := initialModel(ctx, cancel)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		slog.Error("TUI", "error", err)
		os.Exit(1)
	}
}
