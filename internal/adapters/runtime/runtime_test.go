package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"crew/internal/adapters/memory"
	"crew/internal/application"
	"crew/internal/domain"
)

func TestRuntimeMovesSessionAndMessagesThroughBus(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := rt.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()

	reviewer := mustAgent("reviewer")
	if err := rt.SeedAgent(reviewer); err != nil {
		t.Fatalf("SeedAgent() error = %v", err)
	}

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := rt.StartSession(ctx, session.ID); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	if _, err := rt.DispatchMessage(ctx, application.DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"reviewer"},
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "review this",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	snapshot, err := rt.InspectSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}

	if snapshot.Session.Status != domain.SessionStatusRunning {
		t.Fatalf("expected running session, got %q", snapshot.Session.Status)
	}

	if len(snapshot.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(snapshot.Messages))
	}

	if err := waitForStreamEntries(ctx, rt, session.ID, 3); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeStartSeedsAgentsFromAgentsDir(t *testing.T) {
	store := memory.NewStore()
	rt := New(store, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{
		AgentsDir: writeDefaultAgentsDir(t),
	})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	planner, err := store.Agents().GetByID(ctx, "planner")
	if err != nil {
		t.Fatalf("GetByID(planner) error = %v", err)
	}
	if planner.Name != "Planner" {
		t.Fatalf("expected Planner from agents dir, got %q", planner.Name)
	}
}

func TestRuntimeCreateSessionPersistsDefaultActorCatalog(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{
		DefaultActorsSelector: "team-a",
	})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.ActorCatalog != "team-a" {
		t.Fatalf("expected actor catalog team-a, got %q", session.ActorCatalog)
	}
}

func TestRuntimeStartUpdatesExistingAgentsFromAgentsDir(t *testing.T) {
	store := memory.NewStore()
	if err := store.SeedAgent(domain.Agent{
		ID:           "planner",
		Name:         "Old Planner",
		Role:         "planner",
		SystemPrompt: "old",
		Provider:     "local_stub",
		Model:        "local-stub",
		Policies: domain.AgentPolicy{
			CanInitiate:          false,
			RequireDirectMention: false,
			AllowBroadcast:       true,
			AllowToolCalls:       false,
			Weight:               1,
			MaxConsecutiveTurns:  1,
			MaxToolCallsPerTurn:  0,
		},
	}); err != nil {
		t.Fatalf("SeedAgent(old planner) error = %v", err)
	}

	rt := New(store, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{
		AgentsDir: writeDefaultAgentsDir(t),
	})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	planner, err := store.Agents().GetByID(ctx, "planner")
	if err != nil {
		t.Fatalf("GetByID(planner) error = %v", err)
	}
	if planner.Name != "Planner" {
		t.Fatalf("expected planner to be updated from agents dir, got %q", planner.Name)
	}
	if !planner.Policies.AllowSandboxDelegation {
		t.Fatalf("expected planner policy to be updated from agents dir, got %+v", planner.Policies)
	}
}

func TestRuntimeStartDeactivatesAgentsMissingFromAgentsDir(t *testing.T) {
	store := memory.NewStore()
	if err := store.SeedAgent(mustAgent("planner")); err != nil {
		t.Fatalf("SeedAgent(planner) error = %v", err)
	}
	if err := store.SeedAgent(mustAgent("reviewer")); err != nil {
		t.Fatalf("SeedAgent(reviewer) error = %v", err)
	}

	agentsDir := filepath.Join(t.TempDir(), testAgentsDirName)
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", agentsDir, err)
	}
	onlyWriter := `
id: writer
name: Writer
role: writer
system_prompt: Draft the next message or action from the latest session context.
provider: local_stub
model: gpt-5.4
tools: []
policies:
  can_initiate: false
  require_direct_mention: false
  allow_broadcast: true
  allow_tool_calls: false
  allow_sandbox_delegation: false
  allowed_sandbox_runtimes: []
  priority: 100
  weight: 1
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 0
`
	if err := os.WriteFile(filepath.Join(agentsDir, "writer.yaml"), []byte(onlyWriter), 0o644); err != nil {
		t.Fatalf("WriteFile(writer.yaml) error = %v", err)
	}

	rt := New(store, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{
		AgentsDir: agentsDir,
	})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	agents, err := store.Agents().List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "writer" {
		t.Fatalf("expected only active writer after catalog sync, got %+v", agents)
	}
	if _, err := store.Agents().GetByID(ctx, "planner"); err == nil {
		t.Fatal("expected planner to be inactive after catalog sync")
	}
}

