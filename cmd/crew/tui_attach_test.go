package main

import (
	"context"
	"errors"
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

func TestAttachModelSubmitInputPinsConversationAndShowsPendingActivity(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{
			SessionID:     domain.SessionID("session-1"),
			AutoSteps:     3,
			Orchestration: application.OrchestrationModeRoundRobin,
		},
		domain.ConversationID("conversation-2"),
		ui,
	)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 100),
		testAttachAgent("writer", 100),
	}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: []domain.Message{
				{
					ID:             domain.MessageID("message-1"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.UserSender("operator"),
					Channel:        domain.MessageChannelUser,
					Kind:           domain.MessageKindUtterance,
					Body:           "older conversation",
					Timestamp:      time.Now().Add(-time.Minute).UTC(),
				},
			},
		},
		conversations: []domain.ConversationID{"conversation-1", "conversation-2"},
	}
	model.selectedConvID = domain.ConversationID("conversation-1")
	model.width = 120
	model.height = 30
	model.layout()

	model.submitInput("hello room")

	if model.selectedConvID != domain.ConversationID("conversation-2") {
		t.Fatalf("expected selected conversation to switch to send conversation, got %q", model.selectedConvID)
	}
	if len(model.optimistic) != 1 {
		t.Fatalf("expected optimistic message to be appended, got %d", len(model.optimistic))
	}
	if len(model.pendingAgentStates) == 0 {
		t.Fatal("expected pending agent states to be populated immediately")
	}

	rendered := model.renderConversationContent(domain.ConversationID("conversation-2"))
	if !strings.Contains(rendered, "hello room") {
		t.Fatalf("expected optimistic operator message in rendered content, got:\n%s", rendered)
	}
	header := model.renderHeader()
	if !strings.Contains(header, "thinking") {
		t.Fatalf("expected pending agent activity in header, got:\n%s", header)
	}
}

func TestAttachModelSubmitInputUsesDeferredDispatch(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{
			SessionID:     domain.SessionID("session-1"),
			AutoSteps:     2,
			Orchestration: application.OrchestrationModeRoundRobin,
		},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 120
	model.height = 30
	model.layout()

	cmd := model.submitInput("hello")
	msg := cmd()
	dispatchMsg, ok := msg.(attachBeginDispatchMsg)
	if !ok {
		t.Fatalf("expected deferred dispatch msg, got %T", msg)
	}
	if dispatchMsg.request.Body != "hello" || dispatchMsg.autoSteps != 2 {
		t.Fatalf("unexpected deferred dispatch payload: %#v", dispatchMsg)
	}
	if !model.stickyBottom {
		t.Fatal("expected submit input to keep room pinned to bottom")
	}
}

func TestAttachModelRenderConversationShowsReplySummary(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: []domain.Message{
				{
					ID:             domain.MessageID("message-1"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.UserSender("operator"),
					Channel:        domain.MessageChannelUser,
					Kind:           domain.MessageKindUtterance,
					Body:           "hello there with context",
					Timestamp:      now,
				},
				{
					ID:             domain.MessageID("message-2"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.AgentSender("planner"),
					Channel:        domain.MessageChannelBroadcast,
					Kind:           domain.MessageKindUtterance,
					Body:           "reply body",
					ReplyTo:        domain.MessageID("message-1"),
					Timestamp:      now.Add(time.Second),
				},
			},
			Stream: []runtimeadapter.StreamEntry{
				{
					RecordedAt: now,
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-1"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-1"),
							Sender:         domain.UserSender("operator"),
							Channel:        domain.MessageChannelUser,
							Kind:           domain.MessageKindUtterance,
							Body:           "hello there with context",
						},
					},
				},
				{
					RecordedAt: now.Add(time.Second),
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-2"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-1"),
							Sender:         domain.AgentSender("planner"),
							Channel:        domain.MessageChannelBroadcast,
							Kind:           domain.MessageKindUtterance,
							Body:           "reply body",
							ReplyTo:        domain.MessageID("message-1"),
						},
					},
				},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	rendered := model.renderConversationContent(domain.ConversationID("conversation-1"))
	if !strings.Contains(rendered, "in reply to operator: hello there with con") && !strings.Contains(rendered, "in reply to operator: hello there with context") {
		t.Fatalf("expected reply summary in rendered content, got:\n%s", rendered)
	}
}

