package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"crew/internal/domain"
)

func TestSessionServiceCreateAndStart(t *testing.T) {
	repos := newFixture()
	service := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)

	session, err := service.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if session.Status != domain.SessionStatusPending {
		t.Fatalf("expected pending status, got %q", session.Status)
	}

	started, err := service.Start(context.Background(), SessionIDCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if started.Status != domain.SessionStatusRunning {
		t.Fatalf("expected running status, got %q", started.Status)
	}

	if len(repos.outbox.events) != 2 {
		t.Fatalf("expected 2 recorded outbox events, got %d", len(repos.outbox.events))
	}
}

func TestSessionServiceCreatePersistsActorCatalog(t *testing.T) {
	repos := newFixture()
	service := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)

	session, err := service.Create(context.Background(), CreateSessionCommand{
		Mode:         domain.SessionModeFree,
		ActorCatalog: "team-a",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if session.ActorCatalog != "team-a" {
		t.Fatalf("expected actor catalog team-a, got %q", session.ActorCatalog)
	}
}

func TestSessionServiceCreateRollsBackWhenOutboxFails(t *testing.T) {
	repos := newFixture()
	repos.outbox.failAdd = true
	service := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)

	_, err := service.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err == nil {
		t.Fatal("expected Create() to fail when outbox add fails")
	}

	if len(repos.sessions.sessions) != 0 {
		t.Fatalf("expected no sessions to persist after rollback, got %d", len(repos.sessions.sessions))
	}
}

func TestMessageServiceDispatchRequiresRunningSession(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "hello",
	})
	if err == nil {
		t.Fatal("expected dispatch in pending session to fail")
	}

	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}
}

func TestMessageServiceDispatchPersistsAndPublishes(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	message, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"reviewer"},
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "review this",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if message.ID != "message-1" {
		t.Fatalf("expected message-1, got %q", message.ID)
	}

	if len(repos.messages.messagesBySession[session.ID]) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(repos.messages.messagesBySession[session.ID]))
	}

	if repos.outbox.events[len(repos.outbox.events)-1].Topic != TopicMessageDispatched {
		t.Fatalf("expected last outbox topic %q, got %q", TopicMessageDispatched, repos.outbox.events[len(repos.outbox.events)-1].Topic)
	}

	vectorState, err := repos.vector.StateForSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("expected session vector state after dispatch: %v", err)
	}
	if vectorState.Status != VectorIndexStateStatusStale {
		t.Fatalf("expected stale session vector state after dispatch, got %q", vectorState.Status)
	}
}

func TestMessageServiceDispatchRejectsAgentHandoffOutsideAllowedSet(t *testing.T) {
	repos := newFixture()
	writer, err := domain.NewAgent(domain.Agent{
		ID:           "writer",
		Name:         "writer",
		Role:         "writer",
		SystemPrompt: "help",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
		Policies: domain.AgentPolicy{
			RequireDirectMention: true,
			AllowBroadcast:       true,
			AllowedHandoffs:      []domain.AgentID{"reviewer"},
			Weight:               1,
			MaxConsecutiveTurns:  1,
			MaxToolCallsPerTurn:  0,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(writer) error = %v", err)
	}
	planner, err := domain.NewAgent(domain.Agent{
		ID:           "planner",
		Name:         "planner",
		Role:         "planner",
		SystemPrompt: "help",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
		Policies: domain.AgentPolicy{
			CanInitiate:         true,
			AllowBroadcast:      true,
			AllowedHandoffs:     []domain.AgentID{"writer", "reviewer"},
			Weight:              1,
			MaxConsecutiveTurns: 1,
			MaxToolCallsPerTurn: 0,
		},
	})
	if err != nil {
		t.Fatalf("NewAgent(planner) error = %v", err)
	}
	repos.agents.agents["writer"] = writer
	repos.agents.agents["planner"] = planner

	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	directPolicy := domain.DefaultConversationPolicy()
	directPolicy.RequireReplyTargetForDirect = false
	_, err = messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.AgentSender("writer"),
		ToAgentIDs:     []domain.AgentID{"planner"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "@planner please review this",
		Policy:         &directPolicy,
	})
	if err == nil {
		t.Fatal("expected disallowed handoff to be rejected")
	}
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("expected ErrPrecondition, got %v", err)
	}
}

func TestMessageServiceDispatchRollsBackWhenOutboxFails(t *testing.T) {
	repos := newFixture()
	repos.outbox.failAdd = true
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err == nil {
		t.Fatal("expected session create to fail while outbox is failing")
	}

	repos.outbox.failAdd = false
	session, err = sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	repos.outbox.failAdd = true
	_, err = messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"reviewer"},
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "review this",
	})
	if err == nil {
		t.Fatal("expected Dispatch() to fail when outbox add fails")
	}

	if len(repos.messages.messagesBySession[session.ID]) != 0 {
		t.Fatalf("expected no messages to persist after rollback, got %d", len(repos.messages.messagesBySession[session.ID]))
	}
}

func TestWorkflowServiceAdvanceBlocksUnreadyFanIn(t *testing.T) {
	repos := newFixture()
	service := NewWorkflowService(repos.workflows, repos.outbox, repos.events, repos.tx, repos.clock)
	workflow := mustWorkflow()

	registered, err := service.Register(context.Background(), RegisterWorkflowCommand{Workflow: workflow})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	progression, err := service.Advance(context.Background(), AdvanceWorkflowCommand{
		WorkflowID:       registered.ID,
		CurrentStepID:    "draft",
		CompletedStepIDs: []domain.WorkflowStepID{"split"},
	})
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}

	if len(progression.ReadyNextSteps) != 0 {
		t.Fatalf("expected no ready next steps, got %d", len(progression.ReadyNextSteps))
	}

	if len(progression.BlockedNextSteps) != 1 || progression.BlockedNextSteps[0].ID != "merge" {
		t.Fatalf("expected merge to be blocked, got %+v", progression.BlockedNextSteps)
	}
}

