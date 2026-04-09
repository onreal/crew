package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crew/internal/adapters/memory"
	sqliteadapter "crew/internal/adapters/storage/sqlite"
	"crew/internal/application"
	"crew/internal/domain"
)

func TestSQLiteRuntimePersistsSessionStateAcrossRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime.db")

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 17, 0, 0, 0, time.UTC)}, nil, Config{})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	session, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := rt.StartSession(ctx, session.ID); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}

	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopenedStore := openSQLiteRuntimeStore(t, dbPath)
	reopened, err := NewSQLite(ctx, reopenedStore, nil, fixedClock{now: time.Date(2026, 3, 20, 17, 1, 0, 0, time.UTC)}, nil, Config{})
	if err != nil {
		t.Fatalf("NewSQLite() after reopen error = %v", err)
	}
	if err := reopened.Start(ctx); err != nil {
		t.Fatalf("reopened Start() error = %v", err)
	}
	defer reopened.Shutdown(context.Background())
	defer reopenedStore.Close()

	snapshot, err := reopened.InspectSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}

	if snapshot.Session.Status != domain.SessionStatusRunning {
		t.Fatalf("expected running session after reopen, got %q", snapshot.Session.Status)
	}
	if len(snapshot.Stream) < 2 {
		t.Fatalf("expected persisted stream entries after reopen, got %d", len(snapshot.Stream))
	}

	pending, err := reopenedStore.Outbox().ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending() error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending outbox events after flush, got %d", len(pending))
	}
}

func TestSQLiteRuntimeInspectFlushesPendingOutboxRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-pending.db")
	store := openSQLiteRuntimeStore(t, dbPath)

	clock := fixedClock{now: time.Date(2026, 3, 20, 17, 30, 0, 0, time.UTC)}
	ids := memory.NewSequenceIDGenerator()
	sessionService := application.NewSessionService(store.Sessions(), store.Outbox(), store.UnitOfWork(), clock, ids)

	session, err := sessionService.Create(ctx, application.CreateSessionCommand{Mode: domain.SessionModeFree})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := sessionService.Start(ctx, application.SessionIDCommand{SessionID: session.ID}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopenedStore := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, reopenedStore, nil, fixedClock{now: time.Date(2026, 3, 20, 17, 31, 0, 0, time.UTC)}, nil, Config{})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())
	defer reopenedStore.Close()

	snapshot, err := rt.InspectSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if len(snapshot.Stream) < 2 {
		t.Fatalf("expected inspect to flush pending outbox rows into stream, got %d entries", len(snapshot.Stream))
	}

	pending, err := reopenedStore.Outbox().ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending() error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected pending outbox rows to be drained during inspect, got %d", len(pending))
	}
}

func TestSQLiteRuntimePersistsDispatchedMessagesAcrossRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-messages.db")

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 18, 0, 0, 0, time.UTC)}, nil, Config{})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

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
		Sender:         domain.UserSender("operator-1"),
		ToAgentIDs:     []domain.AgentID{reviewer.ID},
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "review this runtime path",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopenedStore := openSQLiteRuntimeStore(t, dbPath)
	reopened, err := NewSQLite(ctx, reopenedStore, nil, fixedClock{now: time.Date(2026, 3, 20, 18, 1, 0, 0, time.UTC)}, nil, Config{})
	if err != nil {
		t.Fatalf("NewSQLite() after reopen error = %v", err)
	}
	if err := reopened.Start(ctx); err != nil {
		t.Fatalf("reopened Start() error = %v", err)
	}
	defer reopened.Shutdown(context.Background())
	defer reopenedStore.Close()

	snapshot, err := reopened.InspectSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}

	if len(snapshot.Messages) != 1 {
		t.Fatalf("expected 1 persisted message after reopen, got %d", len(snapshot.Messages))
	}
	if snapshot.Messages[0].Body != "review this runtime path" {
		t.Fatalf("unexpected persisted message body %q", snapshot.Messages[0].Body)
	}

	topics := make([]string, 0, len(snapshot.Stream))
	for _, entry := range snapshot.Stream {
		topics = append(topics, entry.Topic)
	}
	if !slices.Contains(topics, application.TopicMessageDispatched) {
		t.Fatalf("expected persisted stream to include %q, got %v", application.TopicMessageDispatched, topics)
	}
}

