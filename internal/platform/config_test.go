package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaultsWithoutFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("CREW_APP_NAME", "")
	t.Setenv("CREW_SESSION_MODE", "")
	t.Setenv("CREW_RUNTIME_STATE_PATH", "")

	loaded, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if loaded.Config.App.Name != "crew" {
		t.Fatalf("expected default app name crew, got %q", loaded.Config.App.Name)
	}

	if loaded.Config.Session.Mode != "free" {
		t.Fatalf("expected default session mode free, got %q", loaded.Config.Session.Mode)
	}
	if loaded.Config.Session.OrchestrationMode != "deterministic" {
		t.Fatalf("expected default orchestration mode deterministic, got %q", loaded.Config.Session.OrchestrationMode)
	}
	if loaded.Config.Session.ReplyRoutingMode != "reply_obligations" {
		t.Fatalf("expected default reply routing mode reply_obligations, got %q", loaded.Config.Session.ReplyRoutingMode)
	}

	if loaded.Config.Runtime.StatePath != "./var/crew-runtime.json" {
		t.Fatalf("expected default runtime state path, got %q", loaded.Config.Runtime.StatePath)
	}
	if loaded.Config.Providers["openai"].APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("expected default openai api key env, got %#v", loaded.Config.Providers["openai"])
	}
	if loaded.Config.Providers["gemini"].APIKeyEnv != "GEMINI_API_KEY" {
		t.Fatalf("expected default gemini api key env, got %#v", loaded.Config.Providers["gemini"])
	}
	if loaded.Config.Providers["codex"].Binary != "codex" {
		t.Fatalf("expected default codex text binary, got %#v", loaded.Config.Providers["codex"])
	}
	if loaded.Config.Providers["codex"].WorkingDirectory != "." {
		t.Fatalf("expected default codex working directory '.', got %#v", loaded.Config.Providers["codex"])
	}
	if loaded.Config.Sandbox.DefaultProvider != "disabled" {
		t.Fatalf("expected default sandbox provider disabled, got %q", loaded.Config.Sandbox.DefaultProvider)
	}
	if loaded.Config.Sandbox.SourceWorkspaceRoot != "." {
		t.Fatalf("expected default sandbox source workspace root '.', got %q", loaded.Config.Sandbox.SourceWorkspaceRoot)
	}
	if loaded.Config.Sandbox.Providers["codex"].Binary != "codex" {
		t.Fatalf("expected default codex sandbox binary, got %#v", loaded.Config.Sandbox.Providers["codex"])
	}
	if loaded.Config.Vector.Dimensions != 16 {
		t.Fatalf("expected default vector dimensions 16, got %d", loaded.Config.Vector.Dimensions)
	}
	if loaded.Config.Vector.Embedder != "local_stub" {
		t.Fatalf("expected default vector embedder local_stub, got %q", loaded.Config.Vector.Embedder)
	}
	if loaded.Config.UI.AttachAutoSteps != 1 {
		t.Fatalf("expected default attach auto steps 1, got %d", loaded.Config.UI.AttachAutoSteps)
	}
	if !loaded.Config.UI.AttachSplitPanes {
		t.Fatal("expected default attach split panes true")
	}
	if loaded.Config.UI.Theme != "sunrise" {
		t.Fatalf("expected default ui theme sunrise, got %q", loaded.Config.UI.Theme)
	}
	if !loaded.Config.UI.ShowTimestamps {
		t.Fatal("expected default ui.show_timestamps=true")
	}
	if !loaded.Config.UI.AttachSidebar {
		t.Fatal("expected default ui.attach_sidebar=true")
	}
	if len(loaded.Config.UI.AgentColors) == 0 {
		t.Fatal("expected default agent colors to be populated")
	}
}

func TestLoadConfigFromExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crew.yaml")
	content := []byte(`
app:
  environment: production
  log_level: debug
session:
  mode: sequential
  max_turns: 12
  orchestration_mode: round_robin
  reply_routing_mode: latest_speaker
storage:
  path: /tmp/crew-test.db
providers:
  codex:
    binary: /usr/local/bin/codex
    working_directory: /tmp/crew-project
    timeout_millis: 45000
  openai:
    base_url: http://localhost:8080/v1
    api_key: literal-key
    api_key_env: TEST_OPENAI_KEY
    timeout_millis: 15000
    temperature: 0.1
sandbox:
  default_provider: codex
  source_workspace_root: /tmp/crew-source
  permission_profile: patch
  providers:
    codex:
      binary: /usr/local/bin/codex
      model: codex-mini
      workspace_root: /tmp/crew-sandboxes
      timeout_millis: 120000
vector:
  dimensions: 8
ui:
  refresh_interval_millis: 400
  attach_auto_steps: 3
  attach_split_panes: false
  theme: graphite
  show_timestamps: false
  compact_messages: true
  attach_sidebar: false
  agent_colors:
    planner: "#111111"
`)

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if loaded.Path != path {
		t.Fatalf("expected config path %q, got %q", path, loaded.Path)
	}

	if loaded.Config.Session.Mode != "sequential" {
		t.Fatalf("expected sequential mode, got %q", loaded.Config.Session.Mode)
	}
	if loaded.Config.Session.OrchestrationMode != "round_robin" {
		t.Fatalf("expected round_robin orchestration mode, got %q", loaded.Config.Session.OrchestrationMode)
	}
	if loaded.Config.Session.ReplyRoutingMode != "latest_speaker" {
		t.Fatalf("expected latest_speaker reply routing mode, got %q", loaded.Config.Session.ReplyRoutingMode)
	}

	if loaded.Config.App.LogLevel != "debug" {
		t.Fatalf("expected debug log level, got %q", loaded.Config.App.LogLevel)
	}
	if loaded.Config.Providers["openai"].BaseURL != "http://localhost:8080/v1" {
		t.Fatalf("expected openai base url override, got %#v", loaded.Config.Providers["openai"])
	}
	if loaded.Config.Providers["openai"].APIKey != "literal-key" {
		t.Fatalf("expected openai literal key, got %#v", loaded.Config.Providers["openai"])
	}
	if loaded.Config.Providers["codex"].Binary != "/usr/local/bin/codex" {
		t.Fatalf("expected codex binary override, got %#v", loaded.Config.Providers["codex"])
	}
	if loaded.Config.Providers["codex"].WorkingDirectory != "/tmp/crew-project" {
		t.Fatalf("expected codex working directory override, got %#v", loaded.Config.Providers["codex"])
	}
	if loaded.Config.Sandbox.DefaultProvider != "codex" {
		t.Fatalf("expected sandbox provider codex, got %q", loaded.Config.Sandbox.DefaultProvider)
	}
	if loaded.Config.Sandbox.Providers["codex"].Binary != "/usr/local/bin/codex" {
		t.Fatalf("expected sandbox binary override, got %q", loaded.Config.Sandbox.Providers["codex"].Binary)
	}
	if loaded.Config.Sandbox.Providers["codex"].Model != "codex-mini" {
		t.Fatalf("expected sandbox model codex-mini, got %q", loaded.Config.Sandbox.Providers["codex"].Model)
	}
	if loaded.Config.Sandbox.SourceWorkspaceRoot != "/tmp/crew-source" {
		t.Fatalf("expected sandbox source workspace root, got %q", loaded.Config.Sandbox.SourceWorkspaceRoot)
	}
	if loaded.Config.Sandbox.Providers["codex"].WorkspaceRoot != "/tmp/crew-sandboxes" {
		t.Fatalf("expected sandbox workspace root, got %q", loaded.Config.Sandbox.Providers["codex"].WorkspaceRoot)
	}
	if loaded.Config.Vector.Dimensions != 8 {
		t.Fatalf("expected vector dimensions 8, got %d", loaded.Config.Vector.Dimensions)
	}
	if loaded.Config.UI.AttachAutoSteps != 3 {
		t.Fatalf("expected attach auto steps 3, got %d", loaded.Config.UI.AttachAutoSteps)
	}
	if loaded.Config.UI.AttachSplitPanes {
		t.Fatal("expected ui.attach_split_panes=false")
	}
	if loaded.Config.UI.Theme != "graphite" {
		t.Fatalf("expected ui theme graphite, got %q", loaded.Config.UI.Theme)
	}
	if loaded.Config.UI.ShowTimestamps {
		t.Fatal("expected ui.show_timestamps=false")
	}
	if !loaded.Config.UI.CompactMessages {
		t.Fatal("expected ui.compact_messages=true")
	}
	if loaded.Config.UI.AttachSidebar {
		t.Fatal("expected ui.attach_sidebar=false")
	}
	if loaded.Config.UI.AgentColors["planner"] != "#111111" {
		t.Fatalf("expected planner override color, got %#v", loaded.Config.UI.AgentColors)
	}
}

func TestDefaultConfigPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/crew-config-home")

	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath() error = %v", err)
	}

	if path != "/tmp/crew-config-home/crew/crew.yaml" {
		t.Fatalf("expected xdg config path, got %q", path)
	}
}

func TestConfigValidateRejectsInvalidMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Session.Mode = "chaos"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject invalid session mode")
	}
}

func TestConfigValidateRejectsInvalidOrchestrationMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Session.OrchestrationMode = "chaos"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject invalid orchestration mode")
	}
}

func TestConfigValidateRejectsInvalidReplyRoutingMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Session.ReplyRoutingMode = "chaos"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject invalid reply routing mode")
	}
}

func TestConfigValidateRejectsProviderWithInvalidTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers["openai"] = TextProviderConfig{
		BaseURL:       "https://api.openai.com/v1",
		APIKeyEnv:     "OPENAI_API_KEY",
		TimeoutMillis: 0,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject provider with invalid timeout")
	}
}

func TestConfigValidateRejectsBlankProviderName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers[" "] = TextProviderConfig{
		BaseURL:       "https://api.openai.com/v1",
		TimeoutMillis: 30000,
		Temperature:   0.2,
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject blank provider name")
	}
}

func TestConfigValidateRejectsInvalidSandboxPermissionProfile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sandbox.PermissionProfile = "danger"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject invalid sandbox permission profile")
	}
}

func TestConfigValidateRejectsEmptySandboxSourceWorkspaceRoot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sandbox.SourceWorkspaceRoot = ""

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject empty sandbox source workspace root")
	}
}

func TestConfigValidateRejectsUnknownDefaultSandboxProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sandbox.DefaultProvider = "claude"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject unknown default sandbox provider")
	}
}

func TestLoadConfigNormalizesLegacySandboxFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crew.yaml")
	content := []byte(`
storage:
  path: /tmp/crew-test.db
sandbox:
  provider: codex
  binary: /usr/local/bin/codex
  model: codex-mini
  source_workspace_root: /tmp/crew-source
  workspace_root: /tmp/crew-sandboxes
  permission_profile: patch
  timeout_millis: 120000
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if loaded.Config.Sandbox.DefaultProvider != "codex" {
		t.Fatalf("expected normalized default sandbox provider codex, got %q", loaded.Config.Sandbox.DefaultProvider)
	}
	if loaded.Config.Sandbox.Providers["codex"].Binary != "/usr/local/bin/codex" {
		t.Fatalf("expected normalized codex binary, got %#v", loaded.Config.Sandbox.Providers["codex"])
	}
	if loaded.Config.Sandbox.Providers["codex"].WorkspaceRoot != "/tmp/crew-sandboxes" {
		t.Fatalf("expected normalized codex workspace root, got %#v", loaded.Config.Sandbox.Providers["codex"])
	}
}

func TestConfigValidateRejectsNegativeAttachAutoSteps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UI.AttachAutoSteps = -1

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject negative attach auto steps")
	}
}

func TestConfigValidateRejectsInvalidUITheme(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UI.Theme = "neon-chaos"

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected Validate() to reject invalid ui theme")
	}
}