func TestVectorServiceRecallFallsBackWhenVectorDisabled(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	vectorService := NewVectorService(repos.sessions, repos.messages, repos.vectorIndex, repos.vector, repos.embedder)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "review the architecture plan",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	repos.vectorIndex.status = VectorIndexStatusDisabled

	recall, err := vectorService.RecallSessionMessages(context.Background(), RecallSessionMessagesQuery{
		SessionID: session.ID,
		QueryText: "architecture plan",
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("RecallSessionMessages() error = %v", err)
	}
	if !recall.FallbackUsed {
		t.Fatalf("expected fallback recall when vector backend is disabled")
	}
	if recall.FallbackReason != "disabled" {
		t.Fatalf("expected disabled fallback reason, got %q", recall.FallbackReason)
	}
	if len(recall.Results) != 1 || recall.Results[0].Strategy != "lexical_fallback" {
		t.Fatalf("expected lexical fallback result, got %+v", recall.Results)
	}
}

func TestVectorServiceRecallUsesVectorResultsWhenReady(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	vectorService := NewVectorService(repos.sessions, repos.messages, repos.vectorIndex, repos.vector, repos.embedder)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	message, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "review the runtime recovery path",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	repos.vectorIndex.status = VectorIndexStatusReady
	now := repos.clock.Now()
	repos.vector.states[defaultVectorIndexName(session.ID)] = VectorIndexState{
		IndexName:     defaultVectorIndexName(session.ID),
		Provider:      "fake-index",
		Status:        VectorIndexStateStatusReady,
		LastRebuiltAt: &now,
		UpdatedAt:     now,
	}
	repos.vectorIndex.results = []VectorSearchResult{{MessageID: message.ID, Distance: 0.1}}

	recall, err := vectorService.RecallSessionMessages(context.Background(), RecallSessionMessagesQuery{
		SessionID: session.ID,
		QueryText: "runtime recovery",
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("RecallSessionMessages() error = %v", err)
	}
	if recall.FallbackUsed {
		t.Fatalf("expected vector recall without fallback, got %+v", recall)
	}
	if len(recall.Results) != 1 || recall.Results[0].Strategy != "vector" {
		t.Fatalf("expected vector recall result, got %+v", recall.Results)
	}
	if recall.Results[0].Distance == nil || *recall.Results[0].Distance != 0.1 {
		t.Fatalf("expected vector distance 0.1, got %+v", recall.Results[0].Distance)
	}
}

func TestVectorServiceSessionScopedOpsRejectUnknownSession(t *testing.T) {
	repos := newFixture()
	vectorService := NewVectorService(repos.sessions, repos.messages, repos.vectorIndex, repos.vector, repos.embedder)

	_, _, err := vectorService.Status(context.Background(), VectorStatusQuery{SessionID: "session-999"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected status to reject unknown session, got %v", err)
	}

	_, _, _, err = vectorService.Rebuild(context.Background(), VectorRebuildCommand{SessionID: "session-999"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected rebuild to reject unknown session, got %v", err)
	}
}

func TestAgentTaskIDValidateRejectsPathTraversalShapes(t *testing.T) {
	testCases := []AgentTaskID{
		"../escape",
		`..\\escape`,
		"/tmp/task",
		".",
		"..",
	}

	for _, testCase := range testCases {
		if err := testCase.Validate(); err == nil {
			t.Fatalf("expected task id %q to be rejected", testCase)
		}
	}
}

func TestFreeModeServiceStepDispatchesAgentReply(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, fakeLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "review the runtime path",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	result, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if !result.Stepped {
		t.Fatalf("expected stepped result, got %+v", result)
	}
	if result.Agent == nil || result.Agent.ID != "planner" {
		t.Fatalf("expected planner to be selected first, got %+v", result.Agent)
	}
	if result.OrchestrationMode != OrchestrationModeDeterministic {
		t.Fatalf("expected deterministic orchestration mode, got %q", result.OrchestrationMode)
	}
	if !slices.Equal(result.EligibleAgentIDs, []domain.AgentID{"planner"}) {
		t.Fatalf("unexpected eligible agent ids %v", result.EligibleAgentIDs)
	}
	if !slices.Equal(result.OrderedCandidateIDs, []domain.AgentID{"planner"}) {
		t.Fatalf("unexpected ordered candidate ids %v", result.OrderedCandidateIDs)
	}
	if len(result.BlockedAgents) != 2 {
		t.Fatalf("expected non-initiating specialists to be blocked, got %+v", result.BlockedAgents)
	}
	if result.Message == nil || result.Message.Sender.ID != "planner" {
		t.Fatalf("expected planner message, got %+v", result.Message)
	}
	if len(repos.messages.messagesBySession[session.ID]) != 2 {
		t.Fatalf("expected 2 persisted messages after step, got %d", len(repos.messages.messagesBySession[session.ID]))
	}
}

func TestFreeModeServiceStepRespectsDirectRecipients(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, fakeLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	directPolicy := domain.DefaultConversationPolicy()
	directPolicy.RequireReplyTargetForDirect = false
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"reviewer"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "review this directly",
		Policy:         &directPolicy,
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	result, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if !result.Stepped || result.Agent == nil {
		t.Fatalf("expected stepped direct result, got %+v", result)
	}
	if result.Agent.ID != "reviewer" {
		t.Fatalf("expected reviewer to be the only eligible direct recipient, got %q", result.Agent.ID)
	}
	if !slices.Equal(result.EligibleAgentIDs, []domain.AgentID{"reviewer"}) {
		t.Fatalf("unexpected eligible agent ids %v", result.EligibleAgentIDs)
	}
	if len(result.BlockedAgents) != 2 {
		t.Fatalf("expected 2 blocked agents for direct routing, got %+v", result.BlockedAgents)
	}
}

func TestFreeModeServiceDirectRecipientsRemainTargetedUntilEachReplies(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, fakeLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	directPolicy := domain.DefaultConversationPolicy()
	directPolicy.RequireReplyTargetForDirect = false
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"planner", "reviewer"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "planner and reviewer, both reply",
		Policy:         &directPolicy,
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	first, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if !first.Stepped || first.Agent == nil || first.Agent.ID != "planner" {
		t.Fatalf("expected planner to reply first, got %+v", first)
	}

	second, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if !second.Stepped || second.Agent == nil || second.Agent.ID != "reviewer" {
		t.Fatalf("expected reviewer to remain targeted on second step, got %+v", second)
	}
	if !slices.Equal(second.EligibleAgentIDs, []domain.AgentID{"reviewer"}) {
		t.Fatalf("expected only reviewer eligible on second step, got %v", second.EligibleAgentIDs)
	}
}

func TestFreeModeServiceReplyObligationsPrioritizeOlderUserTargetBeforeAgentHandoff(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, obligationAwareLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	directPolicy := domain.DefaultConversationPolicy()
	directPolicy.RequireReplyTargetForDirect = false
	userMessage, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"planner", "reviewer"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "@planner @reviewer both reply",
		Policy:         &directPolicy,
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	first, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if !first.Stepped || first.Agent == nil || first.Agent.ID != "planner" {
		t.Fatalf("expected planner to satisfy the first user obligation, got %+v", first)
	}
	if first.Message == nil || first.Message.ReplyTo != userMessage.ID {
		t.Fatalf("expected planner reply to thread to user anchor %q, got %+v", userMessage.ID, first.Message)
	}
	if got := first.Message.Metadata["addressed_to_type"]; got != "user" {
		t.Fatalf("expected planner reply addressed_to_type user, got %+v", first.Message.Metadata)
	}
	if got := first.Message.Metadata["addressed_to_id"]; got != "operator" {
		t.Fatalf("expected planner reply addressed_to_id operator, got %+v", first.Message.Metadata)
	}

	second, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if !second.Stepped || second.Agent == nil || second.Agent.ID != "reviewer" {
		t.Fatalf("expected reviewer to satisfy the older user obligation next, got %+v", second)
	}
	if second.Message == nil || second.Message.ReplyTo != userMessage.ID {
		t.Fatalf("expected reviewer user reply to thread to original user anchor %q, got %+v", userMessage.ID, second.Message)
	}
	if got := second.Message.Metadata["addressed_to_type"]; got != "user" {
		t.Fatalf("expected reviewer user reply addressed_to_type user, got %+v", second.Message.Metadata)
	}

	third, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("third Step() error = %v", err)
	}
	if !third.Stepped || third.Agent == nil || third.Agent.ID != "reviewer" {
		t.Fatalf("expected reviewer to satisfy the later planner handoff after the user obligation, got %+v", third)
	}
	if third.Message == nil || third.Message.Channel != domain.MessageChannelDirect {
		t.Fatalf("expected reviewer handoff reply to be direct, got %+v", third.Message)
	}
	if !slices.Equal(third.Message.ToAgentIDs, []domain.AgentID{"planner"}) {
		t.Fatalf("expected reviewer handoff reply to target planner, got %+v", third.Message)
	}
	if third.Message.ReplyTo != first.Message.ID {
		t.Fatalf("expected reviewer handoff reply to thread to planner handoff %q, got %+v", first.Message.ID, third.Message)
	}
}

func TestFreeModeServiceTreatsAgentMentionsAsRealHandoffs(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, incidentalMentionLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "please plan the work",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	first, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if !first.Stepped || first.Agent == nil || first.Agent.ID != "planner" {
		t.Fatalf("expected planner first step, got %+v", first)
	}

	second, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if !second.Stepped {
		t.Fatalf("expected ordinary free-mode continuation, got %+v", second)
	}
	if second.Agent == nil || second.Agent.ID != "reviewer" {
		t.Fatalf("expected explicit @reviewer mention to target reviewer, got %+v", second)
	}
	if !slices.Equal(second.EligibleAgentIDs, []domain.AgentID{"reviewer"}) {
		t.Fatalf("expected explicit @reviewer mention to create reviewer-only eligibility, got %+v", second)
	}
}

func TestFreeModeServiceNormalizesBareSentenceHandoffToHandle(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, bareHandoffLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "please plan the work",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	first, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if first.Message == nil || !strings.Contains(first.Message.Body, "@reviewer") {
		t.Fatalf("expected bare reviewer handoff to be normalized, got %+v", first.Message)
	}
	second, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if second.Agent == nil || second.Agent.ID != "reviewer" {
		t.Fatalf("expected reviewer to become eligible after normalization, got %+v", second)
	}
}

func TestFreeModeServiceRoutesCompletedAgentReplyBackToUser(t *testing.T) {
	repos := newFixture()
	service := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeMode := NewFreeModeService(
		repos.sessions,
		repos.messages,
		repos.agents,
		fakeOrchestrator{},
		obligationAwareLLMProvider{},
		messageService,
	)

	session, err := service.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "hello",
	}); err != nil {
		t.Fatalf("Dispatch(user) error = %v", err)
	}

	first, err := freeMode.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	second, err := freeMode.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	third, err := freeMode.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("third Step() error = %v", err)
	}

	if third.Message == nil || third.Message.Sender.ID != "planner" {
		t.Fatalf("expected planner to respond after reviewer completion, got %+v", third.Message)
	}
	if third.Message.ReplyTo != "message-1" {
		t.Fatalf("expected planner completion to thread back to user message, got %+v", third.Message)
	}
	if got := third.Message.Metadata["addressed_to_type"]; got != "user" {
		t.Fatalf("expected planner completion addressed_to_type user, got %+v", third.Message.Metadata)
	}
	if got := third.Message.Metadata["addressed_to_id"]; got != "operator" {
		t.Fatalf("expected planner completion addressed_to_id operator, got %+v", third.Message.Metadata)
	}
	if second.Message == nil || second.Message.ReplyTo != first.Message.ID {
		t.Fatalf("expected reviewer reply to satisfy planner handoff first, got %+v", second.Message)
	}
}

