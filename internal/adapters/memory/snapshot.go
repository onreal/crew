package memory

import (
	"fmt"

	"crew/internal/application"
	"crew/internal/domain"
)

type Snapshot struct {
	Sessions  map[domain.SessionID]domain.Session                 `json:"sessions"`
	Messages  map[domain.SessionID][]domain.Message               `json:"messages"`
	Workflows map[domain.WorkflowID]domain.Workflow               `json:"workflows"`
	Agents    map[domain.AgentID]domain.Agent                     `json:"agents"`
	Tasks     map[application.AgentTaskID]application.SandboxTask `json:"tasks"`
	Handoffs  map[domain.SessionID][]application.AgentHandoff     `json:"handoffs"`
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Snapshot{
		Sessions:  cloneSessionMap(s.state.sessions),
		Messages:  cloneMessagesMap(s.state.messages),
		Workflows: cloneWorkflowMap(s.state.workflows),
		Agents:    cloneAgentMap(s.state.agents),
		Tasks:     cloneSandboxTaskMap(s.state.tasks),
		Handoffs:  cloneSandboxHandoffsMap(s.state.handoffs),
	}
}

func (s *Store) LoadSnapshot(snapshot Snapshot) error {
	nextState := newState()

	for id, session := range snapshot.Sessions {
		if err := session.Validate(); err != nil {
			return fmt.Errorf("invalid session snapshot %q: %w", id, err)
		}
		nextState.sessions[id] = session
	}

	for sessionID, messages := range snapshot.Messages {
		cloned := make([]domain.Message, len(messages))
		for i, message := range messages {
			if err := message.Validate(); err != nil {
				return fmt.Errorf("invalid message snapshot for session %q: %w", sessionID, err)
			}
			cloned[i] = cloneMessage(message)
		}
		nextState.messages[sessionID] = cloned
	}

	for id, workflow := range snapshot.Workflows {
		if err := workflow.Validate(); err != nil {
			return fmt.Errorf("invalid workflow snapshot %q: %w", id, err)
		}
		nextState.workflows[id] = cloneWorkflow(workflow)
	}

	for id, agent := range snapshot.Agents {
		if err := agent.Validate(); err != nil {
			return fmt.Errorf("invalid agent snapshot %q: %w", id, err)
		}
		nextState.agents[id] = cloneAgent(agent)
		nextState.active[id] = true
	}

	for id, task := range snapshot.Tasks {
		if err := task.Validate(); err != nil {
			return fmt.Errorf("invalid sandbox task snapshot %q: %w", id, err)
		}
		nextState.tasks[id] = cloneSandboxTask(task)
	}

	for sessionID, handoffs := range snapshot.Handoffs {
		cloned := make([]application.AgentHandoff, len(handoffs))
		for i, handoff := range handoffs {
			if err := handoff.Validate(); err != nil {
				return fmt.Errorf("invalid agent handoff snapshot for session %q: %w", sessionID, err)
			}
			cloned[i] = cloneAgentHandoff(handoff)
		}
		nextState.handoffs[sessionID] = cloned
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = nextState
	return nil
}

func (s Snapshot) Clone() Snapshot {
	return Snapshot{
		Sessions:  cloneSessionMap(s.Sessions),
		Messages:  cloneMessagesMap(s.Messages),
		Workflows: cloneWorkflowMap(s.Workflows),
		Agents:    cloneAgentMap(s.Agents),
		Tasks:     cloneSandboxTaskMap(s.Tasks),
		Handoffs:  cloneSandboxHandoffsMap(s.Handoffs),
	}
}

func (s Snapshot) MaxSessionCounter() int {
	var maxValue int
	for sessionID := range s.Sessions {
		if value, ok := parseNumericSuffix(string(sessionID), "session-"); ok && value > maxValue {
			maxValue = value
		}
	}

	return maxValue
}

func (s Snapshot) MaxMessageCounter() int {
	return maxNumericSuffixMessages(s.Messages, "message-")
}
