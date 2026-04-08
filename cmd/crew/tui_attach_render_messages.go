package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
)

func (m attachModel) renderConversationContent(conversationID domain.ConversationID) string {
	events := m.displayEvents(conversationID)
	if len(events) == 0 {
		return m.styles.muted.Render("No messages yet. Type below to begin.")
	}

	blocks := make([]string, 0, len(events))
	for idx := 0; idx < len(events); {
		event := events[idx]
		if event.Kind != "message" {
			blocks = append(blocks, m.renderNonMessageBlock(event))
			idx++
			continue
		}
		group := []attachDisplayEvent{event}
		j := idx + 1
		for j < len(events) && canGroupDisplayEvents(group[len(group)-1], events[j]) {
			group = append(group, events[j])
			j++
		}
		blocks = append(blocks, m.renderMessageGroup(group))
		idx = j
	}

	if m.ui.CompactMessages {
		return strings.Join(blocks, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

func canGroupDisplayEvents(a, b attachDisplayEvent) bool {
	return a.Kind == "message" &&
		b.Kind == "message" &&
		a.Sender == b.Sender &&
		a.ConversationID == b.ConversationID &&
		a.Pending == b.Pending
}

func (m attachModel) displayEvents(conversationID domain.ConversationID) []attachDisplayEvent {
	events := make([]attachDisplayEvent, 0, len(m.room.snapshot.Stream)+len(m.optimistic)+len(m.localNotices))
	replySummaryByID := buildReplySummaryIndex(m.room.snapshot.Messages)
	for _, entry := range m.room.snapshot.Stream {
		event, ok := m.streamEntryToDisplayEvent(entry, conversationID, replySummaryByID)
		if ok {
			events = append(events, event)
		}
	}
	for _, pending := range m.optimistic {
		if conversationID != "" && pending.ConversationID != conversationID {
			continue
		}
		events = append(events, attachDisplayEvent{
			Kind:           "message",
			RecordedAt:     pending.SubmittedAt,
			ConversationID: pending.ConversationID,
			Sender:         pending.Sender,
			Body:           pending.Body,
			ToAgentIDs:     append([]domain.AgentID(nil), pending.ToAgentIDs...),
			Pending:        true,
		})
	}
	for _, notice := range m.localNotices {
		if conversationID != "" && notice.ConversationID != "" && notice.ConversationID != conversationID {
			continue
		}
		events = append(events, notice)
	}
	return events
}

func (m *attachModel) appendLocalNotice(event attachDisplayEvent) {
	m.localNotices = append(m.localNotices, event)
	if len(m.localNotices) > 20 {
		m.localNotices = m.localNotices[len(m.localNotices)-20:]
	}
}

func buildReplySummaryIndex(messages []domain.Message) map[domain.MessageID]string {
	index := make(map[domain.MessageID]string, len(messages))
	for _, message := range messages {
		if message.ID != "" {
			index[message.ID] = fmt.Sprintf("%s: %s", senderNameForMessage(message), trimForSidebar(message.Body))
		}
	}
	return index
}

func (m attachModel) streamEntryToDisplayEvent(
	entry runtimeadapter.StreamEntry,
	conversationID domain.ConversationID,
	replySummaryByID map[domain.MessageID]string,
) (attachDisplayEvent, bool) {
	switch event := entry.Payload.(type) {
	case application.SessionCreatedEvent:
		return attachDisplayEvent{Kind: "system", RecordedAt: entry.RecordedAt, Body: fmt.Sprintf("session created mode=%s status=%s", event.Session.Mode, event.Session.Status)}, true
	case application.SessionUpdatedEvent:
		return attachDisplayEvent{Kind: "system", RecordedAt: entry.RecordedAt, Body: fmt.Sprintf("session updated status=%s", event.Session.Status)}, true
	case application.MessageDispatchedEvent:
		if conversationID != "" && event.Message.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{
			Kind:           "message",
			RecordedAt:     entry.RecordedAt,
			ConversationID: event.Message.ConversationID,
			Sender:         senderNameForMessage(event.Message),
			Body:           sanitizeLiveText(event.Message.Body),
			ReplyTo:        event.Message.ReplyTo,
			ReplySummary:   replySummaryByID[event.Message.ReplyTo],
			ToAgentIDs:     append([]domain.AgentID(nil), event.Message.ToAgentIDs...),
		}, true
	case application.AgentTaskCreatedEvent:
		if conversationID != "" && event.Task.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{Kind: "task", RecordedAt: entry.RecordedAt, ConversationID: event.Task.ConversationID, Body: formatTaskCreatedLine(event.Task, true)}, true
	case application.AgentTaskUpdatedEvent:
		if conversationID != "" && event.Task.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{Kind: "task", RecordedAt: entry.RecordedAt, ConversationID: event.Task.ConversationID, Body: formatTaskUpdatedLine(event.Task, true)}, true
	case application.AgentHandoffCreatedEvent:
		if conversationID != "" && event.Handoff.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{Kind: "task", RecordedAt: entry.RecordedAt, ConversationID: event.Handoff.ConversationID, Body: formatHandoffLine(event.Handoff, true)}, true
	default:
		return attachDisplayEvent{Kind: "system", RecordedAt: entry.RecordedAt, Body: entry.Topic}, true
	}
}

func senderNameForMessage(message domain.Message) string {
	if message.Sender.ID != "" {
		return message.Sender.ID
	}
	return string(message.Sender.Type)
}

func (m attachModel) renderNonMessageBlock(event attachDisplayEvent) string {
	prefix := ""
	if m.ui.ShowTimestamps {
		prefix = m.styles.muted.Render(event.RecordedAt.UTC().Format("15:04:05")) + " "
	}
	switch event.Kind {
	case "task":
		return prefix + m.styles.task.Render(event.Body)
	case "status":
		return prefix + m.styles.statusBusy.Render(event.Body)
	default:
		return prefix + m.styles.system.Render(event.Body)
	}
}

func (m attachModel) renderMessageGroup(group []attachDisplayEvent) string {
	head := group[0]
	timestamp := ""
	if m.ui.ShowTimestamps {
		timestamp = m.styles.muted.Render(head.RecordedAt.UTC().Format("15:04:05")) + " "
	}
	senderStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.lookupAgentColor(head.Sender)))
	header := senderStyle.Render(head.Sender)
	if head.Pending {
		header = m.styles.pendingSender.Render("[pending] " + head.Sender)
	}
	if head.ConversationID != "" {
		header = m.styles.muted.Render("["+string(head.ConversationID)+"] ") + header
	}
	if head.ReplyTo != "" {
		header += m.styles.muted.Render(" ↩ " + string(head.ReplyTo))
	}
	if len(head.ToAgentIDs) > 0 {
		recipients := make([]string, 0, len(head.ToAgentIDs))
		for _, id := range head.ToAgentIDs {
			recipients = append(recipients, string(id))
		}
		header += m.styles.muted.Render(" -> " + strings.Join(recipients, ","))
	}
	if head.ReplySummary != "" {
		header += "\n" + m.styles.muted.Render("  in reply to "+head.ReplySummary)
	}

	bodies := make([]string, 0, len(group))
	for _, event := range group {
		bodies = append(bodies, m.styles.messageBody.Render(event.Body))
	}
	return timestamp + header + "\n" + strings.Join(bodies, "\n")
}

func (m attachModel) renderConversationPreviews() string {
	if m.roomConversationScope() == "" {
		return m.styles.muted.Render("Session timeline already includes all conversations.")
	}

	others := make([]domain.ConversationID, 0)
	for _, id := range m.room.conversations {
		if id != m.sendConversationID {
			others = append(others, id)
		}
	}
	if len(others) == 0 {
		return m.styles.muted.Render("No secondary conversations.")
	}

	sections := make([]string, 0, len(others))
	for _, id := range others {
		lines := m.previewConversationLines(id, 4)
		section := m.styles.sectionTitle.Render(string(id))
		if len(lines) == 0 {
			section += "\n" + m.styles.muted.Render("No messages")
		} else {
			section += "\n" + strings.Join(lines, "\n")
		}
		sections = append(sections, section)
	}
	return strings.Join(sections, "\n\n")
}

func (m attachModel) previewConversationLines(conversationID domain.ConversationID, limit int) []string {
	lines := make([]string, 0, limit)
	for _, message := range m.room.snapshot.Messages {
		if message.ConversationID == conversationID {
			lines = append(lines, fmt.Sprintf("%s: %s", senderNameForMessage(message), trimForSidebar(message.Body)))
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}
