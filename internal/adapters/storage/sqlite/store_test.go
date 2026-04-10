package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"crew/internal/application"
	"crew/internal/domain"
)

func TestStoreMigrateIsIdempotent(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var count int
	err := store.DB().QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master
WHERE type = 'table'
  AND name IN ('schema_migrations', 'sessions', 'agents', 'workflows', 'messages', 'outbox_events', 'session_stream')`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count migrated tables: %v", err)
	}
	if count != 7 {
		t.Fatalf("expected 7 migrated tables, got %d", count)
	}
}

func TestStoreRepositoriesRoundTrip(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	fixedTime := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)

	session, err := domain.NewSession("session-1", domain.SessionModeFree, fixedTime)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	session.ActorCatalog = "team-a"
	session, err = session.Start()
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	agent, err := domain.NewAgent(domain.Agent{
		ID:                   "agent-1",
		Name:                 "Planner",
		Role:                 "planner",
		SystemPrompt:         "Plan work",
		Provider:             "local_stub",
		Model:                "gpt-test",
		ReasoningEffort:      "medium",
		DelegationRuntime:    "codex",
		SandboxWorkspaceRoot: "/tmp/planner-sandbox",
		SandboxWorkspaceMode: "in_place",
		Tools:                []string{"search"},
		Policies: domain.AgentPolicy{
			AllowBroadcast:         true,
			AllowToolCalls:         true,
			AllowSandboxDelegation: true,
			AllowedSandboxRuntimes: []string{"codex"},
			Weight:                 1,
			MaxConsecutiveTurns:    2,
			MaxToolCallsPerTurn:    1,
		},
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	workflow, err := domain.NewWorkflow(domain.Workflow{
		ID:          "workflow-1",
		Name:        "Simple flow",
		EntryStepID: "step-1",
		Steps: []domain.WorkflowStep{
			{
				ID:          "step-1",
				Name:        "Agent step",
				Kind:        domain.WorkflowStepKindAgent,
				ActorID:     agent.ID,
				NextStepIDs: []domain.WorkflowStepID{"step-2"},
			},
			{
				ID:   "step-2",
				Name: "Stop",
				Kind: domain.WorkflowStepKindStop,
			},
		},
	})
	if err != nil {
		t.Fatalf("new workflow: %v", err)
	}

	message, err := domain.NewMessage(domain.Message{
		ID:             "message-1",
		SessionID:      session.ID,
		ConversationID: "conversation-1",
		Sender:         domain.AgentSender(agent.ID),
		ToAgentIDs:     []domain.AgentID{"agent-1"},
		Channel:        domain.MessageChannelDirect,
		Kind:           domain.MessageKindUtterance,
		Body:           "Hello",
		Timestamp:      fixedTime.Add(time.Minute),
		Metadata: map[string]any{
			"priority": "high",
		},
	})
	if err != nil {
		t.Fatalf("new message: %v", err)
	}

	if err := store.UnitOfWork().WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := store.Sessions().Save(txCtx, session); err != nil {
			return err
		}
		if err := store.Agents().Upsert(txCtx, agent); err != nil {
			return err
		}
		if err := store.Workflows().Save(txCtx, workflow); err != nil {
			return err
		}
		if err := store.Messages().Save(txCtx, message); err != nil {
			return err
		}
		if err := store.Outbox().Add(txCtx, application.RecordedEvent{
			Topic:      application.TopicSessionUpdated,
			Payload:    application.SessionUpdatedEvent{Session: session},
			OccurredAt: fixedTime.Add(2 * time.Minute),
		}); err != nil {
			return err
		}
		return store.SessionStreams().Append(txCtx, SessionStreamRecord{
			SessionID:   session.ID,
			Topic:       application.TopicSessionUpdated,
			RecordedAt:  fixedTime.Add(2 * time.Minute),
			PayloadJSON: []byte(`{"status":"running"}`),
		})
	}); err != nil {
		t.Fatalf("persist transaction: %v", err)
	}

	gotSession, err := store.Sessions().GetByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if gotSession.Status != domain.SessionStatusRunning {
		t.Fatalf("expected running session, got %q", gotSession.Status)
	}
	if gotSession.ActorCatalog != "team-a" {
		t.Fatalf("expected actor catalog team-a, got %q", gotSession.ActorCatalog)
	}

	gotAgent, err := store.Agents().GetByID(ctx, agent.ID)
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if gotAgent.Name != agent.Name {
		t.Fatalf("expected agent %q, got %q", agent.Name, gotAgent.Name)
	}
	if gotAgent.ReasoningEffort != agent.ReasoningEffort {
		t.Fatalf("expected reasoning effort %q, got %q", agent.ReasoningEffort, gotAgent.ReasoningEffort)
	}
	if gotAgent.DelegationRuntime != agent.DelegationRuntime {
		t.Fatalf("expected delegation runtime %q, got %q", agent.DelegationRuntime, gotAgent.DelegationRuntime)
	}
	if gotAgent.SandboxWorkspaceRoot != agent.SandboxWorkspaceRoot {
		t.Fatalf("expected sandbox workspace root %q, got %q", agent.SandboxWorkspaceRoot, gotAgent.SandboxWorkspaceRoot)
	}
	if gotAgent.SandboxWorkspaceMode != agent.SandboxWorkspaceMode {
		t.Fatalf("expected sandbox workspace mode %q, got %q", agent.SandboxWorkspaceMode, gotAgent.SandboxWorkspaceMode)
	}

	gotWorkflow, err := store.Workflows().GetByID(ctx, workflow.ID)
	if err != nil {
		t.Fatalf("get workflow: %v", err)
	}
	if gotWorkflow.EntryStepID != workflow.EntryStepID {
		t.Fatalf("expected workflow entry %q, got %q", workflow.EntryStepID, gotWorkflow.EntryStepID)
	}

	messages, err := store.Messages().ListBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Body != message.Body {
		t.Fatalf("expected message body %q, got %q", message.Body, messages[0].Body)
	}

	pending, err := store.Outbox().ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending outbox: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending outbox event, got %d", len(pending))
	}

	stream, err := store.SessionStreams().ListBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatalf("list session stream: %v", err)
	}
	if len(stream) != 1 {
		t.Fatalf("expected 1 session stream record, got %d", len(stream))
	}
}

func TestStoreTransactionRollback(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	session, err := domain.NewSession("session-rollback", domain.SessionModeFree, time.Date(2026, 3, 20, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	rollbackErr := errors.New("rollback")
	err = store.UnitOfWork().WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := store.Sessions().Save(txCtx, session); err != nil {
			return err
		}
		if err := store.Outbox().Add(txCtx, application.RecordedEvent{
			Topic:      application.TopicSessionCreated,
			Payload:    application.SessionCreatedEvent{Session: session},
			OccurredAt: time.Date(2026, 3, 20, 13, 1, 0, 0, time.UTC),
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	if _, err := store.Sessions().GetByID(ctx, session.ID); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("expected session not found after rollback, got %v", err)
	}

	pending, err := store.Outbox().ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("list pending outbox: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending outbox events after rollback, got %d", len(pending))
	}
}

func TestStoreSandboxRepositoriesRoundTrip(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	fixedTime := time.Date(2026, 3, 20, 14, 0, 0, 0, time.UTC)

	session, err := domain.NewSession("session-sandbox", domain.SessionModeFree, fixedTime)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	agent, err := domain.NewAgent(domain.Agent{
		ID:           "agent-sandbox",
		Name:         "Sandbox",
		Role:         "tooling",
		SystemPrompt: "Do sandbox work",
		Provider:     "local_stub",
		Model:        "codex",
		Policies:     domain.DefaultAgentPolicy(),
	})
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if err := store.Agents().Upsert(ctx, agent); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	startedAt := fixedTime.Add(time.Minute)
	completedAt := fixedTime.Add(2 * time.Minute)
	task := application.SandboxTask{
		ID:                 "task-1",
		SessionID:          session.ID,
		ConversationID:     "conversation-1",
		RequestedByAgentID: agent.ID,
		AssignedAgentID:    agent.ID,
		AssignedProvider:   application.AgentProviderClassSandboxedRuntime,
		RuntimeName:        "codex",
		WorkspaceRoot:      "/tmp/workspace",
		PermissionProfile:  application.SandboxPermissionPatch,
		Instruction:        "Update the docs",
		Status:             application.SandboxTaskStatusSucceeded,
		ResultSummary:      "updated docs",
		Artifacts: []application.SandboxTaskArtifact{
			{Path: "README.md", Description: "modified"},
		},
		Metadata:    map[string]any{"provider": "codex"},
		CreatedAt:   fixedTime,
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
	}

	handoff := application.AgentHandoff{
		ID:              "handoff-1",
		SessionID:       session.ID,
		ConversationID:  "conversation-1",
		SourceTaskID:    task.ID,
		TaskID:          task.ID,
		FromAgentID:     agent.ID,
		ToAgentID:       agent.ID,
		ToProviderClass: application.AgentProviderClassSandboxedRuntime,
		Reason:          "delegate patch work",
		CreatedAt:       fixedTime.Add(30 * time.Second),
	}

	if err := store.SandboxTasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := store.SandboxTasks().SaveHandoff(ctx, handoff); err != nil {
		t.Fatalf("SaveHandoff() error = %v", err)
	}

	gotTask, err := store.SandboxTasks().GetTaskByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTaskByID() error = %v", err)
	}
	if gotTask.ResultSummary != task.ResultSummary {
		t.Fatalf("expected result summary %q, got %q", task.ResultSummary, gotTask.ResultSummary)
	}
	if len(gotTask.Artifacts) != 1 || gotTask.Artifacts[0].Path != "README.md" {
		t.Fatalf("unexpected task artifacts %+v", gotTask.Artifacts)
	}

	tasks, err := store.SandboxTasks().ListTasksBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListTasksBySessionID() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("unexpected listed tasks %+v", tasks)
	}

	handoffs, err := store.SandboxTasks().ListHandoffsBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListHandoffsBySessionID() error = %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].ID != handoff.ID {
		t.Fatalf("unexpected listed handoffs %+v", handoffs)
	}
}

func TestMessageRepositoryOrdersSubSecondTimestampsDeterministically(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	session, err := domain.NewSession("session-order", domain.SessionModeFree, time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	session, err = session.Start()
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	base := time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC)
	messages := []domain.Message{
		mustNewTestMessage(t, "message-1", session.ID, base.Add(100*time.Millisecond), "first fractional"),
		mustNewTestMessage(t, "message-2", session.ID, base, "whole second"),
		mustNewTestMessage(t, "message-3", session.ID, base.Add(900*time.Millisecond), "later fractional"),
	}

	for _, message := range messages {
		if err := store.Messages().Save(ctx, message); err != nil {
			t.Fatalf("save message %q: %v", message.ID, err)
		}
	}

	got, err := store.Messages().ListBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}

	gotIDs := make([]domain.MessageID, 0, len(got))
	for _, message := range got {
		gotIDs = append(gotIDs, message.ID)
	}

	wantIDs := []domain.MessageID{"message-2", "message-1", "message-3"}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("unexpected message order: got %v want %v", gotIDs, wantIDs)
	}
}

func TestMessageRepositoryRejectsDuplicateMessageID(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	ctx := context.Background()
	session, err := domain.NewSession("session-duplicate", domain.SessionModeFree, time.Date(2026, 3, 20, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	session, err = session.Start()
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	original := mustNewTestMessage(t, "message-dup", session.ID, time.Date(2026, 3, 20, 16, 1, 0, 0, time.UTC), "original")
	if err := store.Messages().Save(ctx, original); err != nil {
		t.Fatalf("save original message: %v", err)
	}

	duplicate := mustNewTestMessage(t, "message-dup", session.ID, time.Date(2026, 3, 20, 16, 2, 0, 0, time.UTC), "mutated")
	err = store.Messages().Save(ctx, duplicate)
	if !errors.Is(err, application.ErrAlreadyExists) {
		t.Fatalf("expected duplicate message error, got %v", err)
	}

	got, err := store.Messages().ListBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 message after duplicate insert, got %d", len(got))
	}
	if got[0].Body != original.Body {
		t.Fatalf("expected original body %q to remain unchanged, got %q", original.Body, got[0].Body)
	}
}

func TestOutboxFlushClaimsRowsAcrossConcurrentStores(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "concurrent-outbox.db")

	storeA := openStoreAtPath(t, path)
	defer storeA.Close()

	storeB := openStoreAtPath(t, path)
	defer storeB.Close()

	session, err := domain.NewSession("session-concurrent", domain.SessionModeFree, time.Date(2026, 3, 20, 17, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	if err := storeA.Sessions().Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	if err := storeA.Outbox().Add(ctx, application.RecordedEvent{
		Topic:      application.TopicSessionCreated,
		Payload:    application.SessionCreatedEvent{Session: session},
		OccurredAt: time.Date(2026, 3, 20, 17, 0, 1, 0, time.UTC),
	}); err != nil {
		t.Fatalf("add outbox event: %v", err)
	}

	publisher := &countingPublisher{}
	var wg sync.WaitGroup
	wg.Add(2)

	var flushErrs [2]error
	go func() {
		defer wg.Done()
		_, flushErrs[0] = storeA.Outbox().Flush(ctx, publisher)
	}()
	go func() {
		defer wg.Done()
		_, flushErrs[1] = storeB.Outbox().Flush(ctx, publisher)
	}()
	wg.Wait()

	for i, err := range flushErrs {
		if err != nil {
			t.Fatalf("flush %d error = %v", i, err)
		}
	}

	if publisher.count != 1 {
		t.Fatalf("expected exactly one publication, got %d", publisher.count)
	}

	stream, err := storeA.SessionStreams().ListBySessionID(ctx, session.ID)
	if err != nil {
		t.Fatalf("list session stream: %v", err)
	}
	if len(stream) != 1 {
		t.Fatalf("expected exactly one persisted session stream row, got %d", len(stream))
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	return openStoreAtPath(t, filepath.Join(t.TempDir(), "crew-test.db"))
}

func openStoreAtPath(t *testing.T, path string) *Store {
	t.Helper()

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		t.Fatalf("migrate store: %v", err)
	}

	return store
}

func mustNewTestMessage(t *testing.T, id domain.MessageID, sessionID domain.SessionID, timestamp time.Time, body string) domain.Message {
	t.Helper()

	message, err := domain.NewMessage(domain.Message{
		ID:             id,
		SessionID:      sessionID,
		ConversationID: "conversation-1",
		Sender:         domain.UserSender("operator-1"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           body,
		Timestamp:      timestamp,
		Metadata:       map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("new test message %q: %v", id, err)
	}

	return message
}

type countingPublisher struct {
	mu    sync.Mutex
	count int
}

func (p *countingPublisher) Publish(context.Context, string, any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count++
	return nil
}