func TestRuntimeStepResyncsAgentsFromAgentsDir(t *testing.T) {
	agentsDir := writeDefaultAgentsDir(t)
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{
		AgentsDir: agentsDir,
	})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := rt.StartSession(ctx, session.ID); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if _, err := rt.DispatchMessage(ctx, application.DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "hello after agent edit",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	updatedPlanner := `
id: planner
name: Chief Planner
role: planner
system_prompt: Plan the next concrete step from the latest session message.
provider: local_stub
model: gpt-5.4
tools: []
policies:
  can_initiate: false
  require_direct_mention: false
  allow_broadcast: true
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes:
    - codex
  priority: 100
  weight: 3
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 1
`
	if err := os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(updatedPlanner), 0o644); err != nil {
		t.Fatalf("WriteFile(planner.yaml) error = %v", err)
	}

	step, err := rt.StepSession(ctx, application.StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("StepSession() error = %v", err)
	}
	if step.Message == nil {
		t.Fatalf("expected stepped message, got %+v", step)
	}
	if step.Message.Body != "Chief Planner (planner): hello after agent edit" {
		t.Fatalf("expected updated agent name in reply, got %q", step.Message.Body)
	}
}

func TestLocalStubOrchestratorMentionedFirstPrioritizesMentionedAgent(t *testing.T) {
	orchestrator := localStubOrchestrator{}
	candidates := []domain.Agent{
		runtimeTestAgent("planner", 2, 0, 1),
		runtimeTestAgent("reviewer", 2, 0, 1),
		runtimeTestAgent("writer", 2, 0, 1),
	}

	decision, err := orchestrator.SelectNext(context.Background(), application.ConversationState{
		LastMessage: &domain.Message{
			Sender: domain.UserSender("operator"),
			Body:   "writer please draft the response",
		},
		Mode: application.OrchestrationModeMentionedFirst,
	}, candidates)
	if err != nil {
		t.Fatalf("SelectNext() error = %v", err)
	}
	if len(decision.Selected) != 1 || decision.Selected[0].ID != "writer" {
		t.Fatalf("expected writer to be selected first, got %+v", decision.Selected)
	}
	if decision.Strategy != application.OrchestrationModeMentionedFirst {
		t.Fatalf("expected mentioned_first strategy, got %q", decision.Strategy)
	}
}

func TestLocalStubOrchestratorRoundRobinAdvancesPastLastSender(t *testing.T) {
	orchestrator := localStubOrchestrator{}
	candidates := []domain.Agent{
		runtimeTestAgent("planner", 2, 0, 1),
		runtimeTestAgent("reviewer", 2, 0, 1),
		runtimeTestAgent("writer", 2, 0, 1),
	}

	decision, err := orchestrator.SelectNext(context.Background(), application.ConversationState{
		LastMessage: &domain.Message{
			Sender: domain.AgentSender("reviewer"),
			Body:   "review complete",
		},
		Mode: application.OrchestrationModeRoundRobin,
	}, candidates)
	if err != nil {
		t.Fatalf("SelectNext() error = %v", err)
	}
	if len(decision.Selected) != 1 || decision.Selected[0].ID != "writer" {
		t.Fatalf("expected writer after reviewer in round robin, got %+v", decision.Selected)
	}
	if len(decision.OrderedCandidateIDs) < 3 || decision.OrderedCandidateIDs[0] != "writer" {
		t.Fatalf("expected ordered candidates to start with writer, got %v", decision.OrderedCandidateIDs)
	}
}

func TestLocalStubOrchestratorRoundRobinAdvancesPastBlockedLastSender(t *testing.T) {
	orchestrator := localStubOrchestrator{}
	allAgents := []domain.Agent{
		runtimeTestAgent("planner", 1, 0, 1),
		runtimeTestAgent("reviewer", 1, 0, 1),
		runtimeTestAgent("writer", 1, 0, 1),
	}
	candidates := []domain.Agent{
		runtimeTestAgent("planner", 1, 0, 1),
		runtimeTestAgent("writer", 1, 0, 1),
	}

	decision, err := orchestrator.SelectNext(context.Background(), application.ConversationState{
		LastMessage: &domain.Message{
			Sender: domain.AgentSender("reviewer"),
			Body:   "review complete",
		},
		AllAgents: allAgents,
		Mode:      application.OrchestrationModeRoundRobin,
	}, candidates)
	if err != nil {
		t.Fatalf("SelectNext() error = %v", err)
	}
	if len(decision.Selected) != 1 || decision.Selected[0].ID != "writer" {
		t.Fatalf("expected writer after blocked reviewer in round robin, got %+v", decision.Selected)
	}
	if !slices.Equal(decision.OrderedCandidateIDs, []domain.AgentID{"writer", "planner"}) {
		t.Fatalf("unexpected ordered candidates %v", decision.OrderedCandidateIDs)
	}
}

