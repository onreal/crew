package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	App       AppConfig                     `mapstructure:"app" json:"app"`
	Session   SessionConfig                 `mapstructure:"session" json:"session"`
	Storage   StorageConfig                 `mapstructure:"storage" json:"storage"`
	Providers map[string]TextProviderConfig `mapstructure:"providers" json:"providers"`
	Sandbox   SandboxConfig                 `mapstructure:"sandbox" json:"sandbox"`
	Vector    VectorConfig                  `mapstructure:"vector" json:"vector"`
	Runtime   RuntimeConfig                 `mapstructure:"runtime" json:"runtime"`
	UI        UIConfig                      `mapstructure:"ui" json:"ui"`
}

type AppConfig struct {
	Name        string `mapstructure:"name" json:"name"`
	Environment string `mapstructure:"environment" json:"environment"`
	LogLevel    string `mapstructure:"log_level" json:"log_level"`
}

type SessionConfig struct {
	Mode              string `mapstructure:"mode" json:"mode"`
	LoopProtection    bool   `mapstructure:"loop_protection" json:"loop_protection"`
	MaxTurns          int    `mapstructure:"max_turns" json:"max_turns"`
	DefaultAgentMode  string `mapstructure:"default_agent_mode" json:"default_agent_mode"`
	OrchestrationMode string `mapstructure:"orchestration_mode" json:"orchestration_mode"`
	ReplyRoutingMode  string `mapstructure:"reply_routing_mode" json:"reply_routing_mode"`
}

type StorageConfig struct {
	Driver string `mapstructure:"driver" json:"driver"`
	Path   string `mapstructure:"path" json:"path"`
}

type VectorConfig struct {
	Enabled            bool   `mapstructure:"enabled" json:"enabled"`
	Dimensions         int    `mapstructure:"dimensions" json:"dimensions"`
	Embedder           string `mapstructure:"embedder" json:"embedder"`
	DefaultRecallLimit int    `mapstructure:"default_recall_limit" json:"default_recall_limit"`
}

type TextProviderConfig struct {
	Binary           string  `mapstructure:"binary" json:"binary,omitempty"`
	WorkingDirectory string  `mapstructure:"working_directory" json:"working_directory,omitempty"`
	BaseURL          string  `mapstructure:"base_url" json:"base_url"`
	APIKey           string  `mapstructure:"api_key" json:"api_key"`
	APIKeyEnv        string  `mapstructure:"api_key_env" json:"api_key_env"`
	TimeoutMillis    int     `mapstructure:"timeout_millis" json:"timeout_millis"`
	Temperature      float64 `mapstructure:"temperature" json:"temperature"`
}

type SandboxConfig struct {
	DefaultProvider     string                           `mapstructure:"default_provider" json:"default_provider"`
	SourceWorkspaceRoot string                           `mapstructure:"source_workspace_root" json:"source_workspace_root"`
	PermissionProfile   string                           `mapstructure:"permission_profile" json:"permission_profile"`
	Providers           map[string]SandboxProviderConfig `mapstructure:"providers" json:"providers"`

	// Legacy single-provider fields are still accepted during the transition to
	// a named sandbox provider catalog. Normalize them into Providers.
	Provider      string `mapstructure:"provider" json:"provider,omitempty"`
	Binary        string `mapstructure:"binary" json:"binary,omitempty"`
	Model         string `mapstructure:"model" json:"model,omitempty"`
	WorkspaceRoot string `mapstructure:"workspace_root" json:"workspace_root,omitempty"`
	WorkspaceMode string `mapstructure:"workspace_mode" json:"workspace_mode,omitempty"`
	TimeoutMillis int    `mapstructure:"timeout_millis" json:"timeout_millis,omitempty"`
}

type SandboxProviderConfig struct {
	Binary          string   `mapstructure:"binary" json:"binary"`
	Model           string   `mapstructure:"model" json:"model"`
	WorkspaceRoot   string   `mapstructure:"workspace_root" json:"workspace_root"`
	WorkspaceMode   string   `mapstructure:"workspace_mode" json:"workspace_mode"`
	TimeoutMillis   int      `mapstructure:"timeout_millis" json:"timeout_millis"`
	AdditionalWrite []string `mapstructure:"additional_write" json:"additional_write"`
}

type RuntimeConfig struct {
	StatePath string `mapstructure:"state_path" json:"state_path"`
}

