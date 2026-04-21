package main

import (
	"bytes"
	"strings"
	"testing"

	"crew/internal/domain"
)

func TestAttachResumeCommandUsesSessionID(t *testing.T) {
	got := attachResumeCommand(liveViewOptions{SessionID: "session-7"})
	want := "crew tui attach --session-id session-7"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAttachResumeCommandKeepsPinnedConversationAndDebug(t *testing.T) {
	got := attachResumeCommand(liveViewOptions{
		SessionID:          "session-7",
		ConversationID:     domain.ConversationID("conversation-2"),
		TerminalScrollback: true,
		Debug:              true,
		Reasoning:          true,
	})
	want := "crew tui attach --session-id session-7 --conversation-id conversation-2 --terminal-scrollback --debug --reasoning"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrintAttachResumeHintSkipsNonFollowMode(t *testing.T) {
	var out bytes.Buffer
	if err := printAttachResumeHint(&out, liveViewOptions{SessionID: "session-7", Follow: false}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output for non-follow mode, got %q", out.String())
	}
}

func TestPrintAttachResumeHintWritesCommand(t *testing.T) {
	var out bytes.Buffer
	if err := printAttachResumeHint(&out, liveViewOptions{SessionID: "session-7", Follow: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "To resume this session: crew tui attach --session-id session-7") {
		t.Fatalf("expected resume hint, got %q", out.String())
	}
}