func TestLocalStubOrchestratorPrefersHigherPriorityThenWeight(t *testing.T) {
	orchestrator := localStubOrchestrator{}
	candidates := []domain.Agent{
		runtimeTestAgent("planner", 2, 5, 1),
		runtimeTestAgent("reviewer", 2, 5, 3),
		runtimeTestAgent("writer", 2, 7, 1),
	}

	decision, err := orchestrator.SelectNext(context.Background(), application.ConversationState{
		Mode: application.OrchestrationModeDeterministic,
	}, candidates)
	if err != nil {
		t.Fatalf("SelectNext() error = %v", err)
	}
	if len(decision.Selected) != 1 || decision.Selected[0].ID != "writer" {
		t.Fatalf("expected highest-priority writer to be selected first, got %+v", decision.Selected)
	}
	if !slices.Equal(decision.OrderedCandidateIDs, []domain.AgentID{"writer", "reviewer", "planner"}) {
		t.Fatalf("unexpected ordered candidates %v", decision.OrderedCandidateIDs)
	}
}

func TestRuntimePauseResumeStopAndInspect(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := rt.StartSession(ctx, session.ID); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	if _, err := rt.PauseSession(ctx, session.ID); err != nil {
		t.Fatalf("PauseSession() error = %v", err)
	}

	if _, err := rt.ResumeSession(ctx, session.ID); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}

	if _, err := rt.StopSession(ctx, session.ID); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}

	snapshot, err := rt.InspectSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}

	if snapshot.Session.Status != domain.SessionStatusStopped {
		t.Fatalf("expected stopped session, got %q", snapshot.Session.Status)
	}
}

func TestRuntimeAutoSessionRoundRobinAdvancesBeforeLatestSpeakerRoutingCreatesDirectFollowUps(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := rt.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()

	for _, agentID := range []domain.AgentID{"planner", "reviewer", "writer"} {
		if err := rt.SeedAgent(runtimeTestAgent(agentID, 1, 0, 1)); err != nil {
			t.Fatalf("SeedAgent(%s) error = %v", agentID, err)
		}
	}

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := rt.StartSession(ctx, session.ID); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if _, err := rt.DispatchMessage(ctx, application.DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "hello team",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	auto, err := rt.AutoSession(ctx, application.AutoSessionCommand{
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		MaxSteps:          3,
		OrchestrationMode: application.OrchestrationModeRoundRobin,
		ReplyRoutingMode:  application.ReplyRoutingModeLatestSpeaker,
	})
	if err != nil {
		t.Fatalf("AutoSession() error = %v", err)
	}
	if !slices.Equal(auto.SelectedAgentIDs, []domain.AgentID{"planner", "reviewer", "planner"}) {
		t.Fatalf("expected round robin to advance to reviewer before latest-speaker routing narrows the follow-up, got %v", auto.SelectedAgentIDs)
	}
}

func TestRuntimeAutoSessionDirectRecipientsAllReplyBeforeStopping(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		if err := rt.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	}()

	for _, agentID := range []domain.AgentID{"planner", "reviewer", "writer"} {
		if err := rt.SeedAgent(runtimeTestAgent(agentID, 1, 0, 1)); err != nil {
			t.Fatalf("SeedAgent(%s) error = %v", agentID, err)
		}
	}

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := rt.StartSession(ctx, session.ID); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	directPolicy := domain.DefaultConversationPolicy()
	directPolicy.RequireReplyTargetForDirect = false
	if _, err := rt.DispatchMessage(ctx, application.DispatchMessageCommand{
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"planner", "reviewer"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "@planner @reviewer both answer",
		Policy:         &directPolicy,
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	auto, err := rt.AutoSession(ctx, application.AutoSessionCommand{
		SessionID:         session.ID,
		ConversationID:    "conversation-1",
		MaxSteps:          2,
		OrchestrationMode: application.OrchestrationModeDeterministic,
	})
	if err != nil {
		t.Fatalf("AutoSession() error = %v", err)
	}
	if !slices.Equal(auto.SelectedAgentIDs, []domain.AgentID{"planner", "reviewer"}) {
		t.Fatalf("expected direct recipients to reply before others, got %v", auto.SelectedAgentIDs)
	}
}

func TestRuntimeShutdownIsCleanAndIdempotent(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown() error = %v", err)
	}

	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestRuntimeShutdownWaitsForInFlightOperationBeforeClosingBus(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, blockingCh := rt.bus.Subscribe("*", 1)

	// Fill the blocking subscriber buffer so the next publish blocks inside an operation.
	if _, err := rt.CreateSession(ctx, domain.SessionModeFree); err != nil {
		t.Fatalf("initial CreateSession() error = %v", err)
	}

	createDone := make(chan error, 1)
	go func() {
		_, err := rt.CreateSession(ctx, domain.SessionModeFree)
		createDone <- err
	}()

	time.Sleep(50 * time.Millisecond)

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- rt.Shutdown(context.Background())
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown() returned before the in-flight operation completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case envelope := <-blockingCh:
		if envelope.Topic != application.TopicSessionCreated {
			t.Fatalf("expected session.created topic, got %q", envelope.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting to drain blocking subscriber")
	}

	select {
	case err := <-createDone:
		if err != nil {
			t.Fatalf("in-flight CreateSession() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-flight CreateSession() to finish")
	}

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Shutdown() to finish")
	}
}

func TestRuntimeRejectsOperationsBeforeStart(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{})

	if _, err := rt.CreateSession(context.Background(), domain.SessionModeFree); !errors.Is(err, ErrRuntimeNotStarted) {
		t.Fatalf("expected ErrRuntimeNotStarted, got %v", err)
	}
}

func TestRuntimeRejectsOperationsAfterShutdown(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if _, err := rt.CreateSession(context.Background(), domain.SessionModeFree); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("expected ErrRuntimeClosed, got %v", err)
	}

	if err := rt.Start(ctx); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("expected restarting closed runtime to fail with ErrRuntimeClosed, got %v", err)
	}
}

