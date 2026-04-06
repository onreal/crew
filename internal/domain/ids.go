package domain

import (
	"fmt"
	"strings"
)

type AgentID string
type SessionID string
type ConversationID string
type MessageID string
type WorkflowID string
type WorkflowStepID string

func (id AgentID) Validate() error {
	return validateIdentifier("agent_id", string(id))
}

func (id SessionID) Validate() error {
	return validateIdentifier("session_id", string(id))
}

func (id ConversationID) Validate() error {
	return validateIdentifier("conversation_id", string(id))
}

func (id MessageID) Validate() error {
	return validateIdentifier("message_id", string(id))
}

func (id WorkflowID) Validate() error {
	return validateIdentifier("workflow_id", string(id))
}

func (id WorkflowStepID) Validate() error {
	return validateIdentifier("workflow_step_id", string(id))
}

func validateIdentifier(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}

	return nil
}
