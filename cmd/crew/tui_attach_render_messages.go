package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

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
	return m.renderDisplayEvents(events)
}

func (m attachModel) renderDisplayEvents(events []attachDisplayEvent) string {
	blocks := make([]string, 0, len(events))
	for idx := 0; idx < len(events); {
		event := events[idx]
		if event.Kind == "reasoning" {
			blocks = append(blocks, m.renderReasoningBlock(event))
			idx++
			continue
		}
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

func (m attachModel) renderPlainDisplayEvents(events []attachDisplayEvent) string {
	blocks := make([]string, 0, len(events))
	for idx := 0; idx < len(events); {
		event := events[idx]
		if event.Kind == "reasoning" {
			blocks = append(blocks, m.renderPlainReasoningEvent(event))
			idx++
			continue
		}
		if event.Kind != "message" {
			blocks = append(blocks, m.renderPlainNonMessageBlock(event))
			idx++
			continue
		}
		group := []attachDisplayEvent{event}
		j := idx + 1
		for j < len(events) && canGroupDisplayEvents(group[len(group)-1], events[j]) {
			group = append(group, events[j])
			j++
		}
		blocks = append(blocks, m.renderPlainMessageGroup(group))
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
	events := make([]attachDisplayEvent, 0, len(m.room.snapshot.Stream)+len(m.localNotices))
	replySummaryByID := buildReplySummaryIndex(m.room.snapshot.Messages)
	for _, entry := range m.room.snapshot.Stream {
		event, ok := m.streamEntryToDisplayEvent(entry, conversationID, replySummaryByID)
		if ok {
			events = append(events, event)
		}
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
		if !m.options.Debug {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{Kind: "system", RecordedAt: entry.RecordedAt, Body: fmt.Sprintf("session created mode=%s status=%s", event.Session.Mode, event.Session.Status)}, true
	case application.SessionUpdatedEvent:
		if !m.options.Debug {
			return attachDisplayEvent{}, false
		}
		return attachDisplayEvent{Kind: "system", RecordedAt: entry.RecordedAt, Body: fmt.Sprintf("session updated status=%s", event.Session.Status)}, true
	case application.MessageDispatchedEvent:
		if conversationID != "" && event.Message.ConversationID != conversationID {
			return attachDisplayEvent{}, false
		}
		if !shouldSurfaceRoomMessage(event.Message) {
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
		return attachDisplayEvent{}, false
	case application.AgentTaskUpdatedEvent:
		return attachDisplayEvent{}, false
	case application.AgentHandoffCreatedEvent:
		return attachDisplayEvent{}, false
	default:
		return attachDisplayEvent{Kind: "system", RecordedAt: entry.RecordedAt, Body: entry.Topic}, true
	}
}

func shouldSurfaceRoomMessage(message domain.Message) bool {
	if message.Sender.Type != domain.MessageSenderTypeSystem || message.Sender.ID != "sandbox" {
		return true
	}
	if message.Kind == domain.MessageKindError {
		return true
	}
	body := strings.TrimSpace(message.Body)
	return strings.HasPrefix(body, "Sandbox task ")
}

func senderNameForMessage(message domain.Message) string {
	if message.Sender.ID != "" {
		return message.Sender.ID
	}
	return string(message.Sender.Type)
}

func (m attachModel) renderNonMessageBlock(event attachDisplayEvent) string {
	prefix := ""
	if m.options.Debug && m.ui.ShowTimestamps {
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

func (m attachModel) reasoningDisplayEvents(conversationID domain.ConversationID) []attachDisplayEvent {
	if len(m.progressByAgent) == 0 {
		return nil
	}

	events := make([]attachDisplayEvent, 0, len(m.progressByAgent))
	for _, agent := range m.agents {
		progress, ok := m.progressByAgent[agent.ID]
		if !ok || strings.TrimSpace(progress.Text) == "" {
			continue
		}
		event := attachDisplayEvent{
			Kind:           "reasoning",
			RecordedAt:     progressTimestamp(progress),
			ConversationID: m.sendConversationID,
			Sender:         string(agent.ID),
			Body:           progress.Text,
			ProgressKind:   displayProgressKind(progress.Kind),
		}
		if conversationID != "" && event.ConversationID != conversationID {
			continue
		}
		events = append(events, event)
	}
	return events
}

func (m attachModel) renderReasoningBlock(event attachDisplayEvent) string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(m.lookupAgentColor(event.Sender))).
		Render(event.Sender)
	header += m.styles.muted.Render(" " + displayProgressKind(event.ProgressKind))
	body := m.styles.muted.Render(m.renderMentionStyledBody(event.Body))
	return header + "\n" + m.styles.messageBody.Render(body)
}

func progressTimestamp(event application.TransientProgressEvent) time.Time {
	return time.Now().UTC()
}

func (m attachModel) renderMessageGroup(group []attachDisplayEvent) string {
	head := group[0]
	timestamp := ""
	if m.options.Debug && m.ui.ShowTimestamps {
		timestamp = m.styles.muted.Render(head.RecordedAt.UTC().Format("15:04:05")) + " "
	}
	senderStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.lookupAgentColor(head.Sender)))
	header := senderStyle.Render(head.Sender)
	if head.Pending {
		header = m.styles.pendingSender.Render("[pending] " + head.Sender)
	}
	if m.options.Debug && head.ConversationID != "" {
		header = m.styles.muted.Render("["+string(head.ConversationID)+"] ") + header
	}
	if m.options.Debug && head.ReplyTo != "" {
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
		bodies = append(bodies, m.styles.messageBody.Render(m.renderMentionStyledBody(event.Body)))
	}
	return timestamp + header + "\n" + strings.Join(bodies, "\n")
}

func (m attachModel) renderMentionStyledBody(body string) string {
	if body == "" || len(m.agents) == 0 {
		return body
	}

	var rendered strings.Builder
	for idx := 0; idx < len(body); {
		if body[idx] != '@' {
			rendered.WriteByte(body[idx])
			idx++
			continue
		}

		end := idx + 1
		for end < len(body) && isMentionIdentifierChar(rune(body[end])) {
			end++
		}
		if end == idx+1 {
			rendered.WriteByte(body[idx])
			idx++
			continue
		}

		token := body[idx:end]
		if color, ok := m.mentionColor(token[1:]); ok {
			rendered.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(token))
		} else {
			rendered.WriteString(token)
		}
		idx = end
	}
	return rendered.String()
}

func (m attachModel) mentionColor(name string) (string, bool) {
	for _, agent := range m.agents {
		if strings.EqualFold(name, string(agent.ID)) {
			return m.lookupAgentColor(string(agent.ID)), true
		}
	}
	return "", false
}

func isMentionIdentifierChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' || ch == '-'
}

func (m attachModel) activeTaskDisplayEvents(conversationID domain.ConversationID) []attachDisplayEvent {
	if len(m.room.tasks) == 0 {
		return nil
	}

	now := time.Now().UTC()
	tasks := append([]application.SandboxTask(nil), m.room.tasks...)
	slices.SortFunc(tasks, func(a, b application.SandboxTask) int {
		left := a.CreatedAt
		if a.StartedAt != nil {
			left = *a.StartedAt
		}
		right := b.CreatedAt
		if b.StartedAt != nil {
			right = *b.StartedAt
		}
		return left.Compare(right)
	})

	events := make([]attachDisplayEvent, 0, len(tasks))
	for _, task := range tasks {
		if conversationID != "" && task.ConversationID != conversationID {
			continue
		}
		if task.Status != application.SandboxTaskStatusPending && task.Status != application.SandboxTaskStatusRunning {
			continue
		}
		recordedAt := task.CreatedAt
		if task.StartedAt != nil {
			recordedAt = *task.StartedAt
		}
		events = append(events, attachDisplayEvent{
			Kind:           "task",
			RecordedAt:     recordedAt,
			ConversationID: task.ConversationID,
			Sender:         "sandbox",
			Body:           formatActiveTaskLine(task, now),
		})
	}
	return events
}

func formatActiveTaskLine(task application.SandboxTask, now time.Time) string {
	status := string(task.Status)
	if task.Status == application.SandboxTaskStatusPending {
		if summary := trimForSidebar(task.Instruction); summary != "" {
			return fmt.Sprintf("sandbox task %s pending on %s: %s", task.ID, task.RuntimeName, summary)
		}
		return fmt.Sprintf("sandbox task %s pending on %s", task.ID, task.RuntimeName)
	}

	elapsed := now.Sub(task.CreatedAt).Round(time.Second)
	if task.StartedAt != nil {
		elapsed = now.Sub(*task.StartedAt).Round(time.Second)
	}
	if elapsed < 0 {
		elapsed = 0
	}
	if summary := trimForSidebar(task.Instruction); summary != "" {
		return fmt.Sprintf("sandbox task %s %s on %s for %s: %s", task.ID, status, task.RuntimeName, elapsed, summary)
	}
	return fmt.Sprintf("sandbox task %s %s on %s for %s", task.ID, status, task.RuntimeName, elapsed)
}
