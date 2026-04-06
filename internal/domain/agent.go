package domain

import (
	"fmt"
	"slices"
	"strings"
)

type Agent struct {
	ID                   AgentID
	Name                 string
	Role                 string
	SystemPrompt         string
	Provider             string
	Model                string
	DelegationRuntime    string
	SandboxWorkspaceRoot string
	Tools                []string
	Policies             AgentPolicy
}

type AgentPolicy struct {
	CanInitiate            bool
	RequireDirectMention   bool
	AllowBroadcast         bool
	AllowToolCalls         bool
	AllowSandboxDelegation bool
	AllowedSandboxRuntimes []string
	Priority               int
	Weight                 int
	MaxConsecutiveTurns    int
	MaxToolCallsPerTurn    int
}

func DefaultAgentPolicy() AgentPolicy {
	return AgentPolicy{
		CanInitiate:            false,
		RequireDirectMention:   false,
		AllowBroadcast:         true,
		AllowToolCalls:         false,
		AllowSandboxDelegation: false,
		Priority:               0,
		Weight:                 1,
		MaxConsecutiveTurns:    2,
		MaxToolCallsPerTurn:    0,
	}
}

func NewAgent(agent Agent) (Agent, error) {
	if isZeroAgentPolicy(agent.Policies) {
		agent.Policies = DefaultAgentPolicy()
	}
	if agent.Policies.Weight == 0 {
		agent.Policies.Weight = DefaultAgentPolicy().Weight
	}
	agent.DelegationRuntime = strings.TrimSpace(agent.DelegationRuntime)
	agent.SandboxWorkspaceRoot = strings.TrimSpace(agent.SandboxWorkspaceRoot)
	if agent.DelegationRuntime == "" && agent.Policies.AllowSandboxDelegation && len(agent.Policies.AllowedSandboxRuntimes) == 1 {
		agent.DelegationRuntime = strings.TrimSpace(agent.Policies.AllowedSandboxRuntimes[0])
	}

	if err := agent.Validate(); err != nil {
		return Agent{}, err
	}

	agent.Tools = slices.Clone(agent.Tools)
	return agent, nil
}

func isZeroAgentPolicy(policy AgentPolicy) bool {
	return !policy.CanInitiate &&
		!policy.RequireDirectMention &&
		!policy.AllowBroadcast &&
		!policy.AllowToolCalls &&
		!policy.AllowSandboxDelegation &&
		len(policy.AllowedSandboxRuntimes) == 0 &&
		policy.Priority == 0 &&
		policy.Weight == 0 &&
		policy.MaxConsecutiveTurns == 0 &&
		policy.MaxToolCallsPerTurn == 0
}

func (a Agent) Validate() error {
	if err := a.ID.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("agent name must not be empty")
	}

	if strings.TrimSpace(a.Role) == "" {
		return fmt.Errorf("agent role must not be empty")
	}

	if strings.TrimSpace(a.SystemPrompt) == "" {
		return fmt.Errorf("agent system prompt must not be empty")
	}

	if strings.TrimSpace(a.Provider) == "" {
		return fmt.Errorf("agent provider must not be empty")
	}

	if strings.TrimSpace(a.Model) == "" {
		return fmt.Errorf("agent model must not be empty")
	}
	if !a.Policies.AllowSandboxDelegation {
		if a.DelegationRuntime != "" {
			return fmt.Errorf("agent delegation runtime must be empty when sandbox delegation is disabled")
		}
		if a.SandboxWorkspaceRoot != "" {
			return fmt.Errorf("agent sandbox workspace root must be empty when sandbox delegation is disabled")
		}
	} else {
		if a.DelegationRuntime == "" {
			return fmt.Errorf("agent delegation runtime must not be empty when sandbox delegation is enabled")
		}
		if len(a.Policies.AllowedSandboxRuntimes) > 0 && !allowsSandboxRuntime(a.Policies.AllowedSandboxRuntimes, a.DelegationRuntime) {
			return fmt.Errorf("agent delegation runtime %q must be included in allowed sandbox runtimes", a.DelegationRuntime)
		}
	}

	if err := a.Policies.Validate(); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(a.Tools))
	for _, tool := range a.Tools {
		name := strings.TrimSpace(tool)
		if name == "" {
			return fmt.Errorf("agent tools must not contain empty values")
		}

		if _, exists := seen[name]; exists {
			return fmt.Errorf("agent tools must be unique, duplicate %q", name)
		}

		seen[name] = struct{}{}
	}

	return nil
}

func (p AgentPolicy) Validate() error {
	if p.Priority < 0 {
		return fmt.Errorf("agent priority must be >= 0, got %d", p.Priority)
	}

	if p.Weight < 1 {
		return fmt.Errorf("agent weight must be >= 1, got %d", p.Weight)
	}

	if p.MaxConsecutiveTurns < 1 {
		return fmt.Errorf("agent max consecutive turns must be >= 1, got %d", p.MaxConsecutiveTurns)
	}

	if p.MaxToolCallsPerTurn < 0 {
		return fmt.Errorf("agent max tool calls per turn must be >= 0, got %d", p.MaxToolCallsPerTurn)
	}

	if !p.AllowToolCalls && p.MaxToolCallsPerTurn > 0 {
		return fmt.Errorf("agent max tool calls per turn must be 0 when tool calls are disabled")
	}
	if p.AllowSandboxDelegation && !p.AllowToolCalls {
		return fmt.Errorf("agent sandbox delegation requires tool calls to be enabled")
	}
	if !p.AllowSandboxDelegation && len(p.AllowedSandboxRuntimes) > 0 {
		return fmt.Errorf("allowed sandbox runtimes must be empty when sandbox delegation is disabled")
	}
	seen := make(map[string]struct{}, len(p.AllowedSandboxRuntimes))
	for _, runtime := range p.AllowedSandboxRuntimes {
		name := strings.TrimSpace(runtime)
		if name == "" {
			return fmt.Errorf("allowed sandbox runtimes must not contain empty values")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("allowed sandbox runtimes must be unique, duplicate %q", name)
		}
		seen[name] = struct{}{}
	}

	return nil
}

func allowsSandboxRuntime(allowed []string, runtime string) bool {
	runtime = strings.TrimSpace(runtime)
	if runtime == "" {
		return false
	}
	for _, candidate := range allowed {
		name := strings.TrimSpace(candidate)
		if name == "*" || name == runtime {
			return true
		}
	}
	return false
}
