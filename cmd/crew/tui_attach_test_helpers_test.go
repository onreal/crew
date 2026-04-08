package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"crew/internal/domain"
)

func maxRenderedLineWidth(value string) int {
	maxWidth := 0
	for _, line := range strings.Split(value, "\n") {
		if width := lipgloss.Width(line); width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}

func testAttachAgent(id domain.AgentID, priority int) domain.Agent {
	return domain.Agent{
		ID:           id,
		Name:         string(id),
		Role:         "tester",
		SystemPrompt: "test prompt",
		Provider:     "local_stub",
		Model:        "gpt-test",
		Policies: domain.AgentPolicy{
			CanInitiate:         true,
			AllowBroadcast:      true,
			Priority:            priority,
			Weight:              1,
			MaxConsecutiveTurns: 1,
		},
	}
}
