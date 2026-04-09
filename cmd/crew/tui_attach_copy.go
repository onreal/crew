package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"

	"crew/internal/domain"
)

type attachClipboard struct {
	output   io.Writer
	copyText func(string) error
}

func newAttachClipboard(output io.Writer) attachClipboard {
	return attachClipboard{
		output:   output,
		copyText: clipboard.WriteAll,
	}
}

func (c attachClipboard) Copy(text string) error {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return errors.New("nothing to copy")
	}

	var errs []string
	if c.copyText != nil {
		if err := c.copyText(text); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if c.output != nil {
		sequence := osc52.New(text)
		switch {
		case os.Getenv("TMUX") != "":
			sequence = sequence.Tmux()
		case os.Getenv("STY") != "":
			sequence = sequence.Screen()
		}
		if _, err := io.WriteString(c.output, sequence.String()); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *attachModel) copyCurrentTUISnapshot() {
	m.copyToClipboard("copied current TUI snapshot", m.renderPlainTUISnapshot())
}

func (m *attachModel) copyToClipboard(status, text string) {
	if err := m.clipboard.Copy(text); err != nil {
		m.lastError = "clipboard copy failed: " + err.Error()
		m.status = "clipboard copy failed"
		return
	}
	m.lastError = ""
	m.status = status
}

func (m attachModel) renderPlainTUISnapshot() string {
	sections := []string{
		"Crew Room",
		m.renderPlainHeader(),
		fmt.Sprintf("Room (%s)", m.roomConversationLabel()),
		m.lastRoomPlainContent,
	}
	if m.layoutPreviewWidth > 0 {
		sections = append(sections, "Reasoning", m.renderPlainReasoningPane())
	}
	if m.layoutArtworkWidth > 0 {
		sections = append(sections, "Artwork", m.renderPlainArtworkPanel())
	}
	if m.showSidebar {
		sections = append(sections, "Status", m.renderPlainSidebar())
	}
	sections = append(sections, "Input", m.renderPlainInput(), "Footer", attachFooterHelpText())

	filtered := make([]string, 0, len(sections))
	for _, section := range sections {
		if strings.TrimSpace(section) != "" {
			filtered = append(filtered, section)
		}
	}
	return strings.Join(filtered, "\n\n")
}

func (m attachModel) renderPlainHeader() string {
	lines := []string{
		fmt.Sprintf("crew room  session=%s  scope=%s  send=%s  mode=%s  status=%s",
			m.options.SessionID, m.roomConversationLabel(), m.sendConversationID, m.room.snapshot.Session.Mode, m.room.snapshot.Session.Status),
		fmt.Sprintf("orchestration=%s  auto_steps=%d  poll=%s  theme=%s  pending_ops=%d",
			displayOrchestrationMode(m.options.Orchestration), m.options.AutoSteps, m.options.PollInterval, m.ui.Theme, m.pendingOps),
	}
	status := m.status
	if m.lastError != "" {
		status = m.lastError
	} else if line := m.renderPendingStatusLine(); line != "" {
		status = strings.TrimSpace(status + " | " + line)
	}
	lines = append(lines, status)
	return strings.Join(lines, "\n")
}

func (m attachModel) renderPlainInput() string {
	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		value = m.input.Placeholder
	}
	return strings.Join([]string{value, m.renderInputAssistText()}, "\n")
}

func (m attachModel) renderPlainReasoningPane() string {
	title, body, ok := m.activeReasoningPane()
	if !ok {
		return "No active reasoning."
	}
	return title + "\n\n" + body
}

func (m attachModel) renderPlainSidebar() string {
	messageCounts, totalMessages := summarizeMessageCounts(m.room.snapshot.Messages)
	parts := []string{fmt.Sprintf("msg %d", totalMessages)}
	for _, agent := range m.agents {
		name := agent.Name
		if strings.TrimSpace(name) == "" {
			name = string(agent.ID)
		}
		parts = append(parts, fmt.Sprintf("%s %d", name, messageCounts[string(agent.ID)]))
	}
	return strings.Join(parts, " | ")
}

func (m attachModel) renderPlainArtworkPanel() string {
	width := max(m.layoutArtworkWidth-m.styles.preview.GetHorizontalFrameSize(), 24)
	height := max(m.layoutArtworkHeight-m.styles.preview.GetVerticalFrameSize(), 7)
	return renderPlainArtworkBlock(width, height)
}

func (m attachModel) renderConversationPane(conversationID domain.ConversationID, width int) (string, string) {
	if width < 1 {
		width = 1
	}
	events := m.displayEvents(conversationID)
	if len(events) == 0 {
		empty := "No messages yet. Type below to begin."
		return m.styles.muted.Render(empty), empty
	}

	separator := "\n"
	if !m.ui.CompactMessages {
		separator = "\n\n"
	}

	styledBlocks := make([]string, 0, len(events))
	plainBlocks := make([]string, 0, len(events))
	for idx := 0; idx < len(events); {
		event := events[idx]
		if event.Kind != "message" {
			styledBlocks = append(styledBlocks, wrapRenderedText(m.renderNonMessageBlock(event), width))
			plainBlocks = append(plainBlocks, m.renderPlainNonMessageBlock(event))
			idx++
			continue
		}

		group := []attachDisplayEvent{event}
		j := idx + 1
		for j < len(events) && canGroupDisplayEvents(group[len(group)-1], events[j]) {
			group = append(group, events[j])
			j++
		}
		styledBlocks = append(styledBlocks, wrapRenderedText(m.renderMessageGroup(group), width))
		plainBlocks = append(plainBlocks, m.renderPlainMessageGroup(group))
		idx = j
	}
	return strings.Join(styledBlocks, separator), strings.Join(plainBlocks, separator)
}

func (m attachModel) renderPlainNonMessageBlock(event attachDisplayEvent) string {
	prefix := ""
	if m.ui.ShowTimestamps {
		prefix = event.RecordedAt.UTC().Format("15:04:05") + " "
	}
	return prefix + event.Body
}

func (m attachModel) renderPlainMessageGroup(group []attachDisplayEvent) string {
	head := group[0]
	header := ""
	if m.ui.ShowTimestamps {
		header = head.RecordedAt.UTC().Format("15:04:05") + " "
	}
	header += plainMessageHeader(head)
	bodies := make([]string, 0, len(group))
	for _, event := range group {
		bodies = append(bodies, "  "+event.Body)
	}
	return header + "\n" + strings.Join(bodies, "\n")
}

func plainMessageHeader(head attachDisplayEvent) string {
	header := head.Sender
	if head.Pending {
		header = "[pending] " + head.Sender
	}
	if head.ConversationID != "" {
		header = "[" + string(head.ConversationID) + "] " + header
	}
	if head.ReplyTo != "" {
		header += " <- " + string(head.ReplyTo)
	}
	if len(head.ToAgentIDs) > 0 {
		recipients := make([]string, 0, len(head.ToAgentIDs))
		for _, id := range head.ToAgentIDs {
			recipients = append(recipients, string(id))
		}
		header += " -> " + strings.Join(recipients, ",")
	}
	if head.ReplySummary != "" {
		header += "\n  in reply to " + head.ReplySummary
	}
	return header
}
