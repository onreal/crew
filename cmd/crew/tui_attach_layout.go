package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const attachArtworkThresholdWidth = 125

func (m *attachModel) layout() {
	totalWidth := max(m.width, 1)
	m.showSidebar = m.ui.AttachSidebar
	m.layoutSidebarWidth = 0
	m.layoutSidebarHeight = 0
	m.layoutArtworkWidth = 0
	m.layoutArtworkHeight = 0
	m.layoutMainWidth = totalWidth
	if totalWidth >= attachArtworkThresholdWidth {
		artworkWidth := max(totalWidth/5, 24)
		if totalWidth-artworkWidth-1 >= 56 {
			m.layoutArtworkWidth = artworkWidth
		}
	}

	inputWidth := m.layoutMainWidth
	if m.layoutArtworkWidth > 0 {
		inputWidth = max(m.layoutMainWidth-m.layoutArtworkWidth-1, 1)
	}
	m.input.Width = max(inputWidth-m.styles.inputBox.GetHorizontalFrameSize(), 1)
	headerHeight := lipgloss.Height(m.renderHeader())
	inputHeight := lipgloss.Height(m.renderInput())
	footerHeight := lipgloss.Height(m.renderFooter())
	m.layoutBodyHeight = max(m.height-headerHeight-inputHeight-footerHeight, 1)
	if m.showSidebar {
		m.layoutSidebarHeight = 1
	}
	m.layoutRoomHeight = max(m.layoutBodyHeight-m.layoutSidebarHeight, 1)
	m.layoutPreviewHeight = m.layoutRoomHeight
	m.layoutRoomWidth, m.layoutPreviewWidth = m.layoutMainWidth, 0

	if m.canSplitConversations() && m.splitPanes {
		roomWidth := (m.layoutMainWidth * 2) / 3
		previewWidth := m.layoutMainWidth - roomWidth - 1
		if roomWidth >= 40 && previewWidth >= 20 {
			m.layoutRoomWidth, m.layoutPreviewWidth = roomWidth, previewWidth
		}
	}

	m.layoutRoomInnerWidth = max(m.layoutRoomWidth-m.styles.room.GetHorizontalFrameSize(), 1)
	m.layoutRoomInnerHeight = max(m.layoutRoomHeight-m.styles.room.GetVerticalFrameSize(), 1)
	m.viewport.Style = lipgloss.NewStyle()
	m.viewport.Width = m.layoutRoomInnerWidth
	m.viewport.Height = m.layoutRoomInnerHeight
}

func (m attachModel) renderHeader() string {
	width := max(m.width, 1)
	title := renderFixedStyledLine(m.styles.header, fmt.Sprintf(
		" crew room  session=%s  scope=%s  send=%s  mode=%s  status=%s ",
		m.options.SessionID, m.roomConversationLabel(), m.sendConversationID, m.room.snapshot.Session.Mode, m.room.snapshot.Session.Status,
	), width)
	meta := renderFixedStyledLine(m.styles.subheader, fmt.Sprintf(
		" orchestration=%s  auto_steps=%d  poll=%s  theme=%s  pending_ops=%d ",
		displayOrchestrationMode(m.options.Orchestration), m.options.AutoSteps, m.options.PollInterval, m.ui.Theme, m.pendingOps,
	), width)
	status := renderFixedStyledLine(m.styles.status, m.status, width)
	if m.lastError != "" {
		status = renderFixedStyledLine(m.styles.errorText, m.lastError, width)
	}
	if line := m.renderPendingStatusLine(); line != "" && m.lastError == "" {
		status = renderFixedStyledLine(m.styles.subheader, truncatePlainText(" "+m.status+" | "+line, max(width-2, 1)), width)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, meta, status)
}

