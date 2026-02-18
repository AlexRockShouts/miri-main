package main

import (
	"context"
	"fmt"
	"log/slog"
	"miri-main/src/internal/api"
	"miri-main/src/internal/config"
	"miri-main/src/internal/gateway"
	"miri-main/src/internal/storage"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	s, err := storage.New(cfg.StorageDir)
	if err != nil {
		slog.Error("failed to initialize storage", "error", err)
		os.Exit(1)
	}

	soulPath := filepath.Join(cfg.StorageDir, "soul.txt")
	if _, err := os.Stat(soulPath); os.IsNotExist(err) {
		templatePath := filepath.Join(".", "templates", "soul.txt")
		templateData, err := os.ReadFile(templatePath)
		if err != nil {
			slog.Warn("failed to read project template soul.txt", "error", err)
			return
		}
		if err := os.WriteFile(soulPath, templateData, 0644); err != nil {
			slog.Warn("failed to bootstrap soul.txt from template", "error", err)
		} else {
			slog.Info("bootstrapped soul.txt from project template")
		}
	}

	// PID file management
	pidPath := filepath.Join(cfg.StorageDir, "miri.pid")

	// Check if already running
	if pidBytes, err := os.ReadFile(pidPath); err == nil {
		pidStr := strings.TrimSpace(string(pidBytes))
		if pid, err := strconv.Atoi(pidStr); err == nil {
			if proc, err := os.FindProcess(pid); err == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					slog.Error("miri already running", "pid", pid, "pidfile", pidPath)
					os.Exit(1)
				}
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

	gw := gateway.New(cfg, s)

	server := api.NewServer(gw)

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