func TestSQLiteRuntimeDeactivatesAgentsMissingFromCatalogAcrossRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-agents.db")

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 18, 0, 0, 0, time.UTC)}, nil, Config{
		AgentsDir: writeDefaultAgentsDir(t),
	})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	nextAgentsDir := filepath.Join(t.TempDir(), testAgentsDirName)
	if err := os.MkdirAll(nextAgentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", nextAgentsDir, err)
	}
	if err := os.WriteFile(filepath.Join(nextAgentsDir, "writer.yaml"), []byte(`
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
`), 0o644); err != nil {
		t.Fatalf("WriteFile(writer.yaml) error = %v", err)
	}

	reopenedStore := openSQLiteRuntimeStore(t, dbPath)
	reopened, err := NewSQLite(ctx, reopenedStore, nil, fixedClock{now: time.Date(2026, 3, 20, 18, 1, 0, 0, time.UTC)}, nil, Config{
		AgentsDir: nextAgentsDir,
	})
	if err != nil {
		t.Fatalf("NewSQLite() after reopen error = %v", err)
	}
	if err := reopened.Start(ctx); err != nil {
		t.Fatalf("reopened Start() error = %v", err)
	}
	defer reopened.Shutdown(context.Background())
	defer reopenedStore.Close()

	agents, err := reopenedStore.Agents().List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "writer" {
		t.Fatalf("expected only active writer after catalog restart sync, got %+v", agents)
	}
	if _, err := reopenedStore.Agents().GetByID(ctx, "planner"); err == nil {
		t.Fatal("expected planner to be inactive after restart catalog sync")
	}
}

func TestSQLiteRuntimePersistsWorkflowsAcrossRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-workflows.db")

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 19, 0, 0, 0, time.UTC)}, nil, Config{})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	planner := mustAgent("planner")
	if err := rt.SeedAgent(planner); err != nil {
		t.Fatalf("SeedAgent() error = %v", err)
	}

	workflow, err := rt.RegisterWorkflow(ctx, domain.Workflow{
		ID:          "workflow-runtime-1",
		Name:        "Runtime workflow",
		EntryStepID: "plan",
		Steps: []domain.WorkflowStep{
			{
				ID:          "plan",
				Name:        "Plan",
				Kind:        domain.WorkflowStepKindAgent,
				ActorID:     planner.ID,
				NextStepIDs: []domain.WorkflowStepID{"stop"},
			},
			{
				ID:   "stop",
				Name: "Stop",
				Kind: domain.WorkflowStepKindStop,
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterWorkflow() error = %v", err)
	}

	progression, err := rt.AdvanceWorkflow(ctx, application.AdvanceWorkflowCommand{
		WorkflowID:    workflow.ID,
		CurrentStepID: "plan",
	})
	if err != nil {
		t.Fatalf("AdvanceWorkflow() error = %v", err)
	}
	if len(progression.ReadyNextSteps) != 1 || progression.ReadyNextSteps[0].ID != "stop" {
		t.Fatalf("unexpected workflow progression %+v", progression)
	}

	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopenedStore := openSQLiteRuntimeStore(t, dbPath)
	defer reopenedStore.Close()

	persisted, err := reopenedStore.Workflows().GetByID(ctx, workflow.ID)
	if err != nil {
		t.Fatalf("GetByID() after reopen error = %v", err)
	}
	if persisted.EntryStepID != workflow.EntryStepID {
		t.Fatalf("expected persisted workflow entry %q, got %q", workflow.EntryStepID, persisted.EntryStepID)
	}

	reopenedRuntime, err := NewSQLite(ctx, reopenedStore, nil, fixedClock{now: time.Date(2026, 3, 20, 19, 1, 0, 0, time.UTC)}, nil, Config{})
	if err != nil {
		t.Fatalf("NewSQLite() after reopen error = %v", err)
	}
	if err := reopenedRuntime.Start(ctx); err != nil {
		t.Fatalf("reopened Start() error = %v", err)
	}
	defer reopenedRuntime.Shutdown(context.Background())

	progression, err = reopenedRuntime.AdvanceWorkflow(ctx, application.AdvanceWorkflowCommand{
		WorkflowID:    workflow.ID,
		CurrentStepID: "plan",
	})
	if err != nil {
		t.Fatalf("AdvanceWorkflow() after reopen error = %v", err)
	}
	if len(progression.ReadyNextSteps) != 1 || progression.ReadyNextSteps[0].ID != "stop" {
		t.Fatalf("unexpected workflow progression after reopen %+v", progression)
	}

	pending, err := reopenedStore.Outbox().ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending() error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending outbox events after workflow register, got %d", len(pending))
	}
}

func TestSQLiteRuntimeVectorStatusRebuildAndRecall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-vector.db")

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 21, 0, 0, 0, time.UTC)}, nil, Config{
		VectorDimensions: 8,
	})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())
	defer store.Close()

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
		Body:           "runtime recovery review path",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	indexState, backendStatus, err := rt.VectorStatus(ctx, application.VectorStatusQuery{SessionID: session.ID})
	if err != nil {
		t.Fatalf("VectorStatus() error = %v", err)
	}
	if backendStatus != application.VectorIndexStatusDisabled {
		t.Fatalf("expected disabled backend status, got %q", backendStatus)
	}
	if indexState.Status != application.VectorIndexStateStatusStale {
		t.Fatalf("expected stale session vector state after dispatch, got %q", indexState.Status)
	}

	stats, rebuiltState, rebuiltBackendStatus, err := rt.RebuildVectors(ctx, application.VectorRebuildCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("RebuildVectors() error = %v", err)
	}
	if stats.Scanned != 1 {
		t.Fatalf("expected 1 scanned message during rebuild, got %d", stats.Scanned)
	}
	if rebuiltBackendStatus != application.VectorIndexStatusDisabled {
		t.Fatalf("expected disabled backend status after rebuild, got %q", rebuiltBackendStatus)
	}
	if rebuiltState.Status != application.VectorIndexStateStatusDisabled {
		t.Fatalf("expected disabled session vector state after rebuild without sqlite-vec, got %q", rebuiltState.Status)
	}
	if rebuiltState.LastRebuiltAt == nil {
		t.Fatalf("expected rebuilt session state to record last_rebuilt_at")
	}

	recall, err := rt.RecallSessionMessages(ctx, application.RecallSessionMessagesQuery{
		SessionID: session.ID,
		QueryText: "recovery review",
		Limit:     3,
	})
	if err != nil {
		t.Fatalf("RecallSessionMessages() error = %v", err)
	}
	if !recall.FallbackUsed {
		t.Fatalf("expected fallback recall while vector backend is disabled")
	}
	if len(recall.Results) != 1 {
		t.Fatalf("expected 1 recall result, got %d", len(recall.Results))
	}
}

