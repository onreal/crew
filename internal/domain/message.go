package domain

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type MessageChannel string
type MessageKind string
type MessageSenderType string

const (
	MessageChannelDirect    MessageChannel = "direct"
	MessageChannelBroadcast MessageChannel = "broadcast"
	MessageChannelSystem    MessageChannel = "system"
	MessageChannelControl   MessageChannel = "control"
	MessageChannelUser      MessageChannel = "user"
)

const (
	MessageKindUtterance MessageKind = "utterance"
	MessageKindEvent     MessageKind = "event"
	MessageKindControl   MessageKind = "control"
	MessageKindError     MessageKind = "error"
)

const (
	MessageSenderTypeAgent  MessageSenderType = "agent"
	MessageSenderTypeUser   MessageSenderType = "user"
	MessageSenderTypeSystem MessageSenderType = "system"
)

type MessageSender struct {
	Type MessageSenderType
	ID   string
}

type Message struct {
	ID             MessageID
	SessionID      SessionID
	ConversationID ConversationID
	Sender         MessageSender
	ToAgentIDs     []AgentID
	Channel        MessageChannel
	Kind           MessageKind
	Body           string
	ReplyTo        MessageID
	Timestamp      time.Time
	Metadata       map[string]any
}

func NewMessage(message Message) (Message, error) {
	if err := message.Validate(); err != nil {
		return Message{}, err
	}

	message.ToAgentIDs = slices.Clone(message.ToAgentIDs)
	message.Metadata = cloneMetadata(message.Metadata)
	return message, nil
}

func (m Message) Validate() error {
	if err := m.ID.Validate(); err != nil {
		return err
	}

	if err := m.SessionID.Validate(); err != nil {
		return err
	}

	if err := m.ConversationID.Validate(); err != nil {
		return err
	}

	if err := m.Sender.Validate(); err != nil {
		return err
	}

	if err := m.Channel.Validate(); err != nil {
		return err
	}

	if err := m.Kind.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(m.Body) == "" {
		return fmt.Errorf("message body must not be empty")
	}

	if m.Timestamp.IsZero() {
		return fmt.Errorf("message timestamp must not be zero")
	}

	if m.ReplyTo != "" {
		if err := m.ReplyTo.Validate(); err != nil {
			return err
		}
	}

	seenRecipients := make(map[AgentID]struct{}, len(m.ToAgentIDs))
	for _, recipientID := range m.ToAgentIDs {
		if err := recipientID.Validate(); err != nil {
			return err
		}

		if _, exists := seenRecipients[recipientID]; exists {
			return fmt.Errorf("message recipients must be unique, duplicate %q", recipientID)
		}

		seenRecipients[recipientID] = struct{}{}
	}

	if m.Channel == MessageChannelDirect && len(m.ToAgentIDs) == 0 {
		return fmt.Errorf("direct messages must target at least one recipient")
	}

	switch m.Channel {
	case MessageChannelUser:
		if m.Sender.Type != MessageSenderTypeUser {
			return fmt.Errorf("user channel messages must use a user sender")
		}
	case MessageChannelSystem:
		if m.Sender.Type != MessageSenderTypeSystem {
			return fmt.Errorf("system channel messages must use a system sender")
		}
	}

	return nil
}

func (s MessageSender) Validate() error {
	if err := s.Type.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("message sender id must not be empty")
	}

	if s.Type == MessageSenderTypeAgent {
		if err := AgentID(s.ID).Validate(); err != nil {
			return fmt.Errorf("agent sender id is invalid: %w", err)
		}
	}

	return nil
}

func (t MessageSenderType) Validate() error {
	switch t {
	case MessageSenderTypeAgent, MessageSenderTypeUser, MessageSenderTypeSystem:
		return nil
	default:
		return fmt.Errorf("invalid message sender type %q", t)
	}
}

func AgentSender(id AgentID) MessageSender {
	return MessageSender{
		Type: MessageSenderTypeAgent,
		ID:   string(id),
	}
}

func UserSender(id string) MessageSender {
	return MessageSender{
		Type: MessageSenderTypeUser,
		ID:   id,
	}
}

func SystemSender(id string) MessageSender {
	return MessageSender{
		Type: MessageSenderTypeSystem,
		ID:   id,
	}
}

func (c MessageChannel) Validate() error {
	switch c {
	case MessageChannelDirect, MessageChannelBroadcast, MessageChannelSystem, MessageChannelControl, MessageChannelUser:
		return nil
	default:
		return fmt.Errorf("invalid message channel %q", c)
	}
}

func (k MessageKind) Validate() error {
	switch k {
	case MessageKindUtterance, MessageKindEvent, MessageKindControl, MessageKindError:
		return nil
	default:
		return fmt.Errorf("invalid message kind %q", k)
	}
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}

	cloned := make(map[string]any, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}

	return cloned
}
