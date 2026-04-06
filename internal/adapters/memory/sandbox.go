package memory

import (
	"context"
	"slices"

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

	current := r.store.stateFor(ctx)
	current.tasks[task.ID] = cloneSandboxTask(task)
	return nil
}

func (r *SandboxTaskRepository) GetTaskByID(ctx context.Context, id application.AgentTaskID) (application.SandboxTask, error) {
	if _, ok := r.store.txFromContext(ctx); !ok {
		r.store.mu.RLock()
		defer r.store.mu.RUnlock()
	}

	task, exists := r.store.stateFor(ctx).tasks[id]
	if !exists {
		return application.SandboxTask{}, application.NotFoundError{Entity: "sandbox_task", ID: string(id)}
	}

	return cloneSandboxTask(task), nil
}

func (r *SandboxTaskRepository) ListTasksBySessionID(ctx context.Context, sessionID domain.SessionID) ([]application.SandboxTask, error) {
	if _, ok := r.store.txFromContext(ctx); !ok {
		r.store.mu.RLock()
		defer r.store.mu.RUnlock()
	}

	current := r.store.stateFor(ctx)
	tasks := make([]application.SandboxTask, 0)
	for _, task := range current.tasks {
		if task.SessionID == sessionID {
			tasks = append(tasks, cloneSandboxTask(task))
		}
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

	current := r.store.stateFor(ctx)
	handffs := current.handoffs[handoff.SessionID]
	for _, existing := range handffs {
		if existing.ID == handoff.ID {
			for i := range handffs {
				if handffs[i].ID == handoff.ID {
					handffs[i] = cloneAgentHandoff(handoff)
					current.handoffs[handoff.SessionID] = handffs
					return nil
				}
			}
		}
	}

	current.handoffs[handoff.SessionID] = append(current.handoffs[handoff.SessionID], cloneAgentHandoff(handoff))
	return nil
}

func (r *SandboxTaskRepository) ListHandoffsBySessionID(ctx context.Context, sessionID domain.SessionID) ([]application.AgentHandoff, error) {
	if _, ok := r.store.txFromContext(ctx); !ok {
		r.store.mu.RLock()
		defer r.store.mu.RUnlock()
	}

	handoffs := cloneAgentHandoffs(r.store.stateFor(ctx).handoffs[sessionID])
	slices.SortFunc(handoffs, func(a, b application.AgentHandoff) int {
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

	return handoffs, nil
}

func cloneSandboxTaskMap(input map[application.AgentTaskID]application.SandboxTask) map[application.AgentTaskID]application.SandboxTask {
	cloned := make(map[application.AgentTaskID]application.SandboxTask, len(input))
	for id, task := range input {
		cloned[id] = cloneSandboxTask(task)
	}
	return cloned
}

func cloneSandboxHandoffsMap(input map[domain.SessionID][]application.AgentHandoff) map[domain.SessionID][]application.AgentHandoff {
	cloned := make(map[domain.SessionID][]application.AgentHandoff, len(input))
	for sessionID, handoffs := range input {
		cloned[sessionID] = cloneAgentHandoffs(handoffs)
	}
	return cloned
}

func cloneSandboxTask(task application.SandboxTask) application.SandboxTask {
	cloned := task
	cloned.Artifacts = append([]application.SandboxTaskArtifact(nil), task.Artifacts...)
	cloned.Metadata = cloneAnyMap(task.Metadata)
	if task.StartedAt != nil {
		startedAt := *task.StartedAt
		cloned.StartedAt = &startedAt
	}
	if task.CompletedAt != nil {
		completedAt := *task.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	return cloned
}

func cloneAgentHandoff(handoff application.AgentHandoff) application.AgentHandoff {
	return handoff
}

func cloneAgentHandoffs(handoffs []application.AgentHandoff) []application.AgentHandoff {
	cloned := make([]application.AgentHandoff, len(handoffs))
	for i, handoff := range handoffs {
		cloned[i] = cloneAgentHandoff(handoff)
	}
	return cloned
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
