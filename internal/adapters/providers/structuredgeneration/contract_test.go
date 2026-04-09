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
		"Do not mention `@agent` handles hypothetically",
		"If you still need operator input, ask the operator directly and do not hand off yet.",
	} {
		if !strings.Contains(instruction, needle) {
			t.Fatalf("expected instruction to contain %q, got %q", needle, instruction)
		}
	}
}
