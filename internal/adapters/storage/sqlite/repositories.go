package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"

	"crew/internal/application"
	"crew/internal/domain"
)

type SessionRepository struct {
	store *Store
}

func (r *SessionRepository) Save(ctx context.Context, session domain.Session) error {
	if err := session.Validate(); err != nil {
		return err
	}

	_, err := r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO sessions(id, mode, status, actor_catalog, created_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  mode = excluded.mode,
  status = excluded.status,
  actor_catalog = excluded.actor_catalog,
  created_at = excluded.created_at`,
		string(session.ID),
		string(session.Mode),
		string(session.Status),
		session.ActorCatalog,
		formatTimestamp(session.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save session %q: %w", session.ID, err)
	}

	return nil
}

func (r *SessionRepository) GetByID(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	var (
		mode         string
		status       string
		actorCatalog string
		createdAt    string
	)

	err := r.store.execer(ctx).QueryRowContext(
		ctx,
		`SELECT mode, status, actor_catalog, created_at FROM sessions WHERE id = ?`,
		string(id),
	).Scan(&mode, &status, &actorCatalog, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Session{}, application.NotFoundError{Entity: "session", ID: string(id)}
		}
		return domain.Session{}, fmt.Errorf("get session %q: %w", id, err)
	}

	timestamp, err := parseTimestamp(createdAt)
	if err != nil {
		return domain.Session{}, err
	}

	session := domain.Session{
		ID:           id,
		Mode:         domain.SessionMode(mode),
		Status:       domain.SessionStatus(status),
		ActorCatalog: actorCatalog,
		CreatedAt:    timestamp,
	}
	if err := session.Validate(); err != nil {
		return domain.Session{}, fmt.Errorf("decode session %q: %w", id, err)
	}

	return session, nil
}

type MessageRepository struct {
	store *Store
}

func (r *MessageRepository) Save(ctx context.Context, message domain.Message) error {
	message, err := domain.NewMessage(message)
	if err != nil {
		return err
	}

	recipientsJSON, err := marshalJSON(message.ToAgentIDs)
	if err != nil {
		return fmt.Errorf("encode message recipients %q: %w", message.ID, err)
	}

	metadataJSON, err := marshalJSON(message.Metadata)
	if err != nil {
		return fmt.Errorf("encode message metadata %q: %w", message.ID, err)
	}

	var replyTo any
	if message.ReplyTo != "" {
		replyTo = string(message.ReplyTo)
	}

	_, err = r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO messages(
  id,
  session_id,
  conversation_id,
  sender_type,
  sender_id,
  recipient_ids_json,
  channel,
  kind,
  body,
  reply_to,
  recorded_at,
  metadata_json
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(message.ID),
		string(message.SessionID),
		string(message.ConversationID),
		string(message.Sender.Type),
		message.Sender.ID,
		recipientsJSON,
		string(message.Channel),
		string(message.Kind),
		message.Body,
		replyTo,
		formatTimestamp(message.Timestamp),
		metadataJSON,
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return fmt.Errorf("%w: message %q already exists", application.ErrAlreadyExists, message.ID)
		}
		return fmt.Errorf("save message %q: %w", message.ID, err)
	}

	return nil
}

func (r *MessageRepository) ListBySessionID(ctx context.Context, sessionID domain.SessionID) ([]domain.Message, error) {
	rows, err := r.store.execer(ctx).QueryContext(
		ctx,
		`SELECT
  id,
  conversation_id,
  sender_type,
  sender_id,
  recipient_ids_json,
  channel,
  kind,
  body,
  reply_to,
  recorded_at,
  metadata_json
FROM messages
WHERE session_id = ?
ORDER BY recorded_at ASC, id ASC`,
		string(sessionID),
	)
	if err != nil {
		return nil, fmt.Errorf("list messages for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	var messages []domain.Message
	for rows.Next() {
		var (
			id             string
			conversationID string
			senderType     string
			senderID       string
			recipientsJSON string
			channel        string
			kind           string
			body           string
			replyTo        sql.NullString
			recordedAt     string
			metadataJSON   string
		)

		if err := rows.Scan(
			&id,
			&conversationID,
			&senderType,
			&senderID,
			&recipientsJSON,
			&channel,
			&kind,
			&body,
			&replyTo,
			&recordedAt,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan message for session %q: %w", sessionID, err)
		}

		timestamp, err := parseTimestamp(recordedAt)
		if err != nil {
			return nil, err
		}

		var recipients []domain.AgentID
		if err := json.Unmarshal([]byte(recipientsJSON), &recipients); err != nil {
			return nil, fmt.Errorf("decode message recipients %q: %w", id, err)
		}

		var metadata map[string]any
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return nil, fmt.Errorf("decode message metadata %q: %w", id, err)
		}

		message, err := domain.NewMessage(domain.Message{
			ID:             domain.MessageID(id),
			SessionID:      sessionID,
			ConversationID: domain.ConversationID(conversationID),
			Sender: domain.MessageSender{
				Type: domain.MessageSenderType(senderType),
				ID:   senderID,
			},
			ToAgentIDs: recipients,
			Channel:    domain.MessageChannel(channel),
			Kind:       domain.MessageKind(kind),
			Body:       body,
			ReplyTo:    domain.MessageID(replyTo.String),
			Timestamp:  timestamp,
			Metadata:   metadata,
		})
		if err != nil {
			return nil, fmt.Errorf("decode message %q: %w", id, err)
		}

		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages for session %q: %w", sessionID, err)
	}

	return messages, nil
}

func isUniqueConstraintError(err error) bool {
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}

	return sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey ||
		sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique
}

type WorkflowRepository struct {
	store *Store
}

func (r *WorkflowRepository) Save(ctx context.Context, workflow domain.Workflow) error {
	workflow, err := domain.NewWorkflow(workflow)
	if err != nil {
		return err
	}

	workflowJSON, err := marshalJSON(workflow)
	if err != nil {
		return fmt.Errorf("encode workflow %q: %w", workflow.ID, err)
	}

	_, err = r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO workflows(id, name, entry_step_id, workflow_json)
VALUES(?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  entry_step_id = excluded.entry_step_id,
  workflow_json = excluded.workflow_json`,
		string(workflow.ID),
		workflow.Name,
		string(workflow.EntryStepID),
		workflowJSON,
	)
	if err != nil {
		return fmt.Errorf("save workflow %q: %w", workflow.ID, err)
	}

	return nil
}

