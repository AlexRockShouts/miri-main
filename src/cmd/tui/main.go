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
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"maps"
	"slices"

	"io"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"miri-main/src/internal/config"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/storage"

	"github.com/charmbracelet/bubbles/textinput"

	// New UI toolkit (Option C)
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Model struct {
	viewport        viewport.Model
	list            list.Model
	textInput       textinput.Model
	segmentIndex    int
	gw              *gateway.Gateway
	ctx             context.Context
	cancel          context.CancelFunc
	engineCtx       context.Context
	engineCancel    context.CancelFunc
	serverStatus    string
	configContent   string
	installStatus   string
	mode            string // "main", "config_edit", "setup"
	editField       string // "provider_name", "api_key", "base_url", "primary_model"
	selectedChannel string
	enrolling       bool
	channelStatus   string
	prevTabIndex    int
	config          *config.Config
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

	ti := textinput.New()
	ti.Placeholder = "Value..."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 40
	m.textInput = ti

	m.viewport = viewport.New(100, 20)
	m.viewport.Width = 100
	m.viewport.Height = 20
	m.viewport.SetContent("Welcome to Miri TUI! 🚀\n\n↑↓: select segment | q: quit. Server: s/t/r | Config: r reload | etc.")

	items := []list.Item{
		item("Channels"),
		item("Server"),
		item("Config"),
		item("Install"),
	}
	delegate := itemDelegate{}
	m.list = list.New(items, delegate, 80, 6)
	m.list.Title = "Main Segments"
	m.list.SetShowHelp(false)
	m.list.Select(0)

	m.segmentIndex = 0

	cfg, _ := config.Load("")
	m.config = cfg

	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds := []tea.Cmd{cmd}

	if m.mode == "config_edit" {
		m.textInput, cmd = m.textInput.Update(msg)
		cmds = append(cmds, cmd)

		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "enter":
				val := m.textInput.Value()
				if m.config != nil {
					switch m.editField {
					case "api_key":
						// Just assume xai for now if not specified
						prov := m.config.Models.Providers["xai"]
						prov.APIKey = val
						m.config.Models.Providers["xai"] = prov
					case "primary_model":
						m.config.Agents.Defaults.Model.Primary = val
					}
					config.Save(m.config)
					if m.gw != nil {
						m.gw.UpdateConfig(m.config)
					}
				}
				m.mode = "main"
				m.textInput.Blur()
				m.refreshConfig()
			case "esc":
				m.mode = "main"
				m.textInput.Blur()
			}
		}
		return m, tea.Batch(cmds...)
	}

	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	m.segmentIndex = int(m.list.Index())

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
			if m.segmentIndex == 1 && m.gw == nil {
				if err := m.initGateway(); err != nil {
					m.serverStatus = "Start failed: " + err.Error()
				} else {
					m.serverStatus = "Gateway running"
				}
				m.viewport.SetContent(segmentView(m))
			}
		case "t":
			if m.segmentIndex == 1 {
				m.stopGateway()
				m.viewport.SetContent(segmentView(m))
			}
		case "r":
			if m.segmentIndex == 2 {
				m.refreshConfig()
				m.viewport.SetContent(segmentView(m))
			} else if m.segmentIndex == 1 {
				m.restartGateway()
				m.viewport.SetContent(segmentView(m))
			}
		case "k":
			if m.segmentIndex == 2 {
				m.mode = "config_edit"
				m.editField = "api_key"
				m.textInput.Placeholder = "Enter xAI API Key"
				m.textInput.SetValue("")
				m.textInput.Focus()
				return m, nil
			}
		case "m":
			if m.segmentIndex == 2 {
				m.mode = "config_edit"
				m.editField = "primary_model"
				m.textInput.Placeholder = "Enter Primary Model (e.g. xai/grok-beta)"
				m.textInput.SetValue(m.config.Agents.Defaults.Model.Primary)
				m.textInput.Focus()
				return m, nil
			}
		case "i":
			if m.segmentIndex == 3 {
				m.installService()
				m.viewport.SetContent(segmentView(m))
			}
		case "e":
			if m.segmentIndex == 0 {
				if m.gw == nil {
					m.viewport.SetContent("Start gateway first (Server segment → s)")
				} else {
					m.gw.ChannelEnroll("whatsapp", m.engineCtx)
					m.viewport.SetContent("Enrolling whatsapp...\nQR code printed to stdout!\nScan it with WhatsApp app.\nPress 'S' to refresh status.")
				}
				return m, nil
			}
		case "d":
			if m.segmentIndex == 0 && m.gw != nil {
				whatsappDir := filepath.Join(m.gw.Config.StorageDir, "whatsapp")
				dbPath := filepath.Join(whatsappDir, "whatsapp.db")
				err := os.Remove(dbPath)
				if err != nil && !os.IsNotExist(err) {
					m.viewport.SetContent(fmt.Sprintf("Reset failed: %v", err))
				} else {
					m.viewport.SetContent("Whatsapp reset: DB removed.\nEnroll again to start fresh.")
				}
				return m, nil
			}
		case "S":
			if m.segmentIndex == 0 && m.gw != nil {
				status := m.gw.ChannelStatus("whatsapp")
				data, _ := json.MarshalIndent(status, "", "  ")
				m.viewport.SetContent(fmt.Sprintf("Whatsapp Status:\n%s", string(data)))
				return m, nil
			}

		case "enter":
			if m.segmentIndex == 0 {
				if m.gw == nil {
					m.viewport.SetContent("Start gateway first (Server segment → s key)")
				} else {
					m.gw.ChannelEnroll("whatsapp", m.engineCtx)
					m.viewport.SetContent("Enrolling whatsapp...\nQR code printed to stdout!\nScan with WhatsApp app.\nPress S to check status.")
				}
				return m, nil
			} else if m.segmentIndex == 1 {
				m.viewport.SetContent(segmentView(m))
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

func (m *Model) installService() {
	m.installStatus = "Installing..."
	home, _ := os.UserHomeDir()
	binPath, err := os.Executable()
	if err != nil {
		m.installStatus = "Error finding executable: " + err.Error()
		return
	}

	// Detect OS
	goos := os.Getenv("GOOS")
	if goos == "" {
		goos = runtime.GOOS
	}

	switch goos {
	case "darwin":
		plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.miri.agent.plist")
		templatePath := filepath.Join("templates", "launchd", "com.miri.agent.plist")
		data, err := os.ReadFile(templatePath)
		if err != nil {
			m.installStatus = "Template not found: " + err.Error()
			return
		}
		content := string(data)
		content = strings.ReplaceAll(content, "/usr/local/bin/miri", binPath)
		content = strings.ReplaceAll(content, "$HOME", home)

		os.MkdirAll(filepath.Dir(plistPath), 0755)
		err = os.WriteFile(plistPath, []byte(content), 0644)
		if err != nil {
			m.installStatus = "Write failed: " + err.Error()
		} else {
			m.installStatus = "Installed! Run: launchctl load " + plistPath
		}
	case "linux":
		// This usually needs sudo, so we just write the file to ~/.miri/miri.service
		// and tell user to copy it.
		servicePath := filepath.Join(m.config.StorageDir, "miri.service")
		templatePath := filepath.Join("templates", "systemd", "miri.service")
		data, err := os.ReadFile(templatePath)
		if err != nil {
			m.installStatus = "Template not found: " + err.Error()
			return
		}
		content := string(data)
		content = strings.ReplaceAll(content, "%h/bin/miri", binPath)
		// systemd %h and %i work in user mode, but we might want to be explicit
		user := os.Getenv("USER")
		content = strings.ReplaceAll(content, "%i", user)

		err = os.WriteFile(servicePath, []byte(content), 0644)
		if err != nil {
			m.installStatus = "Write failed: " + err.Error()
		} else {
			m.installStatus = "Generated " + servicePath + ". Copy to /etc/systemd/system/"
		}
	default:
		m.installStatus = "Unsupported OS for auto-install: " + goos
	}
}

func (m *Model) refreshConfig() {
	if m.gw != nil {
		data, _ := json.MarshalIndent(m.gw.Config, "", "  ")
		m.configContent = string(data)
	} else {
		m.configContent = "No gateway"
	}
}

func segmentView(m Model) string {
	switch m.segmentIndex {
	case 0:
		if m.gw == nil {
			return "Start gateway first (Server segment → s)"
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
		modelName := m.gw.PrimaryAgent.PrimaryModel()
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
 s: start (no-op) | t: stop | r: restart | S: refresh status`, addr, modelName, chCount, sessCount, chDetails)
	case 2:
		return fmt.Sprintf("Config:\n%s\n\nKeys:\n• k: set xAI API Key\n• m: set Primary Model\n• r: reload from disk", m.configContent)
	case 3:
		return fmt.Sprintf("Install:\nStatus: %s\n\nKeys:\n• i: install service (launchd/systemd)", m.installStatus)
	}
	return "Invalid segment"
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
	if m.mode == "config_edit" {
		return lipgloss.JoinVertical(lipgloss.Left,
			"Editing Config Value:",
			m.textInput.View(),
			"\n(Enter to save, Esc to cancel)",
		)
	}
	m.viewport.SetContent(segmentView(m))
	return lipgloss.JoinVertical(lipgloss.Top,
		lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).MaxHeight(10).Render(m.list.View()),
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62")).PaddingLeft(2).Render("Miri Autonomous Agent"),
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

	if _, err := os.Stat("/dev/tty"); err != nil {
		slog.Error("TUI requires a terminal (TTY)", "err", err)
		fmt.Println("Miri TUI requires an interactive terminal to run.")
		fmt.Println("If you are running in a non-interactive environment, use the server instead.")
		os.Exit(1)
	}

	// Run the new tview-based UI (Option C). Enter works natively on lists, forms and buttons.
	if err := startTviewApp(ctx, cancel); err != nil {
		slog.Error("TUI error", "err", err)
		os.Exit(1)
	}
}

// startTviewApp builds a tview-based interface with a left navigation and right content pages.
func startTviewApp(ctx context.Context, cancel context.CancelFunc) error {
	app := tview.NewApplication()

	// Shared state
	var gw *gateway.Gateway
	var engineCtx context.Context
	var engineCancel context.CancelFunc
	cfg, cfgErr := config.Load("")
	if cfgErr != nil || cfg == nil {
		// Fall back to a safe default config to avoid nil dereferences in the TUI
		home, _ := os.UserHomeDir()
		defaultStorage := filepath.Join(home, ".miri")
		_ = os.MkdirAll(defaultStorage, 0o755)
		cfg = &config.Config{
			Models: config.ModelsConfig{
				Mode:      "merge",
				Providers: map[string]config.ProviderConfig{},
			},
			Agents: config.AgentsConfig{
				Defaults: config.AgentDefaults{Model: config.ModelSelection{Primary: "xai/grok-beta"}},
			},
			Server:     config.ServerConfig{Addr: ":8080"},
			StorageDir: defaultStorage,
		}
	}
	// Ensure providers map exists and 'xai' entry is initialized to avoid map assignment panics later
	if cfg.Models.Providers == nil {
		cfg.Models.Providers = make(map[string]config.ProviderConfig)
	}
	if _, ok := cfg.Models.Providers["xai"]; !ok {
		cfg.Models.Providers["xai"] = config.ProviderConfig{
			BaseURL: "https://api.x.ai/v1",
			API:     "openai-completions",
		}
	}
	// Ensure server addr has a sensible default for first-run
	if strings.TrimSpace(cfg.Server.Addr) == "" {
		cfg.Server.Addr = ":8080"
	}
	// Ensure storage dir exists
	if strings.TrimSpace(cfg.StorageDir) == "" {
		home, _ := os.UserHomeDir()
		cfg.StorageDir = filepath.Join(home, ".miri")
	}
	_ = os.MkdirAll(cfg.StorageDir, 0o755)

	// Helpers
	initGateway := func() error {
		if gw != nil {
			return nil
		}
		st, err := storage.New(cfg.StorageDir)
		if err != nil {
			return err
		}
		soulPath := filepath.Join(cfg.StorageDir, "soul.txt")
		if _, err := os.Stat(soulPath); os.IsNotExist(err) {
			if templateData, err := os.ReadFile(filepath.Join(".", "templates", "soul.txt")); err == nil {
				_ = os.WriteFile(soulPath, templateData, 0o644)
			}
		}
		gw = gateway.New(cfg, st)
		engineCtx, engineCancel = context.WithCancel(context.Background())
		gw.StartEngine(engineCtx)
		return nil
	}
	stopGateway := func() {
		if engineCancel != nil {
			engineCancel()
		}
		gw = nil
		engineCtx = nil
		engineCancel = nil
	}
	restartGateway := func() error {
		stopGateway()
		return initGateway()
	}

	// UI pieces
	nav := tview.NewList().ShowSecondaryText(false)
	nav.SetBorder(true).SetTitle(" Miri ")

	pages := tview.NewPages()

	statusBar := tview.NewTextView().SetDynamicColors(true)
	statusBar.SetBorder(true)
	setStatus := func(s string) { app.QueueUpdateDraw(func() { statusBar.SetText(s) }) }

	// Shared Info view for Gateway status (replaces the one in removed Server page)
	gatewayStatusInfo := tview.NewTextView().SetDynamicColors(true)
	refreshGatewayInfo := func() {
		if gw == nil {
			gatewayStatusInfo.SetText("[yellow]Gateway stopped[-]")
			return
		}
		modelName := gw.PrimaryAgent.PrimaryModel()
		addr := gw.Config.Server.Addr
		chCount := len(gw.Channels)
		sessCount := len(gw.ListSessions())
		var b strings.Builder
		fmt.Fprintf(&b, "Address: %s\nModel: %s\nChannels: %d\nSessions: %d\n\n", addr, modelName, chCount, sessCount)
		for name := range gw.Channels {
			st := gw.ChannelStatus(name)
			fmt.Fprintf(&b, "%s: connected=%v logged_in=%v\n", name, st["connected"], st["logged_in"])
		}
		gatewayStatusInfo.SetText(b.String())
	}

	// Shared Install status
	installStatus := tview.NewTextView().SetText("Ready").SetDynamicColors(true)

	// Channels Page
	channelsDoc := tview.NewTextView().SetDynamicColors(true).SetChangedFunc(func() { app.Draw() })
	channelsDoc.SetBorder(true).SetTitle(" Overview ")
	channelsDoc.SetText(`[green]Channels Overview:[-]
- Use [yellow]Select Channel[-] on the left to choose which integration you want to configure (e.g. WhatsApp or IRC).
- Configuration settings for the selected channel will be saved to ~/.miri/config.yaml using the [yellow]Save[-] button.
- Channel-specific actions like [yellow]Enroll[-] or [yellow]Reset[-] are located at the bottom of each configuration form.

WhatsApp: enroll once by scanning the QR code from stdout; manage allowlist/blocklist.
IRC: set server, identity, channels, and optional NickServ auth.`)

	// Channel list for selection
	channelSelector := tview.NewList().ShowSecondaryText(false)
	channelSelector.SetBorder(true).SetTitle(" Select Channel ")

	channelConfigPages := tview.NewPages()

	updateWAForm := func() *tview.Form {
		return tview.NewForm().
			AddCheckbox("Enabled", cfg.Channels.Whatsapp.Enabled, func(checked bool) { cfg.Channels.Whatsapp.Enabled = checked }).
			AddInputField("Allowlist (comma sep)", strings.Join(cfg.Channels.Whatsapp.Allowlist, ","), 50, nil, func(val string) {
				if val == "" {
					cfg.Channels.Whatsapp.Allowlist = []string{}
				} else {
					cfg.Channels.Whatsapp.Allowlist = strings.Split(val, ",")
				}
			}).
			AddInputField("Blocklist (comma sep)", strings.Join(cfg.Channels.Whatsapp.Blocklist, ","), 50, nil, func(val string) {
				if val == "" {
					cfg.Channels.Whatsapp.Blocklist = []string{}
				} else {
					cfg.Channels.Whatsapp.Blocklist = strings.Split(val, ",")
				}
			}).
			AddButton("Save WhatsApp Config", func() {
				if err := config.Save(cfg); err != nil {
					setStatus("Save failed: " + err.Error())
					return
				}
				if gw != nil {
					gw.UpdateConfig(cfg)
				}
				setStatus("WhatsApp config saved")
			}).
			AddButton("Enroll WhatsApp", func() {
				if gw == nil {
					setStatus("Start server first in Server page (Start)")
					return
				}
				if err := gw.ChannelEnroll("whatsapp", engineCtx); err != nil {
					setStatus("Enroll failed: " + err.Error())
					return
				}
				setStatus("Enrolling whatsapp... check stdout for QR")
			}).
			AddButton("Reset WhatsApp DB", func() {
				if gw == nil {
					setStatus("Gateway stopped")
					return
				}
				whatsappDir := filepath.Join(gw.Config.StorageDir, "whatsapp")
				dbPath := filepath.Join(whatsappDir, "whatsapp.db")
				if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
					setStatus("Reset failed: " + err.Error())
				} else {
					setStatus("Whatsapp reset: DB removed. Enroll again.")
				}
			})
	}

	updateIRCForm := func() *tview.Form {
		return tview.NewForm().
			AddCheckbox("Enabled", cfg.Channels.IRC.Enabled, func(checked bool) { cfg.Channels.IRC.Enabled = checked }).
			AddInputField("Host", cfg.Channels.IRC.Host, 50, nil, func(val string) { cfg.Channels.IRC.Host = val }).
			AddInputField("Port", fmt.Sprintf("%d", cfg.Channels.IRC.Port), 10, nil, func(val string) {
				if p, err := strconv.Atoi(val); err == nil {
					cfg.Channels.IRC.Port = p
				}
			}).
			AddCheckbox("TLS", cfg.Channels.IRC.TLS, func(checked bool) { cfg.Channels.IRC.TLS = checked }).
			AddInputField("Nick", cfg.Channels.IRC.Nick, 50, nil, func(val string) { cfg.Channels.IRC.Nick = val }).
			AddInputField("User", cfg.Channels.IRC.User, 50, nil, func(val string) { cfg.Channels.IRC.User = val }).
			AddInputField("Realname", cfg.Channels.IRC.Realname, 50, nil, func(val string) { cfg.Channels.IRC.Realname = val }).
			AddInputField("Channels (comma sep)", strings.Join(cfg.Channels.IRC.Channels, ","), 50, nil, func(val string) {
				if val == "" {
					cfg.Channels.IRC.Channels = []string{}
				} else {
					cfg.Channels.IRC.Channels = strings.Split(val, ",")
				}
			}).
			AddInputField("Allowlist (comma sep)", strings.Join(cfg.Channels.IRC.Allowlist, ","), 50, nil, func(val string) {
				if val == "" {
					cfg.Channels.IRC.Allowlist = []string{}
				} else {
					cfg.Channels.IRC.Allowlist = strings.Split(val, ",")
				}
			}).
			AddInputField("Blocklist (comma sep)", strings.Join(cfg.Channels.IRC.Blocklist, ","), 50, nil, func(val string) {
				if val == "" {
					cfg.Channels.IRC.Blocklist = []string{}
				} else {
					cfg.Channels.IRC.Blocklist = strings.Split(val, ",")
				}
			}).
			AddCheckbox("NickServ Enabled", cfg.Channels.IRC.NickServ.Enabled, func(checked bool) { cfg.Channels.IRC.NickServ.Enabled = checked }).
			AddPasswordField("NickServ Password", cfg.Channels.IRC.NickServ.Password, 50, '*', func(val string) { cfg.Channels.IRC.NickServ.Password = val }).
			AddButton("Save IRC Config", func() {
				if err := config.Save(cfg); err != nil {
					setStatus("Save failed: " + err.Error())
					return
				}
				if gw != nil {
					gw.UpdateConfig(cfg)
				}
				setStatus("IRC config saved")
			}).
			AddButton("IRC Connect", func() {
				if gw == nil {
					setStatus("Gateway stopped")
					return
				}
				if err := gw.ChannelEnroll("irc", context.Background()); err != nil {
					setStatus("IRC Connect failed: " + err.Error())
				} else {
					setStatus("IRC Connecting...")
				}
			}).
			AddButton("Reset IRC", func() {
				// No specific state to reset for IRC yet, but following user request
				setStatus("IRC Reset: re-initializing client.")
				if err := restartGateway(); err != nil {
					setStatus("IRC Reset failed: " + err.Error())
				} else {
					setStatus("IRC Reset: Gateway restarted.")
				}
			})
	}

	waForm := updateWAForm()
	waForm.SetBorder(true).SetTitle(" WhatsApp Config ")
	channelConfigPages.AddPage("whatsapp", waForm, true, true)

	ircForm := updateIRCForm()
	ircForm.SetBorder(true).SetTitle(" IRC Config ")
	channelConfigPages.AddPage("irc", ircForm, true, false)

	channelSelector.AddItem("WhatsApp", "", 0, func() {
		channelConfigPages.SwitchToPage("whatsapp")
		app.SetFocus(waForm)
	})
	channelSelector.AddItem("IRC", "", 0, func() {
		channelConfigPages.SwitchToPage("irc")
		app.SetFocus(ircForm)
	})

	channelsFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(channelsDoc, 7, 0, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(channelSelector, 20, 0, true).
			AddItem(channelConfigPages, 0, 1, false), 0, 1, true)
	pages.AddPage("Channels", channelsFlex, true, true)

	// Config Page
	configDoc := tview.NewTextView().SetDynamicColors(true).SetChangedFunc(func() { app.Draw() })
	configDoc.SetBorder(true).SetTitle(" Configuration Overview ")
	configDoc.SetText(`[green]Miri Configuration:[-]
- Use [yellow]Select Category[-] on the left to choose what to configure.
- [yellow]LLM:[-] API keys and model selection for xAI/Grok.
- [yellow]Gateway:[-] Server address, security key, and service controls (Start/Stop).
- [yellow]Agent:[-] General agent behavior and reasoning settings.`)

	configSelector := tview.NewList().ShowSecondaryText(false)
	configSelector.SetBorder(true).SetTitle(" Select Category ")

	configCategoryPages := tview.NewPages()

	updateLLMForm := func() *tview.Form {
		return tview.NewForm().
			AddPasswordField("xAI API Key", cfg.Models.Providers["xai"].APIKey, 50, '*', func(val string) {
				prov := cfg.Models.Providers["xai"]
				prov.APIKey = val
				cfg.Models.Providers["xai"] = prov
			}).
			AddInputField("Primary Model", cfg.Agents.Defaults.Model.Primary, 50, nil, func(val string) { cfg.Agents.Defaults.Model.Primary = val }).
			AddButton("Save LLM Config", func() {
				if err := config.Save(cfg); err != nil {
					setStatus("Save failed: " + err.Error())
					return
				}
				if gw != nil {
					gw.UpdateConfig(cfg)
				}
				setStatus("LLM config saved")
			})
	}

	updateGatewayForm := func() *tview.Form {
		f := tview.NewForm().
			AddInputField("Server Addr", cfg.Server.Addr, 30, nil, func(val string) { cfg.Server.Addr = val }).
			AddInputField("Server Key", cfg.Server.Key, 30, nil, func(val string) { cfg.Server.Key = val }).
			AddButton("Save Gateway Config", func() {
				if err := config.Save(cfg); err != nil {
					setStatus("Save failed: " + err.Error())
					return
				}
				if gw != nil {
					gw.UpdateConfig(cfg)
				}
				setStatus("Gateway config saved")
			})

		// Add Server controls directly here
		f.AddButton("Start Server", func() {
			if err := initGateway(); err != nil {
				setStatus("Start failed: " + err.Error())
				return
			}
			setStatus("Gateway running")
			refreshGatewayInfo()
		}).
			AddButton("Stop Server", func() { stopGateway(); setStatus("Gateway stopped"); refreshGatewayInfo() }).
			AddButton("Restart Server", func() {
				if err := restartGateway(); err != nil {
					setStatus("Restart failed: " + err.Error())
				} else {
					setStatus("Gateway restarted")
					refreshGatewayInfo()
				}
			}).
			AddButton("Refresh Status", func() { refreshGatewayInfo() })
		return f
	}

	llmForm := updateLLMForm()
	llmForm.SetBorder(true).SetTitle(" LLM Settings ")
	configCategoryPages.AddPage("llm", llmForm, true, true)

	gwForm := updateGatewayForm()
	gwForm.SetBorder(true).SetTitle(" Gateway Settings ")

	gwFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(gatewayStatusInfo, 8, 1, false).
		AddItem(gwForm, 0, 1, true)
	configCategoryPages.AddPage("gateway", gwFlex, true, false)

	configSelector.AddItem("LLM", "", 0, func() {
		configCategoryPages.SwitchToPage("llm")
		app.SetFocus(llmForm)
	})
	configSelector.AddItem("Gateway", "", 0, func() {
		configCategoryPages.SwitchToPage("gateway")
		app.SetFocus(gwForm)
	})

	configFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(configDoc, 7, 0, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(configSelector, 20, 0, true).
			AddItem(configCategoryPages, 0, 1, false), 0, 1, true)
	pages.AddPage("Config", configFlex, true, false)

	// Models Page
	modelsDoc := tview.NewTextView().SetDynamicColors(true).SetChangedFunc(func() { app.Draw() })
	modelsDoc.SetBorder(true).SetTitle(" Models Overview ")
	modelsDoc.SetText(`[green]Models Overview:[-]
- Manage LLM Providers (xAI, NVIDIA, etc.) and their specific models.
- Select a [yellow]Provider[-] from the list to edit its base URL, API key, and model list.
- Config is saved to ~/.miri/config.yaml.`)

	providerList := tview.NewList().ShowSecondaryText(false)
	providerList.SetBorder(true).SetTitle(" Providers ")

	modelConfigPages := tview.NewPages()

	// Helper to refresh provider list
	var refreshProviders func()
	var updateProviderForm func(pName string) *tview.Form

	updateModelForm := func(pName string, mIdx int) *tview.Form {
		prov := cfg.Models.Providers[pName]
		m := prov.Models[mIdx]
		form := tview.NewForm().
			AddInputField("Model ID", m.ID, 50, nil, func(val string) {
				p := cfg.Models.Providers[pName]
				p.Models[mIdx].ID = val
				cfg.Models.Providers[pName] = p
			}).
			AddInputField("Model Name", m.Name, 50, nil, func(val string) {
				p := cfg.Models.Providers[pName]
				p.Models[mIdx].Name = val
				cfg.Models.Providers[pName] = p
			}).
			AddCheckbox("Reasoning", m.Reasoning, func(checked bool) {
				p := cfg.Models.Providers[pName]
				p.Models[mIdx].Reasoning = checked
				cfg.Models.Providers[pName] = p
			}).
			AddInputField("Input Types (comma sep)", strings.Join(m.Input, ","), 30, nil, func(val string) {
				p := cfg.Models.Providers[pName]
				if val == "" {
					p.Models[mIdx].Input = []string{}
				} else {
					p.Models[mIdx].Input = strings.Split(val, ",")
				}
				cfg.Models.Providers[pName] = p
			}).
			AddInputField("Context Window", fmt.Sprintf("%d", m.ContextWindow), 20, nil, func(val string) {
				if v, err := strconv.Atoi(val); err == nil {
					p := cfg.Models.Providers[pName]
					p.Models[mIdx].ContextWindow = v
					cfg.Models.Providers[pName] = p
				}
			}).
			AddInputField("Max Tokens", fmt.Sprintf("%d", m.MaxTokens), 20, nil, func(val string) {
				if v, err := strconv.Atoi(val); err == nil {
					p := cfg.Models.Providers[pName]
					p.Models[mIdx].MaxTokens = v
					cfg.Models.Providers[pName] = p
				}
			}).
			AddInputField("Cost Input", fmt.Sprintf("%g", m.Cost.Input), 20, nil, func(val string) {
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					p := cfg.Models.Providers[pName]
					p.Models[mIdx].Cost.Input = v
					cfg.Models.Providers[pName] = p
				}
			}).
			AddInputField("Cost Output", fmt.Sprintf("%g", m.Cost.Output), 20, nil, func(val string) {
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					p := cfg.Models.Providers[pName]
					p.Models[mIdx].Cost.Output = v
					cfg.Models.Providers[pName] = p
				}
			}).
			AddInputField("Cost Cache Read", fmt.Sprintf("%g", m.Cost.CacheRead), 20, nil, func(val string) {
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					p := cfg.Models.Providers[pName]
					p.Models[mIdx].Cost.CacheRead = v
					cfg.Models.Providers[pName] = p
				}
			}).
			AddInputField("Cost Cache Write", fmt.Sprintf("%g", m.Cost.CacheWrite), 20, nil, func(val string) {
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					p := cfg.Models.Providers[pName]
					p.Models[mIdx].Cost.CacheWrite = v
					cfg.Models.Providers[pName] = p
				}
			})

		form.AddButton("Back to Provider", func() {
			modelConfigPages.SwitchToPage(pName)
			app.SetFocus(modelConfigPages)
		}).AddButton("Save All", func() {
			if err := config.Save(cfg); err != nil {
				setStatus("Save failed: " + err.Error())
			} else {
				if gw != nil {
					gw.UpdateConfig(cfg)
				}
				setStatus("Config saved")
			}
		})
		return form
	}

	updateProviderForm = func(pName string) *tview.Form {
		prov := cfg.Models.Providers[pName]
		form := tview.NewForm().
			AddInputField("Provider Name", pName, 20, nil, nil). // Read-only for now
			AddInputField("Base URL", prov.BaseURL, 60, nil, func(val string) {
				p := cfg.Models.Providers[pName]
				p.BaseURL = val
				cfg.Models.Providers[pName] = p
			}).
			AddPasswordField("API Key", prov.APIKey, 60, '*', func(val string) {
				p := cfg.Models.Providers[pName]
				p.APIKey = val
				cfg.Models.Providers[pName] = p
			}).
			AddInputField("API Type", prov.API, 20, nil, func(val string) {
				p := cfg.Models.Providers[pName]
				p.API = val
				cfg.Models.Providers[pName] = p
			})

		// Add model selection buttons
		for i, m := range prov.Models {
			mIdx := i
			mName := m.Name
			form.AddButton("Edit Model: "+mName, func() {
				mForm := updateModelForm(pName, mIdx)
				mForm.SetBorder(true).SetTitle(fmt.Sprintf(" %s - %s Config ", pName, mName))
				pageKey := fmt.Sprintf("%s_model_%d", pName, mIdx)
				modelConfigPages.AddPage(pageKey, mForm, true, true)
				modelConfigPages.SwitchToPage(pageKey)
				app.SetFocus(mForm)
			})
		}

		form.AddButton("Save Provider", func() {
			if err := config.Save(cfg); err != nil {
				setStatus("Save failed: " + err.Error())
			} else {
				if gw != nil {
					gw.UpdateConfig(cfg)
				}
				setStatus(fmt.Sprintf("Provider %s saved", pName))
			}
		}).AddButton("Delete Provider", func() {
			delete(cfg.Models.Providers, pName)
			config.Save(cfg)
			refreshProviders()
			setStatus(fmt.Sprintf("Provider %s deleted", pName))
		})

		return form
	}

	refreshProviders = func() {
		providerList.Clear()
		sortedProviders := slices.Sorted(maps.Keys(cfg.Models.Providers))
		for _, p := range sortedProviders {
			pName := p
			providerList.AddItem(pName, "", 0, func() {
				form := updateProviderForm(pName)
				form.SetBorder(true).SetTitle(fmt.Sprintf(" %s Config ", pName))
				modelConfigPages.AddPage(pName, form, true, true)
				modelConfigPages.SwitchToPage(pName)
				app.SetFocus(form)
			})
		}
		providerList.AddItem("[green]+ Add Provider[-]", "", 'a', func() {
			newPForm := tview.NewForm().
				AddInputField("Name", "", 20, nil, nil).
				AddInputField("Base URL", "", 60, nil, nil).
				AddButton("Create", func() {
					// Minimal implementation for now
					setStatus("Provider creation via TUI coming soon. Edit config.yaml directly for now.")
				})
			newPForm.SetBorder(true).SetTitle(" New Provider ")
			modelConfigPages.AddPage("_new_", newPForm, true, true)
			modelConfigPages.SwitchToPage("_new_")
			app.SetFocus(newPForm)
		})
	}

	refreshProviders()

	modelsFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(modelsDoc, 6, 0, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(providerList, 20, 0, true).
			AddItem(modelConfigPages, 0, 1, false), 0, 1, true)
	pages.AddPage("Models", modelsFlex, true, false)

	// Install Page
	installDoc := tview.NewTextView().SetDynamicColors(true).SetChangedFunc(func() { app.Draw() })
	installDoc.SetBorder(true).SetTitle(" Installation Overview ")
	installDoc.SetText(`[green]Service Installation:[-]
- This segment allows you to install Miri as a background service.
- [yellow]macOS:[-] Installs a launchd agent in ~/Library/LaunchAgents.
- [yellow]Linux:[-] Generates a systemd service file in your storage directory.`)

	installSelector := tview.NewList().ShowSecondaryText(false)
	installSelector.SetBorder(true).SetTitle(" Select Target ")

	installPages := tview.NewPages()

	updateInstallForm := func() *tview.Form {
		return tview.NewForm().
			AddButton("Install Service", func() {
				installStatus.SetText("Installing...")
				home, _ := os.UserHomeDir()
				binPath, err := os.Executable()
				if err != nil {
					installStatus.SetText("Error finding executable: " + err.Error())
					return
				}
				goos := os.Getenv("GOOS")
				if goos == "" {
					goos = runtime.GOOS
				}
				switch goos {
				case "darwin":
					plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.miri.agent.plist")
					templatePath := filepath.Join("templates", "launchd", "com.miri.agent.plist")
					data, err := os.ReadFile(templatePath)
					if err != nil {
						installStatus.SetText("Template not found: " + err.Error())
						return
					}
					content := strings.ReplaceAll(string(data), "/usr/local/bin/miri", binPath)
					content = strings.ReplaceAll(content, "$HOME", home)
					_ = os.MkdirAll(filepath.Dir(plistPath), 0o755)
					if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
						installStatus.SetText("Write failed: " + err.Error())
					} else {
						installStatus.SetText("Installed! Run: launchctl load " + plistPath)
					}
				case "linux":
					servicePath := filepath.Join(cfg.StorageDir, "miri.service")
					templatePath := filepath.Join("templates", "systemd", "miri.service")
					data, err := os.ReadFile(templatePath)
					if err != nil {
						installStatus.SetText("Template not found: " + err.Error())
						return
					}
					content := strings.ReplaceAll(string(data), "%h/bin/miri", binPath)
					user := os.Getenv("USER")
					content = strings.ReplaceAll(content, "%i", user)
					if err := os.WriteFile(servicePath, []byte(content), 0o644); err != nil {
						installStatus.SetText("Write failed: " + err.Error())
					} else {
						installStatus.SetText("Generated " + servicePath + ". Copy to /etc/systemd/system/")
					}
				default:
					installStatus.SetText("Unsupported OS: " + goos)
				}
			})
	}

	iForm := updateInstallForm()
	iForm.SetBorder(true).SetTitle(" Service Installation ")
	installPages.AddPage("service", iForm, true, true)

	installSelector.AddItem("Service", "Install background service", 0, func() {
		installPages.SwitchToPage("service")
		app.SetFocus(iForm)
	})

	installFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(installDoc, 7, 0, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(installSelector, 20, 0, true).
			AddItem(installPages, 0, 1, false), 0, 1, true).
		AddItem(installStatus, 3, 0, false)
	pages.AddPage("Install", installFlex, true, false)

	// Layout: nav | pages
	layout := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nav, 24, 0, true).
		AddItem(pages, 0, 1, false)

	// Focus management: Tab / Shift+Tab to switch between Nav and Pages
	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Key() == tcell.KeyTab {
			if app.GetFocus() == nav {
				app.SetFocus(pages)
			} else {
				app.SetFocus(nav)
			}
			return nil
		}
		if ev.Key() == tcell.KeyBacktab {
			if app.GetFocus() == nav {
				app.SetFocus(pages)
			} else {
				app.SetFocus(nav)
			}
			return nil
		}
		if ev.Key() == tcell.KeyCtrlC || ev.Rune() == 'q' || ev.Rune() == 'Q' {
			cancel()
			app.Stop()
			return nil
		}
		return ev
	})

	// Nav behavior
	addNav := func(name string) {
		nav.AddItem(name, "", 0, func() {
			pages.SwitchToPage(name)
			// Focus behavior: when entering segments, focus their respective selector list first
			switch name {
			case "Channels":
				app.SetFocus(channelSelector)
			case "Models":
				app.SetFocus(providerList)
			case "Config":
				app.SetFocus(configSelector)
			case "Install":
				app.SetFocus(installSelector)
			default:
				app.SetFocus(pages)
			}
		})
	}
	addNav("Channels")
	addNav("Models")
	addNav("Config")
	addNav("Install")
	nav.SetCurrentItem(0)
	pages.SwitchToPage("Channels")
	app.SetFocus(channelSelector)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(layout, 0, 1, true).
		AddItem(statusBar, 1, 0, false)

	if err := app.SetRoot(root, true).EnableMouse(true).Run(); err != nil {
		return err
	}
	return nil
}
