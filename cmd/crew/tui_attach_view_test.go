package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

func TestAttachModelViewStaysWithinConfiguredWidth(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 20
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{{
				ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
				Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
				Body: strings.Repeat("operator transcript text that should wrap cleanly ", 4), Timestamp: now,
			}},
			Stream: []runtimeadapter.StreamEntry{{
				RecordedAt: now,
				Payload: application.MessageDispatchedEvent{Message: domain.Message{
					ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
					Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
					Body: strings.Repeat("operator transcript text that should wrap cleanly ", 4),
				}},
			}},
		},
		conversations: []domain.ConversationID{"conversation-1", "conversation-2"},
	}
	model.layout()
	model.syncViewportContent(false)

	view := model.View()
	if got := maxRenderedLineWidth(view); got > model.width {
		t.Fatalf("expected rendered view width <= %d, got %d", model.width, got)
	}
	if !strings.Contains(view, "send=conversation-1") || !strings.Contains(view, "operator") {
		t.Fatalf("expected send target header and transcript to remain visible, got:\n%s", view)
	}
}

func TestAttachModelViewIsBorderlessAndOmitsArtworkChrome(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 140, 24
	model.layout()
	model.syncViewportContent(false)

	view := model.View()
	for _, token := range []string{"│", "─", "┌", "┐", "└", "┘"} {
		if strings.Contains(view, token) {
			t.Fatalf("expected borderless codex-style view without %q, got:\n%s", token, view)
		}
	}
}

func TestAttachModelEmptyStateShowsCrewArtwork(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 120, 24
	model.layout()
	model.syncViewportContent(false)

	view := model.View()
	if !strings.Contains(view, "CREW CLI") || !strings.Contains(view, "NOSTATE") {
		t.Fatalf("expected empty room artwork in view, got:\n%s", view)
	}
}

func TestAttachModelDefaultRoomScopeShowsWholeSessionTimeline(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-2", ui)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "older thread context", Timestamp: now},
				{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-2", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "current thread context", Timestamp: now.Add(time.Second)},
			},
			Stream: []runtimeadapter.StreamEntry{
				{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "older thread context"}}},
				{RecordedAt: now.Add(time.Second), Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-2", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "current thread context"}}},
			},
		},
		conversations: []domain.ConversationID{"conversation-1", "conversation-2"},
	}
	model.width, model.height = 100, 24
	model.layout()
	model.syncViewportContent(false)

	if got := model.roomConversationScope(); got != "" {
		t.Fatalf("expected unpinned attach room to use session scope, got %q", got)
	}
	if !strings.Contains(model.lastRoomContent, "older thread context") || !strings.Contains(model.lastRoomContent, "current thread context") {
		t.Fatalf("expected session timeline to include both conversations, got:\n%s", model.lastRoomContent)
	}
	if strings.Contains(model.lastRoomContent, "conversation-1") || strings.Contains(model.lastRoomContent, "conversation-2") {
		t.Fatalf("expected non-debug transcript to omit conversation ids, got:\n%s", model.lastRoomContent)
	}
	if !strings.Contains(model.renderHeader(), "scope=session") {
		t.Fatalf("expected header to show session scope, got:\n%s", model.renderHeader())
	}
}

func TestAttachModelPinnedSendTargetStillShowsWholeSessionTimeline(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1", ConversationID: "conversation-2"}, "conversation-2", ui)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "older thread context", Timestamp: now},
				{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-2", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "current thread context", Timestamp: now.Add(time.Second)},
			},
			Stream: []runtimeadapter.StreamEntry{
				{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "older thread context"}}},
				{RecordedAt: now.Add(time.Second), Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-2", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "current thread context"}}},
			},
		},
		conversations: []domain.ConversationID{"conversation-1", "conversation-2"},
	}
	model.width, model.height = 100, 24
	model.layout()
	model.syncViewportContent(false)

	if got := model.roomConversationScope(); got != "" {
		t.Fatalf("expected attach room to stay on session scope, got %q", got)
	}
	if !strings.Contains(model.lastRoomContent, "older thread context") || !strings.Contains(model.lastRoomContent, "current thread context") {
		t.Fatalf("expected session timeline to include both conversations, got:\n%s", model.lastRoomContent)
	}
	if strings.Contains(model.lastRoomContent, "conversation-1") || strings.Contains(model.lastRoomContent, "conversation-2") {
		t.Fatalf("expected non-debug transcript to omit conversation ids, got:\n%s", model.lastRoomContent)
	}
	if !strings.Contains(model.renderHeader(), "scope=session") || !strings.Contains(model.renderHeader(), "send=conversation-2") {
		t.Fatalf("expected header to show session scope and send target, got:\n%s", model.renderHeader())
	}
}

func TestAttachModelDebugTranscriptShowsMetadata(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1", Debug: true}, "conversation-1", ui)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "hello", Timestamp: now},
				{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("writer"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply", ReplyTo: "message-1", Timestamp: now.Add(time.Second)},
			},
			Stream: []runtimeadapter.StreamEntry{
				{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "hello"}}},
				{RecordedAt: now.Add(time.Second), Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("writer"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply", ReplyTo: "message-1"}}},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	rendered := model.renderConversationContent("conversation-1")
	for _, expected := range []string{"conversation-1", "message-1", now.Format("15:04:05")} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected debug transcript metadata %q, got:\n%s", expected, rendered)
		}
	}
}

func TestAttachModelCompactStatusShowsTotalAndPerParticipantCounts(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 90), testAttachAgent("writer", 80)}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "hello", Timestamp: now},
				{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply one", Timestamp: now.Add(time.Second)},
				{ID: "message-3", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply two", Timestamp: now.Add(2 * time.Second)},
				{ID: "message-4", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("reviewer"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "review reply", Timestamp: now.Add(3 * time.Second)},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}
	status := model.renderCompactStatus(120)
	for _, expected := range []string{"msg", "4", "planner", "2", "reviewer", "1", "writer", "0"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("expected %q in compact status, got:\n%s", expected, status)
		}
	}
	for _, unexpected := range []string{"Runtime", "Recent Input", "Conversations"} {
		if strings.Contains(status, unexpected) {
			t.Fatalf("expected compact status to omit %q, got:\n%s", unexpected, status)
		}
	}
}

func TestAttachModelCompactStatusTruncatesNamesOnNarrowWidth(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.agents = []domain.Agent{
		testAttachAgent("planner-long-name", 100),
		testAttachAgent("reviewer-long-name", 90),
		testAttachAgent("writer-long-name", 80),
	}
	model.agents[0].Name = "planner-long-name"
	model.agents[1].Name = "reviewer-long-name"
	model.agents[2].Name = "writer-long-name"
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{Sender: domain.AgentSender("planner-long-name")},
				{Sender: domain.AgentSender("reviewer-long-name")},
				{Sender: domain.AgentSender("reviewer-long-name")},
				{Sender: domain.AgentSender("writer-long-name")},
				{Sender: domain.AgentSender("writer-long-name")},
				{Sender: domain.AgentSender("writer-long-name")},
			},
		},
	}

	status := model.renderCompactStatus(34)
	if !strings.Contains(status, "…") {
		t.Fatalf("expected compact status to truncate names on narrow widths, got:\n%s", status)
	}
	if got := lipgloss.Width(status); got > 34 {
		t.Fatalf("expected compact status width <= 34, got %d from:\n%s", got, status)
	}
	for _, expected := range []string{"1", "2", "3"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("expected message counts to remain visible, got:\n%s", status)
		}
	}
}
