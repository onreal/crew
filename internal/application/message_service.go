package application

import (
	"context"
	"fmt"
	"slices"

	"crew/internal/domain"
)

type MessageService struct {
	sessions SessionRepository
	messages MessageRepository
	agents   AgentRepository
	vector   VectorAdmin
	outbox   EventOutbox
	tx       UnitOfWork
	clock    Clock
	ids      IDGenerator
}

func NewMessageService(
	sessions SessionRepository,
	messages MessageRepository,
	agents AgentRepository,
	vector VectorAdmin,
	outbox EventOutbox,
	tx UnitOfWork,
	clock Clock,
	ids IDGenerator,
) *MessageService {
	return &MessageService{
		sessions: sessions,
		messages: messages,
		agents:   agents,
		vector:   vector,
		outbox:   outbox,
		tx:       tx,
		clock:    clock,
		ids:      ids,
	}
}

func (s *MessageService) Dispatch(ctx context.Context, cmd DispatchMessageCommand) (domain.Message, error) {
	if err := cmd.Validate(); err != nil {
		return domain.Message{}, err
	}

	session, err := s.sessions.GetByID(ctx, cmd.SessionID)
	if err != nil {
		return domain.Message{}, err
	}

	if session.Status != domain.SessionStatusRunning {
		return domain.Message{}, fmt.Errorf("%w: session %q is %q, must be running to dispatch messages", ErrInvalidState, session.ID, session.Status)
	}

	if err := s.validateActors(ctx, cmd.Sender, cmd.ToAgentIDs); err != nil {
		return domain.Message{}, err
	}

	messageID, err := s.ids.NewMessageID(ctx)
	if err != nil {
		return domain.Message{}, fmt.Errorf("generate message id: %w", err)
	}

	message, err := domain.NewMessage(domain.Message{
		ID:             messageID,
		SessionID:      cmd.SessionID,
		ConversationID: cmd.ConversationID,
		Sender:         cmd.Sender,
		ToAgentIDs:     slices.Clone(cmd.ToAgentIDs),
		Channel:        cmd.Channel,
		Kind:           cmd.Kind,
		Body:           cmd.Body,
		ReplyTo:        cmd.ReplyTo,
		Timestamp:      s.clock.Now(),
		Metadata:       cloneMetadata(cmd.Metadata),
	})
	if err != nil {
		return domain.Message{}, err
	}

	policy := domain.DefaultConversationPolicy()
	if cmd.Policy != nil {
		policy = *cmd.Policy
	}

	if err := policy.ValidateMessage(message); err != nil {
		return domain.Message{}, err
	}

	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.messages.Save(txCtx, message); err != nil {
			return err
		}
		if s.vector != nil {
			if err := s.vector.MarkSessionStale(txCtx, message.SessionID, message.Timestamp); err != nil {
				return err
			}
		}

		return s.outbox.Add(txCtx, RecordedEvent{
			Topic:      TopicMessageDispatched,
			Payload:    MessageDispatchedEvent{Message: message},
			OccurredAt: s.clock.Now(),
		})
	}); err != nil {
		return domain.Message{}, err
	}

	return message, nil
}

func (s *MessageService) ListBySession(ctx context.Context, query ListSessionMessagesQuery) ([]domain.Message, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}

	return s.messages.ListBySessionID(ctx, query.SessionID)
}

func (s *MessageService) validateActors(ctx context.Context, sender domain.MessageSender, recipients []domain.AgentID) error {
	var senderAgent domain.Agent
	if sender.Type == domain.MessageSenderTypeAgent {
		agent, err := s.agents.GetByID(ctx, domain.AgentID(sender.ID))
		if err != nil {
			return err
		}
		senderAgent = agent
	}

	for _, recipientID := range recipients {
		if _, err := s.agents.GetByID(ctx, recipientID); err != nil {
			return err
		}
		if sender.Type == domain.MessageSenderTypeAgent && !senderAgent.AllowsHandoffTo(recipientID) {
			return fmt.Errorf("%w: agent %s is not allowed to hand off to %s", ErrPrecondition, sender.ID, recipientID)
		}
	}

	return nil
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
