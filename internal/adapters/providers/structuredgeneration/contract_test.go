package structuredgeneration

import (
	"strings"
	"testing"

	"crew/internal/domain"
)

func TestSystemInstructionTreatsAgentMentionsAsRealHandoffs(t *testing.T) {
	agent, err := domain.NewAgent(domain.Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "planner",
		SystemPrompt: "Plan the next step.",
		Provider:     "codex",
		Model:        "gpt-5.4",
		Policies: domain.AgentPolicy{
			CanInitiate:         true,
			AllowBroadcast:      true,
			MaxConsecutiveTurns: 1,
			MaxToolCallsPerTurn: 0,
			Weight:              1,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	instruction := SystemInstruction(agent)
	for _, needle := range []string{
		"Treat any `@agent` mention as a real handoff or routing action.",
		"Use `@agent` only when you are actively handing work to that agent in this reply.",
		"Any exact `@agent` token anywhere in the message body will be treated as a real mention.",
		"If you intend to hand work to another agent, you must use the exact `@agent` handle.",
		"Do not mention `@agent` handles hypothetically",
		"If you still need operator input, ask the operator directly and do not hand off yet.",
	} {
		if !strings.Contains(instruction, needle) {
			t.Fatalf("expected instruction to contain %q, got %q", needle, instruction)
		}
	}
}

func TestSystemInstructionClarifiesSandboxDelegationContract(t *testing.T) {
	agent, err := domain.NewAgent(domain.Agent{
		ID:                "writer",
		Name:              "Writer",
		Role:              "writer",
		SystemPrompt:      "Implement the requested changes.",
		Provider:          "codex",
		Model:             "gpt-5.4",
		DelegationRuntime: "codex",
		Policies: domain.AgentPolicy{
			CanInitiate:            false,
			RequireDirectMention:   true,
			AllowBroadcast:         true,
			AllowToolCalls:         true,
			AllowSandboxDelegation: true,
			AllowedSandboxRuntimes: []string{"codex"},
			MaxConsecutiveTurns:    1,
			MaxToolCallsPerTurn:    1,
			Weight:                 1,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	instruction := SystemInstruction(agent)
	for _, needle := range []string{
		"Your direct text reply runs in read-only mode.",
		"Do not say you are blocked by read-only access when sandbox delegation is available.",
		"instruction must describe the actual implementation task for the sandbox runtime.",
		"Do not use sandbox_request to ask for access, approvals, or a writable sandbox.",
	} {
		if !strings.Contains(instruction, needle) {
			t.Fatalf("expected instruction to contain %q, got %q", needle, instruction)
		}
	}
}
