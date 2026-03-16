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
	Miri       MiriConfig     `mapstructure:"miri" json:"miri"`
	StorageDir string         `mapstructure:"storage_dir" json:"storage_dir"`
	Channels   ChannelsConfig `mapstructure:"channels" json:"channels"`
}

type MiriConfig struct {
	Brain   BrainConfig   `mapstructure:"brain" json:"brain"`
	KeePass KeePassConfig `mapstructure:"keepass" json:"keepass"`
}

type KeePassConfig struct {
	DBPath   string `mapstructure:"db_path" json:"db_path"`
	Password string `mapstructure:"password" json:"password"`
}

type BrainConfig struct {
	Embeddings         EmbeddingConfig `mapstructure:"embeddings" json:"embeddings"`
	Retrieval          RetrievalConfig `mapstructure:"retrieval" json:"retrieval"`
	MaxNodesPerSession int             `mapstructure:"max_nodes_per_session" json:"max_nodes_per_session"`
}

type RetrievalConfig struct {
	GraphSteps    int `mapstructure:"graph_steps" json:"graph_steps"`
	FactsTopK     int `mapstructure:"facts_top_k" json:"facts_top_k"`
	SummariesTopK int `mapstructure:"summaries_top_k" json:"summaries_top_k"`
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
	ID            string    `mapstructure:"id" json:"id"`
	Name          string    `mapstructure:"name" json:"name"`
	ContextWindow int       `mapstructure:"contextWindow" json:"contextWindow"`
	MaxTokens     int       `mapstructure:"maxTokens" json:"maxTokens"`
	Reasoning     bool      `mapstructure:"reasoning" json:"reasoning"`
	Input         []string  `mapstructure:"input" json:"input"`
	Cost          ModelCost `mapstructure:"cost" json:"cost"`
}

type ModelCost struct {
	Input      float64 `mapstructure:"input" json:"input"`
	Output     float64 `mapstructure:"output" json:"output"`
	CacheRead  float64 `mapstructure:"cacheRead" json:"cacheRead"`
	CacheWrite float64 `mapstructure:"cacheWrite" json:"cacheWrite"`
}

type AgentsConfig struct {
	Defaults  AgentDefaults `mapstructure:"defaults" json:"defaults"`
	SubAgents int           `mapstructure:"subagents" json:"subagents"`
	Debug     bool          `mapstructure:"debug" json:"debug"`
}

type AgentDefaults struct {
	Model ModelSelection `mapstructure:"model" json:"model"`
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
	Host          string `mapstructure:"host" json:"host"`
	AdminUser     string `mapstructure:"admin_user" json:"admin_user"`
	AdminPass     string `mapstructure:"admin_pass" json:"admin_pass"`
	EffectiveHost string `mapstructure:"-" json:"effectiveHost"`
	Port          int    `mapstructure:"-" json:"port"`
}

type EmbeddingConfig struct {
	UseNativeEmbeddings bool                 `mapstructure:"use_native_embeddings" json:"use_native_embeddings"`
	Model               EmbeddingModelConfig `mapstructure:"model" json:"model"`
}

type EmbeddingModelConfig struct {
	Type   string `mapstructure:"type" json:"type"`
	APIKey string `mapstructure:"api_key" json:"api_key"`
	Model  string `mapstructure:"model" json:"model"`
	URL    string `mapstructure:"url" json:"url"`
}

type WhatsappConfig struct {
	Enabled   bool     `mapstructure:"enabled" json:"enabled"`
	Allowlist []string `mapstructure:"allowlist" json:"allowlist"`
	Blocklist []string `mapstructure:"blocklist" json:"blocklist"`
}

type IRCConfig struct {
	Enabled   bool           `mapstructure:"enabled" json:"enabled"`
	Host      string         `mapstructure:"host" json:"host"`
	Port      int            `mapstructure:"port" json:"port"`
	TLS       bool           `mapstructure:"tls" json:"tls"`
	Nick      string         `mapstructure:"nick" json:"nick"`
	User      string         `mapstructure:"user" json:"user"`
	Realname  string         `mapstructure:"realname" json:"realname"`
	Channels  []string       `mapstructure:"channels" json:"channels"`
	Password  *string        `mapstructure:"password" json:"password"`
	NickServ  NickServConfig `mapstructure:"nickserv" json:"nickserv"`
	Allowlist []string       `mapstructure:"allowlist" json:"allowlist"`
	Blocklist []string       `mapstructure:"blocklist" json:"blocklist"`
}

type NickServConfig struct {
	Enabled  bool   `mapstructure:"enabled" json:"enabled"`
	Password string `mapstructure:"password" json:"password"`
}