func TestFreeModeServiceStopsAfterDirectSpecialistReplyWithoutHandoff(t *testing.T) {
	repos := newFixture()
	service := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeMode := NewFreeModeService(
		repos.sessions,
		repos.messages,
		repos.agents,
		fakeOrchestrator{},
		fakeLLMProvider{},
		messageService,
	)

	session, err := service.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"reviewer"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "hey @reviewer",
		Policy: &domain.ConversationPolicy{
			MaxTurns:                    domain.DefaultConversationPolicy().MaxTurns,
			MaxConsecutiveTurnsPerAgent: domain.DefaultConversationPolicy().MaxConsecutiveTurnsPerAgent,
			RequireReplyTargetForDirect: false,
		},
	}); err != nil {
		t.Fatalf("Dispatch(user) error = %v", err)
	}

	first, err := freeMode.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if first.Agent == nil || first.Agent.ID != "reviewer" {
		t.Fatalf("expected reviewer first, got %+v", first.Agent)
	}

	second, err := freeMode.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if second.Stepped {
		t.Fatalf("expected no follow-up step after specialist reply without handoff, got %+v", second)
	}
	if second.Reason != stepReasonNoEligibleAgents {
		t.Fatalf("expected no eligible agents stop reason, got %+v", second)
	}
}

func TestBodyMentionsAgentRequiresExactHandleToken(t *testing.T) {
	if !bodyMentionsAgent("Implementation is blocked in read-only mode. @planner", "planner") {
		t.Fatal("expected exact handle mention to count")
	}
	if !bodyMentionsAgent("If needed I can ask @planner later.", "planner") {
		t.Fatal("expected inline exact handle mention to count")
	}
	if bodyMentionsAgent("@plannering should not match planner", "planner") {
		t.Fatal("expected longer handle prefix not to count")
	}
}

func TestFreeModeServiceLatestSpeakerRoutingIgnoresOlderOutstandingObligation(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, obligationAwareLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	directPolicy := domain.DefaultConversationPolicy()
	directPolicy.RequireReplyTargetForDirect = false
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"planner", "reviewer"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "@planner @reviewer both reply",
		Policy:         &directPolicy,
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	first, err := freeModeService.Step(context.Background(), StepSessionCommand{
		SessionID:        session.ID,
		ReplyRoutingMode: ReplyRoutingModeLatestSpeaker,
	})
	if err != nil {
		t.Fatalf("first Step() error = %v", err)
	}
	if !first.Stepped || first.Agent == nil || first.Agent.ID != "planner" {
		t.Fatalf("expected planner to reply first, got %+v", first)
	}

	second, err := freeModeService.Step(context.Background(), StepSessionCommand{
		SessionID:        session.ID,
		ReplyRoutingMode: ReplyRoutingModeLatestSpeaker,
	})
	if err != nil {
		t.Fatalf("second Step() error = %v", err)
	}
	if !second.Stepped || second.Agent == nil || second.Agent.ID != "reviewer" {
		t.Fatalf("expected reviewer to reply second, got %+v", second)
	}
	if second.Message == nil || second.Message.Channel != domain.MessageChannelDirect {
		t.Fatalf("expected latest-speaker routing to produce a direct reviewer reply, got %+v", second.Message)
	}
	if !slices.Equal(second.Message.ToAgentIDs, []domain.AgentID{"planner"}) {
		t.Fatalf("expected latest-speaker routing to target planner, got %+v", second.Message)
	}
	if second.Message.ReplyTo != first.Message.ID {
		t.Fatalf("expected latest-speaker routing to thread to planner message %q, got %+v", first.Message.ID, second.Message)
	}
}

func TestFreeModeServiceStepDelegatesSandboxTaskAndPersistsResultMessages(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, repos.sandbox, repos.outbox, repos.tx, repos.clock)
	repos.sandbox.result = SandboxTaskExecutionResult{
		Summary: "updated README",
		Artifacts: []SandboxTaskArtifact{
			{Path: "README.md", Description: "modified"},
		},
		CompletedAt: repos.clock.Now().Add(time.Minute),
	}

	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, sandboxDelegatingLLMProvider{}, messageService).
		WithSandboxDelegation(coordinationService, "/workspace/source")

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "sandbox: update the README",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	result, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if result.SandboxTask == nil || result.SandboxTask.Status != SandboxTaskStatusSucceeded {
		t.Fatalf("expected succeeded sandbox task in step result, got %+v", result.SandboxTask)
	}
	if result.SandboxTask.AssignedAgentID != "planner" {
		t.Fatalf("expected delegated sandbox task to stay assigned to planner, got %+v", result.SandboxTask)
	}
	if result.SandboxTask.RuntimeName != "codex" {
		t.Fatalf("expected planner to delegate to codex runtime, got %+v", result.SandboxTask)
	}
	if result.SandboxHandoff == nil || result.SandboxHandoff.TaskID != result.SandboxTask.ID {
		t.Fatalf("expected handoff targeting sandbox task, got %+v", result.SandboxHandoff)
	}
	if len(result.TaskMessages) != 2 {
		t.Fatalf("expected 2 sandbox status messages, got %d", len(result.TaskMessages))
	}
	if len(repos.tasks.tasks) != 1 {
		t.Fatalf("expected 1 persisted sandbox task, got %d", len(repos.tasks.tasks))
	}
	if len(repos.messages.messagesBySession[session.ID]) != 4 {
		t.Fatalf("expected user + agent + 2 sandbox messages, got %d", len(repos.messages.messagesBySession[session.ID]))
	}
}

func TestFreeModeServiceStepRejectsSandboxDelegationToUnavailableAgentRuntime(t *testing.T) {
	repos := newFixture()
	planner := repos.agents.agents["planner"]
	planner.DelegationRuntime = "claude"
	planner.Policies.AllowedSandboxRuntimes = []string{"claude"}
	repos.agents.agents["planner"] = planner
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, repos.sandbox, repos.outbox, repos.tx, repos.clock)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, sandboxDelegatingLLMProvider{}, messageService).
		WithSandboxDelegation(coordinationService, "/workspace/source")

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "sandbox: update the README",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	result, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if result.SandboxTask != nil {
		t.Fatalf("expected no sandbox task for unavailable runtime, got %+v", result.SandboxTask)
	}
	if len(result.TaskMessages) != 1 || result.TaskMessages[0].Kind != domain.MessageKindError {
		t.Fatalf("expected one sandbox runtime error message, got %+v", result.TaskMessages)
	}
	if len(repos.tasks.tasks) != 0 {
		t.Fatalf("expected no persisted sandbox tasks, got %d", len(repos.tasks.tasks))
	}
}

func TestFreeModeServiceStepRejectsMalformedSandboxRequestBeforePersistingReply(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, malformedSandboxDelegatingLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "sandbox: update the README",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	_, err = freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err == nil {
		t.Fatal("expected Step() to reject malformed sandbox request")
	}

	if len(repos.messages.messagesBySession[session.ID]) != 1 {
		t.Fatalf("expected only the original user message to persist, got %d messages", len(repos.messages.messagesBySession[session.ID]))
	}
	if len(repos.tasks.tasks) != 0 {
		t.Fatalf("expected no sandbox task persistence after malformed request, got %d tasks", len(repos.tasks.tasks))
	}
}

func TestFreeModeServiceStepReturnsNoMessagesReason(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, fakeLLMProvider{}, NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids))

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	result, err := freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if result.Stepped || result.Reason != "no_messages" {
		t.Fatalf("expected no_messages no-op, got %+v", result)
	}
}

func TestFreeModeServiceStepRejectsSequentialSessions(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, fakeLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeSequential})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "step this workflow",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	_, err = freeModeService.Step(context.Background(), StepSessionCommand{SessionID: session.ID})
	if err == nil {
		t.Fatal("expected Step() to reject sequential sessions")
	}
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}
	if len(repos.messages.messagesBySession[session.ID]) != 1 {
		t.Fatalf("expected no agent reply to be persisted, got %d messages", len(repos.messages.messagesBySession[session.ID]))
	}
}

