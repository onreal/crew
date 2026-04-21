package main

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestAttachModelViewLeavesTranscriptOutOfManagedBody(t *testing.T) {
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

	if got := lipgloss.Height(view); got > model.height {
		t.Fatalf("expected managed view to remain within terminal height, got %d", got)
	}
	if !strings.Contains(view, "message 18") {
		t.Fatalf("expected latest transcript history to remain visible in the managed body, got:\n%s", view)
	}
}

func TestAttachModelViewKeepsPromptAnchoredWhenReasoningGrows(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1", Reasoning: true}, "conversation-1", ui)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 90),
		testAttachAgent("writer", 80),
	}
	model.width, model.height = 80, 10
	model.layout()

	model.progressByAgent["planner"] = application.TransientProgressEvent{
		AgentID: "planner",
		Kind:    "reasoning",
		Text:    strings.Repeat("planner reasoning line ", 8),
	}
	model.progressByAgent["reviewer"] = application.TransientProgressEvent{
		AgentID: "reviewer",
		Kind:    "reasoning",
		Text:    strings.Repeat("reviewer reasoning line ", 8),
	}
	model.progressByAgent["writer"] = application.TransientProgressEvent{
		AgentID: "writer",
		Kind:    "reasoning",
		Text:    strings.Repeat("writer reasoning line ", 8),
	}

	view := model.View()
	if got := lipgloss.Height(view); got > model.height {
		t.Fatalf("expected prompt surface to stay anchored within terminal height, got %d\n%s", got, view)
	}
	if !strings.Contains(view, "Type a message or /help") {
		t.Fatalf("expected compose area to remain visible with active reasoning, got:\n%s", view)
	}
}

func TestAttachModelBodyViewportTracksLayoutSize(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 96, 18
	model.layout()
	model.syncViewportContent(false)

	if model.bodyViewport.Width != model.layoutMainWidth {
		t.Fatalf("expected body viewport width %d, got %d", model.layoutMainWidth, model.bodyViewport.Width)
	}
	if model.bodyViewport.Height != model.layoutBodyHeight {
		t.Fatalf("expected body viewport height %d, got %d", model.layoutBodyHeight, model.bodyViewport.Height)
	}
}

func TestAttachModelWindowResizeUpdatesBodyViewport(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		Messages: []domain.Message{{
			ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
			Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
			Body: strings.Repeat("resized transcript content ", 8), Timestamp: now,
		}},
		Stream: []runtimeadapter.StreamEntry{{
			RecordedAt: now,
			Payload: application.MessageDispatchedEvent{Message: domain.Message{
				ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
				Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
				Body: strings.Repeat("resized transcript content ", 8),
			}},
		}},
	}, conversations: []domain.ConversationID{"conversation-1"}}
	model.width, model.height = 96, 18
	model.layout()
	model.syncViewportContent(false)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 54, Height: 12})
	next := updated.(attachModel)

	if next.bodyViewport.Width != next.layoutMainWidth {
		t.Fatalf("expected resized body viewport width %d, got %d", next.layoutMainWidth, next.bodyViewport.Width)
	}
	if next.bodyViewport.Height != next.layoutBodyHeight {
		t.Fatalf("expected resized body viewport height %d, got %d", next.layoutBodyHeight, next.bodyViewport.Height)
	}
	if got := maxRenderedLineWidth(next.bodyViewport.View()); got > next.bodyViewport.Width {
		t.Fatalf("expected viewport body width <= %d, got %d", next.bodyViewport.Width, got)
	}
	if !strings.Contains(next.View(), "resized transcript content") {
		t.Fatalf("expected resized view to preserve transcript content, got:\n%s", next.View())
	}
}

func TestAttachFooterHelpMentionsLatestHistoryInsteadOfViewportScroll(t *testing.T) {
	help := attachFooterHelpText()
	if !strings.Contains(help, "latest history stays on screen") {
		t.Fatalf("expected footer help to mention in-room history, got %q", help)
	}
	for _, removed := range []string{"PgUp", "PgDn", "Home", "End"} {
		if strings.Contains(help, removed) {
			t.Fatalf("expected footer help to omit %q, got %q", removed, help)
		}
	}
}