type ChannelsConfig struct {
	Whatsapp WhatsappConfig `mapstructure:"whatsapp" json:"whatsapp"`
	IRC      IRCConfig      `mapstructure:"irc" json:"irc"`
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

	// Expansion for embedding API key
	embKey := cfg.Miri.Brain.Embeddings.Model.APIKey
	if strings.HasPrefix(embKey, "$") {
		varName := strings.TrimPrefix(embKey, "$")
		if envVal := os.Getenv(varName); envVal != "" {
			embKey = envVal
		} else {
			embKey = ""
		}
	}
	cfg.Miri.Brain.Embeddings.Model.APIKey = embKey

	// Expansion for KeePass password
	kpPass := cfg.Miri.KeePass.Password
	if strings.HasPrefix(kpPass, "$") {
		varName := strings.TrimPrefix(kpPass, "$")
		if envVal := os.Getenv(varName); envVal != "" {
			kpPass = envVal
		} else {
			kpPass = ""
		}
	}
	cfg.Miri.KeePass.Password = kpPass

	// Expansion for KeePass DB path
	kpPath := cfg.Miri.KeePass.DBPath
	if strings.HasPrefix(kpPath, "~/") {
		kpPath = filepath.Join(home, kpPath[2:])
	}
	cfg.Miri.KeePass.DBPath = kpPath

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
	viper.Set("storage_dir", cfg.StorageDir)

	// Models
	viper.Set("models.mode", cfg.Models.Mode)
	for p, prov := range cfg.Models.Providers {
		prefix := "models.providers." + p
		viper.Set(prefix+".baseUrl", prov.BaseURL)
		viper.Set(prefix+".apiKey", prov.APIKey)
		viper.Set(prefix+".api", prov.API)
		for i, m := range prov.Models {
			mPrefix := prefix + ".models." + strconv.Itoa(i)
			viper.Set(mPrefix+".id", m.ID)
			viper.Set(mPrefix+".name", m.Name)
			viper.Set(mPrefix+".contextWindow", m.ContextWindow)
			viper.Set(mPrefix+".maxTokens", m.MaxTokens)
			viper.Set(mPrefix+".reasoning", m.Reasoning)
			viper.Set(mPrefix+".input", m.Input)
			viper.Set(mPrefix+".cost.input", m.Cost.Input)
			viper.Set(mPrefix+".cost.output", m.Cost.Output)
			viper.Set(mPrefix+".cost.cacheRead", m.Cost.CacheRead)
			viper.Set(mPrefix+".cost.cacheWrite", m.Cost.CacheWrite)
		}
	}

	// Agents
	viper.Set("agents.defaults.model.primary", cfg.Agents.Defaults.Model.Primary)
	for i, fb := range cfg.Agents.Defaults.Model.Fallbacks {
		viper.Set("agents.defaults.model.fallbacks."+strconv.Itoa(i), fb)
	}
	viper.Set("agents.subagents", cfg.Agents.SubAgents)
	viper.Set("agents.debug", cfg.Agents.Debug)

	// Server
	viper.Set("server.addr", cfg.Server.Addr)
	viper.Set("server.key", cfg.Server.Key)
	viper.Set("server.host", cfg.Server.Host)
	viper.Set("server.admin_user", cfg.Server.AdminUser)
	viper.Set("server.admin_pass", cfg.Server.AdminPass)

	// Miri Brain
	viper.Set("miri.brain.embeddings.use_native_embeddings", cfg.Miri.Brain.Embeddings.UseNativeEmbeddings)
	viper.Set("miri.brain.embeddings.model.type", cfg.Miri.Brain.Embeddings.Model.Type)
	viper.Set("miri.brain.embeddings.model.api_key", cfg.Miri.Brain.Embeddings.Model.APIKey)
	viper.Set("miri.brain.embeddings.model.model", cfg.Miri.Brain.Embeddings.Model.Model)
	viper.Set("miri.brain.embeddings.model.url", cfg.Miri.Brain.Embeddings.Model.URL)
	viper.Set("miri.brain.retrieval.graph_steps", cfg.Miri.Brain.Retrieval.GraphSteps)
	viper.Set("miri.brain.retrieval.facts_top_k", cfg.Miri.Brain.Retrieval.FactsTopK)
	viper.Set("miri.brain.retrieval.summaries_top_k", cfg.Miri.Brain.Retrieval.SummariesTopK)
	viper.Set("miri.brain.max_nodes_per_session", cfg.Miri.Brain.MaxNodesPerSession)

	// Miri KeePass
	viper.Set("miri.keepass.db_path", cfg.Miri.KeePass.DBPath)
	viper.Set("miri.keepass.password", cfg.Miri.KeePass.Password)

	// Channels
	viper.Set("channels.whatsapp.enabled", cfg.Channels.Whatsapp.Enabled)
	viper.Set("channels.whatsapp.allowlist", cfg.Channels.Whatsapp.Allowlist)
	viper.Set("channels.whatsapp.blocklist", cfg.Channels.Whatsapp.Blocklist)
	viper.Set("channels.irc.enabled", cfg.Channels.IRC.Enabled)
	viper.Set("channels.irc.host", cfg.Channels.IRC.Host)
	viper.Set("channels.irc.port", cfg.Channels.IRC.Port)
	viper.Set("channels.irc.tls", cfg.Channels.IRC.TLS)
	viper.Set("channels.irc.nick", cfg.Channels.IRC.Nick)
	viper.Set("channels.irc.user", cfg.Channels.IRC.User)
	viper.Set("channels.irc.realname", cfg.Channels.IRC.Realname)
	viper.Set("channels.irc.channels", cfg.Channels.IRC.Channels)
	viper.Set("channels.irc.password", cfg.Channels.IRC.Password)
	viper.Set("channels.irc.nickserv.enabled", cfg.Channels.IRC.NickServ.Enabled)
	viper.Set("channels.irc.nickserv.password", cfg.Channels.IRC.NickServ.Password)
	viper.Set("channels.irc.allowlist", cfg.Channels.IRC.Allowlist)
	viper.Set("channels.irc.blocklist", cfg.Channels.IRC.Blocklist)

	configPath := filepath.Join(cfg.StorageDir, "config.yaml")
	viper.SetConfigType("yaml")
	return viper.WriteConfigAs(configPath)
}