func TestFreeModeServiceStepScopesToRequestedConversation(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, recordingLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	messageA1, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-a",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "first thread context",
	})
	if err != nil {
		t.Fatalf("Dispatch(conversation-a #1) error = %v", err)
	}
	messageA2, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-a",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "target thread latest",
		ReplyTo:        messageA1.ID,
	})
	if err != nil {
		t.Fatalf("Dispatch(conversation-a #2) error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-b",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "newer unrelated thread",
	}); err != nil {
		t.Fatalf("Dispatch(conversation-b) error = %v", err)
	}

	result, err := freeModeService.Step(context.Background(), StepSessionCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-a",
	})
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if !result.Stepped {
		t.Fatalf("expected stepped result, got %+v", result)
	}
	if result.ConversationID != "conversation-a" {
		t.Fatalf("expected stepped conversation-a, got %q", result.ConversationID)
	}
	if result.Message == nil {
		t.Fatal("expected generated message")
	}
	if result.Message.ConversationID != "conversation-a" {
		t.Fatalf("expected generated reply in conversation-a, got %q", result.Message.ConversationID)
	}
	if result.Message.ReplyTo != messageA2.ID {
		t.Fatalf("expected reply to latest conversation-a message %q, got %q", messageA2.ID, result.Message.ReplyTo)
	}
	if result.Message.Body != "planner saw: target thread latest" {
		t.Fatalf("expected generation to use conversation-a context, got %q", result.Message.Body)
	}
}

func TestFreeModeServiceAutoStopsAtMaxSteps(t *testing.T) {
	repos := newFixture()
	planner := repos.agents.agents["planner"]
	planner.Policies.AllowedHandoffs = append(planner.Policies.AllowedHandoffs, "planner")
	repos.agents.agents["planner"] = planner
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, selfHandoffLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "plan the next step",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	result, err := freeModeService.Auto(context.Background(), AutoSessionCommand{
		SessionID: session.ID,
		MaxSteps:  2,
	})
	if err != nil {
		t.Fatalf("Auto() error = %v", err)
	}
	if result.CompletedSteps != 2 {
		t.Fatalf("expected 2 completed steps, got %d", result.CompletedSteps)
	}
	if result.StopReason != "max_steps_reached" {
		t.Fatalf("expected max_steps_reached stop reason, got %q", result.StopReason)
	}
	if !result.VectorStateMarkedStale {
		t.Fatal("expected vector state to be marked stale after persisted auto steps")
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(result.Steps))
	}
	if !slices.Equal(result.SelectedAgentIDs, []domain.AgentID{"planner", "planner"}) {
		t.Fatalf("unexpected selected agents %v", result.SelectedAgentIDs)
	}
}

func TestFreeModeServiceAutoStopsAtConversationTurnLimit(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, fakeLLMProvider{}, NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids))

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	messages := make([]domain.Message, 0, domain.DefaultConversationPolicy().MaxTurns)
	for idx := 0; idx < domain.DefaultConversationPolicy().MaxTurns; idx++ {
		recordedAt := repos.clock.Now().Add(time.Duration(idx) * time.Second)
		message, err := domain.NewMessage(domain.Message{
			ID:             domain.MessageID(fmt.Sprintf("message-preloaded-%d", idx+1)),
			SessionID:      session.ID,
			ConversationID: "conversation-1",
			Sender:         domain.UserSender("operator"),
			Channel:        domain.MessageChannelUser,
			Kind:           domain.MessageKindUtterance,
			Body:           fmt.Sprintf("message %d", idx+1),
			Timestamp:      recordedAt,
		})
		if err != nil {
			t.Fatalf("NewMessage() error = %v", err)
		}
		messages = append(messages, message)
	}
	repos.messages.messagesBySession[session.ID] = messages

	result, err := freeModeService.Auto(context.Background(), AutoSessionCommand{
		SessionID: session.ID,
		MaxSteps:  3,
	})
	if err != nil {
		t.Fatalf("Auto() error = %v", err)
	}
	if result.CompletedSteps != 0 {
		t.Fatalf("expected 0 completed steps at max-turn boundary, got %d", result.CompletedSteps)
	}
	if result.StopReason != "policy_max_turns_reached" {
		t.Fatalf("expected policy_max_turns_reached, got %q", result.StopReason)
	}
}

func TestFreeModeServiceAutoStopsOnConsecutiveTurnLimit(t *testing.T) {
	repos := newFixture()
	repos.agents.agents = map[domain.AgentID]domain.Agent{
		"planner": mustAgent("planner"),
	}
	planner := repos.agents.agents["planner"]
	planner.Policies.AllowedHandoffs = append(planner.Policies.AllowedHandoffs, "planner")
	repos.agents.agents["planner"] = planner
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	messageService := NewMessageService(repos.sessions, repos.messages, repos.agents, repos.vector, repos.outbox, repos.tx, repos.clock, repos.ids)
	freeModeService := NewFreeModeService(repos.sessions, repos.messages, repos.agents, fakeOrchestrator{}, selfHandoffLLMProvider{}, messageService)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(context.Background(), SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := messageService.Dispatch(context.Background(), DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "start planning",
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	result, err := freeModeService.Auto(context.Background(), AutoSessionCommand{
		SessionID: session.ID,
		MaxSteps:  5,
	})
	if err != nil {
		t.Fatalf("Auto() error = %v", err)
	}
	if result.CompletedSteps != 2 {
		t.Fatalf("expected 2 completed steps before consecutive limit, got %d", result.CompletedSteps)
	}
	if result.StopReason != "policy_max_consecutive_turns_reached" {
		t.Fatalf("expected policy_max_consecutive_turns_reached, got %q", result.StopReason)
	}
	if !slices.Equal(result.SelectedAgentIDs, []domain.AgentID{"planner", "planner"}) {
		t.Fatalf("unexpected selected agents %v", result.SelectedAgentIDs)
	}
}

func TestWorkflowServiceAdvanceReadiesFanInWhenTwoPredecessorsCompleted(t *testing.T) {
	repos := newFixture()
	service := NewWorkflowService(repos.workflows, repos.outbox, repos.events, repos.tx, repos.clock)

	workflow, err := service.Register(context.Background(), RegisterWorkflowCommand{Workflow: mustWorkflow()})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	progression, err := service.Advance(context.Background(), AdvanceWorkflowCommand{
		WorkflowID:       workflow.ID,
		CurrentStepID:    "review",
		CompletedStepIDs: []domain.WorkflowStepID{"split", "draft"},
	})
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}

	if len(progression.ReadyNextSteps) != 1 || progression.ReadyNextSteps[0].ID != "merge" {
		t.Fatalf("expected merge to be ready, got %+v", progression.ReadyNextSteps)
	}
}

func TestWorkflowServiceAdvanceRejectsJumpToUnreadyCurrentStep(t *testing.T) {
	repos := newFixture()
	service := NewWorkflowService(repos.workflows, repos.outbox, repos.events, repos.tx, repos.clock)

	workflow, err := service.Register(context.Background(), RegisterWorkflowCommand{Workflow: mustWorkflow()})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err = service.Advance(context.Background(), AdvanceWorkflowCommand{
		WorkflowID:    workflow.ID,
		CurrentStepID: "merge",
		CompletedStepIDs: []domain.WorkflowStepID{
			"draft",
		},
	})
	if err == nil {
		t.Fatal("expected jump to unready merge step to fail")
	}

	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("expected ErrPrecondition, got %v", err)
	}
}

func TestWorkflowServiceAdvanceRequiresAllFanInPredecessors(t *testing.T) {
	repos := newFixture()
	service := NewWorkflowService(repos.workflows, repos.outbox, repos.events, repos.tx, repos.clock)

	workflow, err := service.Register(context.Background(), RegisterWorkflowCommand{Workflow: mustThreeWayMergeWorkflow()})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	progression, err := service.Advance(context.Background(), AdvanceWorkflowCommand{
		WorkflowID:       workflow.ID,
		CurrentStepID:    "draft-a",
		CompletedStepIDs: []domain.WorkflowStepID{"split", "draft-b"},
	})
	if err != nil {
		t.Fatalf("Advance() error = %v", err)
	}

	if len(progression.ReadyNextSteps) != 0 {
		t.Fatalf("expected no ready next steps for incomplete 3-way merge, got %+v", progression.ReadyNextSteps)
	}

	if len(progression.BlockedNextSteps) != 1 || progression.BlockedNextSteps[0].ID != "merge" {
		t.Fatalf("expected merge to stay blocked, got %+v", progression.BlockedNextSteps)
	}
}

