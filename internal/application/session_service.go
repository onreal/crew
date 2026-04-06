package application

import (
	"context"
	"fmt"

	"crew/internal/domain"
)

type SessionService struct {
	sessions SessionRepository
	outbox   EventOutbox
	tx       UnitOfWork
	clock    Clock
	ids      IDGenerator
}

func NewSessionService(
	sessions SessionRepository,
	outbox EventOutbox,
	tx UnitOfWork,
	clock Clock,
	ids IDGenerator,
) *SessionService {
	return &SessionService{
		sessions: sessions,
		outbox:   outbox,
		tx:       tx,
		clock:    clock,
		ids:      ids,
	}
}

func (s *SessionService) Create(ctx context.Context, cmd CreateSessionCommand) (domain.Session, error) {
	if err := cmd.Validate(); err != nil {
		return domain.Session{}, err
	}

	sessionID, err := s.ids.NewSessionID(ctx)
	if err != nil {
		return domain.Session{}, fmt.Errorf("generate session id: %w", err)
	}

	session, err := domain.NewSessionWithActorCatalog(sessionID, cmd.Mode, cmd.ActorCatalog, s.clock.Now())
	if err != nil {
		return domain.Session{}, err
	}

	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.sessions.Save(txCtx, session); err != nil {
			return err
		}

		return s.outbox.Add(txCtx, RecordedEvent{
			Topic:      TopicSessionCreated,
			Payload:    SessionCreatedEvent{Session: session},
			OccurredAt: s.clock.Now(),
		})
	}); err != nil {
		return domain.Session{}, err
	}

	return session, nil
}

func (s *SessionService) Get(ctx context.Context, query GetSessionQuery) (domain.Session, error) {
	if err := query.Validate(); err != nil {
		return domain.Session{}, err
	}

	return s.sessions.GetByID(ctx, query.SessionID)
}

func (s *SessionService) Start(ctx context.Context, cmd SessionIDCommand) (domain.Session, error) {
	return s.transition(ctx, cmd, func(session domain.Session) (domain.Session, error) {
		return session.Start()
	})
}

func (s *SessionService) Pause(ctx context.Context, cmd SessionIDCommand) (domain.Session, error) {
	return s.transition(ctx, cmd, func(session domain.Session) (domain.Session, error) {
		return session.Pause()
	})
}

func (s *SessionService) Resume(ctx context.Context, cmd SessionIDCommand) (domain.Session, error) {
	return s.transition(ctx, cmd, func(session domain.Session) (domain.Session, error) {
		return session.Resume()
	})
}

func (s *SessionService) Stop(ctx context.Context, cmd SessionIDCommand) (domain.Session, error) {
	return s.transition(ctx, cmd, func(session domain.Session) (domain.Session, error) {
		return session.Stop()
	})
}

func (s *SessionService) Complete(ctx context.Context, cmd SessionIDCommand) (domain.Session, error) {
	return s.transition(ctx, cmd, func(session domain.Session) (domain.Session, error) {
		return session.Complete()
	})
}

func (s *SessionService) Fail(ctx context.Context, cmd SessionIDCommand) (domain.Session, error) {
	return s.transition(ctx, cmd, func(session domain.Session) (domain.Session, error) {
		return session.Fail()
	})
}

func (s *SessionService) transition(ctx context.Context, cmd SessionIDCommand, apply func(domain.Session) (domain.Session, error)) (domain.Session, error) {
	if err := cmd.Validate(); err != nil {
		return domain.Session{}, err
	}

	session, err := s.sessions.GetByID(ctx, cmd.SessionID)
	if err != nil {
		return domain.Session{}, err
	}

	session, err = apply(session)
	if err != nil {
		return domain.Session{}, fmt.Errorf("%w: %v", ErrInvalidState, err)
	}

	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.sessions.Save(txCtx, session); err != nil {
			return err
		}

		return s.outbox.Add(txCtx, RecordedEvent{
			Topic:      TopicSessionUpdated,
			Payload:    SessionUpdatedEvent{Session: session},
			OccurredAt: s.clock.Now(),
		})
	}); err != nil {
		return domain.Session{}, err
	}

	return session, nil
}