func TestSQLiteRuntimeStepPersistsGeneratedAgentReply(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-step.db")

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 22, 0, 0, 0, time.UTC)}, nil, Config{
		AgentsDir: writeDefaultAgentsDir(t),
	})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())
	defer store.Close()

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
		Body:           "plan the next step",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	step, err := rt.StepSession(ctx, application.StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("StepSession() error = %v", err)
	}
	if !step.Stepped {
		t.Fatalf("expected stepped result, got %+v", step)
	}
	if step.Agent == nil || step.Message == nil {
		t.Fatalf("expected agent and message in step result, got %+v", step)
	}
	if step.Message.Sender.Type != domain.MessageSenderTypeAgent {
		t.Fatalf("expected agent sender, got %+v", step.Message.Sender)
	}

	snapshot, err := rt.InspectSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if len(snapshot.Messages) != 2 {
		t.Fatalf("expected 2 persisted messages after step, got %d", len(snapshot.Messages))
	}
}

func TestSQLiteRuntimeAutoPersistsGeneratedReplies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-auto.db")

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 22, 30, 0, 0, time.UTC)}, nil, Config{
		AgentsDir: writeDefaultAgentsDir(t),
	})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())
	defer store.Close()

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
		Body:           "plan the next steps",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	auto, err := rt.AutoSession(ctx, application.AutoSessionCommand{
		SessionID: session.ID,
		MaxSteps:  2,
	})
	if err != nil {
		t.Fatalf("AutoSession() error = %v", err)
	}
	if auto.CompletedSteps != 1 {
		t.Fatalf("expected 1 completed step, got %d", auto.CompletedSteps)
	}
	if auto.StopReason != "no_eligible_agents" {
		t.Fatalf("expected no_eligible_agents stop reason, got %q", auto.StopReason)
	}
	if !auto.VectorStateMarkedStale {
		t.Fatal("expected auto run to mark vector state stale")
	}

	snapshot, err := rt.InspectSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if len(snapshot.Messages) != 2 {
		t.Fatalf("expected 2 persisted messages after auto run, got %d", len(snapshot.Messages))
	}
}

