package main

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

func TestAttachModelRenderInputShowsCommandAssist(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 24
	model.layout()
	model.input.SetValue("/")
	model.input.CursorEnd()
	model.refreshInputAssist()

	rendered := model.renderInput()
	if strings.Contains(strings.ToLower(rendered), " message ") {
		t.Fatalf("expected input wrapper to omit legacy message label, got:\n%s", rendered)
	}
	for _, command := range []string{"/help", "/step", "/auto", "/quit"} {
		if !strings.Contains(rendered, command) {
			t.Fatalf("expected command %q in input assist, got:\n%s", command, rendered)
		}
	}
}

func TestAttachModelRenderInputShowsSinglePlaceholderWhenEmpty(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 24
	model.layout()

	rendered := model.renderInput()
	if count := strings.Count(rendered, "Type a message or /help"); count != 1 {
		t.Fatalf("expected one placeholder line, got %d in:\n%s", count, rendered)
	}
}

func TestAttachModelRenderInputHidesPlaceholderWhileTyping(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 100, 24
	model.input.SetValue("hello there")
	model.input.CursorEnd()
	model.layout()

	rendered := model.renderInput()
	if strings.Contains(rendered, "Type a message or /help") {
		t.Fatalf("expected placeholder to disappear while typing, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "hello there") {
		t.Fatalf("expected typed message in input, got:\n%s", rendered)
	}
}

func TestAttachModelAcceptsMentionSuggestion(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 90)}
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
	agents := []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 90), testAttachAgent("writer", 80)}
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
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1", AutoSteps: 2}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 90), testAttachAgent("writer", 80)}
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
	}, conversations: []domain.ConversationID{"conversation-1"}}
	model.width, model.height = 100, 24
	model.layout()

	model.submitInput("@planner and @reviewer please review this")

	if len(model.optimistic) != 1 {
		t.Fatalf("expected one optimistic message, got %d", len(model.optimistic))
	}
	if got := model.optimistic[0].ToAgentIDs; len(got) != 2 || got[0] != "planner" || got[1] != "reviewer" {
		t.Fatalf("expected direct optimistic recipients [planner reviewer], got %#v", got)
	}
	if rendered := model.renderConversationContent("conversation-1"); strings.Contains(rendered, "planner,reviewer") {
		t.Fatalf("expected optimistic direct recipients to stay out of transcript until persisted, got:\n%s", rendered)
	}
	if input := model.renderInput(); !strings.Contains(input, "activity:") {
		t.Fatalf("expected submit activity below input, got:\n%s", input)
	}
}

func TestAttachModelSubmitInputRaisesAutoStepsToMentionCount(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1", AutoSteps: 1}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 90)}
	model.width, model.height = 100, 24
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

func TestAttachModelCtrlYCopiesCurrentTUISnapshot(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	var copied string
	model.clipboard = attachClipboard{copyText: func(text string) error { copied = text; return nil }}
	model.width, model.height = 120, 28
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session:  domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		Messages: []domain.Message{{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply body"}},
		Stream:   []runtimeadapter.StreamEntry{{Payload: application.MessageDispatchedEvent{Message: domain.Message{ID: "message-1", SessionID: "session-1", ConversationID: "conversation-1", Sender: domain.AgentSender("planner"), Channel: domain.MessageChannelBroadcast, Kind: domain.MessageKindUtterance, Body: "reply body"}}}},
	}, conversations: []domain.ConversationID{"conversation-1"}}
	model.input.SetValue("draft input")
	model.layout()
	model.syncViewportContent(true)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	model = updated.(attachModel)
	if model.status != "copied current TUI snapshot" {
		t.Fatalf("expected copy status, got %q", model.status)
	}
	for _, expected := range []string{"Crew Room", "Room (session)", "reply body", "draft input"} {
		if !strings.Contains(copied, expected) {
			t.Fatalf("expected %q in copied snapshot, got:\n%s", expected, copied)
		}
	}
}

func TestAttachModelRenderConversationHighlightsAgentMentions(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 90)}
	model.room = attachRoomState{snapshot: runtimeadapter.SessionSnapshot{
		Session: domain.Session{ID: "session-1", Mode: domain.SessionModeFree, Status: domain.SessionStatusRunning},
		Stream: []runtimeadapter.StreamEntry{{
			Payload: application.MessageDispatchedEvent{Message: domain.Message{
				ID:             "message-1",
				SessionID:      "session-1",
				ConversationID: "conversation-1",
				Sender:         domain.AgentSender("writer"),
				Channel:        domain.MessageChannelBroadcast,
				Kind:           domain.MessageKindUtterance,
				Body:           "Please sync with @planner and then send review to @reviewer.",
			}},
		}},
	}, conversations: []domain.ConversationID{"conversation-1"}}

	rendered := model.renderConversationContent("conversation-1")
	wantPlanner := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(model.lookupAgentColor("planner"))).Render("@planner")
	wantReviewer := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(model.lookupAgentColor("reviewer"))).Render("@reviewer")
	if !strings.Contains(rendered, wantPlanner) {
		t.Fatalf("expected planner mention to be colorized, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, wantReviewer) {
		t.Fatalf("expected reviewer mention to be colorized, got:\n%s", rendered)
	}
}

func TestAttachModelPrintedStreamEntriesRetainSenderAndMentionColors(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 120, 24
	model.layout()
	model.agents = []domain.Agent{testAttachAgent("planner", 100), testAttachAgent("reviewer", 90)}

	events := []attachDisplayEvent{{
		Kind:           "message",
		ConversationID: "conversation-1",
		Sender:         "planner",
		Body:           "Please sync with @reviewer.",
	}}

	rendered := model.renderDisplayEvents(events)
	wantSender := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(model.lookupAgentColor("planner"))).Render("planner")
	wantReviewer := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(model.lookupAgentColor("reviewer"))).Render("@reviewer")
	if !strings.Contains(rendered, wantSender) {
		t.Fatalf("expected sender color in printed stream entries, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, wantReviewer) {
		t.Fatalf("expected mention color in printed stream entries, got:\n%s", rendered)
	}
}

func TestAttachModelRenderedTranscriptDoesNotHardWrapToWindowWidth(t *testing.T) {
	ui := platform.DefaultConfig().UI
	model := newAttachModel(context.Background(), nil, liveViewOptions{SessionID: "session-1"}, "conversation-1", ui)
	model.width, model.height = 40, 20
	model.layout()

	events := []attachDisplayEvent{{
		Kind:           "message",
		ConversationID: "conversation-1",
		Sender:         "planner",
		Body:           "this is a long transcript line that should be left to the terminal for resize reflow",
	}}

	rendered := model.renderDisplayEvents(events)
	if strings.Contains(rendered, "reflow\n") || strings.Count(rendered, "\n") > 1 {
		t.Fatalf("expected rendered transcript to avoid hard wrapping, got:\n%s", rendered)
	}
}