type UIConfig struct {
	RefreshIntervalMillis int               `mapstructure:"refresh_interval_millis" json:"refresh_interval_millis"`
	AttachAutoSteps       int               `mapstructure:"attach_auto_steps" json:"attach_auto_steps"`
	Theme                 string            `mapstructure:"theme" json:"theme"`
	ShowTimestamps        bool              `mapstructure:"show_timestamps" json:"show_timestamps"`
	CompactMessages       bool              `mapstructure:"compact_messages" json:"compact_messages"`
	AgentColors           map[string]string `mapstructure:"agent_colors" json:"agent_colors"`
}

type LoadedConfig struct {
	Config Config `json:"config"`
	Path   string `json:"path"`
}

func DefaultConfig() Config {
	return Config{
		App: AppConfig{
			Name:        "crew",
			Environment: "development",
			LogLevel:    "info",
		},
		Session: SessionConfig{
			Mode:              "free",
			LoopProtection:    true,
			MaxTurns:          64,
			DefaultAgentMode:  "reactive",
			OrchestrationMode: "deterministic",
			ReplyRoutingMode:  "reply_obligations",
		},
		Storage: StorageConfig{
			Driver: "sqlite",
			Path:   "./var/crew.db",
		},
		Providers: map[string]TextProviderConfig{
			"openai": {
				BaseURL:       "https://api.openai.com/v1",
				APIKey:        "",
				APIKeyEnv:     "OPENAI_API_KEY",
				TimeoutMillis: 30000,
				Temperature:   0.2,
			},
			"gemini": {
				BaseURL:       "https://generativelanguage.googleapis.com/v1beta/openai",
				APIKey:        "",
				APIKeyEnv:     "GEMINI_API_KEY",
				TimeoutMillis: 30000,
				Temperature:   0.2,
			},
			"grok": {
				BaseURL:       "https://api.x.ai/v1",
				APIKey:        "",
				APIKeyEnv:     "XAI_API_KEY",
				TimeoutMillis: 30000,
				Temperature:   0.2,
			},
			"codex": {
				Binary:           "codex",
				WorkingDirectory: ".",
				TimeoutMillis:    0,
			},
		},
		Sandbox: SandboxConfig{
			DefaultProvider:     "disabled",
			Model:               "",
			SourceWorkspaceRoot: ".",
			PermissionProfile:   "patch",
			Providers: map[string]SandboxProviderConfig{
				"codex": {
					Binary:        "codex",
					Model:         "",
					WorkspaceRoot: "./var/sandboxes",
					WorkspaceMode: "copied",
					TimeoutMillis: 0,
				},
			},
		},
		Vector: VectorConfig{
			Enabled:            false,
			Dimensions:         16,
			Embedder:           "local_stub",
			DefaultRecallLimit: 5,
		},
		Runtime: RuntimeConfig{
			StatePath: "./var/crew-runtime.json",
		},
		UI: UIConfig{
			RefreshIntervalMillis: 250,
			AttachAutoSteps:       1,
			Theme:                 "sunrise",
			ShowTimestamps:        true,
			CompactMessages:       false,
			AgentColors: map[string]string{
				"operator": "#f97316",
				"system":   "#fbbf24",
				"task":     "#a78bfa",
			},
		},
	}
}

func LoadConfig(configPath string) (LoadedConfig, error) {
	v := viper.New()
	configureViper(v)

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("crew")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		if home, err := os.UserHomeDir(); err == nil {
			v.AddConfigPath(filepath.Join(home, ".config", "crew"))
		}
	}

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFound) {
			return LoadedConfig{}, fmt.Errorf("read config: %w", err)
		}
	}

	cfg := DefaultConfig()
	if err := v.Unmarshal(&cfg); err != nil {
		return LoadedConfig{}, fmt.Errorf("decode config: %w", err)
	}
	cfg.normalizeSandboxConfig()

	if err := cfg.Validate(); err != nil {
		return LoadedConfig{}, err
	}

	return LoadedConfig{
		Config: cfg,
		Path:   v.ConfigFileUsed(),
	}, nil
}

func DefaultConfigPath() (string, error) {
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for config: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}

	return filepath.Join(configHome, "crew", "crew.yaml"), nil
}