func TestWorkflowServiceRegisterRollsBackWhenOutboxFails(t *testing.T) {
	repos := newFixture()
	repos.outbox.failAdd = true
	service := NewWorkflowService(repos.workflows, repos.outbox, repos.events, repos.tx, repos.clock)

	_, err := service.Register(context.Background(), RegisterWorkflowCommand{Workflow: mustWorkflow()})
	if err == nil {
		t.Fatal("expected Register() to fail when outbox add fails")
	}

	if len(repos.workflows.workflows) != 0 {
		t.Fatalf("expected no workflows to persist after rollback, got %d", len(repos.workflows.workflows))
	}
}

func TestCoordinationServiceCreateSandboxTaskPersistsTask(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, nil, repos.outbox, repos.tx, repos.clock)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	task, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:             "task-1",
		SessionID:          session.ID,
		ConversationID:     "conversation-1",
		RequestedByAgentID: "planner",
		AssignedAgentID:    "writer",
		AssignedProvider:   AgentProviderClassSandboxedRuntime,
		RuntimeName:        "codex",
		WorkspaceRoot:      "/tmp/worktree",
		PermissionProfile:  SandboxPermissionPatch,
		Instruction:        "apply the requested patch",
		Metadata: map[string]any{
			"origin": "planner",
		},
	})
	if err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	if task.Status != SandboxTaskStatusPending {
		t.Fatalf("expected pending task, got %q", task.Status)
	}
	if _, err := repos.tasks.GetTaskByID(context.Background(), task.ID); err != nil {
		t.Fatalf("expected persisted task lookup to work: %v", err)
	}
	if len(repos.outbox.events) != 2 || repos.outbox.events[len(repos.outbox.events)-1].Topic != TopicAgentTaskCreated {
		t.Fatalf("expected task created event, got %+v", repos.outbox.events)
	}
}

func TestCoordinationServiceRecordAgentHandoffPersistsHandoff(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, nil, repos.outbox, repos.tx, repos.clock)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:            "task-1",
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		AssignedAgentID:   "writer",
		AssignedProvider:  AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     "/tmp/worktree",
		PermissionProfile: SandboxPermissionPatch,
		Instruction:       "apply the requested patch",
	}); err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	handoff, err := coordinationService.RecordAgentHandoff(context.Background(), CreateAgentHandoffCommand{
		HandoffID:       "handoff-1",
		SessionID:       session.ID,
		ConversationID:  "conversation-1",
		SourceMessageID: "message-1",
		TaskID:          "task-1",
		FromAgentID:     "planner",
		ToAgentID:       "writer",
		ToProviderClass: AgentProviderClassSandboxedRuntime,
		Reason:          "writer should apply the patch",
	})
	if err != nil {
		t.Fatalf("RecordAgentHandoff() error = %v", err)
	}

	handoffs, err := repos.tasks.ListHandoffsBySessionID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ListHandoffsBySessionID() error = %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].ID != handoff.ID {
		t.Fatalf("expected persisted handoff, got %+v", handoffs)
	}
	if len(repos.outbox.events) != 3 || repos.outbox.events[len(repos.outbox.events)-1].Topic != TopicAgentHandoffCreated {
		t.Fatalf("expected handoff created event, got %+v", repos.outbox.events)
	}
}

func TestCoordinationServiceRecordAgentHandoffRejectsCrossSessionTask(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, nil, repos.outbox, repos.tx, repos.clock)

	sessionA, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create(sessionA) error = %v", err)
	}
	sessionB, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create(sessionB) error = %v", err)
	}
	if _, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:            "task-1",
		SessionID:         sessionB.ID,
		ConversationID:    "conversation-b",
		AssignedAgentID:   "writer",
		AssignedProvider:  AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     "/tmp/worktree",
		PermissionProfile: SandboxPermissionPatch,
		Instruction:       "apply the requested patch",
	}); err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	_, err = coordinationService.RecordAgentHandoff(context.Background(), CreateAgentHandoffCommand{
		HandoffID:       "handoff-1",
		SessionID:       sessionA.ID,
		ConversationID:  "conversation-a",
		SourceMessageID: "message-1",
		TaskID:          "task-1",
		FromAgentID:     "planner",
		ToAgentID:       "writer",
		ToProviderClass: AgentProviderClassSandboxedRuntime,
		Reason:          "writer should apply the patch",
	})
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("expected ErrPrecondition for cross-session handoff, got %v", err)
	}
}

func TestCoordinationServiceExecuteSandboxTaskPersistsSuccess(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	repos.sandbox.result = SandboxTaskExecutionResult{
		Summary: "applied patch successfully",
		Artifacts: []SandboxTaskArtifact{
			{Path: "main.go", Description: "updated command wiring"},
		},
		Metadata: map[string]any{"runtime": "codex"},
	}
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, repos.sandbox, repos.outbox, repos.tx, repos.clock)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:            "task-1",
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		AssignedAgentID:   "writer",
		AssignedProvider:  AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     "/tmp/worktree",
		PermissionProfile: SandboxPermissionPatch,
		Instruction:       "apply the requested patch",
	}); err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	task, err := coordinationService.ExecuteSandboxTask(context.Background(), ExecuteSandboxTaskCommand{TaskID: "task-1"})
	if err != nil {
		t.Fatalf("ExecuteSandboxTask() error = %v", err)
	}
	if task.Status != SandboxTaskStatusSucceeded {
		t.Fatalf("expected succeeded task, got %q", task.Status)
	}
	if task.RuntimeName != "codex" {
		t.Fatalf("expected runtime name codex, got %q", task.RuntimeName)
	}
	if len(task.Artifacts) != 1 || task.Artifacts[0].Path != "main.go" {
		t.Fatalf("expected persisted artifacts, got %+v", task.Artifacts)
	}
	if len(repos.outbox.events) != 4 {
		t.Fatalf("expected session + created + running + completed events, got %+v", repos.outbox.events)
	}
	if repos.outbox.events[len(repos.outbox.events)-2].Topic != TopicAgentTaskUpdated || repos.outbox.events[len(repos.outbox.events)-1].Topic != TopicAgentTaskUpdated {
		t.Fatalf("expected running and completed task updates, got %+v", repos.outbox.events)
	}
}

func TestCoordinationServiceExecuteSandboxTaskPersistsFailureState(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	repos.sandbox.err = errors.New("sandbox command failed")
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, repos.sandbox, repos.outbox, repos.tx, repos.clock)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:            "task-1",
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		AssignedAgentID:   "writer",
		AssignedProvider:  AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     "/tmp/worktree",
		PermissionProfile: SandboxPermissionPatch,
		Instruction:       "apply the requested patch",
	}); err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	task, err := coordinationService.ExecuteSandboxTask(context.Background(), ExecuteSandboxTaskCommand{TaskID: "task-1"})
	if err != nil {
		t.Fatalf("ExecuteSandboxTask() transport error = %v", err)
	}
	if task.Status != SandboxTaskStatusFailed {
		t.Fatalf("expected failed task, got %q", task.Status)
	}
	if task.ErrorMessage == "" {
		t.Fatal("expected persisted task error message")
	}
}

func TestCoordinationServiceExecuteSandboxTaskRejectsMismatchedRuntime(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	repos.sandbox.name = "claude"
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, repos.sandbox, repos.outbox, repos.tx, repos.clock)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:            "task-1",
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		AssignedAgentID:   "writer",
		AssignedProvider:  AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     "/tmp/worktree",
		PermissionProfile: SandboxPermissionPatch,
		Instruction:       "apply the requested patch",
	}); err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	_, err = coordinationService.ExecuteSandboxTask(context.Background(), ExecuteSandboxTaskCommand{TaskID: "task-1"})
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("expected ErrPrecondition for mismatched runtime, got %v", err)
	}

	task, taskErr := repos.tasks.GetTaskByID(context.Background(), "task-1")
	if taskErr != nil {
		t.Fatalf("GetTaskByID() error = %v", taskErr)
	}
	if task.Status != SandboxTaskStatusPending {
		t.Fatalf("expected task to remain pending, got %q", task.Status)
	}
}