func (r *WorkflowRepository) GetByID(ctx context.Context, id domain.WorkflowID) (domain.Workflow, error) {
	var payload string
	err := r.store.execer(ctx).QueryRowContext(
		ctx,
		`SELECT workflow_json FROM workflows WHERE id = ?`,
		string(id),
	).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Workflow{}, application.NotFoundError{Entity: "workflow", ID: string(id)}
		}
		return domain.Workflow{}, fmt.Errorf("get workflow %q: %w", id, err)
	}

	var workflow domain.Workflow
	if err := json.Unmarshal([]byte(payload), &workflow); err != nil {
		return domain.Workflow{}, fmt.Errorf("decode workflow %q: %w", id, err)
	}

	workflow, err = domain.NewWorkflow(workflow)
	if err != nil {
		return domain.Workflow{}, fmt.Errorf("validate workflow %q: %w", id, err)
	}

	return workflow, nil
}

type AgentRepository struct {
	store *Store
}

func (r *AgentRepository) Upsert(ctx context.Context, agent domain.Agent) error {
	agent, err := domain.NewAgent(agent)
	if err != nil {
		return err
	}

	toolsJSON, err := marshalJSON(agent.Tools)
	if err != nil {
		return fmt.Errorf("encode agent tools %q: %w", agent.ID, err)
	}

	policiesJSON, err := marshalJSON(agent.Policies)
	if err != nil {
		return fmt.Errorf("encode agent policy %q: %w", agent.ID, err)
	}

	_, err = r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO agents(id, name, role, system_prompt, provider, model, reasoning_effort, delegation_runtime, sandbox_workspace_root, sandbox_workspace_mode, tools_json, policies_json, active)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  role = excluded.role,
  system_prompt = excluded.system_prompt,
  provider = excluded.provider,
  model = excluded.model,
  reasoning_effort = excluded.reasoning_effort,
  delegation_runtime = excluded.delegation_runtime,
  sandbox_workspace_root = excluded.sandbox_workspace_root,
  sandbox_workspace_mode = excluded.sandbox_workspace_mode,
  tools_json = excluded.tools_json,
  policies_json = excluded.policies_json,
  active = 1`,
		string(agent.ID),
		agent.Name,
		agent.Role,
		agent.SystemPrompt,
		agent.Provider,
		agent.Model,
		agent.ReasoningEffort,
		agent.DelegationRuntime,
		agent.SandboxWorkspaceRoot,
		agent.SandboxWorkspaceMode,
		toolsJSON,
		policiesJSON,
	)
	if err != nil {
		return fmt.Errorf("save agent %q: %w", agent.ID, err)
	}

	return nil
}

func (r *AgentRepository) GetByID(ctx context.Context, id domain.AgentID) (domain.Agent, error) {
	var (
		name                 string
		role                 string
		systemPrompt         string
		provider             string
		model                string
		reasoningEffort      string
		delegationRuntime    string
		sandboxWorkspaceRoot string
		sandboxWorkspaceMode string
		toolsJSON            string
		policiesJSON         string
	)

	err := r.store.execer(ctx).QueryRowContext(
		ctx,
		`SELECT name, role, system_prompt, provider, model, reasoning_effort, delegation_runtime, sandbox_workspace_root, sandbox_workspace_mode, tools_json, policies_json
FROM agents
WHERE id = ? AND active = 1`,
		string(id),
	).Scan(&name, &role, &systemPrompt, &provider, &model, &reasoningEffort, &delegationRuntime, &sandboxWorkspaceRoot, &sandboxWorkspaceMode, &toolsJSON, &policiesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Agent{}, application.NotFoundError{Entity: "agent", ID: string(id)}
		}
		return domain.Agent{}, fmt.Errorf("get agent %q: %w", id, err)
	}

	var tools []string
	if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent tools %q: %w", id, err)
	}

	var policies domain.AgentPolicy
	if err := json.Unmarshal([]byte(policiesJSON), &policies); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent policies %q: %w", id, err)
	}

	agent, err := domain.NewAgent(domain.Agent{
		ID:                   id,
		Name:                 name,
		Role:                 role,
		SystemPrompt:         systemPrompt,
		Provider:             provider,
		Model:                model,
		ReasoningEffort:      reasoningEffort,
		DelegationRuntime:    delegationRuntime,
		SandboxWorkspaceRoot: sandboxWorkspaceRoot,
		SandboxWorkspaceMode: sandboxWorkspaceMode,
		Tools:                tools,
		Policies:             policies,
	})
	if err != nil {
		return domain.Agent{}, fmt.Errorf("validate agent %q: %w", id, err)
	}

	return agent, nil
}

func (r *AgentRepository) List(ctx context.Context) ([]domain.Agent, error) {
	rows, err := r.store.execer(ctx).QueryContext(
		ctx,
		`SELECT id, name, role, system_prompt, provider, model, reasoning_effort, delegation_runtime, sandbox_workspace_root, sandbox_workspace_mode, tools_json, policies_json
FROM agents
WHERE active = 1
ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	agents := make([]domain.Agent, 0)
	for rows.Next() {
		var (
			id                   string
			name                 string
			role                 string
			systemPrompt         string
			provider             string
			model                string
			reasoningEffort      string
			delegationRuntime    string
			sandboxWorkspaceRoot string
			sandboxWorkspaceMode string
			toolsJSON            string
			policiesJSON         string
		)
		if err := rows.Scan(&id, &name, &role, &systemPrompt, &provider, &model, &reasoningEffort, &delegationRuntime, &sandboxWorkspaceRoot, &sandboxWorkspaceMode, &toolsJSON, &policiesJSON); err != nil {
			return nil, fmt.Errorf("scan listed agent: %w", err)
		}

		var tools []string
		if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
			return nil, fmt.Errorf("decode listed agent tools %q: %w", id, err)
		}

		var policies domain.AgentPolicy
		if err := json.Unmarshal([]byte(policiesJSON), &policies); err != nil {
			return nil, fmt.Errorf("decode listed agent policies %q: %w", id, err)
		}

		agent, err := domain.NewAgent(domain.Agent{
			ID:                   domain.AgentID(id),
			Name:                 name,
			Role:                 role,
			SystemPrompt:         systemPrompt,
			Provider:             provider,
			Model:                model,
			ReasoningEffort:      reasoningEffort,
			DelegationRuntime:    delegationRuntime,
			SandboxWorkspaceRoot: sandboxWorkspaceRoot,
			SandboxWorkspaceMode: sandboxWorkspaceMode,
			Tools:                tools,
			Policies:             policies,
		})
		if err != nil {
			return nil, fmt.Errorf("validate listed agent %q: %w", id, err)
		}

		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate listed agents: %w", err)
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

func (r *AgentRepository) SyncCatalog(ctx context.Context, agents []domain.Agent) error {
	return r.store.UnitOfWork().WithinTransaction(ctx, func(txCtx context.Context) error {
		if _, err := r.store.execer(txCtx).ExecContext(txCtx, `UPDATE agents SET active = 0 WHERE active != 0`); err != nil {
			return fmt.Errorf("deactivate sqlite agents before sync: %w", err)
		}
		for _, agent := range agents {
			if err := r.Upsert(txCtx, agent); err != nil {
				return err
			}
		}
		return nil
	})
}

type OutboxRepository struct {
	store *Store
}

const outboxClaimTTL = 30 * time.Second

type PendingOutboxEvent struct {
	Sequence    int64
	Topic       string
	OccurredAt  time.Time
	PayloadJSON json.RawMessage
}

func (r *OutboxRepository) Add(ctx context.Context, event application.RecordedEvent) error {
	if event.Topic == "" {
		return fmt.Errorf("outbox event topic must not be empty")
	}
	if event.OccurredAt.IsZero() {
		return fmt.Errorf("outbox event occurred_at must not be zero")
	}

	payloadJSON, err := marshalJSON(event.Payload)
	if err != nil {
		return fmt.Errorf("encode outbox payload for topic %q: %w", event.Topic, err)
	}

	_, err = r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO outbox_events(topic, occurred_at, payload_json, published_at)
VALUES(?, ?, ?, NULL)`,
		event.Topic,
		formatTimestamp(event.OccurredAt),
		payloadJSON,
	)
	if err != nil {
		return fmt.Errorf("add outbox event %q: %w", event.Topic, err)
	}

	return nil
}

func (r *OutboxRepository) ListPending(ctx context.Context, limit int) ([]PendingOutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.store.execer(ctx).QueryContext(
		ctx,
		`SELECT sequence, topic, occurred_at, payload_json
FROM outbox_events
WHERE published_at IS NULL
ORDER BY sequence ASC
LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox events: %w", err)
	}
	defer rows.Close()

	var events []PendingOutboxEvent
	for rows.Next() {
		var (
			event      PendingOutboxEvent
			occurredAt string
			payload    string
		)

		if err := rows.Scan(&event.Sequence, &event.Topic, &occurredAt, &payload); err != nil {
			return nil, fmt.Errorf("scan pending outbox event: %w", err)
		}

		event.OccurredAt, err = parseTimestamp(occurredAt)
		if err != nil {
			return nil, err
		}
		event.PayloadJSON = json.RawMessage(payload)
		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending outbox events: %w", err)
	}

	return events, nil
}

