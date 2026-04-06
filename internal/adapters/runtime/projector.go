package runtime

import (
	"slices"
	"sync"
	"time"

	"crew/internal/application"
	"crew/internal/domain"
)

type StreamEntry struct {
	Topic      string
	RecordedAt time.Time
	Payload    any
}

type StreamProjector struct {
	mu        sync.RWMutex
	bySession map[domain.SessionID][]StreamEntry
}

func NewStreamProjector() *StreamProjector {
	return &StreamProjector{
		bySession: make(map[domain.SessionID][]StreamEntry),
	}
}

func (p *StreamProjector) Apply(event application.RecordedEvent) {
	sessionID, ok := sessionIDFromPayload(event.Payload)
	if !ok {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.bySession[sessionID] = append(p.bySession[sessionID], StreamEntry{
		Topic:      event.Topic,
		RecordedAt: event.OccurredAt,
		Payload:    event.Payload,
	})
}

func (p *StreamProjector) SessionStream(sessionID domain.SessionID) []StreamEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return slices.Clone(p.bySession[sessionID])
}

func (p *StreamProjector) Snapshot() map[domain.SessionID][]StreamEntry {
	p.mu.RLock()
	defer p.mu.RUnlock()

	snapshot := make(map[domain.SessionID][]StreamEntry, len(p.bySession))
	for sessionID, entries := range p.bySession {
		snapshot[sessionID] = slices.Clone(entries)
	}

	return snapshot
}

func (p *StreamProjector) LoadSnapshot(snapshot map[domain.SessionID][]StreamEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.bySession = make(map[domain.SessionID][]StreamEntry, len(snapshot))
	for sessionID, entries := range snapshot {
		p.bySession[sessionID] = slices.Clone(entries)
	}
}

func sessionIDFromPayload(payload any) (domain.SessionID, bool) {
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
