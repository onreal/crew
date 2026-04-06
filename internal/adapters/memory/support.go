package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"crew/internal/domain"
)

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type SequenceIDGenerator struct {
	mu             sync.Mutex
	sessionCounter int
	messageCounter int
}

func NewSequenceIDGenerator() *SequenceIDGenerator {
	return &SequenceIDGenerator{}
}

func NewSequenceIDGeneratorWithCounters(sessionCounter, messageCounter int) *SequenceIDGenerator {
	return &SequenceIDGenerator{
		sessionCounter: sessionCounter,
		messageCounter: messageCounter,
	}
}

func (g *SequenceIDGenerator) NewSessionID(_ context.Context) (domain.SessionID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.sessionCounter++
	return domain.SessionID(fmt.Sprintf("session-%d", g.sessionCounter)), nil
}

func (g *SequenceIDGenerator) NewMessageID(_ context.Context) (domain.MessageID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.messageCounter++
	return domain.MessageID(fmt.Sprintf("message-%d", g.messageCounter)), nil
}

func maxNumericSuffixMessages(messages map[domain.SessionID][]domain.Message, prefix string) int {
	var maxValue int
	for _, sessionMessages := range messages {
		for _, message := range sessionMessages {
			if value, ok := parseNumericSuffix(string(message.ID), prefix); ok && value > maxValue {
				maxValue = value
			}
		}
	}

	return maxValue
}

func parseNumericSuffix(value, prefix string) (int, bool) {
	if !strings.HasPrefix(value, prefix) {
		return 0, false
	}

	number, err := strconv.Atoi(strings.TrimPrefix(value, prefix))
	if err != nil {
		return 0, false
	}

	return number, true
}