func (r *OutboxRepository) MarkPublished(ctx context.Context, sequences []int64, publishedAt time.Time) error {
	if len(sequences) == 0 {
		return nil
	}
	if publishedAt.IsZero() {
		return fmt.Errorf("published_at must not be zero")
	}

	txExec := r.store.execer(ctx)
	for _, sequence := range sequences {
		if _, err := txExec.ExecContext(
			ctx,
			`UPDATE outbox_events SET published_at = ? WHERE sequence = ?`,
			formatTimestamp(publishedAt),
			sequence,
		); err != nil {
			return fmt.Errorf("mark outbox event %d published: %w", sequence, err)
		}
	}

	return nil
}

func (r *OutboxRepository) Flush(ctx context.Context, publisher application.EventBus) ([]application.RecordedEvent, error) {
	var published []application.RecordedEvent

	for {
		current, claimToken, err := r.claimNextPending(ctx)
		if err != nil {
			return published, err
		}
		if current == nil {
			return published, nil
		}

		event, err := decodeRecordedEvent(current.Topic, formatTimestamp(current.OccurredAt), current.PayloadJSON)
		if err != nil {
			return published, fmt.Errorf("decode pending outbox event %d: %w", current.Sequence, err)
		}

		if err := publisher.Publish(ctx, event.Topic, event.Payload); err != nil {
			return published, err
		}

		if err := r.store.UnitOfWork().WithinTransaction(ctx, func(txCtx context.Context) error {
			if sessionID, ok := eventSessionID(event.Payload); ok {
				if err := r.store.SessionStreams().Append(txCtx, SessionStreamRecord{
					SessionID:      sessionID,
					Topic:          event.Topic,
					RecordedAt:     event.OccurredAt,
					PayloadJSON:    current.PayloadJSON,
					OutboxSequence: &current.Sequence,
				}); err != nil {
					return err
				}
			}

			return r.markPublishedClaimed(txCtx, current.Sequence, claimToken, time.Now().UTC())
		}); err != nil {
			return published, err
		}

		published = append(published, event)
	}
}

