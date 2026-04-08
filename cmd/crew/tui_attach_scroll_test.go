package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

func TestAttachModelViewportUsesInnerPaneDimensions(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 24
	model.layout()

	wantWidth := max(model.layoutRoomWidth-model.styles.room.GetHorizontalFrameSize(), 1)
	wantHeight := max(model.layoutRoomHeight-model.styles.room.GetVerticalFrameSize(), 1)
	if model.viewport.Width != wantWidth {
		t.Fatalf("expected viewport width %d, got %d", wantWidth, model.viewport.Width)
	}
	if model.viewport.Height != wantHeight {
		t.Fatalf("expected viewport height %d, got %d", wantHeight, model.viewport.Height)
	}
}

func TestAttachModelPinnedBottomKeepsLastMessageVisibleAcrossRefreshAndTyping(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 92, 20

	stream := make([]runtimeadapter.StreamEntry, 0, 24)
	messages := make([]domain.Message, 0, 24)
	for i := 0; i < 24; i++ {
		body := strings.Repeat("wrapped room content ", 3)
		if i == 23 {
			body = "final marker should stay visible at bottom"
		}
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
	model.syncViewportContent(true)
	if !strings.Contains(model.View(), "final marker should stay visible at bottom") {
		t.Fatalf("expected pinned bottom view to include final message, got:\n%s", model.View())
	}
	baseOffset := model.viewport.YOffset
	model.input.SetValue(strings.Repeat("typing should not move the last visible message ", 10))
	updated, _ := model.Update(attachRoomStateMsg{state: model.room})
	model = updated.(attachModel)

	if model.viewport.YOffset != baseOffset {
		t.Fatalf("expected bottom-pinned offset %d after refresh, got %d", baseOffset, model.viewport.YOffset)
	}
	if !strings.Contains(model.View(), "final marker should stay visible at bottom") {
		t.Fatalf("expected final message to remain visible after refresh and typing, got:\n%s", model.View())
	}
}

func TestAttachModelPinnedBottomSkipsUnchangedViewportRefresh(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 92, 20
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session:  domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		Messages: []domain.Message{{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: strings.Repeat("wrapped message ", 20), Timestamp: now}},
		Stream:   []runtimeadapter.StreamEntry{{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: strings.Repeat("wrapped message ", 20)}}}},
	}, conversations: []domain.ConversationID{"conversation-1"}}
	model.layout()
	model.syncViewportContent(true)
	baseOffset := model.viewport.YOffset
	baseContent := model.lastViewportContent
	model.input.SetValue(strings.Repeat("typing while room polls should not move the chatboard ", 6))

	for idx := 0; idx < 5; idx++ {
		updated, _ := model.Update(attachRoomStateMsg{state: model.room})
		model = updated.(attachModel)
		if model.viewport.YOffset != baseOffset {
			t.Fatalf("expected unchanged refresh to keep offset %d, got %d on cycle %d", baseOffset, model.viewport.YOffset, idx)
		}
		if model.lastViewportContent != baseContent {
			t.Fatalf("expected unchanged refresh to keep cached viewport content stable on cycle %d", idx)
		}
	}
}

func TestAttachModelUsesCatalogColorThenSessionFallback(t *testing.T) {
	setAgentsDirResolverForTest(t, testAgentsCatalogDir(t))

	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	if got := model.lookupAgentColor("planner"); got != "#fb7185" {
		t.Fatalf("expected planner color from agents_test catalog, got %q", got)
	}
	fallback := model.lookupAgentColor("custom-agent")
	if fallback == "" {
		t.Fatal("expected generated fallback color for unknown participant")
	}
	if again := model.lookupAgentColor("custom-agent"); again != fallback {
		t.Fatalf("expected fallback color to stay stable within the session, got %q then %q", fallback, again)
	}
}

func TestAttachFooterHelpMentionsTerminalSelection(t *testing.T) {
	if !strings.Contains(attachFooterHelpText(), "terminal mouse selection enabled") {
		t.Fatalf("expected footer help to mention terminal selection, got %q", attachFooterHelpText())
	}
}
