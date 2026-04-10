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
	headerHeight := lipgloss.Height(m.renderHeader())
	inputHeight := lipgloss.Height(m.renderInput())
	footerHeight := lipgloss.Height(m.renderFooter())
	m.layoutBodyHeight = max(m.height-headerHeight-inputHeight-footerHeight, 0)
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
	body := ""
	if strings.TrimSpace(m.lastRoomPlainContent) == "" || strings.Contains(m.lastRoomPlainContent, "No messages yet.") {
		styled, _ := m.renderEmptyConversationPane(max(m.layoutMainWidth, 1))
		body = styled
	} else {
		body = visibleTailLines(m.lastRoomContent, m.layoutBodyHeight)
	}
	if !m.options.Reasoning {
		if _, _, ok := m.activeReasoningPane(); ok {
			body = visibleTailLines(body+"\n\n"+m.renderReasoningPane(), m.layoutBodyHeight)
		}
	}
	return lipgloss.NewStyle().Height(m.layoutBodyHeight).MaxHeight(m.layoutBodyHeight).Render(body)
}

func (m attachModel) renderFooter() string {
	return renderFixedStyledLine(m.styles.footer, attachFooterHelpText(), max(m.layoutMainWidth, 1))
}

func (m *attachModel) syncViewportContent(_ bool) {
	contentWidth := max(m.layoutMainWidth, 1)
	content, plain := m.renderConversationPane(m.roomConversationScope(), contentWidth)
	m.lastRoomContent = content
	m.lastRoomPlainContent = plain
}

func attachFooterHelpText() string {
	return "Enter send/accept | / commands | @ mentions | Up/Down history or assist | [/ ] send target | Tab accept | Ctrl+Y copy transcript | latest history stays on screen | Ctrl+L refresh"
}

func (m attachModel) renderReasoningPane() string {
	title, body, ok := m.activeReasoningPane()
	if !ok {
		return m.styles.muted.Render("No active reasoning.")
	}
	width := max(m.layoutMainWidth, 1)
	lines := []string{renderFixedStyledLine(m.styles.sectionTitle, title, width)}
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, wrapRenderedText(m.styles.muted.Render(strings.TrimSpace(line)), width))
	}
	return strings.Join(lines, "\n\n")
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