func TestAttachModelStepProgressClearsPendingBeforeRefresh(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{
			SessionID:     domain.SessionID("session-1"),
			AutoSteps:     3,
			Orchestration: application.OrchestrationModeRoundRobin,
		},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 100),
		testAttachAgent("writer", 100),
	}
	model.width = 120
	model.height = 30
	model.layout()
	model.pendingAgentStates = map[domain.AgentID]string{
		"reviewer": "thinking",
		"writer":   "queued",
	}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: []domain.Message{
				{
					ID:             domain.MessageID("message-1"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.UserSender("operator"),
					Channel:        domain.MessageChannelUser,
					Kind:           domain.MessageKindUtterance,
					Body:           "hello",
					Timestamp:      now,
				},
				{
					ID:             domain.MessageID("message-2"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.AgentSender("planner"),
					Channel:        domain.MessageChannelBroadcast,
					Kind:           domain.MessageKindUtterance,
					Body:           "first reply",
					ReplyTo:        domain.MessageID("message-1"),
					Timestamp:      now.Add(time.Second),
				},
			},
			Stream: []runtimeadapter.StreamEntry{
				{
					RecordedAt: now,
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-1"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-1"),
							Sender:         domain.UserSender("operator"),
							Channel:        domain.MessageChannelUser,
							Kind:           domain.MessageKindUtterance,
							Body:           "hello",
						},
					},
				},
				{
					RecordedAt: now.Add(time.Second),
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-2"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-1"),
							Sender:         domain.AgentSender("planner"),
							Channel:        domain.MessageChannelBroadcast,
							Kind:           domain.MessageKindUtterance,
							Body:           "first reply",
							ReplyTo:        domain.MessageID("message-1"),
						},
					},
				},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	updated, _ := model.Update(attachStepProgressMsg{
		state:     model.room,
		step:      application.SessionStepResult{Stepped: true, Agent: &model.agents[0]},
		remaining: 1,
	})
	next := updated.(attachModel)
	if len(next.pendingAgentStates) != 0 {
		t.Fatalf("expected pending agent states cleared after final step, got %#v", next.pendingAgentStates)
	}
	if strings.Contains(next.renderHeader(), "thinking") || strings.Contains(next.renderHeader(), "queued") {
		t.Fatalf("expected no pending activity in header after final step, got:\n%s", next.renderHeader())
	}
	if !strings.Contains(next.renderConversationContent(domain.ConversationID("conversation-1")), "first reply") {
		t.Fatalf("expected persisted reply to remain visible in conversation content")
	}
}

func TestAttachModelStepProgressUsesDeferredContinuation(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{
			SessionID:     domain.SessionID("session-1"),
			AutoSteps:     3,
			Orchestration: application.OrchestrationModeRoundRobin,
		},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 100),
		testAttachAgent("writer", 100),
	}
	model.width = 120
	model.height = 30
	model.layout()

	updated, cmd := model.Update(attachStepProgressMsg{
		state: attachRoomState{
			snapshot: runtimeadapter.SessionSnapshot{
				Session: domain.Session{
					ID:     domain.SessionID("session-1"),
					Mode:   domain.SessionModeFree,
					Status: domain.SessionStatusRunning,
				},
			},
			conversations: []domain.ConversationID{"conversation-1"},
		},
		step:      application.SessionStepResult{Stepped: true, Agent: &model.agents[0]},
		remaining: 3,
	})
	next := updated.(attachModel)
	if next.pendingOps != 1 {
		t.Fatalf("expected pending ops to remain active, got %d", next.pendingOps)
	}
	msg := cmd()
	continueMsg, ok := msg.(attachContinueAutoMsg)
	if !ok {
		t.Fatalf("expected deferred auto continuation msg, got %T", msg)
	}
	if continueMsg.remaining != 2 {
		t.Fatalf("expected remaining=2, got %#v", continueMsg)
	}
}

func TestAttachModelErrorRendersNoticeInConversation(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 120
	model.height = 30
	model.layout()

	updated, _ := model.Update(attachErrMsg{err: errors.New("provider timeout")})
	next := updated.(attachModel)

	if !strings.Contains(next.renderConversationContent(domain.ConversationID("conversation-1")), "room error: provider timeout") {
		t.Fatalf("expected room error notice in conversation content, got:\n%s", next.renderConversationContent(domain.ConversationID("conversation-1")))
	}
}

