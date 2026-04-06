package domain

import (
	"testing"
	"time"
)

func TestNewAgentAppliesDefaultPolicy(t *testing.T) {
	agent, err := NewAgent(Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "Breaks work into steps",
		SystemPrompt: "Plan carefully",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	if agent.Policies.MaxConsecutiveTurns != 2 {
		t.Fatalf("expected default max consecutive turns 2, got %d", agent.Policies.MaxConsecutiveTurns)
	}
	if agent.Policies.Priority != 0 {
		t.Fatalf("expected default priority 0, got %d", agent.Policies.Priority)
	}
	if agent.Policies.Weight != 1 {
		t.Fatalf("expected default weight 1, got %d", agent.Policies.Weight)
	}
}

func TestNewAgentRejectsDuplicateTools(t *testing.T) {
	_, err := NewAgent(Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "Breaks work into steps",
		SystemPrompt: "Plan carefully",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
		Tools:        []string{"search", "search"},
		Policies:     DefaultAgentPolicy(),
	})
	if err == nil {
		t.Fatal("expected duplicate tools to be rejected")
	}
}

func TestNewAgentRejectsSandboxDelegationWithoutToolCalls(t *testing.T) {
	_, err := NewAgent(Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "Breaks work into steps",
		SystemPrompt: "Plan carefully",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
		Policies: AgentPolicy{
			AllowSandboxDelegation: true,
			AllowedSandboxRuntimes: []string{"codex"},
			MaxConsecutiveTurns:    2,
			MaxToolCallsPerTurn:    0,
		},
	})
	if err == nil {
		t.Fatal("expected sandbox delegation without tool calls to be rejected")
	}
}

