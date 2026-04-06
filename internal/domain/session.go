package domain

import (
	"fmt"
	"strings"
	"time"
)

type SessionMode string
type SessionStatus string

const (
	SessionModeFree       SessionMode = "free"
	SessionModeSequential SessionMode = "sequential"
)

const (
	SessionStatusPending   SessionStatus = "pending"
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusPaused    SessionStatus = "paused"
	SessionStatusStopped   SessionStatus = "stopped"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
)

type Session struct {
	ID           SessionID
	Mode         SessionMode
	Status       SessionStatus
	ActorCatalog string
	CreatedAt    time.Time
}

func NewSession(id SessionID, mode SessionMode, createdAt time.Time) (Session, error) {
	return NewSessionWithActorCatalog(id, mode, "", createdAt)
}

func NewSessionWithActorCatalog(id SessionID, mode SessionMode, actorCatalog string, createdAt time.Time) (Session, error) {
	session := Session{
		ID:           id,
		Mode:         mode,
		Status:       SessionStatusPending,
		ActorCatalog: actorCatalog,
		CreatedAt:    createdAt,
	}

	if err := session.Validate(); err != nil {
		return Session{}, err
	}

	return session, nil
}

func (s Session) Validate() error {
	if err := s.ID.Validate(); err != nil {
		return err
	}

	if err := s.Mode.Validate(); err != nil {
		return err
	}

	if err := s.Status.Validate(); err != nil {
		return err
	}

	if s.ActorCatalog != "" && s.ActorCatalog != strings.TrimSpace(s.ActorCatalog) {
		return fmt.Errorf("session actor catalog must not contain surrounding whitespace")
	}

	if s.CreatedAt.IsZero() {
		return fmt.Errorf("session created_at must not be zero")
	}

	return nil
}

func (s Session) Start() (Session, error) {
	return s.transition(SessionStatusRunning, SessionStatusPending)
}

func (s Session) Pause() (Session, error) {
	return s.transition(SessionStatusPaused, SessionStatusRunning)
}

func (s Session) Resume() (Session, error) {
	return s.transition(SessionStatusRunning, SessionStatusPaused)
}

func (s Session) Stop() (Session, error) {
	return s.transition(SessionStatusStopped, SessionStatusPending, SessionStatusRunning, SessionStatusPaused)
}

func (s Session) Complete() (Session, error) {
	return s.transition(SessionStatusCompleted, SessionStatusRunning, SessionStatusPaused)
}

func (s Session) Fail() (Session, error) {
	return s.transition(SessionStatusFailed, SessionStatusPending, SessionStatusRunning, SessionStatusPaused)
}

func (s Session) transition(next SessionStatus, allowed ...SessionStatus) (Session, error) {
	if err := s.Validate(); err != nil {
		return Session{}, err
	}

	for _, status := range allowed {
		if s.Status == status {
			s.Status = next
			return s, nil
		}
	}

	return Session{}, fmt.Errorf("invalid session transition from %q to %q", s.Status, next)
}

func (m SessionMode) Validate() error {
	switch m {
	case SessionModeFree, SessionModeSequential:
		return nil
	default:
		return fmt.Errorf("invalid session mode %q", m)
	}
}

func (s SessionStatus) Validate() error {
	switch s {
	case SessionStatusPending, SessionStatusRunning, SessionStatusPaused, SessionStatusStopped, SessionStatusCompleted, SessionStatusFailed:
		return nil
	default:
		return fmt.Errorf("invalid session status %q", s)
	}
}