func (m attachModel) renderInput() string {
	width := max(m.layoutMainWidth, 1)
	composeWidth := width
	if m.layoutArtworkWidth > 0 {
		composeWidth = max(width-m.layoutArtworkWidth-1, 1)
	}
	wrapWidth := max(composeWidth-m.styles.inputWrap.GetHorizontalFrameSize(), 1)
	box := renderStaticPane(m.styles.inputBox, wrapWidth, m.styles.inputBox.GetVerticalFrameSize()+1,
		m.renderInputValue(max(wrapWidth-m.styles.inputBox.GetHorizontalFrameSize(), 1)))
	assist := renderFixedStyledLine(m.styles.inputAssist, m.renderInputAssistText(), wrapWidth)
	content := lipgloss.JoinVertical(lipgloss.Left, box, assist)
	compose := renderStaticPane(m.styles.inputWrap, composeWidth, m.styles.inputWrap.GetVerticalFrameSize()+lipgloss.Height(content), content)
	if m.layoutArtworkWidth == 0 {
		return compose
	}
	artHeight := lipgloss.Height(compose)
	m.layoutArtworkHeight = artHeight
	artwork := renderStaticPane(m.styles.preview, m.layoutArtworkWidth, artHeight, m.renderArtworkPanel(m.layoutArtworkWidth, artHeight))
	return lipgloss.JoinHorizontal(lipgloss.Top, compose, " ", artwork)
}

func (m attachModel) renderInputValue(width int) string {
	if width < 1 {
		return ""
	}
	if m.input.Value() == "" {
		return m.styles.muted.Render(truncatePlainText(m.input.Placeholder, width))
	}
	value := m.input.Value()
	if m.input.Focused() {
		value += "|"
	}
	return truncateTailPlainText(value, width)
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
	main := renderStaticPane(m.styles.room, m.layoutRoomWidth, m.layoutRoomHeight, m.viewport.View())
	mainBlock := main
	if m.layoutPreviewWidth > 0 {
		preview := renderStaticPane(m.styles.preview, m.layoutPreviewWidth, m.layoutPreviewHeight, m.renderConversationPreviews())
		mainBlock = lipgloss.JoinHorizontal(lipgloss.Top, main, " ", preview)
	}
	if !m.showSidebar {
		return mainBlock
	}
	status := padStyledLine(m.renderCompactStatus(max(m.width, 1)), max(m.width, 1))
	return lipgloss.JoinVertical(lipgloss.Left, mainBlock, status)
}

func (m attachModel) renderFooter() string {
	return renderFixedStyledLine(m.styles.footer, attachFooterHelpText(), max(m.width, 1))
}

func (m *attachModel) syncViewportContent(forceBottom bool) {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		return
	}
	contentWidth := max(m.viewport.Width, 1)
	content, plain := m.renderConversationPane(m.roomConversationScope(), contentWidth)
	m.lastRoomPlainContent = plain
	if content == m.lastViewportContent &&
		m.viewport.Width == m.lastViewportWidth &&
		m.viewport.Height == m.lastViewportHeight {
		if forceBottom && !m.viewport.AtBottom() {
			m.viewport.GotoBottom()
			m.stickyBottom = true
		}
		return
	}

	offset := m.viewport.YOffset
	shouldPinBottom := forceBottom || m.stickyBottom
	m.viewport.SetContent(content)
	m.lastViewportContent = content
	m.lastViewportWidth = m.viewport.Width
	m.lastViewportHeight = m.viewport.Height
	if shouldPinBottom {
		m.viewport.GotoBottom()
		m.stickyBottom = true
		return
	}
	m.viewport.SetYOffset(offset)
	m.stickyBottom = m.viewport.AtBottom()
}

func attachFooterHelpText() string {
	return "Enter send/accept | / commands | @ mentions | Up/Down history or assist | PgUp/PgDn scroll | [/ ] conversation | Tab accept or panes | Ctrl+Y copy TUI | terminal mouse selection enabled | Ctrl+L refresh"
}

func (m attachModel) renderArtworkPanel(width, height int) string {
	return renderArtworkBlock(
		max(width-m.styles.preview.GetHorizontalFrameSize(), 1),
		max(height-m.styles.preview.GetVerticalFrameSize(), 1),
		m.styles.artworkDots,
		m.styles.artworkAlert,
		m.styles.artworkBrand,
	)
}
