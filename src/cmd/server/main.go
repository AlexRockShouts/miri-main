//go:build !test

package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"miri-main/src/internal/api"
	"miri-main/src/internal/config"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/storage"
	"miri-main/src/internal/system"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

//go:embed all:dashboard
var embeddedDashboard embed.FS

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "path to config file to load first")

	var setupFlag bool
	flag.BoolVar(&setupFlag, "setup", false, "Run interactive setup wizard to configure Miri")
	var resetFlag bool
	flag.BoolVar(&resetFlag, "reset-config", false, "Delete config.yaml and run setup wizard")

	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	homeDir, _ := os.UserHomeDir()
	appDir := filepath.Join(homeDir, ".miri")
	defaultConfigPath := filepath.Join(appDir, "config.yaml")

	if resetFlag {
		if err := os.Remove(defaultConfigPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Error("failed to reset config", "error", err)
			os.Exit(1)
		}
		slog.Info("config reset complete")
	}

	cfgPath := configFile
	if cfgPath == "" {
		cfgPath = defaultConfigPath
	}

	cfg, err := config.Load(cfgPath)
	if err != nil || setupFlag || (cfg.Models.Providers == nil || len(cfg.Models.Providers) == 0) {
		slog.Info("running setup wizard")
		if err := runSetupWizard(os.Stdin, cfg, cfgPath, homeDir); err != nil {
			slog.Error("setup wizard failed", "error", err)
			os.Exit(1)
		}
		cfg, err = config.Load(cfgPath)
		if err != nil {
			slog.Error("failed to load generated config", "error", err)
			os.Exit(1)
		}
	}

	s, err := storage.New(cfg.StorageDir)
	if err != nil {
		slog.Error("failed to initialize storage", "error", err)
		os.Exit(1)
	}

	// PID file management
	pidPath := filepath.Join(cfg.StorageDir, "miri.pid")

	// Check if already running
	if pidBytes, err := os.ReadFile(pidPath); err == nil {
		pidStr := strings.TrimSpace(string(pidBytes))
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			p, _ := os.FindProcess(pid)
			if p.Signal(syscall.Signal(0)) == nil {
				slog.Error("miri already running", "pid", pid, "pidfile", pidPath)
				os.Exit(1)
			}
			// Stale PID: clean up
			if err := os.Remove(pidPath); err != nil {
				slog.Warn("failed to remove stale pidfile", "path", pidPath, "error", err)
			} else {
				slog.Info("cleaned stale pidfile", "pid", pid)
			}
		}
	}

	// Write current PID to file
	pidFile, err := os.OpenFile(pidPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		slog.Error("failed to create pidfile", "path", pidPath, "error", err)
		os.Exit(1)
	}
	defer pidFile.Close()

	if _, err := fmt.Fprintf(pidFile, "%d\n", os.Getpid()); err != nil {
		slog.Error("failed to write pidfile", "path", pidPath, "error", err)
		os.Exit(1)
	}

	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			slog.Error("failed to remove pidfile", "path", name, "error", err)
		}
	}(pidPath)

	// Warn if non-loopback bind without key (validation in config.Load)
	isLoopback := cfg.Server.EffectiveHost == "127.0.0.1" || cfg.Server.EffectiveHost == "localhost" || cfg.Server.EffectiveHost == "::1" || cfg.Server.EffectiveHost == "[::1]"
	if !isLoopback && cfg.Server.Key == "" {
		slog.Warn("binding to non-loopback address without server key; recommend setting config.server.key", "host", cfg.Server.EffectiveHost)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gw := gateway.New(cfg, s)

	// Copy skills from templates
	templateSkillsPath := filepath.Join(system.GetProjectRoot(), "templates", "skills")
	if err := s.CopySkills(templateSkillsPath); err != nil {
		slog.Warn("failed to copy skills from templates", "error", err)
	} else {
		slog.Info("copied skills from templates")
	}

	gw.StartEngine(ctx)

	system.LogMemoryUsage("post_engine_start")

	server := api.NewServer(gw)
	dashboardFS, err := fs.Sub(embeddedDashboard, "dashboard")
	if err == nil {
		server.DashboardFS = dashboardFS
	} else {
		slog.Warn("failed to load embedded dashboard", "error", err)
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		slog.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	slog.Info("starting agent service", "addr", cfg.Server.Addr)
	if err := server.ListenAndServe(ctx, cfg.Server.Addr); err != nil {
		slog.Error("server ListenAndServe failed", "error", err)
		os.Exit(1)
	}
}

func getProviderBaseURL(provider string) string {
	switch provider {
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	default:
		return "https://api.x.ai/v1"
	}
}

func runSetupWizard(stdin io.Reader, cfg *config.Config, cfgPath string, homeDir string) error {
	reader := bufio.NewReader(stdin)

	fmt.Println("\n=== Miri Setup Wizard ===")
	fmt.Println("Configure LLM provider, API key, model, and basics.")

	// Provider
	fmt.Print("LLM Provider (xai/openai/anthropic/groq) [xai]: ")
	provLine, _ := reader.ReadString('\n')
	provider := strings.TrimSpace(provLine)
	if provider == "" {
		provider = "xai"
	}
	provider = strings.ToLower(provider)

	// API Key
	fmt.Print("Enter API Key/Token: ")
	keyLine, _ := reader.ReadString('\n')
	apiKey := strings.TrimSpace(keyLine)

	// Model
	fmt.Print("Default Model [grok-beta]: ")
	modelLine, _ := reader.ReadString('\n')
	model := strings.TrimSpace(modelLine)
	if model == "" {
		model = "grok-beta"
	}

	// Storage Dir
	fmt.Print("Storage Directory [~/.miri]: ")
	storLine, _ := reader.ReadString('\n')
	storDir := strings.TrimSpace(storLine)
	if storDir == "" {
		storDir = filepath.Join(homeDir, ".miri")
	}

	// Server Addr
	fmt.Print("Server Address [:8080]: ")
	addrLine, _ := reader.ReadString('\n')
	addr := strings.TrimSpace(addrLine)
	if addr == "" {
		addr = ":8080"
	}

	// Server Key
	fmt.Print("Server Key [devkey123]: ")
	keyLine2, _ := reader.ReadString('\n')
	serverKey := strings.TrimSpace(keyLine2)
	if serverKey == "" {
		serverKey = "devkey123"
	}

	// Admin User
	fmt.Print("Admin Username [admin]: ")
	userLine, _ := reader.ReadString('\n')
	adminUser := strings.TrimSpace(userLine)
	if adminUser == "" {
		adminUser = "admin"
	}

	// Admin Password
	fmt.Print("Admin Password [admin]: ")
	passLine, _ := reader.ReadString('\n')
	adminPass := strings.TrimSpace(passLine)
	if adminPass == "" {
		adminPass = "admin"
	}

	// Populate cfg
	cfg.StorageDir = storDir
	cfg.Server.Addr = addr
	cfg.Server.Key = serverKey
	cfg.Server.AdminUser = adminUser
	cfg.Server.AdminPass = adminPass

	baseURL := getProviderBaseURL(provider)

	modelCfg := config.ModelConfig{
		ID:            model,
		Name:          model,
		ContextWindow: 131072,
		MaxTokens:     8192,
		Reasoning:     true,
		Input:         []string{"text"},
		Cost: config.ModelCost{
			Input:      0.15,
			Output:     0.45,
			CacheRead:  0.075,
			CacheWrite: 0.225,
		},
	}

	provCfg := config.ProviderConfig{
		BaseURL: baseURL,
		APIKey:  apiKey,
		API:     "openai",
		Models:  []config.ModelConfig{modelCfg},
	}

	cfg.Models = config.ModelsConfig{
		Mode:      "chat",
		Providers: map[string]config.ProviderConfig{provider: provCfg},
	}

	cfg.Agents.Defaults.Model.Primary = model

	// Minimal defaults
	cfg.Miri.Brain.MaxNodesPerSession = 50
	cfg.Miri.Brain.Retrieval.GraphSteps = 5
	cfg.Miri.Brain.Retrieval.FactsTopK = 20
	cfg.Miri.Brain.Retrieval.SummariesTopK = 10
	cfg.Agents.SubAgents = 3

	// Save
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Setup complete! Config saved to %s\n", cfgPath)
	fmt.Println("You can edit config.yaml manually or restart the server.")
	return nil
}