func TestAttachModelHeaderAndInputHeightsStayFixed(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 100
	model.height = 30
	model.layout()
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}
	model.syncViewportContent(true)

	baseHeaderHeight := lipgloss.Height(model.renderHeader())
	baseInputHeight := lipgloss.Height(model.renderInput())
	if baseHeaderHeight != 3 {
		t.Fatalf("expected fixed header height 3, got %d", baseHeaderHeight)
	}
	if baseInputHeight < 4 {
		t.Fatalf("expected input area to occupy at least 4 lines, got %d", baseInputHeight)
	}
	baseViewHeight := lipgloss.Height(model.View())
	if baseViewHeight != model.height {
		t.Fatalf("expected base view height %d, got %d", model.height, baseViewHeight)
	}

	model.status = strings.Repeat("very long status ", 20)
	model.pendingAgentStates = map[domain.AgentID]string{
		"planner":  "thinking",
		"reviewer": "queued",
		"writer":   "queued",
	}
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
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 100
	model.height = 30
	model.layout()
	model.viewport.SetContent(strings.Repeat("line\n", 100))
	model.viewport.GotoBottom()

	if !model.stickyBottom {
		t.Fatal("expected sticky bottom enabled by default")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	model = updated.(attachModel)
	if model.stickyBottom {
		t.Fatal("expected sticky bottom disabled after pgup")
	}
	if model.viewport.YOffset == 0 {
		t.Fatal("expected viewport offset to remain above top after pgup")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	model = updated.(attachModel)
	if !model.stickyBottom {
		t.Fatal("expected sticky bottom re-enabled after end")
	}
	if !model.viewport.AtBottom() {
		t.Fatalf("expected viewport to return to bottom, offset=%d max=%d", model.viewport.YOffset, model.viewport.TotalLineCount()-model.viewport.Height)
	}
}

func TestAttachModelViewFitsWindowWithSidebarAndWrappedMessages(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 132
	model.height = 28
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: []domain.Message{
				{
					ID:             domain.MessageID("message-1"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.UserSender("operator"),
					Channel:        domain.MessageChannelUser,
					Kind:           domain.MessageKindUtterance,
					Body:           strings.Repeat("operator text that should wrap cleanly across the room pane and stay visible inside the viewport ", 3),
					Timestamp:      now,
				},
			},
			Stream: []runtimeadapter.StreamEntry{
				{
					RecordedAt: now,
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-1"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-1"),
							Sender:         domain.UserSender("operator"),
							Channel:        domain.MessageChannelUser,
							Kind:           domain.MessageKindUtterance,
							Body:           strings.Repeat("operator text that should wrap cleanly across the room pane and stay visible inside the viewport ", 3),
						},
					},
				},
			},
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
	if model.layoutSidebarWidth == 0 {
		t.Fatal("expected sidebar to be enabled for wide layout")
	}
}

func TestAttachModelDefaultRoomScopeShowsWholeSessionTimeline(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-2"),
		ui,
	)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: []domain.Message{
				{
					ID:             domain.MessageID("message-1"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.UserSender("operator"),
					Channel:        domain.MessageChannelUser,
					Kind:           domain.MessageKindUtterance,
					Body:           "older thread context",
					Timestamp:      now,
				},
				{
					ID:             domain.MessageID("message-2"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-2"),
					Sender:         domain.AgentSender("planner"),
					Channel:        domain.MessageChannelBroadcast,
					Kind:           domain.MessageKindUtterance,
					Body:           "current thread context",
					Timestamp:      now.Add(time.Second),
				},
			},
			Stream: []runtimeadapter.StreamEntry{
				{
					RecordedAt: now,
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-1"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-1"),
							Sender:         domain.UserSender("operator"),
							Channel:        domain.MessageChannelUser,
							Kind:           domain.MessageKindUtterance,
							Body:           "older thread context",
						},
					},
				},
				{
					RecordedAt: now.Add(time.Second),
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-2"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-2"),
							Sender:         domain.AgentSender("planner"),
							Channel:        domain.MessageChannelBroadcast,
							Kind:           domain.MessageKindUtterance,
							Body:           "current thread context",
						},
					},
				},
			},
		},
		conversations: []domain.ConversationID{"conversation-1", "conversation-2"},
	}
	model.width = 100
	model.height = 24
	model.layout()
	model.syncViewportContent(true)

	if got := model.roomConversationScope(); got != "" {
		t.Fatalf("expected unpinned attach room to use session scope, got %q", got)
	}
	content := model.lastViewportContent
	if !strings.Contains(content, "conversation-1") || !strings.Contains(content, "conversation-2") {
		t.Fatalf("expected session timeline to include both conversation labels, got:\n%s", content)
	}
	if !strings.Contains(model.renderHeader(), "scope=session") {
		t.Fatalf("expected header to show session scope, got:\n%s", model.renderHeader())
	}
}

