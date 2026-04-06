package sqlite

import (
	"encoding/json"
	"fmt"

	"crew/internal/application"
	"crew/internal/domain"
)

func decodeRecordedEvent(topic string, occurredAt string, payloadJSON []byte) (application.RecordedEvent, error) {
	recordedAt, err := parseTimestamp(occurredAt)
	if err != nil {
		return application.RecordedEvent{}, err
	}

	payload, err := decodeEventPayload(topic, payloadJSON)
	if err != nil {
		return application.RecordedEvent{}, err
	}

	return application.RecordedEvent{
		Topic:      topic,
		Payload:    payload,
		OccurredAt: recordedAt,
	}, nil
}

func decodeEventPayload(topic string, payloadJSON []byte) (any, error) {
	switch topic {
	case application.TopicSessionCreated:
		var event application.SessionCreatedEvent
		if err := json.Unmarshal(payloadJSON, &event); err != nil {
			return nil, fmt.Errorf("decode session.created payload: %w", err)
		}
		if err := event.Session.Validate(); err != nil {
			return nil, fmt.Errorf("validate session.created payload: %w", err)
		}
		return event, nil
	case application.TopicSessionUpdated:
		var event application.SessionUpdatedEvent
		if err := json.Unmarshal(payloadJSON, &event); err != nil {
			return nil, fmt.Errorf("decode session.updated payload: %w", err)
		}
		if err := event.Session.Validate(); err != nil {
			return nil, fmt.Errorf("validate session.updated payload: %w", err)
		}
		return event, nil
	case application.TopicMessageDispatched:
		var event application.MessageDispatchedEvent
		if err := json.Unmarshal(payloadJSON, &event); err != nil {
			return nil, fmt.Errorf("decode message.dispatched payload: %w", err)
		}
		if err := event.Message.Validate(); err != nil {
			return nil, fmt.Errorf("validate message.dispatched payload: %w", err)
		}
		return event, nil
	case application.TopicAgentTaskCreated:
		var event application.AgentTaskCreatedEvent
		if err := json.Unmarshal(payloadJSON, &event); err != nil {
			return nil, fmt.Errorf("decode agent_task.created payload: %w", err)
		}
		if err := event.Task.Validate(); err != nil {
			return nil, fmt.Errorf("validate agent_task.created payload: %w", err)
		}
		return event, nil
	case application.TopicAgentTaskUpdated:
		var event application.AgentTaskUpdatedEvent
		if err := json.Unmarshal(payloadJSON, &event); err != nil {
			return nil, fmt.Errorf("decode agent_task.updated payload: %w", err)
		}
		if err := event.Task.Validate(); err != nil {
			return nil, fmt.Errorf("validate agent_task.updated payload: %w", err)
		}
		return event, nil
	case application.TopicAgentHandoffCreated:
		var event application.AgentHandoffCreatedEvent
		if err := json.Unmarshal(payloadJSON, &event); err != nil {
			return nil, fmt.Errorf("decode agent_handoff.created payload: %w", err)
		}
		if err := event.Handoff.Validate(); err != nil {
			return nil, fmt.Errorf("validate agent_handoff.created payload: %w", err)
		}
		return event, nil
	case application.TopicWorkflowRegistered:
		var event application.WorkflowRegisteredEvent
		if err := json.Unmarshal(payloadJSON, &event); err != nil {
			return nil, fmt.Errorf("decode workflow.registered payload: %w", err)
		}
		if err := event.Workflow.Validate(); err != nil {
			return nil, fmt.Errorf("validate workflow.registered payload: %w", err)
		}
		return event, nil
	default:
		return nil, fmt.Errorf("unsupported recorded event topic %q", topic)
	}
}

func eventSessionID(payload any) (domain.SessionID, bool) {
	switch event := payload.(type) {
	case application.SessionCreatedEvent:
		return event.Session.ID, true
	case application.SessionUpdatedEvent:
		return event.Session.ID, true
	case application.MessageDispatchedEvent:
		return event.Message.SessionID, true
	case application.AgentTaskCreatedEvent:
		return event.Task.SessionID, true
	case application.AgentTaskUpdatedEvent:
		return event.Task.SessionID, true
	case application.AgentHandoffCreatedEvent:
		return event.Handoff.SessionID, true
	default:
		return "", false
	}
}
