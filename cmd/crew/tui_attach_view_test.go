package main

import (
	"context"
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

func TestAttachModelHeaderAndInputHeightsStayFixed(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 30
	model.layout()
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
	}, conversations: []domain.ConversationID{"conversation-1"}}
	model.syncViewportContent(true)

	baseHeaderHeight := lipgloss.Height(model.renderHeader())
	baseInputHeight := lipgloss.Height(model.renderInput())
	if baseHeaderHeight != 3 {
		t.Fatalf("expected fixed header height 3, got %d", baseHeaderHeight)
	}
	if baseInputHeight < 4 {
		t.Fatalf("expected input area to occupy at least 4 lines, got %d", baseInputHeight)
	}
	if got := lipgloss.Height(model.View()); got != model.height {
		t.Fatalf("expected base view height %d, got %d", model.height, got)
	}

	model.status = strings.Repeat("very long status ", 20)
	model.pendingAgentStates = map[domain.AgentID]string{"planner": "thinking", "reviewer": "queued", "writer": "queued"}
	model.input.SetValue(strings.Repeat("typing into the input should not move the board ", 12))
	model.layout()
	model.syncViewportContent(true)

	if got := lipgloss.Height(model.renderHeader()); got != 3 {
		t.Fatalf("expected fixed header height after long status, got %d", got)
	}
	if got := lipgloss.Height(model.renderInput()); got != baseInputHeight {
		t.Fatalf("expected fixed input render height %d after long input, got %d", baseInputHeight, got)
	}
	if got := lipgloss.Height(model.View()); got != model.height {
		t.Fatalf("expected rendered view height %d after long status/input, got %d", model.height, got)
	}
	if got := maxRenderedLineWidth(model.View()); got > model.width {
		t.Fatalf("expected rendered view width <= %d, got %d", model.width, got)
	}
}

func TestAttachModelStickyBottomTogglesWithScrollKeys(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 30
	model.layout()
	model.viewport.SetContent(strings.Repeat("line\n", 100))
	model.viewport.GotoBottom()

	if !model.stickyBottom {
		t.Fatal("expected sticky bottom enabled by default")
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(attachModel)
	if model.stickyBottom || model.viewport.YOffset == 0 {
		t.Fatal("expected sticky bottom disabled and offset above top after pgup")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	model = updated.(attachModel)
	if !model.stickyBottom || !model.viewport.AtBottom() {
		t.Fatalf("expected viewport to return to bottom, offset=%d max=%d", model.viewport.YOffset, model.viewport.TotalLineCount()-model.viewport.Height)
	}
}

func TestAttachModelViewFitsWindowWithCompactStatusAndWrappedMessages(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 132, 28
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{{
				ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
				Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
				Body: strings.Repeat("operator text that should wrap cleanly across the room pane and stay visible inside the viewport ", 3), Timestamp: now,
			}},
			Stream: []runtimeadapter.StreamEntry{{
				RecordedAt: now,
				Payload: application.MessageDispatchedEvent{Message: domain.Message{
					ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
					Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
					Body: strings.Repeat("operator text that should wrap cleanly across the room pane and stay visible inside the viewport ", 3),
				}},
			}},
		},
		conversations: []domain.ConversationID{"conversation-1", "conversation-2"},
	}
	model.layout()
	model.syncViewportContent(true)

	view := model.View()
	if got := lipgloss.Height(view); got != model.height {
		t.Fatalf("expected rendered view height %d, got %d", model.height, got)
	}
	if got := maxRenderedLineWidth(view); got > model.width {
		t.Fatalf("expected rendered view width <= %d, got %d", model.width, got)
	}
	if !strings.Contains(view, "conversation-1") {
		t.Fatalf("expected active conversation to remain visible, got:\n%s", view)
	}
	if !strings.Contains(model.renderInput(), "NOSTATE") || !strings.Contains(model.renderInput(), "CREW CLI") {
		t.Fatalf("expected wide layout to render artwork beside input, got:\n%s", model.renderInput())
	}
	if strings.Contains(model.renderBody(), "NOSTATE") || strings.Contains(model.renderBody(), "CREW CLI") {
		t.Fatalf("expected artwork to stay out of the chat body, got:\n%s", model.renderBody())
	}
	if model.layoutMainWidth != model.width {
		t.Fatalf("expected chat layout width to remain full screen, got main=%d total=%d", model.layoutMainWidth, model.width)
	}
	if model.layoutSidebarWidth != 0 {
		t.Fatalf("expected no right sidebar width reservation, got %d", model.layoutSidebarWidth)
	}
	if model.layoutArtworkWidth == 0 {
		t.Fatal("expected wide layout to reserve artwork width")
	}
}

func TestAttachModelNarrowWindowOmitsArtworkPanel(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 24
	model.layout()
	if model.layoutArtworkWidth != 0 {
		t.Fatalf("expected no artwork width on narrow layout, got %d", model.layoutArtworkWidth)
	}
	if strings.Contains(model.renderInput(), "NOSTATE") || strings.Contains(model.renderInput(), "CREW CLI") {
		t.Fatalf("expected narrow layout to omit artwork panel, got:\n%s", model.renderInput())
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
	model.syncViewportContent(true)

	if got := model.roomConversationScope(); got != "" {
		t.Fatalf("expected unpinned attach room to use session scope, got %q", got)
	}
	if !strings.Contains(model.lastViewportContent, "conversation-1") || !strings.Contains(model.lastViewportContent, "conversation-2") {
		t.Fatalf("expected session timeline to include both conversation labels, got:\n%s", model.lastViewportContent)
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
	model.syncViewportContent(true)

	if got := model.roomConversationScope(); got != "" {
		t.Fatalf("expected attach room to stay on session scope, got %q", got)
	}
	if !strings.Contains(model.lastViewportContent, "conversation-1") || !strings.Contains(model.lastViewportContent, "conversation-2") {
		t.Fatalf("expected session timeline to include both conversations, got:\n%s", model.lastViewportContent)
	}
	if !strings.Contains(model.renderHeader(), "scope=session") || !strings.Contains(model.renderHeader(), "send=conversation-2") {
		t.Fatalf("expected header to show session scope and send target, got:\n%s", model.renderHeader())
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