func TestAttachModelSidebarShowsMessageTotalsAndRemovesKeysBlock(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 90),
		testAttachAgent("writer", 80),
	}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: []domain.Message{
				{
					ID:             domain.MessageID("message-1"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.UserSender("operator"),
					Channel:        domain.MessageChannelUser,
					Kind:           domain.MessageKindUtterance,
					Body:           "hello",
					Timestamp:      now,
				},
				{
					ID:             domain.MessageID("message-2"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.AgentSender("planner"),
					Channel:        domain.MessageChannelBroadcast,
					Kind:           domain.MessageKindUtterance,
					Body:           "reply one",
					Timestamp:      now.Add(time.Second),
				},
				{
					ID:             domain.MessageID("message-3"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.AgentSender("planner"),
					Channel:        domain.MessageChannelBroadcast,
					Kind:           domain.MessageKindUtterance,
					Body:           "reply two",
					Timestamp:      now.Add(2 * time.Second),
				},
				{
					ID:             domain.MessageID("message-4"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.AgentSender("reviewer"),
					Channel:        domain.MessageChannelBroadcast,
					Kind:           domain.MessageKindUtterance,
					Body:           "review reply",
					Timestamp:      now.Add(3 * time.Second),
				},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	sidebar := model.renderSidebar()
	for _, expected := range []string{"Messages", "total: 4", "operator: 1", "planner: 2", "reviewer: 1"} {
		if !strings.Contains(sidebar, expected) {
			t.Fatalf("expected %q in sidebar, got:\n%s", expected, sidebar)
		}
	}
	if strings.Contains(sidebar, "writer: 0") {
		t.Fatalf("expected silent participants to be omitted from message metrics, got:\n%s", sidebar)
	}
	if strings.Contains(sidebar, "planner  last  msgs:2") || strings.Contains(sidebar, "reviewer  idle  msgs:1") {
		t.Fatalf("expected participant rows to omit message counters, got:\n%s", sidebar)
	}
	if strings.Contains(sidebar, "Keys") {
		t.Fatalf("expected keys block to be removed, got:\n%s", sidebar)
	}
}

func TestAttachModelViewportUsesInnerPaneDimensions(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 100
	model.height = 24
	model.layout()

	wantWidth := max(model.layoutRoomWidth-model.styles.room.GetHorizontalFrameSize(), 1)
	wantHeight := max(model.layoutBodyHeight-model.styles.room.GetVerticalFrameSize(), 1)
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
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 92
	model.height = 20

	stream := make([]runtimeadapter.StreamEntry, 0, 24)
	messages := make([]domain.Message, 0, 24)
	for i := 0; i < 24; i++ {
		body := strings.Repeat("wrapped room content ", 3)
		if i == 23 {
			body = "final marker should stay visible at bottom"
		}
		message := domain.Message{
			ID:             domain.MessageID("message-" + strconv.Itoa(i+1)),
			SessionID:      domain.SessionID("session-1"),
			ConversationID: domain.ConversationID("conversation-1"),
			Sender:         domain.UserSender("operator"),
			Channel:        domain.MessageChannelUser,
			Kind:           domain.MessageKindUtterance,
			Body:           body,
			Timestamp:      now.Add(time.Duration(i) * time.Second),
		}
		messages = append(messages, message)
		stream = append(stream, runtimeadapter.StreamEntry{
			RecordedAt: message.Timestamp,
			Payload: application.MessageDispatchedEvent{
				Message: message,
			},
		})
	}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: messages,
			Stream:   stream,
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

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
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 92
	model.height = 20
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
			Messages: []domain.Message{
				{
					ID:             domain.MessageID("message-1"),
					SessionID:      domain.SessionID("session-1"),
					ConversationID: domain.ConversationID("conversation-1"),
					Sender:         domain.UserSender("operator"),
					Channel:        domain.MessageChannelUser,
					Kind:           domain.MessageKindUtterance,
					Body:           strings.Repeat("wrapped message ", 20),
					Timestamp:      now,
				},
			},
			Stream: []runtimeadapter.StreamEntry{
				{
					RecordedAt: now,
					Payload: application.MessageDispatchedEvent{
						Message: domain.Message{
							ID:             domain.MessageID("message-1"),
							SessionID:      domain.SessionID("session-1"),
							ConversationID: domain.ConversationID("conversation-1"),
							Sender:         domain.UserSender("operator"),
							Channel:        domain.MessageChannelUser,
							Kind:           domain.MessageKindUtterance,
							Body:           strings.Repeat("wrapped message ", 20),
						},
					},
				},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

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
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)

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

func TestAttachModelRenderInputShowsCommandAssist(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.width = 100
	model.height = 24
	model.layout()
	model.input.SetValue("/")
	model.input.CursorEnd()
	model.refreshInputAssist()

	rendered := model.renderInput()
	for _, command := range []string{"/help", "/step", "/auto", "/quit"} {
		if !strings.Contains(rendered, command) {
			t.Fatalf("expected command %q in input assist, got:\n%s", command, rendered)
		}
	}
}

func TestAttachModelAcceptsMentionSuggestion(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{SessionID: domain.SessionID("session-1")},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 90),
	}
	model.input.SetValue("hello @pl")
	model.input.CursorEnd()
	model.refreshInputAssist()

	if model.inputAssist.Kind != attachInputAssistMention {
		t.Fatalf("expected mention assist, got %#v", model.inputAssist)
	}
	if !model.acceptSelectedInputAssist(false) {
		t.Fatal("expected mention suggestion to be accepted")
	}
	if got := model.input.Value(); got != "hello @planner " {
		t.Fatalf("expected accepted mention in input, got %q", got)
	}
}

func TestMentionedAgentIDsReturnsUniqueRecipientsInMessageOrder(t *testing.T) {
	agents := []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 90),
		testAttachAgent("writer", 80),
	}
	got := mentionedAgentIDs("@reviewer please sync with @planner and @reviewer again", agents)
	want := []domain.AgentID{"reviewer", "planner"}
	if len(got) != len(want) {
		t.Fatalf("expected %d recipients, got %#v", len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("expected recipients %#v, got %#v", want, got)
		}
	}
}

func TestAttachModelSubmitInputMentionsTargetsMultipleAgents(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{
			SessionID: domain.SessionID("session-1"),
			AutoSteps: 2,
		},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 90),
		testAttachAgent("writer", 80),
	}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{
				ID:     domain.SessionID("session-1"),
				Mode:   domain.SessionModeFree,
				Status: domain.SessionStatusRunning,
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}
	model.width = 100
	model.height = 24
	model.layout()

	model.submitInput("@planner and @reviewer please review this")

	if len(model.optimistic) != 1 {
		t.Fatalf("expected one optimistic message, got %d", len(model.optimistic))
	}
	if got := model.optimistic[0].ToAgentIDs; len(got) != 2 || got[0] != "planner" || got[1] != "reviewer" {
		t.Fatalf("expected direct optimistic recipients [planner reviewer], got %#v", got)
	}
	rendered := model.renderConversationContent(domain.ConversationID("conversation-1"))
	if !strings.Contains(rendered, "-> planner,reviewer") {
		t.Fatalf("expected direct recipients in rendered conversation, got:\n%s", rendered)
	}
}