func (c Config) Validate() error {
	if c.App.Name == "" {
		return errors.New("app.name must not be empty")
	}

	switch c.App.Environment {
	case "development", "test", "production":
	default:
		return fmt.Errorf("invalid app.environment %q", c.App.Environment)
	}

	if c.App.LogLevel == "" {
		return errors.New("app.log_level must not be empty")
	}

	switch c.Session.Mode {
	case "free", "sequential":
	default:
		return fmt.Errorf("invalid session.mode %q", c.Session.Mode)
	}

	if c.Session.MaxTurns < 1 {
		return fmt.Errorf("session.max_turns must be >= 1, got %d", c.Session.MaxTurns)
	}

	switch c.Session.DefaultAgentMode {
	case "reactive", "orchestrated":
	default:
		return fmt.Errorf("invalid session.default_agent_mode %q", c.Session.DefaultAgentMode)
	}
	switch c.Session.OrchestrationMode {
	case "deterministic", "round_robin", "mentioned_first":
	default:
		return fmt.Errorf("invalid session.orchestration_mode %q", c.Session.OrchestrationMode)
	}

	switch c.Session.ReplyRoutingMode {
	case "latest_speaker", "reply_obligations":
	default:
		return fmt.Errorf("invalid session.reply_routing_mode %q", c.Session.ReplyRoutingMode)
	}

	if c.Storage.Driver != "sqlite" {
		return fmt.Errorf("invalid storage.driver %q: only sqlite is supported in the bootstrap phase", c.Storage.Driver)
	}

	if c.Storage.Path == "" {
		return errors.New("storage.path must not be empty")
	}

	for name, provider := range c.Providers {
		if strings.TrimSpace(name) == "" {
			return errors.New("providers must not contain empty provider names")
		}
		if provider.TimeoutMillis < 0 || (provider.TimeoutMillis == 0 && name != "codex") {
			return fmt.Errorf("providers.%s.timeout_millis must be >= 1, got %d", name, provider.TimeoutMillis)
		}
		if provider.APIKeyEnv != "" && strings.TrimSpace(provider.APIKeyEnv) == "" {
			return fmt.Errorf("providers.%s.api_key_env must not be blank when set", name)
		}
	}

	if c.Sandbox.SourceWorkspaceRoot == "" {
		return errors.New("sandbox.source_workspace_root must not be empty")
	}

	switch c.Sandbox.PermissionProfile {
	case "read_only", "patch", "full_task":
	default:
		return fmt.Errorf("invalid sandbox.permission_profile %q", c.Sandbox.PermissionProfile)
	}

	defaultProvider := strings.TrimSpace(c.Sandbox.DefaultProvider)
	if defaultProvider == "" {
		return errors.New("sandbox.default_provider must not be empty")
	}
	if defaultProvider != "disabled" {
		if _, ok := c.Sandbox.Providers[defaultProvider]; !ok {
			return fmt.Errorf("sandbox.default_provider %q is not configured under sandbox.providers", defaultProvider)
		}
	}

	for name, provider := range c.Sandbox.Providers {
		if strings.TrimSpace(name) == "" {
			return errors.New("sandbox.providers must not contain empty provider names")
		}
		mode := strings.TrimSpace(provider.WorkspaceMode)
		if mode == "" {
			mode = "copied"
		}
		if mode != "copied" && mode != "in_place" {
			return fmt.Errorf("sandbox.providers.%s.workspace_mode must be one of copied or in_place, got %q", name, provider.WorkspaceMode)
		}
		if mode == "copied" && strings.TrimSpace(provider.WorkspaceRoot) == "" {
			return fmt.Errorf("sandbox.providers.%s.workspace_root must not be empty", name)
		}
		if provider.TimeoutMillis < 1 {
			if name == "codex" && provider.TimeoutMillis == 0 {
				continue
			}
			return fmt.Errorf("sandbox.providers.%s.timeout_millis must be >= 1, got %d", name, provider.TimeoutMillis)
		}
		for idx, path := range provider.AdditionalWrite {
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("sandbox.providers.%s.additional_write[%d] must not be empty", name, idx)
			}
		}
	}

	if c.Vector.Dimensions < 1 {
		return fmt.Errorf("vector.dimensions must be >= 1, got %d", c.Vector.Dimensions)
	}

	switch c.Vector.Embedder {
	case "local_stub":
	default:
		return fmt.Errorf("invalid vector.embedder %q", c.Vector.Embedder)
	}

	if c.Vector.DefaultRecallLimit < 1 {
		return fmt.Errorf("vector.default_recall_limit must be >= 1, got %d", c.Vector.DefaultRecallLimit)
	}

	if c.UI.RefreshIntervalMillis < 50 {
		return fmt.Errorf("ui.refresh_interval_millis must be >= 50, got %d", c.UI.RefreshIntervalMillis)
	}
	if c.UI.AttachAutoSteps < 0 {
		return fmt.Errorf("ui.attach_auto_steps must be >= 0, got %d", c.UI.AttachAutoSteps)
	}
	switch c.UI.Theme {
	case "sunrise", "graphite":
	default:
		return fmt.Errorf("invalid ui.theme %q", c.UI.Theme)
	}
	for agentID, color := range c.UI.AgentColors {
		if strings.TrimSpace(agentID) == "" {
			return errors.New("ui.agent_colors must not contain empty agent ids")
		}
		if strings.TrimSpace(color) == "" {
			return fmt.Errorf("ui.agent_colors[%q] must not be empty", agentID)
		}
	}

	return nil
}