func TestSQLiteRuntimeUsesConfiguredOpenAIProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "runtime-provider.db")
	agentsDir := writeDefaultAgentsDir(t)
	plannerPath := filepath.Join(agentsDir, "planner.yaml")
	plannerContent, err := os.ReadFile(plannerPath)
	if err != nil {
		t.Fatalf("ReadFile(planner.yaml) error = %v", err)
	}
	updatedPlanner := strings.Replace(string(plannerContent), "provider: local_stub", "provider: openai", 1)
	if err := os.WriteFile(plannerPath, []byte(updatedPlanner), 0o644); err != nil {
		t.Fatalf("WriteFile(planner.yaml) error = %v", err)
	}

	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 23, 0, 0, 0, time.UTC)}, nil, Config{
		AgentsDir: agentsDir,
		TextProviders: map[string]TextProviderConfig{
			"openai": {
				BaseURL: "http://provider.test/v1",
				APIKey:  "secret-key",
				Timeout: 5 * time.Second,
				HTTPClient: &http.Client{
					Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						return jsonResponse(http.StatusOK, map[string]any{
							"model": "gpt-test",
							"choices": []map[string]any{
								{
									"message": map[string]any{
										"content": "provider-backed reply",
									},
								},
							},
						}), nil
					}),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())
	defer store.Close()

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
		Body:           "plan the next step",
	}); err != nil {
		t.Fatalf("DispatchMessage() error = %v", err)
	}

	step, err := rt.StepSession(ctx, application.StepSessionCommand{SessionID: session.ID})
	if err != nil {
		t.Fatalf("StepSession() error = %v", err)
	}
	if !step.Stepped || step.Message == nil {
		t.Fatalf("expected provider-backed step result, got %+v", step)
	}
	if step.Message.Body != "provider-backed reply" {
		t.Fatalf("expected provider reply body, got %q", step.Message.Body)
	}
	if step.Message.Metadata["generated_by"] != "openai_llm" {
		t.Fatalf("expected openai metadata, got %+v", step.Message.Metadata)
	}
}

func TestSQLiteRuntimeDispatchReseedsSessionActorCatalogBeforeRecipientValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()
	teamDir := writeSelectorAgentsDir(t, rootDir, "team-a")
	agentsRoot := filepath.Dir(teamDir)
	for _, name := range []string{"planner.yaml", "writer.yaml"} {
		if err := os.Remove(filepath.Join(teamDir, name)); err != nil {
			t.Fatalf("Remove(%q) error = %v", name, err)
		}
	}

	dbPath := filepath.Join(rootDir, "runtime-dispatch-catalog.db")
	store := openSQLiteRuntimeStore(t, dbPath)
	rt, err := NewSQLite(ctx, store, nil, fixedClock{now: time.Date(2026, 3, 20, 23, 30, 0, 0, time.UTC)}, nil, Config{
		AgentsDir:             agentsRoot,
		DefaultActorsSelector: "team-a",
	})
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer rt.Shutdown(context.Background())
	defer store.Close()

	sessionA, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession(sessionA) error = %v", err)
	}
	if _, err := rt.StartSession(ctx, sessionA.ID); err != nil {
		t.Fatalf("StartSession(sessionA) error = %v", err)
	}

	rt.defaultActorsSelector = ""
	sessionB, err := rt.CreateSession(ctx, domain.SessionModeFree)
	if err != nil {
		t.Fatalf("CreateSession(sessionB) error = %v", err)
	}
	if _, err := rt.StartSession(ctx, sessionB.ID); err != nil {
		t.Fatalf("StartSession(sessionB) error = %v", err)
	}

	message, err := rt.DispatchMessage(ctx, application.DispatchMessageCommand{
		SessionID:      sessionA.ID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator"),
		ToAgentIDs:     []domain.AgentID{"reviewer"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "@reviewer please reply",
		Policy: &domain.ConversationPolicy{
			MaxTurns:                    64,
			LoopProtectionEnabled:       true,
			MaxConsecutiveTurnsPerAgent: 2,
			AllowBroadcastMessages:      true,
			RequireReplyTargetForDirect: false,
		},
	})
	if err != nil {
		t.Fatalf("DispatchMessage(sessionA) error = %v", err)
	}
	if len(message.ToAgentIDs) != 1 || message.ToAgentIDs[0] != "reviewer" {
		t.Fatalf("expected dispatch to reviewer, got %+v", message)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, payload map[string]any) *http.Response {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func openSQLiteRuntimeStore(t *testing.T, path string) *sqliteadapter.Store {
	t.Helper()

	store, err := sqliteadapter.Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		t.Fatalf("Migrate() error = %v", err)
	}

	return store
}
