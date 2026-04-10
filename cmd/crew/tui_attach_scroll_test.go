package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

func TestAttachModelTranscriptUsesFullWidthWithoutViewportClipping(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 72, 12
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		Messages: []domain.Message{{
			ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
			Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
			Body: strings.Repeat("wrapped transcript content ", 6), Timestamp: now,
		}},
		Stream: []runtimeadapter.StreamEntry{{
			RecordedAt: now,
			Payload: application.MessageDispatchedEvent{Message: domain.Message{
				ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
				Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
				Body: strings.Repeat("wrapped transcript content ", 6),
			}},
		}},
	}, conversations: []domain.ConversationID{"conversation-1"}}
	model.layout()
	model.syncViewportContent(false)

	if strings.Contains(model.lastRoomContent, "│") || strings.Contains(model.lastRoomContent, "─") {
		t.Fatalf("expected borderless transcript content, got:\n%s", model.lastRoomContent)
	}
	if got := maxRenderedLineWidth(model.lastRoomContent); got > model.width {
		t.Fatalf("expected transcript width <= %d, got %d", model.width, got)
	}
}

func TestAttachModelTranscriptRefreshStaysStableAcrossNoopPolls(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 92, 20
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		Messages: []domain.Message{{
			ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
			Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
			Body: strings.Repeat("steady transcript ", 12), Timestamp: now,
		}},
		Stream: []runtimeadapter.StreamEntry{{
			RecordedAt: now,
			Payload: application.MessageDispatchedEvent{Message: domain.Message{
				ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
				Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
				Body: strings.Repeat("steady transcript ", 12),
			}},
		}},
	}, conversations: []domain.ConversationID{"conversation-1"}}
	model.layout()
	model.syncViewportContent(false)
	baseContent := model.lastRoomContent

	for idx := 0; idx < 3; idx++ {
		updated, _ := model.Update(attachRoomStateMsg{state: model.room})
		model = updated.(attachModel)
		if model.lastRoomContent != baseContent {
			t.Fatalf("expected unchanged refresh to keep transcript stable on cycle %d", idx)
		}
	}
}

func TestAttachModelViewKeepsManagedChromeWithinWindowHeight(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 80, 10

	stream := make([]runtimeadapter.StreamEntry, 0, 18)
	messages := make([]domain.Message, 0, 18)
	for i := 0; i < 18; i++ {
		body := "message " + strconv.Itoa(i+1)
		message := domain.Message{
			ID: domain.MessageID("message-" + strconv.Itoa(i+1)), SessionID: "session-1", ConversationID: "conversation-1",
			Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
			Body: body, Timestamp: now.Add(time.Duration(i) * time.Second),
		}
		messages = append(messages, message)
		stream = append(stream, runtimeadapter.StreamEntry{RecordedAt: message.Timestamp, Payload: application.MessageDispatchedEvent{Message: message}})
	}
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session:  domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		Messages: messages, Stream: stream,
	}, conversations: []domain.ConversationID{"conversation-1"}}

	model.layout()
	model.syncViewportContent(false)
	view := model.View()

	if got := lipgloss.Height(view); got != model.height {
		t.Fatalf("expected managed view height %d, got %d", model.height, got)
	}
	if !strings.Contains(view, "message 18") {
		t.Fatalf("expected latest transcript history to remain visible on screen, got:\n%s", view)
	}
}

func TestAttachFooterHelpMentionsLatestHistoryInsteadOfViewportScroll(t *testing.T) {
	help := attachFooterHelpText()
	if !strings.Contains(help, "latest history stays on screen") {
		t.Fatalf("expected footer help to mention on-screen history, got %q", help)
	}
	for _, removed := range []string{"PgUp", "PgDn", "Home", "End"} {
		if strings.Contains(help, removed) {
			t.Fatalf("expected footer help to omit %q, got %q", removed, help)
		}
	}
}
