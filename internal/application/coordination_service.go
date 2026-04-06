package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"crew/internal/domain"
)

type CoordinationService struct {
	sessions SessionRepository
	agents   AgentRepository
	tasks    SandboxTaskRepository
	sandbox  SandboxedAgentRuntime
	outbox   EventOutbox
	tx       UnitOfWork
	clock    Clock
}

func NewCoordinationService(
	sessions SessionRepository,
	agents AgentRepository,
	tasks SandboxTaskRepository,
	sandbox SandboxedAgentRuntime,
	outbox EventOutbox,
	tx UnitOfWork,
	clock Clock,
) *CoordinationService {
	return &CoordinationService{
		sessions: sessions,
		agents:   agents,
		tasks:    tasks,
		sandbox:  sandbox,
		outbox:   outbox,
		tx:       tx,
		clock:    clock,
	}
}

func (s *CoordinationService) CreateSandboxTask(ctx context.Context, cmd CreateSandboxTaskCommand) (SandboxTask, error) {
	if err := cmd.Validate(); err != nil {
		return SandboxTask{}, err
	}
	if err := s.ensureCoordinationEntities(ctx, cmd.SessionID, cmd.RequestedByAgentID, cmd.AssignedAgentID); err != nil {
		return SandboxTask{}, err
	}

	task := SandboxTask{
		ID:                 cmd.TaskID,
		SessionID:          cmd.SessionID,
		ConversationID:     cmd.ConversationID,
		RequestedByAgentID: cmd.RequestedByAgentID,
		AssignedAgentID:    cmd.AssignedAgentID,
		AssignedProvider:   cmd.AssignedProvider,
		RuntimeName:        strings.TrimSpace(cmd.RuntimeName),
		WorkspaceRoot:      strings.TrimSpace(cmd.WorkspaceRoot),
		SandboxRoot:        strings.TrimSpace(cmd.SandboxRoot),
		PermissionProfile:  cmd.PermissionProfile,
		Instruction:        strings.TrimSpace(cmd.Instruction),
		Status:             SandboxTaskStatusPending,
		Metadata:           cloneAnyMap(cmd.Metadata),
		CreatedAt:          s.clock.Now(),
	}

	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.tasks.SaveTask(txCtx, task); err != nil {
			return err
		}
		return s.outbox.Add(txCtx, RecordedEvent{
			Topic:      TopicAgentTaskCreated,
			Payload:    AgentTaskCreatedEvent{Task: task},
			OccurredAt: task.CreatedAt,
		})
	}); err != nil {
		return SandboxTask{}, err
	}

	return task, nil
}

func (s *CoordinationService) RecordAgentHandoff(ctx context.Context, cmd CreateAgentHandoffCommand) (AgentHandoff, error) {
	if err := cmd.Validate(); err != nil {
		return AgentHandoff{}, err
	}
	if err := s.ensureCoordinationEntities(ctx, cmd.SessionID, cmd.FromAgentID, cmd.ToAgentID); err != nil {
		return AgentHandoff{}, err
	}
	task, err := s.tasks.GetTaskByID(ctx, cmd.TaskID)
	if err != nil {
		return AgentHandoff{}, err
	}
	if err := validateTaskScope(task, cmd.SessionID, cmd.ConversationID, "handoff task"); err != nil {
		return AgentHandoff{}, err
	}
	if cmd.SourceTaskID != "" {
		sourceTask, err := s.tasks.GetTaskByID(ctx, cmd.SourceTaskID)
		if err != nil {
			return AgentHandoff{}, err
		}
		if err := validateTaskScope(sourceTask, cmd.SessionID, cmd.ConversationID, "handoff source task"); err != nil {
			return AgentHandoff{}, err
		}
	}

	handoff := AgentHandoff{
		ID:              cmd.HandoffID,
		SessionID:       cmd.SessionID,
		ConversationID:  cmd.ConversationID,
		SourceMessageID: cmd.SourceMessageID,
		SourceTaskID:    cmd.SourceTaskID,
		TaskID:          cmd.TaskID,
		FromAgentID:     cmd.FromAgentID,
		ToAgentID:       cmd.ToAgentID,
		ToProviderClass: cmd.ToProviderClass,
		Reason:          strings.TrimSpace(cmd.Reason),
		CreatedAt:       s.clock.Now(),
	}

	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.tasks.SaveHandoff(txCtx, handoff); err != nil {
			return err
		}
		return s.outbox.Add(txCtx, RecordedEvent{
			Topic:      TopicAgentHandoffCreated,
			Payload:    AgentHandoffCreatedEvent{Handoff: handoff},
			OccurredAt: handoff.CreatedAt,
		})
	}); err != nil {
		return AgentHandoff{}, err
	}

	return handoff, nil
}