func TestCoordinationServiceExecuteSandboxTaskRejectsMissingRuntime(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, nil, repos.outbox, repos.tx, repos.clock)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:            "task-1",
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		AssignedAgentID:   "writer",
		AssignedProvider:  AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     "/tmp/worktree",
		PermissionProfile: SandboxPermissionPatch,
		Instruction:       "apply the requested patch",
	}); err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	_, err = coordinationService.ExecuteSandboxTask(context.Background(), ExecuteSandboxTaskCommand{TaskID: "task-1"})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestCoordinationServiceExecuteSandboxTaskRecoversTerminalStateWhenFinalPersistFails(t *testing.T) {
	repos := newFixture()
	sessionService := NewSessionService(repos.sessions, repos.outbox, repos.tx, repos.clock, repos.ids)
	repos.sandbox.result = SandboxTaskExecutionResult{
		Summary: "applied patch successfully",
	}
	repos.outbox.failOnAddCall = 4
	coordinationService := NewCoordinationService(repos.sessions, repos.agents, repos.tasks, repos.sandbox, repos.outbox, repos.tx, repos.clock)

	session, err := sessionService.Create(context.Background(), CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := coordinationService.CreateSandboxTask(context.Background(), CreateSandboxTaskCommand{
		TaskID:            "task-1",
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		AssignedAgentID:   "writer",
		AssignedProvider:  AgentProviderClassSandboxedRuntime,
		RuntimeName:       "codex",
		WorkspaceRoot:     "/tmp/worktree",
		PermissionProfile: SandboxPermissionPatch,
		Instruction:       "apply the requested patch",
	}); err != nil {
		t.Fatalf("CreateSandboxTask() error = %v", err)
	}

	task, err := coordinationService.ExecuteSandboxTask(context.Background(), ExecuteSandboxTaskCommand{TaskID: "task-1"})
	if err == nil {
		t.Fatal("expected ExecuteSandboxTask() to report terminal event persistence failure")
	}
	if task.Status != SandboxTaskStatusSucceeded {
		t.Fatalf("expected recovered terminal task status succeeded, got %q", task.Status)
	}

	persisted, getErr := repos.tasks.GetTaskByID(context.Background(), "task-1")
	if getErr != nil {
		t.Fatalf("GetTaskByID() error = %v", getErr)
	}
	if persisted.Status != SandboxTaskStatusSucceeded || persisted.CompletedAt == nil {
		t.Fatalf("expected persisted terminal task recovery, got %+v", persisted)
	}
}

type fixture struct {
	sessions    *fakeSessionRepository
	messages    *fakeMessageRepository
	workflows   *fakeWorkflowRepository
	agents      *fakeAgentRepository
	tasks       *fakeSandboxTaskRepository
	sandbox     *fakeSandboxedAgentRuntime
	vector      *fakeVectorAdmin
	vectorIndex *fakeVectorIndex
	outbox      *fakeOutbox
	events      *fakeEventBus
	tx          *fakeUnitOfWork
	clock       fakeClock
	ids         *fakeIDGenerator
	embedder    fakeEmbedder
}

func newFixture() fixture {
	fixture := fixture{
		sessions:  &fakeSessionRepository{sessions: make(map[domain.SessionID]domain.Session)},
		messages:  &fakeMessageRepository{messagesBySession: make(map[domain.SessionID][]domain.Message)},
		workflows: &fakeWorkflowRepository{workflows: make(map[domain.WorkflowID]domain.Workflow)},
		agents: &fakeAgentRepository{agents: map[domain.AgentID]domain.Agent{
			"writer":   mustAgent("writer"),
			"reviewer": mustAgent("reviewer"),
			"planner":  mustAgent("planner"),
		}},
		tasks:       &fakeSandboxTaskRepository{tasks: make(map[AgentTaskID]SandboxTask)},
		sandbox:     &fakeSandboxedAgentRuntime{name: "codex", class: AgentProviderClassSandboxedRuntime},
		vector:      &fakeVectorAdmin{},
		vectorIndex: &fakeVectorIndex{status: VectorIndexStatusDisabled},
		outbox:      &fakeOutbox{},
		events:      &fakeEventBus{},
		clock:       fakeClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)},
		ids: &fakeIDGenerator{
			sessionIDs: []domain.SessionID{"session-1", "session-2"},
			messageIDs: []domain.MessageID{"message-1", "message-2", "message-3", "message-4", "message-5"},
		},
		embedder: fakeEmbedder{id: "fake-embedder"},
	}

	fixture.tx = &fakeUnitOfWork{
		participants: []txParticipant{
			fixture.sessions,
			fixture.messages,
			fixture.workflows,
			fixture.tasks,
			fixture.vector,
			fixture.outbox,
		},
	}

	return fixture
}

type fakeSessionRepository struct {
	sessions map[domain.SessionID]domain.Session
	staged   map[domain.SessionID]domain.Session
}

func (r *fakeSessionRepository) Save(_ context.Context, session domain.Session) error {
	r.current()[session.ID] = session
	return nil
}

func (r *fakeSessionRepository) GetByID(_ context.Context, id domain.SessionID) (domain.Session, error) {
	session, exists := r.current()[id]
	if !exists {
		return domain.Session{}, NotFoundError{Entity: "session", ID: string(id)}
	}

	return session, nil
}

func (r *fakeSessionRepository) begin() {
	r.staged = mapsClone(r.sessions)
}

func (r *fakeSessionRepository) commit() {
	r.sessions = r.staged
	r.staged = nil
}

func (r *fakeSessionRepository) rollback() {
	r.staged = nil
}

func (r *fakeSessionRepository) current() map[domain.SessionID]domain.Session {
	if r.staged != nil {
		return r.staged
	}

	return r.sessions
}

type fakeMessageRepository struct {
	messagesBySession map[domain.SessionID][]domain.Message
	staged            map[domain.SessionID][]domain.Message
}

func (r *fakeMessageRepository) Save(_ context.Context, message domain.Message) error {
	current := r.current()
	current[message.SessionID] = append(current[message.SessionID], message)
	return nil
}

func (r *fakeMessageRepository) ListBySessionID(_ context.Context, sessionID domain.SessionID) ([]domain.Message, error) {
	return r.current()[sessionID], nil
}

func (r *fakeMessageRepository) begin() {
	r.staged = cloneMessagesBySession(r.messagesBySession)
}

func (r *fakeMessageRepository) commit() {
	r.messagesBySession = r.staged
	r.staged = nil
}

func (r *fakeMessageRepository) rollback() {
	r.staged = nil
}

func (r *fakeMessageRepository) current() map[domain.SessionID][]domain.Message {
	if r.staged != nil {
		return r.staged
	}

	return r.messagesBySession
}

type fakeWorkflowRepository struct {
	workflows map[domain.WorkflowID]domain.Workflow
	staged    map[domain.WorkflowID]domain.Workflow
}

func (r *fakeWorkflowRepository) Save(_ context.Context, workflow domain.Workflow) error {
	r.current()[workflow.ID] = workflow
	return nil
}

func (r *fakeWorkflowRepository) GetByID(_ context.Context, id domain.WorkflowID) (domain.Workflow, error) {
	workflow, exists := r.current()[id]
	if !exists {
		return domain.Workflow{}, NotFoundError{Entity: "workflow", ID: string(id)}
	}

	return workflow, nil
}

func (r *fakeWorkflowRepository) begin() {
	r.staged = cloneWorkflowMap(r.workflows)
}

func (r *fakeWorkflowRepository) commit() {
	r.workflows = r.staged
	r.staged = nil
}

func (r *fakeWorkflowRepository) rollback() {
	r.staged = nil
}

func (r *fakeWorkflowRepository) current() map[domain.WorkflowID]domain.Workflow {
	if r.staged != nil {
		return r.staged
	}

	return r.workflows
}

type fakeAgentRepository struct {
	agents map[domain.AgentID]domain.Agent
}

func (r *fakeAgentRepository) GetByID(_ context.Context, id domain.AgentID) (domain.Agent, error) {
	agent, exists := r.agents[id]
	if !exists {
		return domain.Agent{}, NotFoundError{Entity: "agent", ID: string(id)}
	}

	return agent, nil
}

