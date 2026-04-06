package memory

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrBusClosed = errors.New("memory event bus closed")

type Envelope struct {
	Topic       string
	Payload     any
	PublishedAt time.Time
}

type EventBus struct {
	mu          sync.RWMutex
	nextID      int
	subscribers map[string]map[int]chan Envelope
	closed      bool
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]map[int]chan Envelope),
	}
}

func (b *EventBus) Publish(ctx context.Context, topic string, event any) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}

	targets := b.collectSubscribers(topic)
	b.mu.RUnlock()

	envelope := Envelope{
		Topic:       topic,
		Payload:     event,
		PublishedAt: time.Now().UTC(),
	}

	for _, ch := range targets {
		select {
		case ch <- envelope:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (b *EventBus) Subscribe(topic string, buffer int) (int, <-chan Envelope) {
	if buffer < 1 {
		buffer = 1
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[int]chan Envelope)
	}

	b.nextID++
	ch := make(chan Envelope, buffer)
	b.subscribers[topic][b.nextID] = ch
	return b.nextID, ch
}

func (b *EventBus) Unsubscribe(topic string, id int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	topicSubscribers := b.subscribers[topic]
	if topicSubscribers == nil {
		return
	}

	ch, exists := topicSubscribers[id]
	if !exists {
		return
	}

	close(ch)
	delete(topicSubscribers, id)
	if len(topicSubscribers) == 0 {
		delete(b.subscribers, topic)
	}
}

func (b *EventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	for topic, topicSubscribers := range b.subscribers {
		for id, ch := range topicSubscribers {
			close(ch)
			delete(topicSubscribers, id)
		}
		delete(b.subscribers, topic)
	}

	b.closed = true
}

func (b *EventBus) collectSubscribers(topic string) []chan Envelope {
	var targets []chan Envelope
	for _, key := range []string{topic, "*"} {
		for _, ch := range b.subscribers[key] {
			targets = append(targets, ch)
		}
	}

	return targets
}
