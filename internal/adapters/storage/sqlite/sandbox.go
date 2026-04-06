package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"crew/internal/application"
	"crew/internal/domain"
)

type SandboxTaskRepository struct {
	store *Store
}

func (r *SandboxTaskRepository) SaveTask(ctx context.Context, task application.SandboxTask) error {
	if err := task.Validate(); err != nil {
		return err
	}

	artifactsJSON, err := marshalJSON(task.Artifacts)
	if err != nil {
		return fmt.Errorf("encode sandbox task artifacts %q: %w", task.ID, err)
	}

	metadataJSON, err := marshalJSON(task.Metadata)
	if err != nil {
		return fmt.Errorf("encode sandbox task metadata %q: %w", task.ID, err)
	}

	var (
		requestedBy any
		assignedTo  any
		startedAt   any
		completedAt any
	)
	if task.RequestedByAgentID != "" {
		requestedBy = string(task.RequestedByAgentID)
	}
	if task.AssignedAgentID != "" {
		assignedTo = string(task.AssignedAgentID)
	}
	if task.StartedAt != nil {
		startedAt = formatTimestamp(*task.StartedAt)
	}
	if task.CompletedAt != nil {
		completedAt = formatTimestamp(*task.CompletedAt)
	}

	_, err = r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO agent_tasks(
  id,
  session_id,
  conversation_id,
  requested_by_agent_id,
  assigned_agent_id,
  assigned_provider,
  runtime_name,
  workspace_root,
  sandbox_root,
  permission_profile,
  instruction,
  status,
  result_summary,
  error_message,
  artifacts_json,
  metadata_json,
  created_at,
  started_at,
  completed_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  session_id = excluded.session_id,
  conversation_id = excluded.conversation_id,
  requested_by_agent_id = excluded.requested_by_agent_id,
  assigned_agent_id = excluded.assigned_agent_id,
  assigned_provider = excluded.assigned_provider,
  runtime_name = excluded.runtime_name,
  workspace_root = excluded.workspace_root,
  sandbox_root = excluded.sandbox_root,
  permission_profile = excluded.permission_profile,
  instruction = excluded.instruction,
  status = excluded.status,
  result_summary = excluded.result_summary,
  error_message = excluded.error_message,
  artifacts_json = excluded.artifacts_json,
  metadata_json = excluded.metadata_json,
  created_at = excluded.created_at,
  started_at = excluded.started_at,
  completed_at = excluded.completed_at`,
		string(task.ID),
		string(task.SessionID),
		string(task.ConversationID),
		requestedBy,
		assignedTo,
		string(task.AssignedProvider),
		task.RuntimeName,
		task.WorkspaceRoot,
		task.SandboxRoot,
		string(task.PermissionProfile),
		task.Instruction,
		string(task.Status),
		task.ResultSummary,
		task.ErrorMessage,
		artifactsJSON,
		metadataJSON,
		formatTimestamp(task.CreatedAt),
		startedAt,
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("save sandbox task %q: %w", task.ID, err)
	}

	return nil
}

func (r *SandboxTaskRepository) GetTaskByID(ctx context.Context, id application.AgentTaskID) (application.SandboxTask, error) {
	row := r.store.execer(ctx).QueryRowContext(
		ctx,
		`SELECT
  session_id,
  conversation_id,
  requested_by_agent_id,
  assigned_agent_id,
  assigned_provider,
  runtime_name,
  workspace_root,
  sandbox_root,
  permission_profile,
  instruction,
  status,
  result_summary,
  error_message,
  artifacts_json,
  metadata_json,
  created_at,
  started_at,
  completed_at
FROM agent_tasks
WHERE id = ?`,
		string(id),
	)

	task, err := scanSandboxTask(row, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return application.SandboxTask{}, application.NotFoundError{Entity: "sandbox_task", ID: string(id)}
		}
		return application.SandboxTask{}, fmt.Errorf("get sandbox task %q: %w", id, err)
	}

	return task, nil
}

func (r *SandboxTaskRepository) ListTasksBySessionID(ctx context.Context, sessionID domain.SessionID) ([]application.SandboxTask, error) {
	rows, err := r.store.execer(ctx).QueryContext(
		ctx,
		`SELECT
  id,
  conversation_id,
	requested_by_agent_id,
	assigned_agent_id,
	assigned_provider,
	runtime_name,
	workspace_root,
	sandbox_root,
	permission_profile,
  instruction,
  status,
  result_summary,
  error_message,
  artifacts_json,
  metadata_json,
  created_at,
  started_at,
  completed_at
FROM agent_tasks
WHERE session_id = ?
ORDER BY created_at ASC, id ASC`,
		string(sessionID),
	)
	if err != nil {
		return nil, fmt.Errorf("list sandbox tasks for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	tasks := make([]application.SandboxTask, 0)
	for rows.Next() {
		var (
			id                string
			conversationID    string
			requestedBy       sql.NullString
			assignedTo        sql.NullString
			assignedProvider  string
			runtimeName       string
			workspaceRoot     string
			sandboxRoot       string
			permissionProfile string
			instruction       string
			status            string
			resultSummary     string
			errorMessage      string
			artifactsJSON     string
			metadataJSON      string
			createdAt         string
			startedAt         sql.NullString
			completedAt       sql.NullString
		)

		if err := rows.Scan(
			&id,
			&conversationID,
			&requestedBy,
			&assignedTo,
			&assignedProvider,
			&runtimeName,
			&workspaceRoot,
			&sandboxRoot,
			&permissionProfile,
			&instruction,
			&status,
			&resultSummary,
			&errorMessage,
			&artifactsJSON,
			&metadataJSON,
			&createdAt,
			&startedAt,
			&completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan sandbox task for session %q: %w", sessionID, err)
		}

		task, err := decodeSandboxTask(
			application.AgentTaskID(id),
			sessionID,
			conversationID,
			requestedBy,
			assignedTo,
			assignedProvider,
			runtimeName,
			workspaceRoot,
			sandboxRoot,
			permissionProfile,
			instruction,
			status,
			resultSummary,
			errorMessage,
			artifactsJSON,
			metadataJSON,
			createdAt,
			startedAt,
			completedAt,
		)
		if err != nil {
			return nil, err
		}

		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sandbox tasks for session %q: %w", sessionID, err)
	}

	slices.SortFunc(tasks, func(a, b application.SandboxTask) int {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			if a.CreatedAt.Before(b.CreatedAt) {
				return -1
			}
			return 1
		}
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

func (r *SandboxTaskRepository) SaveHandoff(ctx context.Context, handoff application.AgentHandoff) error {
	if err := handoff.Validate(); err != nil {
		return err
	}

	var (
		sourceMessageID any
		sourceTaskID    any
		fromAgentID     any
		toAgentID       any
	)
	if handoff.SourceMessageID != "" {
		sourceMessageID = string(handoff.SourceMessageID)
	}
	if handoff.SourceTaskID != "" {
		sourceTaskID = string(handoff.SourceTaskID)
	}
	if handoff.FromAgentID != "" {
		fromAgentID = string(handoff.FromAgentID)
	}
	if handoff.ToAgentID != "" {
		toAgentID = string(handoff.ToAgentID)
	}

	_, err := r.store.execer(ctx).ExecContext(
		ctx,
		`INSERT INTO agent_handoffs(
  id,
  session_id,
  conversation_id,
  source_message_id,
  source_task_id,
  task_id,
  from_agent_id,
  to_agent_id,
  to_provider_class,
  reason,
  created_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  session_id = excluded.session_id,
  conversation_id = excluded.conversation_id,
  source_message_id = excluded.source_message_id,
  source_task_id = excluded.source_task_id,
  task_id = excluded.task_id,
  from_agent_id = excluded.from_agent_id,
  to_agent_id = excluded.to_agent_id,
  to_provider_class = excluded.to_provider_class,
  reason = excluded.reason,
  created_at = excluded.created_at`,
		string(handoff.ID),
		string(handoff.SessionID),
		string(handoff.ConversationID),
		sourceMessageID,
		sourceTaskID,
		string(handoff.TaskID),
		fromAgentID,
		toAgentID,
		string(handoff.ToProviderClass),
		handoff.Reason,
		formatTimestamp(handoff.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save agent handoff %q: %w", handoff.ID, err)
	}

	return nil
}

func (r *SandboxTaskRepository) ListHandoffsBySessionID(ctx context.Context, sessionID domain.SessionID) ([]application.AgentHandoff, error) {
	rows, err := r.store.execer(ctx).QueryContext(
		ctx,
		`SELECT
  id,
  conversation_id,
  source_message_id,
  source_task_id,
  task_id,
  from_agent_id,
  to_agent_id,
  to_provider_class,
  reason,
  created_at
FROM agent_handoffs
WHERE session_id = ?
ORDER BY created_at ASC, id ASC`,
		string(sessionID),
	)
	if err != nil {
		return nil, fmt.Errorf("list agent handoffs for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	handoffs := make([]application.AgentHandoff, 0)
	for rows.Next() {
		var (
			id              string
			conversationID  string
			sourceMessageID sql.NullString
			sourceTaskID    sql.NullString
			taskID          string
			fromAgentID     sql.NullString
			toAgentID       sql.NullString
			toProviderClass string
			reason          string
			createdAt       string
		)

		if err := rows.Scan(
			&id,
			&conversationID,
			&sourceMessageID,
			&sourceTaskID,
			&taskID,
			&fromAgentID,
			&toAgentID,
			&toProviderClass,
			&reason,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent handoff for session %q: %w", sessionID, err)
		}

		handoff, err := decodeAgentHandoff(
			application.AgentHandoffID(id),
			sessionID,
			conversationID,
			sourceMessageID,
			sourceTaskID,
			taskID,
			fromAgentID,
			toAgentID,
			toProviderClass,
			reason,
			createdAt,
		)
		if err != nil {
			return nil, err
		}

		handoffs = append(handoffs, handoff)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent handoffs for session %q: %w", sessionID, err)
	}

	return handoffs, nil
}

func scanSandboxTask(row interface{ Scan(dest ...any) error }, id application.AgentTaskID) (application.SandboxTask, error) {
	var (
		sessionID         string
		conversationID    string
		requestedBy       sql.NullString
		assignedTo        sql.NullString
		assignedProvider  string
		runtimeName       string
		workspaceRoot     string
		sandboxRoot       string
		permissionProfile string
		instruction       string
		status            string
		resultSummary     string
		errorMessage      string
		artifactsJSON     string
		metadataJSON      string
		createdAt         string
		startedAt         sql.NullString
		completedAt       sql.NullString
	)

	if err := row.Scan(
		&sessionID,
		&conversationID,
		&requestedBy,
		&assignedTo,
		&assignedProvider,
		&runtimeName,
		&workspaceRoot,
		&sandboxRoot,
		&permissionProfile,
		&instruction,
		&status,
		&resultSummary,
		&errorMessage,
		&artifactsJSON,
		&metadataJSON,
		&createdAt,
		&startedAt,
		&completedAt,
	); err != nil {
		return application.SandboxTask{}, err
	}

	return decodeSandboxTask(
		id,
		domain.SessionID(sessionID),
		conversationID,
		requestedBy,
		assignedTo,
		assignedProvider,
		runtimeName,
		workspaceRoot,
		sandboxRoot,
		permissionProfile,
		instruction,
		status,
		resultSummary,
		errorMessage,
		artifactsJSON,
		metadataJSON,
		createdAt,
		startedAt,
		completedAt,
	)
}

func decodeSandboxTask(
	id application.AgentTaskID,
	sessionID domain.SessionID,
	conversationID string,
	requestedBy sql.NullString,
	assignedTo sql.NullString,
	assignedProvider string,
	runtimeName string,
	workspaceRoot string,
	sandboxRoot string,
	permissionProfile string,
	instruction string,
	status string,
	resultSummary string,
	errorMessage string,
	artifactsJSON string,
	metadataJSON string,
	createdAt string,
	startedAt sql.NullString,
	completedAt sql.NullString,
) (application.SandboxTask, error) {
	created, err := parseTimestamp(createdAt)
	if err != nil {
		return application.SandboxTask{}, err
	}

	var parsedStartedAt *time.Time
	if startedAt.Valid {
		parsed, err := parseTimestamp(startedAt.String)
		if err != nil {
			return application.SandboxTask{}, err
		}
		parsedStartedAt = &parsed
	}

	var parsedCompletedAt *time.Time
	if completedAt.Valid {
		parsed, err := parseTimestamp(completedAt.String)
		if err != nil {
			return application.SandboxTask{}, err
		}
		parsedCompletedAt = &parsed
	}

	var artifacts []application.SandboxTaskArtifact
	if err := json.Unmarshal([]byte(artifactsJSON), &artifacts); err != nil {
		return application.SandboxTask{}, fmt.Errorf("decode sandbox task artifacts %q: %w", id, err)
	}

	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return application.SandboxTask{}, fmt.Errorf("decode sandbox task metadata %q: %w", id, err)
	}

	task := application.SandboxTask{
		ID:                 id,
		SessionID:          sessionID,
		ConversationID:     domain.ConversationID(conversationID),
		RequestedByAgentID: domain.AgentID(requestedBy.String),
		AssignedAgentID:    domain.AgentID(assignedTo.String),
		AssignedProvider:   application.AgentProviderClass(assignedProvider),
		RuntimeName:        runtimeName,
		WorkspaceRoot:      workspaceRoot,
		SandboxRoot:        sandboxRoot,
		PermissionProfile:  application.SandboxPermissionProfile(permissionProfile),
		Instruction:        instruction,
		Status:             application.SandboxTaskStatus(status),
		ResultSummary:      resultSummary,
		ErrorMessage:       errorMessage,
		Artifacts:          artifacts,
		Metadata:           metadata,
		CreatedAt:          created,
		StartedAt:          parsedStartedAt,
		CompletedAt:        parsedCompletedAt,
	}
	if err := task.Validate(); err != nil {
		return application.SandboxTask{}, fmt.Errorf("validate sandbox task %q: %w", id, err)
	}

	return task, nil
}

func decodeAgentHandoff(
	id application.AgentHandoffID,
	sessionID domain.SessionID,
	conversationID string,
	sourceMessageID sql.NullString,
	sourceTaskID sql.NullString,
	taskID string,
	fromAgentID sql.NullString,
	toAgentID sql.NullString,
	toProviderClass string,
	reason string,
	createdAt string,
) (application.AgentHandoff, error) {
	created, err := parseTimestamp(createdAt)
	if err != nil {
		return application.AgentHandoff{}, err
	}

	handoff := application.AgentHandoff{
		ID:              id,
		SessionID:       sessionID,
		ConversationID:  domain.ConversationID(conversationID),
		SourceMessageID: domain.MessageID(sourceMessageID.String),
		SourceTaskID:    application.AgentTaskID(sourceTaskID.String),
		TaskID:          application.AgentTaskID(taskID),
		FromAgentID:     domain.AgentID(fromAgentID.String),
		ToAgentID:       domain.AgentID(toAgentID.String),
		ToProviderClass: application.AgentProviderClass(toProviderClass),
		Reason:          reason,
		CreatedAt:       created,
	}
	if err := handoff.Validate(); err != nil {
		return application.AgentHandoff{}, fmt.Errorf("validate agent handoff %q: %w", id, err)
	}

	return handoff, nil
}