func (r *fakeAgentRepository) List(context.Context) ([]domain.Agent, error) {
	agents := make([]domain.Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		agents = append(agents, agent)
	}
	slices.SortFunc(agents, func(a, b domain.Agent) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return agents, nil
}

type fakeSandboxTaskRepository struct {
	tasks          map[AgentTaskID]SandboxTask
	handoffs       map[domain.SessionID][]AgentHandoff
	stagedTasks    map[AgentTaskID]SandboxTask
	stagedHandoffs map[domain.SessionID][]AgentHandoff
}

func (r *fakeSandboxTaskRepository) SaveTask(_ context.Context, task SandboxTask) error {
	r.currentTasks()[task.ID] = cloneSandboxTask(task)
	return nil
}

func (r *fakeSandboxTaskRepository) GetTaskByID(_ context.Context, id AgentTaskID) (SandboxTask, error) {
	task, exists := r.currentTasks()[id]
	if !exists {
		return SandboxTask{}, NotFoundError{Entity: "sandbox_task", ID: string(id)}
	}
	return cloneSandboxTask(task), nil
}

func (r *fakeSandboxTaskRepository) ListTasksBySessionID(_ context.Context, sessionID domain.SessionID) ([]SandboxTask, error) {
	tasks := make([]SandboxTask, 0)
	for _, task := range r.currentTasks() {
		if task.SessionID == sessionID {
			tasks = append(tasks, cloneSandboxTask(task))
		}
	}
	slices.SortFunc(tasks, func(a, b SandboxTask) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return tasks, nil
}

func (r *fakeSandboxTaskRepository) SaveHandoff(_ context.Context, handoff AgentHandoff) error {
	current := r.currentHandoffs()
	current[handoff.SessionID] = append(current[handoff.SessionID], handoff)
	return nil
}

func (r *fakeSandboxTaskRepository) ListHandoffsBySessionID(_ context.Context, sessionID domain.SessionID) ([]AgentHandoff, error) {
	return append([]AgentHandoff(nil), r.currentHandoffs()[sessionID]...), nil
}

func (r *fakeSandboxTaskRepository) begin() {
	r.stagedTasks = cloneSandboxTaskMap(r.tasks)
	r.stagedHandoffs = cloneHandoffsBySession(r.handoffs)
}

func (r *fakeSandboxTaskRepository) commit() {
	r.tasks = r.stagedTasks
	r.handoffs = r.stagedHandoffs
	r.stagedTasks = nil
	r.stagedHandoffs = nil
}

func (r *fakeSandboxTaskRepository) rollback() {
	r.stagedTasks = nil
	r.stagedHandoffs = nil
}

func (r *fakeSandboxTaskRepository) currentTasks() map[AgentTaskID]SandboxTask {
	if r.stagedTasks != nil {
		return r.stagedTasks
	}
	return r.tasks
}

func (r *fakeSandboxTaskRepository) currentHandoffs() map[domain.SessionID][]AgentHandoff {
	if r.stagedHandoffs != nil {
		return r.stagedHandoffs
	}
	if r.handoffs == nil {
		r.handoffs = make(map[domain.SessionID][]AgentHandoff)
	}
	return r.handoffs
}

type fakeSandboxedAgentRuntime struct {
	name   string
	class  AgentProviderClass
	result SandboxTaskExecutionResult
	err    error
}

func (r *fakeSandboxedAgentRuntime) ExecuteTask(_ context.Context, task SandboxTask) (SandboxTaskExecutionResult, error) {
	result := r.result
	if result.CompletedAt.IsZero() {
		result.CompletedAt = task.CreatedAt.Add(2 * time.Minute)
	}
	return result, r.err
}

func (r *fakeSandboxedAgentRuntime) ProviderClass() AgentProviderClass {
	return r.class
}

func (r *fakeSandboxedAgentRuntime) SupportsRuntime(name string) bool {
	return r.name == name
}

type fakeVectorAdmin struct {
	states map[string]VectorIndexState
	staged map[string]VectorIndexState
}

type fakeVectorIndex struct {
	status  VectorIndexStatus
	results []VectorSearchResult
}

func (v *fakeVectorIndex) Status(context.Context) (VectorIndexStatus, error) {
	return v.status, nil
}

func (v *fakeVectorIndex) UpsertMessageEmbedding(context.Context, MessageEmbeddingRecord) error {
	return nil
}

func (v *fakeVectorIndex) DeleteMessageEmbedding(context.Context, domain.MessageID) error {
	return nil
}

func (v *fakeVectorIndex) SearchMessages(context.Context, VectorSearchQuery) ([]VectorSearchResult, error) {
	return append([]VectorSearchResult(nil), v.results...), nil
}

func (v *fakeVectorAdmin) State(_ context.Context) (VectorIndexState, error) {
	state, exists := v.current()[defaultVectorIndexName("")]
	if !exists {
		return VectorIndexState{}, NotFoundError{Entity: "vector_index_state", ID: defaultVectorIndexName("")}
	}
	return state, nil
}

func (v *fakeVectorAdmin) StateForSession(_ context.Context, sessionID domain.SessionID) (VectorIndexState, error) {
	key := defaultVectorIndexName(sessionID)
	state, exists := v.current()[key]
	if !exists {
		return VectorIndexState{}, NotFoundError{Entity: "vector_index_state", ID: key}
	}
	return state, nil
}

func (v *fakeVectorAdmin) MarkSessionStale(_ context.Context, sessionID domain.SessionID, occurredAt time.Time) error {
	key := defaultVectorIndexName(sessionID)
	current := v.current()
	state := current[key]
	state.IndexName = key
	state.Status = VectorIndexStateStatusStale
	state.UpdatedAt = occurredAt
	current[key] = state
	return nil
}

func (v *fakeVectorAdmin) RebuildFromCanonicalMessages(_ context.Context, embedder Embedder, options VectorRebuildOptions) (VectorRebuildStats, error) {
	key := defaultVectorIndexName(options.SessionID)
	now := time.Date(2026, 3, 20, 12, 30, 0, 0, time.UTC)
	state := VectorIndexState{
		IndexName:     key,
		Provider:      embedder.EmbeddingIdentity(),
		Status:        VectorIndexStateStatusReady,
		LastRebuiltAt: &now,
		UpdatedAt:     now,
	}
	v.current()[key] = state
	return VectorRebuildStats{
		Scanned:    1,
		Upserted:   1,
		StartedAt:  now,
		FinishedAt: now,
	}, nil
}

func (v *fakeVectorAdmin) begin() {
	v.staged = mapsClone(v.states)
}

func (v *fakeVectorAdmin) commit() {
	v.states = v.staged
	v.staged = nil
}

func (v *fakeVectorAdmin) rollback() {
	v.staged = nil
}

func (v *fakeVectorAdmin) current() map[string]VectorIndexState {
	if v.states == nil {
		v.states = make(map[string]VectorIndexState)
	}
	if v.staged != nil {
		return v.staged
	}
	return v.states
}

type fakeEventBus struct {
	topics []string
	events []any
}

func (b *fakeEventBus) Publish(_ context.Context, topic string, event any) error {
	b.topics = append(b.topics, topic)
	b.events = append(b.events, event)
	return nil
}

type fakeClock struct {
	now time.Time
}

func (c fakeClock) Now() time.Time {
	return c.now
}

type fakeOutbox struct {
	events        []RecordedEvent
	staged        []RecordedEvent
	inTx          bool
	failAdd       bool
	addCalls      int
	failOnAddCall int
}

func (o *fakeOutbox) Add(_ context.Context, event RecordedEvent) error {
	o.addCalls++
	if o.failAdd {
		return errors.New("outbox add failed")
	}
	if o.failOnAddCall > 0 && o.addCalls == o.failOnAddCall {
		return errors.New("outbox add failed on configured call")
	}

	if o.inTx {
		o.staged = append(o.staged, event)
		return nil
	}

	o.events = append(o.events, event)
	return nil
}

func (o *fakeOutbox) begin() {
	o.staged = append([]RecordedEvent{}, o.events...)
	o.inTx = true
}

func (o *fakeOutbox) commit() {
	o.events = o.staged
	o.staged = nil
	o.inTx = false
}

func (o *fakeOutbox) rollback() {
	o.staged = nil
	o.inTx = false
}

type txParticipant interface {
	begin()
	commit()
	rollback()
}

type fakeUnitOfWork struct {
	participants []txParticipant
}

func (u *fakeUnitOfWork) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	for _, participant := range u.participants {
		participant.begin()
	}

	if err := fn(ctx); err != nil {
		for _, participant := range u.participants {
			participant.rollback()
		}
		return err
	}

	for _, participant := range u.participants {
		participant.commit()
	}

	return nil
}

type fakeIDGenerator struct {
	sessionIDs []domain.SessionID
	messageIDs []domain.MessageID
}

type fakeEmbedder struct {
	id string
}

type fakeOrchestrator struct{}

func (fakeOrchestrator) SelectNext(_ context.Context, state ConversationState, candidates []domain.Agent) (OrchestrationDecision, error) {
	if len(candidates) == 0 {
		return OrchestrationDecision{Strategy: state.Mode}, nil
	}
	ordered := make([]domain.AgentID, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate.ID)
	}
	return OrchestrationDecision{
		Selected:            []domain.Agent{candidates[0]},
		OrderedCandidateIDs: ordered,
		Strategy:            state.Mode,
	}, nil
}

type fakeLLMProvider struct{}

func (fakeLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	return GenerationResult{
		MessageBody: request.Agent.Name + " reply",
	}, nil
}