func TestRuntimeLoadStateRejectsActiveRuntime(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	err := rt.LoadState(StateSnapshot{})
	if !errors.Is(err, ErrRuntimeActive) {
		t.Fatalf("expected ErrRuntimeActive, got %v", err)
	}
}

func TestRuntimeLoadStateRejectsClosedRuntime(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{})
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	err := rt.LoadState(StateSnapshot{})
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("expected ErrRuntimeClosed, got %v", err)
	}
}

func TestRuntimeSnapshotRejectsClosedRuntime(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{})
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if _, err := rt.Snapshot(); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("expected ErrRuntimeClosed, got %v", err)
	}
}

func TestRuntimeSnapshotAllowedWhileActive(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	snapshot, err := rt.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if _, exists := snapshot.Store.Sessions[session.ID]; !exists {
		t.Fatalf("expected active snapshot to include session %q", session.ID)
	}
}

func runtimeTestAgent(id domain.AgentID, maxConsecutiveTurns int, priority int, weight int) domain.Agent {
	agent, err := domain.NewAgent(domain.Agent{
		ID:           id,
		Name:         string(id),
		Role:         "test",
		SystemPrompt: "help",
		Provider:     "local_stub",
		Model:        "local-stub",
		Policies: domain.AgentPolicy{
			CanInitiate:          false,
			RequireDirectMention: false,
			AllowBroadcast:       true,
			AllowToolCalls:       false,
			Priority:             priority,
			Weight:               weight,
			MaxConsecutiveTurns:  maxConsecutiveTurns,
			MaxToolCallsPerTurn:  0,
		},
	})
	if err != nil {
		panic(err)
	}
	return agent
}