func (s *CoordinationService) ExecuteSandboxTask(ctx context.Context, cmd ExecuteSandboxTaskCommand) (SandboxTask, error) {
	if err := cmd.Validate(); err != nil {
		return SandboxTask{}, err
	}
	if s.sandbox == nil {
		return SandboxTask{}, ErrDisabled
	}

	task, err := s.tasks.GetTaskByID(ctx, cmd.TaskID)
	if err != nil {
		return SandboxTask{}, err
	}
	if task.Status != SandboxTaskStatusPending {
		return SandboxTask{}, fmt.Errorf("%w: sandbox task %q is %q, must be pending to execute", ErrInvalidState, task.ID, task.Status)
	}
	if err := validateSandboxRuntimeMatch(task, s.sandbox); err != nil {
		return SandboxTask{}, err
	}

	startedAt := s.clock.Now()
	task.Status = SandboxTaskStatusRunning
	task.StartedAt = timePtr(startedAt)

	if err := s.persistTaskUpdate(ctx, task, startedAt); err != nil {
		return SandboxTask{}, err
	}

	result, execErr := s.sandbox.ExecuteTask(ctx, task)
	completedTask := task
	completedAt := result.CompletedAt
	if completedAt.IsZero() {
		completedAt = s.clock.Now()
	}
	completedTask.ResultSummary = strings.TrimSpace(result.Summary)
	completedTask.ErrorMessage = strings.TrimSpace(result.ErrorText)
	completedTask.Artifacts = cloneArtifacts(result.Artifacts)
	completedTask.Metadata = cloneAnyMap(result.Metadata)
	completedTask.CompletedAt = timePtr(completedAt)
	if execErr != nil || completedTask.ErrorMessage != "" {
		completedTask.Status = SandboxTaskStatusFailed
		if completedTask.ErrorMessage == "" && execErr != nil {
			completedTask.ErrorMessage = execErr.Error()
		}
	} else {
		completedTask.Status = SandboxTaskStatusSucceeded
	}

	if err := s.persistTaskUpdate(ctx, completedTask, completedAt); err != nil {
		recoveryErr := s.tasks.SaveTask(ctx, completedTask)
		if recoveryErr != nil {
			return completedTask, errors.Join(
				fmt.Errorf("persist terminal sandbox task state after execution: %w", err),
				fmt.Errorf("recover terminal sandbox task state outside transaction: %w", recoveryErr),
			)
		}
		return completedTask, fmt.Errorf("persist terminal sandbox task event after execution: %w (task state recovered without event)", err)
	}

	return completedTask, nil
}

func (s *CoordinationService) GetSandboxTask(ctx context.Context, query GetSandboxTaskQuery) (SandboxTask, error) {
	if err := query.Validate(); err != nil {
		return SandboxTask{}, err
	}
	return s.tasks.GetTaskByID(ctx, query.TaskID)
}

func (s *CoordinationService) ListSandboxTasksBySession(ctx context.Context, query ListSandboxTasksQuery) ([]SandboxTask, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.sessions.GetByID(ctx, query.SessionID); err != nil {
		return nil, err
	}
	return s.tasks.ListTasksBySessionID(ctx, query.SessionID)
}

func (s *CoordinationService) ListAgentHandoffsBySession(ctx context.Context, query ListAgentHandoffsQuery) ([]AgentHandoff, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.sessions.GetByID(ctx, query.SessionID); err != nil {
		return nil, err
	}
	return s.tasks.ListHandoffsBySessionID(ctx, query.SessionID)
}

func (s *CoordinationService) SupportsRuntime(name string) bool {
	if s == nil || s.sandbox == nil {
		return false
	}
	return s.sandbox.SupportsRuntime(name)
}

func (s *CoordinationService) persistTaskUpdate(ctx context.Context, task SandboxTask, occurredAt time.Time) error {
	return s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.tasks.SaveTask(txCtx, task); err != nil {
			return err
		}
		return s.outbox.Add(txCtx, RecordedEvent{
			Topic:      TopicAgentTaskUpdated,
			Payload:    AgentTaskUpdatedEvent{Task: task},
			OccurredAt: occurredAt,
		})
	})
}

func (s *CoordinationService) ensureCoordinationEntities(ctx context.Context, sessionID domain.SessionID, fromAgentID, toAgentID domain.AgentID) error {
	if _, err := s.sessions.GetByID(ctx, sessionID); err != nil {
		return err
	}
	if fromAgentID != "" {
		if _, err := s.agents.GetByID(ctx, fromAgentID); err != nil {
			return err
		}
	}
	if toAgentID != "" {
		if _, err := s.agents.GetByID(ctx, toAgentID); err != nil {
			return err
		}
	}
	return nil
}

func validateTaskScope(task SandboxTask, sessionID domain.SessionID, conversationID domain.ConversationID, label string) error {
	if task.SessionID != sessionID {
		return fmt.Errorf("%w: %s %q belongs to session %q, not %q", ErrPrecondition, label, task.ID, task.SessionID, sessionID)
	}
	if task.ConversationID != conversationID {
		return fmt.Errorf("%w: %s %q belongs to conversation %q, not %q", ErrPrecondition, label, task.ID, task.ConversationID, conversationID)
	}
	return nil
}

func validateSandboxRuntimeMatch(task SandboxTask, runtime SandboxedAgentRuntime) error {
	if runtime == nil {
		return ErrDisabled
	}
	if !runtime.SupportsRuntime(task.RuntimeName) {
		return fmt.Errorf("%w: sandbox task %q targets unsupported configured runtime %q", ErrPrecondition, task.ID, task.RuntimeName)
	}
	if task.AssignedProvider != runtime.ProviderClass() {
		return fmt.Errorf("%w: sandbox task %q targets provider class %q, but configured runtime is %q", ErrPrecondition, task.ID, task.AssignedProvider, runtime.ProviderClass())
	}
	return nil
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func cloneArtifacts(artifacts []SandboxTaskArtifact) []SandboxTaskArtifact {
	return append([]SandboxTaskArtifact(nil), artifacts...)
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
