package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	runtimeadapter "crew/internal/adapters/runtime"
	sqliteadapter "crew/internal/adapters/storage/sqlite"
	"crew/internal/application"
	"crew/internal/platform"
)

var (
	agentsDirResolver          = resolveAgentsDir
	agentsDirResolverIsDefault = true
)

const agentsDirEnvVar = "CREW_AGENTS_DIR"
const localAgentsDirName = "crew_agents"
const legacyInstalledAgentsDirName = "agents"

func (s *runtimeState) withLocalRuntime(
	ctx context.Context,
	_ bool,
	fn func(rt *runtimeadapter.Runtime) (any, error),
) (any, error) {
	if err := s.bootstrap(); err != nil {
		return nil, err
	}

	store, err := openRuntimeStore(ctx, s.loaded.Config.Storage.Path)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	agentsRootDir, err := agentsDirResolver()
	if err != nil {
		return nil, err
	}
	if _, err := resolveSelectedAgentsDir(s.actors); err != nil {
		return nil, err
	}
	textProviders := resolveTextProviders(s.loaded.Config.Providers)
	sandboxProviders := resolveSandboxProviders(s.loaded.Config.Sandbox)

	rt, err := runtimeadapter.NewSQLite(ctx, store, nil, nil, nil, runtimeadapter.Config{
		ProjectionBuffer:           64,
		AgentsDir:                  agentsRootDir,
		DefaultActorsSelector:      strings.TrimSpace(s.actors),
		OrchestrationMode:          application.OrchestrationMode(s.loaded.Config.Session.OrchestrationMode),
		ReplyRoutingMode:           application.ReplyRoutingMode(s.loaded.Config.Session.ReplyRoutingMode),
		VectorEnabled:              s.loaded.Config.Vector.Enabled,
		VectorDimensions:           s.loaded.Config.Vector.Dimensions,
		TextProviders:              textProviders,
		SandboxDefaultProvider:     s.loaded.Config.Sandbox.DefaultProvider,
		SandboxProviders:           sandboxProviders,
		SandboxSourceWorkspaceRoot: s.loaded.Config.Sandbox.SourceWorkspaceRoot,
		SandboxPermissionProfile:   s.loaded.Config.Sandbox.PermissionProfile,
	})
	if err != nil {
		return nil, err
	}

	if err := rt.Start(ctx); err != nil {
		return nil, err
	}
	defer rt.Shutdown(context.Background())

	return fn(rt)
}

func openRuntimeStore(ctx context.Context, path string) (*sqliteadapter.Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite storage directory for %q: %w", path, err)
	}

	store, err := sqliteadapter.Open(path)
	if err != nil {
		return nil, err
	}

	if err := store.Migrate(ctx); err != nil {
		store.Close()
		return nil, fmt.Errorf("migrate sqlite storage %q: %w", path, err)
	}

	return store, nil
}

func resolveTextProviders(cfgs map[string]platform.TextProviderConfig) map[string]runtimeadapter.TextProviderConfig {
	if len(cfgs) == 0 {
		return nil
	}

	resolved := make(map[string]runtimeadapter.TextProviderConfig, len(cfgs))
	for name, cfg := range cfgs {
		apiKey := strings.TrimSpace(cfg.APIKey)
		if apiKey == "" && strings.TrimSpace(cfg.APIKeyEnv) != "" {
			apiKey = strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
		}

		resolved[name] = runtimeadapter.TextProviderConfig{
			BaseURL:     strings.TrimSpace(cfg.BaseURL),
			APIKey:      apiKey,
			BinaryPath:  strings.TrimSpace(cfg.Binary),
			WorkingDir:  strings.TrimSpace(cfg.WorkingDirectory),
			Timeout:     time.Duration(cfg.TimeoutMillis) * time.Millisecond,
			Temperature: cfg.Temperature,
		}
	}

	return resolved
}

func resolveSandboxProviders(cfg platform.SandboxConfig) map[string]runtimeadapter.SandboxProviderConfig {
	if len(cfg.Providers) == 0 {
		return nil
	}

	resolved := make(map[string]runtimeadapter.SandboxProviderConfig, len(cfg.Providers))
	for name, provider := range cfg.Providers {
		resolved[name] = runtimeadapter.SandboxProviderConfig{
			BinaryPath:      strings.TrimSpace(provider.Binary),
			Model:           strings.TrimSpace(provider.Model),
			SandboxRoot:     strings.TrimSpace(provider.WorkspaceRoot),
			Timeout:         time.Duration(provider.TimeoutMillis) * time.Millisecond,
			AdditionalWrite: append([]string(nil), provider.AdditionalWrite...),
		}
	}
	return resolved
}

func resolveAgentsDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(agentsDirEnvVar)); configured != "" {
		return requireDirectory(configured, agentsDirEnvVar)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory for agents: %w", err)
	}

	if discovered, err := findNearestAgentsDir(workingDir); err != nil {
		return "", err
	} else if discovered != "" {
		return discovered, nil
	}

	if fallback, err := resolveInstalledAgentsFallbackDir(); err != nil {
		return "", err
	} else if fallback != "" {
		return fallback, nil
	}

	return "", fmt.Errorf("could not find %s directory from %q upward and no installed fallback catalog was found", localAgentsDirName, workingDir)
}

func (s *runtimeState) resolveActiveAgentsDir() (string, error) {
	return resolveSelectedAgentsDir(s.actors)
}

func resolveSelectedAgentsDir(selection string) (string, error) {
	rootDir, err := agentsDirResolver()
	if err != nil {
		return "", err
	}

	return resolveActorsDirFromRoot(rootDir, selection)
}

func resolveActorsDirFromRoot(rootDir string, selection string) (string, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return rootDir, nil
	}
	if filepath.IsAbs(selection) {
		return "", fmt.Errorf("actors selector %q must be relative to %q", selection, rootDir)
	}

	cleaned := filepath.Clean(selection)
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("actors selector %q resolves to the root catalog; omit --actors to use %q", selection, rootDir)
	}

	candidate := filepath.Join(rootDir, cleaned)
	relative, err := filepath.Rel(rootDir, candidate)
	if err != nil {
		return "", fmt.Errorf("resolve actors selector %q under %q: %w", selection, rootDir, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("actors selector %q escapes the root catalog %q", selection, rootDir)
	}

	info, err := os.Stat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("actors catalog %q was not found under %q", selection, rootDir)
		}
		return "", fmt.Errorf("stat actors catalog %q: %w", candidate, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("actors catalog %q is not a directory", candidate)
	}
	return candidate, nil
}

func requireDirectory(path string, label string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s %q was not found", label, path)
		}
		return "", fmt.Errorf("stat %s %q: %w", label, path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s %q is not a directory", label, path)
	}
	return path, nil
}

func findNearestAgentsDir(start string) (string, error) {
	current := start
	for {
		candidate := filepath.Join(current, localAgentsDirName)
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s directory %q: %w", localAgentsDirName, candidate, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", nil
}

func resolveInstalledAgentsFallbackDir() (string, error) {
	dataHome, err := crewDataHome()
	if err != nil {
		return "", err
	}

	candidates := []string{
		filepath.Join(dataHome, "crew", localAgentsDirName),
		filepath.Join(dataHome, "crew", legacyInstalledAgentsDirName),
	}
	for _, candidate := range candidates {
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.IsDir() {
			return candidate, nil
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat installed agent catalog %q: %w", candidate, statErr)
		}
	}

	return "", nil
}

func crewDataHome() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); configured != "" {
		return configured, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for installed agent catalog: %w", err)
	}
	return filepath.Join(home, ".local", "share"), nil
}