func TestNewAgentUsesSingleAllowedSandboxRuntimeAsDelegationRuntime(t *testing.T) {
	agent, err := NewAgent(Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "Breaks work into steps",
		SystemPrompt: "Plan carefully",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
		Policies: AgentPolicy{
			AllowToolCalls:         true,
			AllowSandboxDelegation: true,
			AllowedSandboxRuntimes: []string{"codex"},
			MaxConsecutiveTurns:    2,
			MaxToolCallsPerTurn:    1,
			Weight:                 1,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	if agent.DelegationRuntime != "codex" {
		t.Fatalf("expected delegation runtime codex, got %q", agent.DelegationRuntime)
	}
}

func TestNewAgentRejectsDelegationRuntimeOutsideAllowedSet(t *testing.T) {
	_, err := NewAgent(Agent{
		ID:                "planner",
		Name:              "Planner",
		Role:              "Breaks work into steps",
		SystemPrompt:      "Plan carefully",
		Provider:          "local_stub",
		Model:             "gpt-5.4",
		DelegationRuntime: "claude",
		Policies: AgentPolicy{
			AllowToolCalls:         true,
			AllowSandboxDelegation: true,
			AllowedSandboxRuntimes: []string{"codex"},
			MaxConsecutiveTurns:    2,
			MaxToolCallsPerTurn:    1,
			Weight:                 1,
		},
	})
	if err == nil {
		t.Fatal("expected delegation runtime outside allowed set to be rejected")
	}
}

func TestNewAgentRejectsInvalidPriorityAndWeight(t *testing.T) {
	_, err := NewAgent(Agent{
		ID:           "planner",
		Name:         "Planner",
		Role:         "Breaks work into steps",
		SystemPrompt: "Plan carefully",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
		Policies: AgentPolicy{
			Priority:            -1,
			Weight:              -1,
			MaxConsecutiveTurns: 2,
			MaxToolCallsPerTurn: 0,
		},
	})
	if err == nil {
		t.Fatal("expected invalid priority and weight to be rejected")
	}
}

func TestNewMessageRejectsDirectMessagesWithoutRecipients(t *testing.T) {
	_, err := NewMessage(Message{
		ID:             "msg-1",
		SessionID:      "session-1",
		ConversationID: "conversation-1",
		Sender:         AgentSender("planner"),
		Channel:        MessageChannelDirect,
		Kind:           MessageKindUtterance,
		Body:           "Need review",
		Timestamp:      time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected direct message without recipients to be rejected")
	}
}

func TestConversationPolicyRejectsDirectMessageWithoutReplyTarget(t *testing.T) {
	policy := DefaultConversationPolicy()

	message, err := NewMessage(Message{
		ID:             "msg-1",
		SessionID:      "session-1",
		ConversationID: "conversation-1",
		Sender:         AgentSender("planner"),
		ToAgentIDs:     []AgentID{"reviewer"},
		Channel:        MessageChannelDirect,
		Kind:           MessageKindUtterance,
		Body:           "Need review",
		Timestamp:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("NewMessage() error = %v", err)
	}

	if err := policy.ValidateMessage(message); err == nil {
		t.Fatal("expected conversation policy to reject direct message without reply target")
	}
}

func TestNewMessageAcceptsUserSenderWithoutSyntheticAgent(t *testing.T) {
	message, err := NewMessage(Message{
		ID:             "msg-1",
		SessionID:      "session-1",
		ConversationID: "conversation-1",
		Sender:         UserSender("operator"),
		Channel:        MessageChannelUser,
		Kind:           MessageKindUtterance,
		Body:           "Start the review flow",
		Timestamp:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("NewMessage() error = %v", err)
	}

	if message.Sender.Type != MessageSenderTypeUser {
		t.Fatalf("expected user sender type, got %q", message.Sender.Type)
	}
}

func TestNewMessageRejectsSystemChannelWithAgentSender(t *testing.T) {
	_, err := NewMessage(Message{
		ID:             "msg-1",
		SessionID:      "session-1",
		ConversationID: "conversation-1",
		Sender:         AgentSender("planner"),
		Channel:        MessageChannelSystem,
		Kind:           MessageKindEvent,
		Body:           "session resumed",
		Timestamp:      time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected system channel to reject agent sender")
	}
}

func TestSessionTransitions(t *testing.T) {
	session, err := NewSession("session-1", SessionModeFree, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	session, err = session.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	session, err = session.Pause()
	if err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	session, err = session.Resume()
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	session, err = session.Complete()
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if session.Status != SessionStatusCompleted {
		t.Fatalf("expected completed status, got %q", session.Status)
	}
}

func TestSessionRejectsInvalidTransition(t *testing.T) {
	session, err := NewSession("session-1", SessionModeFree, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if _, err := session.Resume(); err == nil {
		t.Fatal("expected resume from pending to fail")
	}
}

func TestNewWorkflowValidatesStepGraph(t *testing.T) {
	workflow, err := NewWorkflow(Workflow{
		ID:          "workflow-1",
		Name:        "Review Flow",
		EntryStepID: "draft",
		Steps: []WorkflowStep{
			{
				ID:          "draft",
				Name:        "Draft",
				Kind:        WorkflowStepKindAgent,
				ActorID:     "writer",
				NextStepIDs: []WorkflowStepID{"review"},
			},
			{
				ID:          "review",
				Name:        "Review",
				Kind:        WorkflowStepKindAgent,
				ActorID:     "reviewer",
				NextStepIDs: []WorkflowStepID{"done"},
			},
			{
				ID:   "done",
				Name: "Done",
				Kind: WorkflowStepKindStop,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewWorkflow() error = %v", err)
	}

	if workflow.EntryStepID != "draft" {
		t.Fatalf("expected entry step draft, got %q", workflow.EntryStepID)
	}
}

func TestWorkflowRejectsInvalidFanOutStep(t *testing.T) {
	_, err := NewWorkflow(Workflow{
		ID:          "workflow-1",
		Name:        "Broken Flow",
		EntryStepID: "split",
		Steps: []WorkflowStep{
			{
				ID:          "split",
				Name:        "Split",
				Kind:        WorkflowStepKindFanOut,
				NextStepIDs: []WorkflowStepID{"only-one"},
			},
			{
				ID:   "only-one",
				Name: "Only One",
				Kind: WorkflowStepKindStop,
			},
		},
	})
	if err == nil {
		t.Fatal("expected invalid fan-out workflow to be rejected")
	}
}

func TestNewWorkflowClonesStepsAndNextStepIDs(t *testing.T) {
	steps := []WorkflowStep{
		{
			ID:          "draft",
			Name:        "Draft",
			Kind:        WorkflowStepKindAgent,
			ActorID:     "writer",
			NextStepIDs: []WorkflowStepID{"done"},
		},
		{
			ID:   "done",
			Name: "Done",
			Kind: WorkflowStepKindStop,
		},
	}

	workflow, err := NewWorkflow(Workflow{
		ID:          "workflow-1",
		Name:        "Stable Flow",
		EntryStepID: "draft",
		Steps:       steps,
	})
	if err != nil {
		t.Fatalf("NewWorkflow() error = %v", err)
	}

	steps[0].Name = "Mutated"
	steps[0].NextStepIDs[0] = "mutated"

	if workflow.Steps[0].Name != "Draft" {
		t.Fatalf("expected cloned step name to remain Draft, got %q", workflow.Steps[0].Name)
	}

	if workflow.Steps[0].NextStepIDs[0] != "done" {
		t.Fatalf("expected cloned next step to remain done, got %q", workflow.Steps[0].NextStepIDs[0])
	}
}

func TestWorkflowRejectsFanInWithoutMultiplePredecessors(t *testing.T) {
	_, err := NewWorkflow(Workflow{
		ID:          "workflow-1",
		Name:        "Broken Merge",
		EntryStepID: "draft",
		Steps: []WorkflowStep{
			{
				ID:          "draft",
				Name:        "Draft",
				Kind:        WorkflowStepKindAgent,
				ActorID:     "writer",
				NextStepIDs: []WorkflowStepID{"merge"},
			},
			{
				ID:          "merge",
				Name:        "Merge",
				Kind:        WorkflowStepKindFanIn,
				NextStepIDs: []WorkflowStepID{"done"},
			},
			{
				ID:   "done",
				Name: "Done",
				Kind: WorkflowStepKindStop,
			},
		},
	})
	if err == nil {
		t.Fatal("expected fan-in step without multiple predecessors to be rejected")
	}
}
