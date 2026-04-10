package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

func TestAttachModelSubmitInputPinsConversationAndShowsPendingActivity(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{
		SessionID: domain.SessionID("session-1"), AutoSteps: 3, Orchestration: application.OrchestrationModeRoundRobin,
	}, domain.ConversationID("conversation-2"), ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 100), testAttachAgent("writer", 100)}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{{
				ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1",
				Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance,
				Body: "older conversation", Timestamp: time.Now().Add(-time.Minute).UTC(),
			}},
		},
		conversations: []domain.ConversationID{"conversation-1", "conversation-2"},
	}
	model.selectedConvID = "conversation-1"
	model.width, model.height = 120, 30
	model.layout()

	model.submitInput("hello room")

	if model.selectedConvID != "conversation-2" {
		t.Fatalf("expected selected conversation to switch to send conversation, got %q", model.selectedConvID)
	}
	if len(model.optimistic) != 1 {
		t.Fatalf("expected optimistic message to be appended, got %d", len(model.optimistic))
	}
	if len(model.pendingAgentStates) == 0 {
		t.Fatal("expected pending agent states to be populated immediately")
	}
	if rendered := model.renderConversationContent("conversation-2"); !strings.Contains(rendered, "hello room") {
		t.Fatalf("expected optimistic operator message in rendered content, got:\n%s", rendered)
	}
	if header := model.renderHeader(); !strings.Contains(header, "thinking") {
		t.Fatalf("expected pending agent activity in header, got:\n%s", header)
	}
}

func TestAttachModelSubmitInputUsesDeferredDispatch(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{
		SessionID: "session-1", AutoSteps: 2, Orchestration: application.OrchestrationModeRoundRobin,
	}, "conversation-1", ui)
	model.width, model.height = 120, 30
	model.layout()

	msg := model.submitInput("hello")()
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
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "hello there with context", Timestamp: now},
				{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply body", ReplyTo: "message-1", Timestamp: now.Add(time.Second)},
			},
			Stream: []runtimeadapter.StreamEntry{
				{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "hello there with context"}}},
				{RecordedAt: now.Add(time.Second), Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply body", ReplyTo: "message-1"}}},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	rendered := model.renderConversationContent("conversation-1")
	if !strings.Contains(rendered, "in reply to operator: hello there with con") &&
		!strings.Contains(rendered, "in reply to operator: hello there with context") {
		t.Fatalf("expected reply summary in rendered content, got:\n%s", rendered)
	}
}

func TestAttachModelStepProgressClearsPendingBeforeRefresh(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{
		SessionID: "session-1", AutoSteps: 3, Orchestration: application.OrchestrationModeRoundRobin,
	}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 100), testAttachAgent("writer", 100)}
	model.width, model.height = 120, 30
	model.layout()
	model.pendingAgentStates = map[domain.AgentID]string{"reviewer": "thinking", "writer": "queued"}
	model.reasoningByAgent = map[domain.AgentID]string{"reviewer": "checking the latest patch"}
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "hello", Timestamp: now},
				{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "first reply", ReplyTo: "message-1", Timestamp: now.Add(time.Second)},
			},
			Stream: []runtimeadapter.StreamEntry{
				{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "hello"}}},
				{RecordedAt: now.Add(time.Second), Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "first reply", ReplyTo: "message-1"}}},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	updated, _ := model.Update(attachStepProgressMsg{state: model.room, step: application.SessionStepResult{Stepped: true, Agent: &model.agents[0]}, remaining: 1})
	next := updated.(attachModel)
	if len(next.pendingAgentStates) != 0 {
		t.Fatalf("expected pending agent states cleared after final step, got %#v", next.pendingAgentStates)
	}
	if len(next.reasoningByAgent) != 0 {
		t.Fatalf("expected reasoning state cleared after final step, got %#v", next.reasoningByAgent)
	}
	if strings.Contains(next.renderHeader(), "thinking") || strings.Contains(next.renderHeader(), "queued") {
		t.Fatalf("expected no pending activity in header after final step, got:\n%s", next.renderHeader())
	}
	if !strings.Contains(next.renderConversationContent("conversation-1"), "first reply") {
		t.Fatalf("expected persisted reply to remain visible in conversation content")
	}
}

func TestAttachModelStepProgressUsesDeferredContinuation(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{
		SessionID: "session-1", AutoSteps: 3, Orchestration: application.OrchestrationModeRoundRobin,
	}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 100), testAttachAgent("writer", 100)}
	model.width, model.height = 120, 30
	model.layout()

	updated, cmd := model.Update(attachStepProgressMsg{
		state: attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		}, conversations: []domain.ConversationID{"conversation-1"}},
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
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 120, 30
	model.layout()

	updated, _ := model.Update(attachErrMsg{err: errors.New("provider timeout")})
	next := updated.(attachModel)
	if !strings.Contains(next.renderConversationContent("conversation-1"), "room error: provider timeout") {
		t.Fatalf("expected room error notice in conversation content, got:\n%s", next.renderConversationContent("conversation-1"))
	}
}

func TestAttachModelReasoningUpdateStaysOutOfConversation(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100)}
	model.width, model.height = 120, 30
	model.layout()
	updated, _ := model.Update(attachStepStreamStartedMsg{events: make(chan tea.Msg)})
	model = updated.(attachModel)

	updated, _ = model.Update(attachReasoningMsg{agentID: "planner", text: "checking the workspace layout"})
	next := updated.(attachModel)
	if !strings.Contains(next.renderHeader(), "planner reasoning: checking the workspace layout") {
		t.Fatalf("expected reasoning in header, got:\n%s", next.renderHeader())
	}
	if !strings.Contains(next.renderBody(), "planner reasoning") || !strings.Contains(next.renderBody(), "checking the workspace") {
		t.Fatalf("expected dedicated reasoning pane in body, got:\n%s", next.renderBody())
	}
	if strings.Contains(next.renderConversationContent("conversation-1"), "checking the workspace layout") {
		t.Fatalf("expected reasoning to stay out of conversation content, got:\n%s", next.renderConversationContent("conversation-1"))
	}
}