func (r *OutboxRepository) claimNextPending(ctx context.Context) (*PendingOutboxEvent, string, error) {
	claimToken, err := newClaimToken()
	if err != nil {
		return nil, "", err
	}

	now := time.Now().UTC()
	claimDeadline := now.Add(outboxClaimTTL)
	result, err := r.store.db.ExecContext(
		ctx,
		`UPDATE outbox_events
SET claim_token = ?, claim_deadline = ?
WHERE sequence = (
  SELECT sequence
  FROM outbox_events
  WHERE published_at IS NULL
    AND (claim_deadline IS NULL OR claim_deadline < ?)
  ORDER BY sequence ASC
  LIMIT 1
)
AND published_at IS NULL
AND (claim_deadline IS NULL OR claim_deadline < ?)`,
		claimToken,
		formatTimestamp(claimDeadline),
		formatTimestamp(now),
		formatTimestamp(now),
	)
	if err != nil {
		return nil, "", fmt.Errorf("claim next pending outbox event: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, "", fmt.Errorf("read claimed outbox rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, "", nil
	}

	var (
		event      PendingOutboxEvent
		occurredAt string
		payload    string
	)
	if err := r.store.db.QueryRowContext(
		ctx,
		`SELECT sequence, topic, occurred_at, payload_json
FROM outbox_events
WHERE claim_token = ?`,
		claimToken,
	).Scan(&event.Sequence, &event.Topic, &occurredAt, &payload); err != nil {
		return nil, "", fmt.Errorf("load claimed outbox event %q: %w", claimToken, err)
	}

	event.OccurredAt, err = parseTimestamp(occurredAt)
	if err != nil {
		return nil, "", err
	}
	event.PayloadJSON = json.RawMessage(payload)

	return &event, claimToken, nil
}

func (r *OutboxRepository) markPublishedClaimed(ctx context.Context, sequence int64, claimToken string, publishedAt time.Time) error {
	result, err := r.store.execer(ctx).ExecContext(
		ctx,
		`UPDATE outbox_events
SET published_at = ?, claim_token = NULL, claim_deadline = NULL
WHERE sequence = ? AND claim_token = ?`,
		formatTimestamp(publishedAt),
		sequence,
		claimToken,
	)
	if err != nil {
		return fmt.Errorf("mark claimed outbox event %d published: %w", sequence, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read marked outbox rows affected for %d: %w", sequence, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("mark claimed outbox event %d published: expected 1 affected row, got %d", sequence, rowsAffected)
	}

	return nil
}

type SessionStreamRepository struct {
	store *Store
}

type SessionStreamRecord struct {
	Sequence       int64
	SessionID      domain.SessionID
	Topic          string
	RecordedAt     time.Time
	PayloadJSON    json.RawMessage
	OutboxSequence *int64
}

func (r *SessionStreamRepository) Append(ctx context.Context, record SessionStreamRecord) error {
	if err := record.SessionID.Validate(); err != nil {
		return err
	}
	if record.Topic == "" {
		return fmt.Errorf("session stream topic must not be empty")
	}
	if record.RecordedAt.IsZero() {
		return fmt.Errorf("session stream recorded_at must not be zero")
	}
	if len(record.PayloadJSON) == 0 {
		return fmt.Errorf("session stream payload must not be empty")
	}

	_, err := r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO session_stream(session_id, topic, recorded_at, payload_json, outbox_sequence)
VALUES(?, ?, ?, ?, ?)`,
		string(record.SessionID),
		record.Topic,
		formatTimestamp(record.RecordedAt),
		string(record.PayloadJSON),
		record.OutboxSequence,
	)
	if err != nil {
		return fmt.Errorf("append session stream record for %q: %w", record.SessionID, err)
	}

	return nil
}

func (r *SessionStreamRepository) ListBySessionID(ctx context.Context, sessionID domain.SessionID) ([]SessionStreamRecord, error) {
	rows, err := r.store.execer(ctx).QueryContext(
		ctx,
		`SELECT sequence, topic, recorded_at, payload_json, outbox_sequence
FROM session_stream
WHERE session_id = ?
ORDER BY sequence ASC`,
		string(sessionID),
	)
	if err != nil {
		return nil, fmt.Errorf("list session stream for %q: %w", sessionID, err)
	}
	defer rows.Close()

	var records []SessionStreamRecord
	for rows.Next() {
		var (
			record     SessionStreamRecord
			recordedAt string
			payload    string
			outboxSeq  sql.NullInt64
		)

		if err := rows.Scan(&record.Sequence, &record.Topic, &recordedAt, &payload, &outboxSeq); err != nil {
			return nil, fmt.Errorf("scan session stream for %q: %w", sessionID, err)
		}

		record.SessionID = sessionID
		record.RecordedAt, err = parseTimestamp(recordedAt)
		if err != nil {
			return nil, err
		}
		record.PayloadJSON = json.RawMessage(payload)
		if outboxSeq.Valid {
			record.OutboxSequence = &outboxSeq.Int64
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session stream for %q: %w", sessionID, err)
	}

	return records, nil
}

func (r *SessionStreamRepository) ListRecordedBySessionID(ctx context.Context, sessionID domain.SessionID) ([]application.RecordedEvent, error) {
	rows, err := r.ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	recorded := make([]application.RecordedEvent, 0, len(rows))
	for _, row := range rows {
		event, err := decodeRecordedEvent(row.Topic, formatTimestamp(row.RecordedAt), row.PayloadJSON)
		if err != nil {
			return nil, fmt.Errorf("decode persisted session stream %d for %q: %w", row.Sequence, sessionID, err)
		}
		recorded = append(recorded, event)
	}

	return recorded, nil
}
