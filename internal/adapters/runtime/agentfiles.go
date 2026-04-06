package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"crew/internal/domain"
)

type agentFile struct {
	ID                   string          `yaml:"id"`
	Name                 string          `yaml:"name"`
	Role                 string          `yaml:"role"`
	SystemPrompt         string          `yaml:"system_prompt"`
	Provider             string          `yaml:"provider"`
	Model                string          `yaml:"model"`
	DelegationRuntime    string          `yaml:"delegation_runtime"`
	SandboxWorkspaceRoot string          `yaml:"sandbox_workspace_root"`
	Color                string          `yaml:"color"`
	Tools                []string        `yaml:"tools"`
	Policies             agentFilePolicy `yaml:"policies"`
}

type agentFilePolicy struct {
	CanInitiate            bool     `yaml:"can_initiate"`
	RequireDirectMention   bool     `yaml:"require_direct_mention"`
	AllowBroadcast         bool     `yaml:"allow_broadcast"`
	AllowToolCalls         bool     `yaml:"allow_tool_calls"`
	AllowSandboxDelegation bool     `yaml:"allow_sandbox_delegation"`
	AllowedSandboxRuntimes []string `yaml:"allowed_sandbox_runtimes"`
	Priority               int      `yaml:"priority"`
	Weight                 int      `yaml:"weight"`
	MaxConsecutiveTurns    int      `yaml:"max_consecutive_turns"`
	MaxToolCallsPerTurn    int      `yaml:"max_tool_calls_per_turn"`
}

type AgentCatalogEntry struct {
	Agent domain.Agent
	Color string
}

func loadAgentsDir(dir string) ([]domain.Agent, error) {
	entries, err := loadAgentCatalogDir(dir)
	if err != nil {
		return nil, err
	}

	agents := make([]domain.Agent, 0, len(entries))
	for _, entry := range entries {
		agents = append(agents, entry.Agent)
	}

	return agents, nil
}

func loadAgentCatalogDir(dir string) ([]AgentCatalogEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read agents directory %q: %w", dir, err)
	}

	filenames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		filenames = append(filenames, entry.Name())
	}
	sort.Strings(filenames)

	agents := make([]AgentCatalogEntry, 0, len(filenames))
	seen := make(map[domain.AgentID]string, len(filenames))
	for _, filename := range filenames {
		path := filepath.Join(dir, filename)
		agent, err := loadAgentFile(path)
		if err != nil {
			return nil, err
		}
		if previous, exists := seen[agent.Agent.ID]; exists {
			return nil, fmt.Errorf("duplicate agent id %q in %q and %q", agent.Agent.ID, previous, path)
		}
		seen[agent.Agent.ID] = path
		agents = append(agents, agent)
	}

	return agents, nil
}

func LoadAgentsDir(dir string) ([]domain.Agent, error) {
	return loadAgentsDir(dir)
}

func LoadAgentCatalogDir(dir string) ([]AgentCatalogEntry, error) {
	return loadAgentCatalogDir(dir)
}

func loadAgentFile(path string) (AgentCatalogEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return AgentCatalogEntry{}, fmt.Errorf("read agent file %q: %w", path, err)
	}

	var spec agentFile
	if err := yaml.Unmarshal(content, &spec); err != nil {
		return AgentCatalogEntry{}, fmt.Errorf("decode agent file %q: %w", path, err)
	}

	agent, err := domain.NewAgent(domain.Agent{
		ID:                   domain.AgentID(spec.ID),
		Name:                 spec.Name,
		Role:                 spec.Role,
		SystemPrompt:         spec.SystemPrompt,
		Provider:             spec.Provider,
		Model:                spec.Model,
		DelegationRuntime:    spec.DelegationRuntime,
		SandboxWorkspaceRoot: spec.SandboxWorkspaceRoot,
		Tools:                append([]string(nil), spec.Tools...),
		Policies: domain.AgentPolicy{
			CanInitiate:            spec.Policies.CanInitiate,
			RequireDirectMention:   spec.Policies.RequireDirectMention,
			AllowBroadcast:         spec.Policies.AllowBroadcast,
			AllowToolCalls:         spec.Policies.AllowToolCalls,
			AllowSandboxDelegation: spec.Policies.AllowSandboxDelegation,
			AllowedSandboxRuntimes: append([]string(nil), spec.Policies.AllowedSandboxRuntimes...),
			Priority:               spec.Policies.Priority,
			Weight:                 spec.Policies.Weight,
			MaxConsecutiveTurns:    spec.Policies.MaxConsecutiveTurns,
			MaxToolCallsPerTurn:    spec.Policies.MaxToolCallsPerTurn,
		},
	})
	if err != nil {
		return AgentCatalogEntry{}, fmt.Errorf("validate agent file %q: %w", path, err)
	}

	return AgentCatalogEntry{
		Agent: agent,
		Color: strings.TrimSpace(spec.Color),
	}, nil
}
