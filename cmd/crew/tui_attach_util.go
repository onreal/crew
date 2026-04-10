package main

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

const (
	attachArtworkAlertLabel = "Ⓐ NOSTATE"
	attachArtworkBrandLabel = "CREW CLI"
)

func renderFixedStyledLine(style lipgloss.Style, text string, width int) string {
	if width < 1 {
		width = 1
	}
	contentWidth := max(width-style.GetHorizontalFrameSize(), 1)
	return style.Width(contentWidth).Height(1).MaxWidth(contentWidth).MaxHeight(1).Render(truncatePlainText(text, contentWidth))
}

func wrapRenderedText(content string, width int) string {
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(content)
}

func padStyledLine(content string, width int) string {
	if width < 1 {
		width = 1
	}
	if padding := width - lipgloss.Width(content); padding > 0 {
		return content + strings.Repeat(" ", padding)
	}
	return content
}

func renderArtworkBlock(width, height int, dots, alert, brand lipgloss.Style) string {
	lines := renderArtworkLines(width, height)
	alertRow := height / 2
	brandRow := min(alertRow+2, height-1)
	lines[alertRow] = renderDottedCenteredLine(attachArtworkAlertLabel, width, dots, alert)
	lines[brandRow] = renderDottedCenteredLine(attachArtworkBrandLabel, width, dots, brand)
	return strings.Join(lines, "\n")
}

func renderPlainArtworkBlock(width, height int) string {
	lines := renderArtworkLines(width, height)
	alertRow := height / 2
	brandRow := min(alertRow+2, height-1)
	lines[alertRow] = plainCenteredOverlay(attachArtworkAlertLabel, width)
	lines[brandRow] = plainCenteredOverlay(attachArtworkBrandLabel, width)
	return strings.Join(lines, "\n")
}

func renderArtworkLines(width, height int) []string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	lines := make([]string, height)
	for i := range lines {
		lines[i] = strings.Repeat(".", width)
	}
	return lines
}

func renderDottedCenteredLine(label string, width int, dots, text lipgloss.Style) string {
	left, right := centeredLinePadding(label, width)
	return dots.Render(strings.Repeat(".", left)) + text.Render(label) + dots.Render(strings.Repeat(".", right))
}

func plainCenteredOverlay(label string, width int) string {
	label = truncatePlainText(label, width)
	left, right := centeredLinePadding(label, width)
	return strings.Repeat(".", left) + label + strings.Repeat(".", right)
}

func centeredLinePadding(label string, width int) (int, int) {
	labelWidth := lipgloss.Width(truncatePlainText(label, width))
	if labelWidth >= width {
		return 0, 0
	}
	left := (width - labelWidth) / 2
	right := width - left - labelWidth
	return left, right
}

func truncatePlainText(value string, limit int) string {
	if limit < 1 {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}

func truncateTailPlainText(value string, limit int) string {
	if limit < 1 {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-limit+1:])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
