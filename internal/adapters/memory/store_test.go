package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"crew/internal/application"
)

func TestOutboxFlushIsSerializedAcrossConcurrentCallers(t *testing.T) {
	store := NewStore()
	outbox := store.Outbox()

	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	if err := outbox.Add(context.Background(), application.RecordedEvent{
		Topic:      "event.one",
		Payload:    "one",
		OccurredAt: now,
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := outbox.Add(context.Background(), application.RecordedEvent{
		Topic:      "event.two",
		Payload:    "two",
		OccurredAt: now,
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	publisher := &countingPublisher{
		delay: 20 * time.Millisecond,
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := outbox.Flush(context.Background(), publisher); err != nil {
				t.Errorf("Flush() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if publisher.count("event.one") != 1 {
		t.Fatalf("expected event.one to publish once, got %d", publisher.count("event.one"))
	}

	if publisher.count("event.two") != 1 {
		t.Fatalf("expected event.two to publish once, got %d", publisher.count("event.two"))
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.state.outbox) != 0 {
		t.Fatalf("expected outbox to be empty after flush, got %d", len(store.state.outbox))
	}
}

type countingPublisher struct {
	mu        sync.Mutex
	delay     time.Duration
	published []string
}

func (p *countingPublisher) Publish(_ context.Context, topic string, event any) error {
	time.Sleep(p.delay)

	p.mu.Lock()
	defer p.mu.Unlock()
	_ = event
	p.published = append(p.published, topic)
	return nil
}

func (p *countingPublisher) count(topic string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	var total int
	for _, publishedTopic := range p.published {
		if publishedTopic == topic {
			total++
		}
	}

	return total
}
