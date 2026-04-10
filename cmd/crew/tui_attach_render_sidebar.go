package main

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"crew/internal/application"
	"crew/internal/domain"
)

var attachSpinnerFrames = []string{"-", "\\", "|", "/"}

func (m attachModel) renderCompactStatus(width int) string {
	messageCounts, totalMessages := summarizeMessageCounts(m.room.snapshot.Messages)
	separator := " | "
	segments := []string{fmt.Sprintf("msg %d", totalMessages)}
	segments = append(segments, m.renderParticipantSegments(width, separator, messageCounts)...)
	line := strings.Join(segments, separator)
	if padding := width - lipgloss.Width(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

func (m attachModel) renderParticipantSegments(width int, separator string, messageCounts map[string]int) []string {
	if len(m.agents) == 0 {
		return nil
	}
	if width < 1 {
		width = 1
	}

	names := make([]string, 0, len(m.agents))
	countWidths := make([]int, 0, len(m.agents))
	totalCountWidth := 0
	for _, agent := range m.agents {
		name := agent.Name
		if strings.TrimSpace(name) == "" {
			name = string(agent.ID)
		}
		names = append(names, name)
		countWidth := lipgloss.Width(fmt.Sprintf("%d", messageCounts[string(agent.ID)]))
		countWidths = append(countWidths, countWidth)
		totalCountWidth += countWidth + 1
	}

	separatorWidth := lipgloss.Width(separator)
	baseOverhead := lipgloss.Width("msg") + 1 + lipgloss.Width(fmt.Sprintf("%d", summarizeTotalMessages(messageCounts)))
	available := width - baseOverhead - separatorWidth*len(m.agents)
	if available < len(m.agents) {
		available = len(m.agents)
	}
	labelBudgets := distributeLabelWidths(names, available-totalCountWidth)

	segments := make([]string, 0, len(m.agents))
	for idx, agent := range m.agents {
		count := fmt.Sprintf("%d", messageCounts[string(agent.ID)])
		label := truncatePlainText(names[idx], labelBudgets[idx])
		segments = append(segments, label+" "+count)
	}
	return segments
}

func summarizeTotalMessages(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func distributeLabelWidths(labels []string, available int) []int {
	widths := make([]int, len(labels))
	if len(labels) == 0 {
		return widths
	}
	if available <= 0 {
		for idx := range widths {
			widths[idx] = 1
		}
		return widths
	}
	for idx := range widths {
		widths[idx] = 1
	}
	remaining := available - len(labels)
	if remaining <= 0 {
		return widths
	}

	type item struct {
		idx    int
		target int
	}
	items := make([]item, 0, len(labels))
	for idx, label := range labels {
		items = append(items, item{idx: idx, target: lipgloss.Width(label)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].target < items[j].target })

	for remaining > 0 {
		progress := false
		for _, item := range items {
			if widths[item.idx] >= item.target {
				continue
			}
			widths[item.idx]++
			remaining--
			progress = true
			if remaining == 0 {
				break
			}
		}
		if !progress {
			break
		}
	}
	return widths
}

func (m attachModel) renderPendingStatusLine() string {
	parts := make([]string, 0, len(m.pendingAgentStates)+2)
	if len(m.optimistic) > 0 {
		parts = append(parts, fmt.Sprintf("%s sending", m.spinnerFrameGlyph()))
	}
	if summary := m.primaryActiveTaskSummary(); summary != "" {
		parts = append(parts, fmt.Sprintf("%s %s", m.spinnerFrameGlyph(), summary))
	}
	for _, agent := range m.agents {
		if state, exists := m.pendingAgentStates[agent.ID]; exists {
			switch state {
			case "queued":
				parts = append(parts, fmt.Sprintf("%s queued", agent.ID))
			default:
				parts = append(parts, fmt.Sprintf("%s %s", agent.ID, m.spinnerFrameGlyph()))
			}
		}
	}
	if event, ok := m.primaryProgressEvent(); ok {
		parts = append(parts, fmt.Sprintf("%s %s: %s", event.AgentID, displayProgressKind(event.Kind), truncatePlainText(event.Text, 80)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "activity: " + strings.Join(parts, "  |  ")
}

func (m attachModel) primaryProgressEvent() (application.TransientProgressEvent, bool) {
	for _, agent := range m.agents {
		if event, ok := m.progressByAgent[agent.ID]; ok && strings.TrimSpace(event.Text) != "" {
			return event, true
		}
	}
	return application.TransientProgressEvent{}, false
}

func (m attachModel) primaryPendingAgent() (domain.AgentID, bool) {
	for _, state := range []string{"reasoning", "thinking"} {
		for _, agent := range m.agents {
			if m.pendingAgentStates[agent.ID] == state {
				return agent.ID, true
			}
		}
	}
	return "", false
}

func (m attachModel) activeReasoningPane() (string, string, bool) {
	if event, ok := m.primaryProgressEvent(); ok {
		return string(event.AgentID) + " " + displayProgressKind(event.Kind), truncatePlainText(event.Text, 80), true
	}
	return "", "", false
}

func displayProgressKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "progress"
	}
	return kind
}

func (m attachModel) spinnerFrameGlyph() string {
	if len(attachSpinnerFrames) == 0 {
		return "-"
	}
	return attachSpinnerFrames[m.spinnerFrame%len(attachSpinnerFrames)]
}

func (m attachModel) primaryActiveTaskSummary() string {
	if len(m.room.tasks) == 0 {
		return ""
	}
	now := time.Now().UTC()
	tasks := append([]application.SandboxTask(nil), m.room.tasks...)
	sort.Slice(tasks, func(i, j int) bool {
		left := tasks[i].CreatedAt
		if tasks[i].StartedAt != nil {
			left = *tasks[i].StartedAt
		}
		right := tasks[j].CreatedAt
		if tasks[j].StartedAt != nil {
			right = *tasks[j].StartedAt
		}
		return left.Before(right)
	})
	for _, task := range tasks {
		if task.Status != application.SandboxTaskStatusPending && task.Status != application.SandboxTaskStatusRunning {
			continue
		}
		return formatActiveTaskLine(task, now)
	}
	return ""
}

func summarizeMessageCounts(messages []domain.Message) (map[string]int, int) {
	counts := make(map[string]int)
	total := 0
	for _, message := range messages {
		counts[senderNameForMessage(message)]++
		total++
	}
	return counts, total
}

func trimForSidebar(value string) string {
	value = sanitizeLiveText(value)
	if len(value) <= 26 {
		return value
	}
	return value[:23] + "..."
}

func (m attachModel) lookupAgentColor(agentID string) string {
	if color, exists := m.ui.AgentColors[agentID]; exists {
		return color
	}
	if color, exists := m.agentColors[agentID]; exists {
		return color
	}
	switch agentID {
	case "operator":
		return "#f97316"
	case "system":
		return "#fbbf24"
	case "task":
		return "#a78bfa"
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(agentID))
	var seed [8]byte
	binary.LittleEndian.PutUint64(seed[:], m.colorSeed)
	_, _ = hash.Write(seed[:])
	value := hash.Sum64()
	color := fmt.Sprintf("#%02x%02x%02x", 96+int(value&0x5f), 96+int((value>>8)&0x5f), 96+int((value>>16)&0x5f))
	m.agentColors[agentID] = color
	return color
}
