package memory

import (
	"slices"

	"crew/internal/application"
	"crew/internal/domain"
)

func cloneSessionMap(src map[domain.SessionID]domain.Session) map[domain.SessionID]domain.Session {
	dst := make(map[domain.SessionID]domain.Session, len(src))
	for key, value := range src {
		dst[key] = value
	}

	return dst
}

func cloneMessagesMap(src map[domain.SessionID][]domain.Message) map[domain.SessionID][]domain.Message {
	dst := make(map[domain.SessionID][]domain.Message, len(src))
	for key, value := range src {
		dst[key] = cloneMessages(value)
	}

	return dst
}

func cloneMessages(src []domain.Message) []domain.Message {
	cloned := make([]domain.Message, len(src))
	for i, message := range src {
		cloned[i] = cloneMessage(message)
	}

	return cloned
}

func cloneMessage(message domain.Message) domain.Message {
	message.ToAgentIDs = slices.Clone(message.ToAgentIDs)
	if message.Metadata != nil {
		metadata := make(map[string]any, len(message.Metadata))
		for key, value := range message.Metadata {
			metadata[key] = value
		}
		message.Metadata = metadata
	}

	return message
}

func cloneWorkflowMap(src map[domain.WorkflowID]domain.Workflow) map[domain.WorkflowID]domain.Workflow {
	dst := make(map[domain.WorkflowID]domain.Workflow, len(src))
	for key, workflow := range src {
		dst[key] = cloneWorkflow(workflow)
	}

	return dst
}

func cloneWorkflow(workflow domain.Workflow) domain.Workflow {
	workflow.Steps = slices.Clone(workflow.Steps)
	for i := range workflow.Steps {
		workflow.Steps[i].NextStepIDs = slices.Clone(workflow.Steps[i].NextStepIDs)
	}

	return workflow
}

func cloneAgentMap(src map[domain.AgentID]domain.Agent) map[domain.AgentID]domain.Agent {
	dst := make(map[domain.AgentID]domain.Agent, len(src))
	for key, agent := range src {
		dst[key] = cloneAgent(agent)
	}

	return dst
}

func cloneAgent(agent domain.Agent) domain.Agent {
	agent.Tools = slices.Clone(agent.Tools)
	return agent
}

func cloneRecordedEvents(events []application.RecordedEvent) []application.RecordedEvent {
	return slices.Clone(events)
}