func TestRuntimeSnapshotWaitsForInFlightMutation(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)}, memory.NewSequenceIDGenerator(), Config{})
	ctx := context.Background()

	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())

	_, blockingCh := rt.bus.Subscribe("*", 1)

	if _, err := rt.CreateSession(ctx, domain.SessionModeFree); err != nil {
		t.Fatalf("initial CreateSession() error = %v", err)
	}

	createDone := make(chan error, 1)
	go func() {
		_, err := rt.CreateSession(ctx, domain.SessionModeFree)
		createDone <- err
	}()

	time.Sleep(50 * time.Millisecond)

	snapshotDone := make(chan snapshotResult, 1)
	go func() {
		snapshot, err := rt.Snapshot()
		snapshotDone <- snapshotResult{snapshot: snapshot, err: err}
	}()

	select {
	case result := <-snapshotDone:
		t.Fatalf("Snapshot() returned before the in-flight mutation completed: err=%v snapshot=%+v", result.err, result.snapshot)
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case envelope := <-blockingCh:
		if envelope.Topic != application.TopicSessionCreated {
			t.Fatalf("expected session.created topic, got %q", envelope.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting to drain blocking subscriber")
	}

	select {
	case err := <-createDone:
		if err != nil {
			t.Fatalf("in-flight CreateSession() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-flight CreateSession() to finish")
	}

	select {
	case result := <-snapshotDone:
		if result.err != nil {
			t.Fatalf("Snapshot() error = %v", result.err)
		}
		if len(result.snapshot.Store.Sessions) != 2 {
			t.Fatalf("expected snapshot to include both sessions, got %d", len(result.snapshot.Store.Sessions))
		}
		if len(result.snapshot.Streams["session-2"]) == 0 {
			t.Fatalf("expected snapshot to include projected stream for session-2, got %+v", result.snapshot.Streams["session-2"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Snapshot() to finish")
	}
}

func TestRuntimeProviderCatalogReflectsConfiguredProviderClasses(t *testing.T) {
	rt := New(nil, nil, fixedClock{now: time.Now().UTC()}, memory.NewSequenceIDGenerator(), Config{
		TextProviders: map[string]TextProviderConfig{
			"codex":  {BinaryPath: "codex", WorkingDir: "."},
			"openai": {APIKey: "secret-key"},
			"grok":   {},
		},
		SandboxDefaultProvider: "codex",
		SandboxProviders: map[string]SandboxProviderConfig{
			"codex": {
				BinaryPath:  "codex",
				SandboxRoot: "/tmp/worktree",
				Timeout:     time.Minute,
			},
		},
		SandboxPermissionProfile: "patch",
	})

	catalog := rt.ProviderCatalog()
	if len(catalog.TextGeneration) != 4 {
		t.Fatalf("expected local stub plus three configured text providers, got %+v", catalog.TextGeneration)
	}
	if catalog.TextGeneration[0].Class != application.AgentProviderClassTextLLM || catalog.TextGeneration[0].Name != "local_stub" || !catalog.TextGeneration[0].Enabled {
		t.Fatalf("unexpected text provider catalog %+v", catalog.TextGeneration)
	}
	if catalog.TextGeneration[1].Name != "codex" || !catalog.TextGeneration[1].Enabled {
		t.Fatalf("expected codex to be present and enabled, got %+v", catalog.TextGeneration[1])
	}
	if catalog.TextGeneration[2].Name != "grok" || catalog.TextGeneration[2].Enabled {
		t.Fatalf("expected grok to be present but disabled without credentials, got %+v", catalog.TextGeneration[2])
	}
	if catalog.TextGeneration[3].Name != "openai" || !catalog.TextGeneration[3].Enabled {
		t.Fatalf("expected openai to be enabled, got %+v", catalog.TextGeneration[3])
	}
	if len(catalog.SandboxedRuntimes) != 1 {
		t.Fatalf("expected one sandbox runtime binding, got %+v", catalog.SandboxedRuntimes)
	}
	if catalog.SandboxedRuntimes[0].Class != application.AgentProviderClassSandboxedRuntime {
		t.Fatalf("unexpected sandbox provider class %+v", catalog.SandboxedRuntimes[0])
	}
	if catalog.SandboxedRuntimes[0].Name != "codex" || !catalog.SandboxedRuntimes[0].Enabled {
		t.Fatalf("expected configured codex sandbox provider metadata, got %+v", catalog.SandboxedRuntimes[0])
	}
}

type fixedClock struct {
	now time.Time
}

type snapshotResult struct {
	snapshot StateSnapshot
	err      error
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func mustAgent(id domain.AgentID) domain.Agent {
	agent, err := domain.NewAgent(domain.Agent{
		ID:           id,
		Name:         string(id),
		Role:         "runtime test agent",
		SystemPrompt: "help",
		Provider:     "local_stub",
		Model:        "gpt-5.4",
	})
	if err != nil {
		panic(err)
	}

	return agent
}

func waitForStreamEntries(ctx context.Context, rt *Runtime, sessionID domain.SessionID, minEntries int) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := rt.InspectSession(ctx, sessionID)
		if err != nil {
			return err
		}

		if len(snapshot.Stream) >= minEntries {
			return nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return context.DeadlineExceeded
}