func TestAttachModelHidesReasoningPaneUntilProgressArrives(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100)}
	model.pendingAgentStates = map[domain.AgentID]string{"planner": "thinking"}
	model.width, model.height = 120, 30
	model.layout()

	updated, _ := model.Update(attachStepStreamStartedMsg{events: make(chan tea.Msg)})
	next := updated.(attachModel)
	if strings.Contains(next.renderBody(), "No reasoning emitted yet.") {
		t.Fatalf("expected blank reasoning pane to stay hidden, got:\n%s", next.renderBody())
	}
	if next.layoutPreviewWidth != 0 {
		t.Fatalf("expected no preview pane without progress text, got width %d", next.layoutPreviewWidth)
	}
}

func TestAttachModelCopyFailureSetsClipboardError(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.clipboard = attachClipboard{copyText: func(string) error { return errors.New("clipboard unavailable") }}
	model.width, model.height = 100, 24
	model.layout()
	model.syncViewportContent(true)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	model = updated.(attachModel)
	if !strings.Contains(model.lastError, "clipboard copy failed: clipboard unavailable") {
		t.Fatalf("expected clipboard error, got %q", model.lastError)
	}
}

func TestAttachModelHidesSandboxNoiseButKeepsCompletionSummary(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "Planning done.\n\n@writer", Timestamp: now},
				{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.SystemSender("sandbox"), Channel: domain.MessageChannelSystem, Kind: domain.MessageKindEvent, Body: "Planner delegated sandbox task task-1 to codex: build it", Timestamp: now.Add(time.Second)},
				{ID: "message-3", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.SystemSender("sandbox"), Channel: domain.MessageChannelSystem, Kind: domain.MessageKindEvent, Body: "Sandbox task task-1 completed on codex: built the site", Timestamp: now.Add(2 * time.Second)},
			},
			Stream: []runtimeadapter.StreamEntry{
				{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "Planning done.\n\n@writer"}}},
				{RecordedAt: now.Add(time.Millisecond), Payload: application.AgentTaskCreatedEvent{Task: application.SandboxTask{ID: "task-1", SessionID: "session-1", ConversationID: "conversation-1", RuntimeName: "codex", Instruction: "build it"}}},
				{RecordedAt: now.Add(2 * time.Millisecond), Payload: application.AgentHandoffCreatedEvent{Handoff: application.AgentHandoff{ID: "handoff-1", SessionID: "session-1", ConversationID: "conversation-1", FromAgentID: "planner", ToProviderClass: application.AgentProviderClassSandboxedRuntime, TaskID: "task-1", Reason: "Delegated sandbox task"}}},
				{RecordedAt: now.Add(time.Second), Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-2", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.SystemSender("sandbox"), Channel: domain.MessageChannelSystem, Kind: domain.MessageKindEvent, Body: "Planner delegated sandbox task task-1 to codex: build it"}}},
				{RecordedAt: now.Add(time.Second + time.Millisecond), Payload: application.AgentTaskUpdatedEvent{Task: application.SandboxTask{ID: "task-1", SessionID: "session-1", ConversationID: "conversation-1", RuntimeName: "codex", Status: application.SandboxTaskStatusRunning}}},
				{RecordedAt: now.Add(2 * time.Second), Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-3", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.SystemSender("sandbox"), Channel: domain.MessageChannelSystem, Kind: domain.MessageKindEvent, Body: "Sandbox task task-1 completed on codex: built the site"}}},
			},
		},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	rendered := model.renderConversationContent("conversation-1")
	if strings.Contains(rendered, "delegated sandbox task") || strings.Contains(rendered, "running") || strings.Contains(rendered, "handoff") {
		t.Fatalf("expected sandbox chatter to stay hidden, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Sandbox task task-1 completed on codex: built the site") {
		t.Fatalf("expected final sandbox completion summary to remain visible, got:\n%s", rendered)
	}
}

func TestAttachModelShowsActiveSandboxTaskProgressLine(t *testing.T) {
	ui := platform.DefaultConfig().UI
	now := time.Now().UTC()
	startedAt := now.Add(-90 * time.Second)
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.room = attachRoomState{
		snapshot: runtimeadapter.SessionSnapshot{
			Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
			Messages: []domain.Message{
				{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "build it", Timestamp: now},
			},
			Stream: []runtimeadapter.StreamEntry{
				{RecordedAt: now, Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.UserSender("operator"), Channel: domain.MessageChannelUser, Kind: domain.MessageKindUtterance, Body: "build it"}}},
			},
		},
		tasks: []application.SandboxTask{{
			ID:             "task-1",
			SessionID:      "session-1",
			ConversationID: "conversation-1",
			RuntimeName:    "codex",
			Instruction:    "Implement a polished one-page website in the current workspace",
			Status:         application.SandboxTaskStatusRunning,
			CreatedAt:      now.Add(-2 * time.Minute),
			StartedAt:      &startedAt,
		}},
		conversations: []domain.ConversationID{"conversation-1"},
	}

	rendered := model.renderConversationContent("conversation-1")
	if !strings.Contains(rendered, "sandbox task task-1 running on codex") {
		t.Fatalf("expected active sandbox task line, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Implement a polished on...") {
		t.Fatalf("expected active sandbox task instruction summary, got:\n%s", rendered)
	}
}