func TestAttachModelSubmitInputRaisesAutoStepsToMentionCount(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(
		context.Background(),
		nil,
		liveViewOptions{
			SessionID: domain.SessionID("session-1"),
			AutoSteps: 1,
		},
		domain.ConversationID("conversation-1"),
		ui,
	)
	model.agents = []domain.Agent{
		testAttachAgent("planner", 100),
		testAttachAgent("reviewer", 90),
	}
	model.width = 100
	model.height = 24
	model.layout()

	msg := model.submitInput("@planner @reviewer please reply")()
	dispatchMsg, ok := msg.(attachBeginDispatchMsg)
	if !ok {
		t.Fatalf("expected deferred dispatch msg, got %T", msg)
	}
	if dispatchMsg.autoSteps != 2 {
		t.Fatalf("expected auto steps to expand to mention count, got %d", dispatchMsg.autoSteps)
	}
}

func maxRenderedLineWidth(value string) int {
	maxWidth := 0
	for _, line := range strings.Split(value, "\n") {
		if width := lipgloss.Width(line); width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}

func testAttachAgent(id domain.AgentID, priority int) domain.Agent {
	return domain.Agent{
		ID:           id,
		Name:         string(id),
		Role:         "tester",
		SystemPrompt: "test prompt",
		Provider:     "local_stub",
		Model:        "gpt-test",
		Policies: domain.AgentPolicy{
			CanInitiate:         true,
			AllowBroadcast:      true,
			Priority:            priority,
			Weight:              1,
			MaxConsecutiveTurns: 1,
		},
	}
}