type recordingLLMProvider struct{}

func (recordingLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	last := request.Messages[len(request.Messages)-1]
	return GenerationResult{
		MessageBody: request.Agent.Name + " saw: " + last.Body,
	}, nil
}

type obligationAwareLLMProvider struct{}

func (obligationAwareLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	switch request.Agent.ID {
	case "planner":
		return GenerationResult{
			MessageBody: "planner reply to operator\n@reviewer",
		}, nil
	case "reviewer":
		if request.ReplyRouting.RecipientType == "agent" {
			return GenerationResult{
				MessageBody: "reviewer reply to planner",
			}, nil
		}
		return GenerationResult{
			MessageBody: "reviewer reply to operator",
		}, nil
	default:
		return GenerationResult{
			MessageBody: request.Agent.Name + " reply",
		}, nil
	}
}

type incidentalMentionLLMProvider struct{}

func (incidentalMentionLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	return GenerationResult{
		MessageBody: "I may hand this to @reviewer later, but I still need more operator input before doing that.",
	}, nil
}

type bareHandoffLLMProvider struct{}

func (bareHandoffLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	if request.Agent.ID == "planner" {
		return GenerationResult{
			MessageBody: "Planning is complete. reviewer Review the latest implementation and report issues.",
		}, nil
	}
	return GenerationResult{
		MessageBody: request.Agent.Name + " reply",
	}, nil
}

type sandboxDelegatingLLMProvider struct{}

func (sandboxDelegatingLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	return GenerationResult{
		MessageBody: request.Agent.Name + " delegated sandbox work",
		SandboxRequest: &SandboxTaskRequest{
			Instruction:       "update the README",
			PermissionProfile: SandboxPermissionPatch,
		},
	}, nil
}

type malformedSandboxDelegatingLLMProvider struct{}

func (malformedSandboxDelegatingLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	return GenerationResult{
		MessageBody: request.Agent.Name + " delegated malformed sandbox work",
		SandboxRequest: &SandboxTaskRequest{
			Instruction:       "",
			PermissionProfile: SandboxPermissionPatch,
		},
	}, nil
}

func (f fakeEmbedder) EmbedText(_ context.Context, text string) ([]float32, error) {
	return []float32{float32(len(text)), 1, 2}, nil
}

func (f fakeEmbedder) EmbeddingIdentity() string {
	return f.id
}

func (g *fakeIDGenerator) NewSessionID(_ context.Context) (domain.SessionID, error) {
	if len(g.sessionIDs) == 0 {
		return "", errors.New("no session IDs available")
	}

	id := g.sessionIDs[0]
	g.sessionIDs = g.sessionIDs[1:]
	return id, nil
}

func (g *fakeIDGenerator) NewMessageID(_ context.Context) (domain.MessageID, error) {
	if len(g.messageIDs) == 0 {
		return "", errors.New("no message IDs available")
	}

	id := g.messageIDs[0]
	g.messageIDs = g.messageIDs[1:]
	return id, nil
}

func mustAgent(id domain.AgentID) domain.Agent {
	policy := domain.DefaultAgentPolicy()
	if id == "planner" {
		policy.CanInitiate = true
		policy.AllowToolCalls = true
		policy.AllowSandboxDelegation = true
		policy.AllowedHandoffs = []domain.AgentID{"writer", "reviewer"}
		policy.AllowedSandboxRuntimes = []string{"codex"}
		policy.MaxToolCallsPerTurn = 1
	}

	agentSpec := domain.Agent{
		ID:           id,
		Name:         string(id),
		Role:         "test agent",
		SystemPrompt: "help",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
		Policies:     policy,
	}
	if policy.AllowSandboxDelegation {
		agentSpec.DelegationRuntime = "codex"
	}

	agent, err := domain.NewAgent(agentSpec)
	if err != nil {
		panic(err)
	}

	return agent
}

func mustWorkflow() domain.Workflow {
	return domain.Workflow{
		ID:          "workflow-1",
		Name:        "Review",
		EntryStepID: "split",
		Steps: []domain.WorkflowStep{
			{
				ID:          "split",
				Name:        "Split",
				Kind:        domain.WorkflowStepKindFanOut,
				NextStepIDs: []domain.WorkflowStepID{"draft", "review"},
			},
			{
				ID:          "draft",
				Name:        "Draft",
				Kind:        domain.WorkflowStepKindAgent,
				ActorID:     "writer",
				NextStepIDs: []domain.WorkflowStepID{"merge"},
			},
			{
				ID:          "review",
				Name:        "Review",
				Kind:        domain.WorkflowStepKindAgent,
				ActorID:     "reviewer",
				NextStepIDs: []domain.WorkflowStepID{"merge"},
			},
			{
				ID:          "merge",
				Name:        "Merge",
				Kind:        domain.WorkflowStepKindFanIn,
				NextStepIDs: []domain.WorkflowStepID{"done"},
			},
			{
				ID:   "done",
				Name: "Done",
				Kind: domain.WorkflowStepKindStop,
			},
		},
	}
}

type selfHandoffLLMProvider struct{}

func (selfHandoffLLMProvider) Generate(_ context.Context, request GenerationRequest) (GenerationResult, error) {
	return GenerationResult{
		MessageBody: request.Agent.Name + " reply\n@planner",
	}, nil
}

func mustThreeWayMergeWorkflow() domain.Workflow {
	return domain.Workflow{
		ID:          "workflow-3way",
		Name:        "Three Way Review",
		EntryStepID: "split",
		Steps: []domain.WorkflowStep{
			{
				ID:          "split",
				Name:        "Split",
				Kind:        domain.WorkflowStepKindFanOut,
				NextStepIDs: []domain.WorkflowStepID{"draft-a", "draft-b", "draft-c"},
			},
			{
				ID:          "draft-a",
				Name:        "Draft A",
				Kind:        domain.WorkflowStepKindAgent,
				ActorID:     "writer",
				NextStepIDs: []domain.WorkflowStepID{"merge"},
			},
			{
				ID:          "draft-b",
				Name:        "Draft B",
				Kind:        domain.WorkflowStepKindAgent,
				ActorID:     "reviewer",
				NextStepIDs: []domain.WorkflowStepID{"merge"},
			},
			{
				ID:          "draft-c",
				Name:        "Draft C",
				Kind:        domain.WorkflowStepKindAgent,
				ActorID:     "planner",
				NextStepIDs: []domain.WorkflowStepID{"merge"},
			},
			{
				ID:          "merge",
				Name:        "Merge",
				Kind:        domain.WorkflowStepKindFanIn,
				NextStepIDs: []domain.WorkflowStepID{"done"},
			},
			{
				ID:   "done",
				Name: "Done",
				Kind: domain.WorkflowStepKindStop,
			},
		},
	}
}

func mapsClone[K comparable, V any](src map[K]V) map[K]V {
	dst := make(map[K]V, len(src))
	for key, value := range src {
		dst[key] = value
	}

	return dst
}

func cloneMessagesBySession(src map[domain.SessionID][]domain.Message) map[domain.SessionID][]domain.Message {
	dst := make(map[domain.SessionID][]domain.Message, len(src))
	for key, value := range src {
		dst[key] = slices.Clone(value)
	}

	return dst
}

func cloneWorkflowMap(src map[domain.WorkflowID]domain.Workflow) map[domain.WorkflowID]domain.Workflow {
	dst := make(map[domain.WorkflowID]domain.Workflow, len(src))
	for key, workflow := range src {
		steps := slices.Clone(workflow.Steps)
		for i := range steps {
			steps[i].NextStepIDs = slices.Clone(steps[i].NextStepIDs)
		}
		workflow.Steps = steps
		dst[key] = workflow
	}

	return dst
}

func cloneSandboxTaskMap(src map[AgentTaskID]SandboxTask) map[AgentTaskID]SandboxTask {
	dst := make(map[AgentTaskID]SandboxTask, len(src))
	for key, task := range src {
		dst[key] = cloneSandboxTask(task)
	}
	return dst
}

func cloneSandboxTask(task SandboxTask) SandboxTask {
	task.Artifacts = append([]SandboxTaskArtifact(nil), task.Artifacts...)
	task.Metadata = cloneAnyMap(task.Metadata)
	return task
}

func cloneHandoffsBySession(src map[domain.SessionID][]AgentHandoff) map[domain.SessionID][]AgentHandoff {
	dst := make(map[domain.SessionID][]AgentHandoff, len(src))
	for key, handoffs := range src {
		dst[key] = append([]AgentHandoff(nil), handoffs...)
	}
	return dst
}