func configureViper(v *viper.Viper) {
	v.SetEnvPrefix("CREW")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	defaults := DefaultConfig()
	v.SetDefault("app.name", defaults.App.Name)
	v.SetDefault("app.environment", defaults.App.Environment)
	v.SetDefault("app.log_level", defaults.App.LogLevel)
	v.SetDefault("session.mode", defaults.Session.Mode)
	v.SetDefault("session.loop_protection", defaults.Session.LoopProtection)
	v.SetDefault("session.max_turns", defaults.Session.MaxTurns)
	v.SetDefault("session.default_agent_mode", defaults.Session.DefaultAgentMode)
	v.SetDefault("storage.driver", defaults.Storage.Driver)
	v.SetDefault("storage.path", defaults.Storage.Path)
	v.SetDefault("providers", defaults.Providers)
	v.SetDefault("sandbox.default_provider", defaults.Sandbox.DefaultProvider)
	v.SetDefault("sandbox.source_workspace_root", defaults.Sandbox.SourceWorkspaceRoot)
	v.SetDefault("sandbox.permission_profile", defaults.Sandbox.PermissionProfile)
	v.SetDefault("sandbox.providers", defaults.Sandbox.Providers)
	v.SetDefault("vector.enabled", defaults.Vector.Enabled)
	v.SetDefault("vector.dimensions", defaults.Vector.Dimensions)
	v.SetDefault("vector.embedder", defaults.Vector.Embedder)
	v.SetDefault("vector.default_recall_limit", defaults.Vector.DefaultRecallLimit)
	v.SetDefault("runtime.state_path", defaults.Runtime.StatePath)
	v.SetDefault("ui.refresh_interval_millis", defaults.UI.RefreshIntervalMillis)
	v.SetDefault("ui.attach_auto_steps", defaults.UI.AttachAutoSteps)
	v.SetDefault("ui.theme", defaults.UI.Theme)
	v.SetDefault("ui.show_timestamps", defaults.UI.ShowTimestamps)
	v.SetDefault("ui.compact_messages", defaults.UI.CompactMessages)
	v.SetDefault("ui.agent_colors", defaults.UI.AgentColors)
}

func (c *Config) normalizeSandboxConfig() {
	if c == nil {
		return
	}
	if c.Sandbox.Providers == nil {
		c.Sandbox.Providers = make(map[string]SandboxProviderConfig)
	}

	legacyProvider := strings.TrimSpace(c.Sandbox.Provider)
	if legacyProvider != "" && (strings.TrimSpace(c.Sandbox.DefaultProvider) == "" || strings.TrimSpace(c.Sandbox.DefaultProvider) == "disabled") {
		c.Sandbox.DefaultProvider = legacyProvider
	}
	if strings.TrimSpace(c.Sandbox.DefaultProvider) == "" {
		c.Sandbox.DefaultProvider = "disabled"
	}

	if legacyProvider == "" || legacyProvider == "disabled" {
		return
	}

	legacyCfg, ok := c.Sandbox.Providers[legacyProvider]
	if !ok {
		legacyCfg = SandboxProviderConfig{}
	}
	if strings.TrimSpace(c.Sandbox.Binary) != "" {
		legacyCfg.Binary = c.Sandbox.Binary
	}
	if strings.TrimSpace(c.Sandbox.Model) != "" {
		legacyCfg.Model = c.Sandbox.Model
	}
	if strings.TrimSpace(c.Sandbox.WorkspaceRoot) != "" {
		legacyCfg.WorkspaceRoot = c.Sandbox.WorkspaceRoot
	}
	if strings.TrimSpace(c.Sandbox.WorkspaceMode) != "" {
		legacyCfg.WorkspaceMode = c.Sandbox.WorkspaceMode
	}
	if c.Sandbox.TimeoutMillis > 0 {
		legacyCfg.TimeoutMillis = c.Sandbox.TimeoutMillis
	}
	c.Sandbox.Providers[legacyProvider] = legacyCfg
}
