package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Models     ModelsConfig   `mapstructure:"models" json:"models"`
	Agents     AgentsConfig   `mapstructure:"agents" json:"agents"`
	Server     ServerConfig   `mapstructure:"server" json:"server"`
	StorageDir string         `mapstructure:"storage_dir" json:"storage_dir"`
	Tools      ToolsConfig    `mapstructure:"tools" json:"tools"`
	Channels   ChannelsConfig `mapstructure:"channels" json:"channels"`
}

type ModelsConfig struct {
	Mode      string                    `mapstructure:"mode" json:"mode"`
	Providers map[string]ProviderConfig `mapstructure:"providers" json:"providers"`
}

type ProviderConfig struct {
	BaseURL string        `mapstructure:"baseUrl" json:"baseUrl"`
	APIKey  string        `mapstructure:"apiKey" json:"apiKey,omitempty"`
	API     string        `mapstructure:"api" json:"api"`
	Models  []ModelConfig `mapstructure:"models" json:"models"`
}

type ModelConfig struct {
	ID            string `mapstructure:"id" json:"id"`
	Name          string `mapstructure:"name" json:"name"`
	ContextWindow int    `mapstructure:"contextWindow" json:"contextWindow"`
	MaxTokens     int    `mapstructure:"maxTokens" json:"maxTokens"`
	Reasoning     bool   `mapstructure:"reasoning" json:"reasoning"`
}

type AgentsConfig struct {
	Defaults  AgentDefaults `mapstructure:"defaults" json:"defaults"`
	SubAgents int           `mapstructure:"subagents" json:"subagents"`
	Debug     bool          `mapstructure:"debug" json:"debug"`
}

type AgentDefaults struct {
	Model  ModelSelection `mapstructure:"model" json:"model"`
	Engine string         `mapstructure:"engine" json:"engine"`
}

type ModelSelection struct {
	Primary   string   `mapstructure:"primary" json:"primary"`
	Fallbacks []string `mapstructure:"fallbacks" json:"fallbacks"`
}

type XAIConfig struct {
	APIKey string `mapstructure:"api_key" json:"api_key"`
	Model  string `mapstructure:"model" json:"model"`
}

type AgentConfig struct{}

type ServerConfig struct {
	Addr          string `mapstructure:"addr" json:"addr"`
	Key           string `mapstructure:"key" json:"key"`
	EffectiveHost string `mapstructure:"-" json:"effectiveHost"`
	Port          int    `mapstructure:"-" json:"port"`
}

type ToolsConfig struct {
	Enabled          bool `mapstructure:"enabled" json:"enabled"`
	WebSearchEnabled bool `mapstructure:"web_search_enabled" json:"web_search_enabled"`
	WebFetchEnabled  bool `mapstructure:"web_fetch_enabled" json:"web_fetch_enabled"`
	CronEnabled      bool `mapstructure:"cron_enabled" json:"cron_enabled"`
}

type WhatsappConfig struct {
	Enabled bool `mapstructure:"enabled" json:"enabled"`
}

type ChannelsConfig struct {
	Whatsapp WhatsappConfig `mapstructure:"whatsapp" json:"whatsapp"`
}

func Load(override string) (*Config, error) {

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	appDir := filepath.Join(home, ".miri")
	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		_ = os.MkdirAll(appDir, 0755)
	}

	// Environment overrides
	if envDir := os.Getenv("MIRI_STORAGE_DIR"); envDir != "" {
		appDir = envDir
		_ = os.MkdirAll(appDir, 0755)
	}

	if override != "" {
		viper.AddConfigPath(".")
		viper.SetConfigFile(override)
		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, err
			}
		}
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(appDir)
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Compute effective host/port from addr
	host, portStr, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		return nil, fmt.Errorf("invalid server.addr %q: %w", cfg.Server.Addr, err)
	}
	cfg.Server.EffectiveHost = host
	if cfg.Server.EffectiveHost == "" {
		cfg.Server.EffectiveHost = "0.0.0.0"
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port %q in server.addr %q: %w", portStr, cfg.Server.Addr, err)
	}
	cfg.Server.Port = p

	if cfg.StorageDir == "" {
		cfg.StorageDir = appDir
	}

	if strings.HasPrefix(cfg.StorageDir, "~/") {
		cfg.StorageDir = filepath.Join(home, cfg.StorageDir[2:])
	}

	// Override API keys from inline placeholders ($VAR) or default environment variables
	for p, prov := range cfg.Models.Providers {
		apiKey := prov.APIKey
		if strings.HasPrefix(apiKey, "$") {
			varName := strings.TrimPrefix(apiKey, "$")
			if envVal := os.Getenv(varName); envVal != "" {
				apiKey = envVal
			} else {
				apiKey = ""
			}
		} else if apiKey == "" {
			varName := strings.ToUpper(p) + "_API_KEY"
			if envVal := os.Getenv(varName); envVal != "" {
				apiKey = envVal
			}
		}
		prov.APIKey = apiKey
		cfg.Models.Providers[p] = prov
	}

	return &cfg, nil
}

func Save(cfg *Config) error {
	// Ensure storage directory exists
	if _, err := os.Stat(cfg.StorageDir); os.IsNotExist(err) {
		if err := os.MkdirAll(cfg.StorageDir, 0755); err != nil {
			return err
		}
	}

	// Update viper state
	viper.Set("models.mode", cfg.Models.Mode)
	for p, prov := range cfg.Models.Providers {
		viper.Set("models.providers."+p+".baseUrl", prov.BaseURL)
		viper.Set("models.providers."+p+".apiKey", prov.APIKey)
		viper.Set("models.providers."+p+".api", prov.API)
		for i, m := range prov.Models {
			viper.Set("models.providers."+p+".models."+strconv.Itoa(i)+".id", m.ID)
			viper.Set("models.providers."+p+".models."+strconv.Itoa(i)+".name", m.Name)
			viper.Set("models.providers."+p+".models."+strconv.Itoa(i)+".contextWindow", m.ContextWindow)
			viper.Set("models.providers."+p+".models."+strconv.Itoa(i)+".maxTokens", m.MaxTokens)
			viper.Set("models.providers."+p+".models."+strconv.Itoa(i)+".reasoning", m.Reasoning)
		}
	}
	viper.Set("agents.defaults.model.primary", cfg.Agents.Defaults.Model.Primary)
	for i, fb := range cfg.Agents.Defaults.Model.Fallbacks {
		viper.Set("agents.defaults.model.fallbacks."+strconv.Itoa(i), fb)
	}
	viper.Set("server.addr", cfg.Server.Addr)
	viper.Set("server.key", cfg.Server.Key)
	viper.Set("tools.enabled", cfg.Tools.Enabled)
	viper.Set("tools.web_search_enabled", cfg.Tools.WebSearchEnabled)
	viper.Set("tools.web_fetch_enabled", cfg.Tools.WebFetchEnabled)
	viper.Set("tools.cron_enabled", cfg.Tools.CronEnabled)
	viper.Set("channels.whatsapp.enabled", cfg.Channels.Whatsapp.Enabled)
	viper.Set("storage_dir", cfg.StorageDir)

	configPath := filepath.Join(cfg.StorageDir, "config.yaml")
	viper.SetConfigType("yaml")
	return viper.WriteConfigAs(configPath)
}
