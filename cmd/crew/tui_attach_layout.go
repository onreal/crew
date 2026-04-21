package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *attachModel) layout() {
	totalWidth := max(m.width, 1)
	m.layoutMainWidth = max(totalWidth-1, 1)
	m.input.Width = max(m.layoutMainWidth-2, 1)
	inputHeight := lipgloss.Height(m.renderInput())
	footerHeight := lipgloss.Height(m.renderFooter())
	m.layoutBodyHeight = max(m.height-inputHeight-footerHeight, 0)
	m.resizeBodyViewport()
}

func (m attachModel) renderHeader() string {
	width := max(m.layoutMainWidth, 1)
	title := renderFixedStyledLine(m.styles.header, fmt.Sprintf(
		"crew room  session=%s  scope=%s  send=%s  mode=%s  status=%s",
		m.options.SessionID, m.roomConversationLabel(), m.sendConversationID, m.room.snapshot.Session.Mode, m.room.snapshot.Session.Status,
	), width)
	meta := renderFixedStyledLine(m.styles.subheader, fmt.Sprintf(
		"orchestration=%s  auto_steps=%d  poll=%s  theme=%s",
		displayOrchestrationMode(m.options.Orchestration), m.options.AutoSteps, m.options.PollInterval, m.ui.Theme,
	), width)
	statusText := strings.TrimSpace(m.status)
	if m.lastError != "" {
		statusText = m.lastError
	}
	if statusText == "" {
		statusText = "ready"
	}
	statusStyle := m.styles.status
	if m.lastError != "" {
		statusStyle = m.styles.errorText
	}
	status := renderFixedStyledLine(statusStyle, statusText, width)
	return lipgloss.JoinVertical(lipgloss.Left, title, meta, status)
}

func (m attachModel) renderInput() string {
	width := max(m.layoutMainWidth, 1)
	box := m.renderInputBox(width)
	assist := renderFixedStyledLine(m.styles.inputAssist, m.renderInputAssistText(), width)
	sections := []string{box, assist}
	if status := strings.TrimSpace(m.renderManagedStatus(width)); status != "" {
		sections = append(sections, status)
	}
	if status := strings.TrimSpace(m.renderCompactStatus(width)); status != "" {
		sections = append(sections, truncatePlainText(status, width))
	}
	if activity := strings.TrimSpace(m.renderPendingStatusLine()); activity != "" {
		sections = append(sections, truncatePlainText(activity, width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m attachModel) renderInputBox(width int) string {
	if width < 1 {
		return ""
	}
	contentWidth := max(width-2, 1)
	content := padStyledLine(m.renderInputValue(contentWidth), contentWidth)
	blank := strings.Repeat(" ", width)
	lines := []string{
		m.styles.inputBox.Render(blank),
		m.styles.inputBox.Render(" " + content + " "),
		m.styles.inputBox.Render(blank),
	}
	return strings.Join(lines, "\n")
}

func (m attachModel) renderInputValue(width int) string {
	if width < 1 {
		return ""
	}
	if m.input.Value() == "" {
		return m.styles.muted.Render(truncatePlainText("> "+m.input.Placeholder, width))
	}
	value := []rune(strings.ReplaceAll(m.input.Value(), "\n", " "))
	if m.input.Focused() {
		pos := min(max(m.input.Position(), 0), len(value))
		value = append(value[:pos], append([]rune{'|'}, value[pos:]...)...)
	}
	return truncateTailPlainText("> "+string(value), width)
}

func (m attachModel) renderInputAssistText() string {
	if len(m.inputAssist.Suggestions) == 0 {
		return "Use / for commands, @ to mention agents, Tab to accept suggestions."
	}
	parts := make([]string, 0, len(m.inputAssist.Suggestions))
	for idx, suggestion := range m.inputAssist.Suggestions {
		label := suggestion.Label
		if idx == m.inputAssist.Selected {
			label = "[" + label + "]"
		}
		parts = append(parts, label)
	}
	prefix := "Commands"
	if m.inputAssist.Kind == attachInputAssistMention {
		prefix = "Mention"
	}
	return prefix + ": " + strings.Join(parts, "  ") + "  | Tab accept | Up/Down choose"
}

func (m attachModel) renderBody() string {
	if m.layoutBodyHeight <= 0 {
		return ""
	}
	return m.bodyViewport.View()
}

func (m attachModel) renderFooter() string {
	return renderFixedStyledLine(m.styles.footer, attachFooterHelpText(), max(m.layoutMainWidth, 1))
}

func (m *attachModel) syncViewportContent(_ bool) {
	contentWidth := max(m.layoutMainWidth, 1)
	content, plain := m.renderConversationPane(m.roomConversationScope(), contentWidth)
	m.lastRoomContent = content
	m.lastRoomPlainContent = plain
	m.syncBodyViewport()
}

func attachFooterHelpText() string {
	return "Enter send/accept | / commands | @ mentions | Up/Down history or assist | [/ ] send target | Tab accept | Ctrl+Y copy transcript | latest history stays on screen | Ctrl+L refresh"
}

func (m attachModel) renderManagedStatus(width int) string {
	status := strings.TrimSpace(m.status)
	if m.lastError != "" {
		return renderFixedStyledLine(m.styles.errorText, m.lastError, width)
	}
	if status == "" || strings.HasPrefix(status, "attached to ") {
		return ""
	}
	return renderFixedStyledLine(m.styles.statusLine, status, width)
}

func (m attachModel) renderReasoningPane() string {
	events := m.reasoningDisplayEvents(m.roomConversationScope())
	if len(events) == 0 {
		return ""
	}
	width := max(m.layoutMainWidth, 1)
	lines := make([]string, 0, len(events))
	for _, event := range events {
		lines = append(lines, wrapRenderedText(m.renderReasoningBlock(event), width))
	}
	return strings.Join(lines, "\n\n")
}

func (m attachModel) managedBodyContent() string {
	body := m.lastRoomContent
	if strings.TrimSpace(body) == "" {
		styled, _ := m.renderEmptyConversationPane(max(m.layoutMainWidth, 1))
		body = styled
	}
	if m.options.Reasoning {
		if reasoning := strings.TrimSpace(m.renderReasoningPane()); reasoning != "" {
			body = body + "\n\n" + reasoning
		}
	}
	return body
}

func (m *attachModel) resizeBodyViewport() {
	m.bodyViewport.Width = max(m.layoutMainWidth, 1)
	m.bodyViewport.Height = max(m.layoutBodyHeight, 0)
}

func (m *attachModel) syncBodyViewport() {
	m.resizeBodyViewport()
	m.bodyViewport.SetContent(m.managedBodyContent())
	if m.layoutBodyHeight > 0 {
		m.bodyViewport.GotoBottom()
	}
}

func limitRenderedHeight(content string, height int) string {
	if height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return content
	}
	return strings.Join(lines[len(lines)-height:], "\n")
}

func visibleTailLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}
