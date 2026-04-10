package main

import (
	"fmt"
	"strings"
	"time"

	"crew/internal/application"
	"crew/internal/domain"
)

func (m *attachModel) ensureActiveConversation() {
	if m.options.ConversationID != "" {
		m.sendConversationID = m.options.ConversationID
		return
	}
	if containsConversationID(m.room.conversations, m.sendConversationID) {
		return
	}
	if len(m.room.conversations) > 0 {
		m.sendConversationID = m.room.conversations[0]
	}
}

func containsConversationID(ids []domain.ConversationID, target domain.ConversationID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func (m *attachModel) cycleConversation(delta int) {
	if len(m.room.conversations) == 0 {
		return
	}
	idx := 0
	for i, id := range m.room.conversations {
		if id == m.sendConversationID {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(m.room.conversations)) % len(m.room.conversations)
	m.sendConversationID = m.room.conversations[idx]
	m.status = fmt.Sprintf("active conversation: %s", m.sendConversationID)
	m.syncViewportContent(false)
}

func (m *attachModel) setPendingSequence(maxSteps int) {
	m.setPendingSequenceFromHistory(activeConversationMessages(m.room.snapshot.Messages, m.sendConversationID), maxSteps)
}

func (m *attachModel) setPendingSequenceFromHistory(history []domain.Message, maxSteps int) {
	clear(m.pendingAgentStates)
	clear(m.progressByAgent)
	sequence := estimatePendingSequence(history, m.agents, m.options.Orchestration, maxSteps)
	for idx, agentID := range sequence {
		if idx == 0 {
			m.pendingAgentStates[agentID] = "thinking"
		} else {
			m.pendingAgentStates[agentID] = "queued"
		}
	}
}

func (m *attachModel) commitProgressHistory() {
	if !m.options.Reasoning || len(m.progressByAgent) == 0 {
		return
	}
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
		if len(m.progressHistory) > 0 {
			last := m.progressHistory[len(m.progressHistory)-1]
			if last.Sender == event.Sender && last.Body == event.Body && last.ProgressKind == event.ProgressKind {
				continue
			}
		}
		m.progressHistory = append(m.progressHistory, event)
	}
}

func activeConversationMessages(messages []domain.Message, conversationID domain.ConversationID) []domain.Message {
	filtered := make([]domain.Message, 0, len(messages))
	for _, message := range messages {
		if conversationID != "" && message.ConversationID != conversationID {
			continue
		}
		filtered = append(filtered, message)
	}
	return filtered
}

func estimatePendingSequence(messages []domain.Message, agents []domain.Agent, mode application.OrchestrationMode, maxSteps int) []domain.AgentID {
	history := append([]domain.Message(nil), messages...)
	sequence := make([]domain.AgentID, 0, maxSteps)
	for step := 0; step < maxSteps; step++ {
		if len(history) == 0 {
			break
		}
		lastMessage := history[len(history)-1]
		candidates := eligibleAgentsForUI(history, lastMessage, agents)
		if len(candidates) == 0 {
			break
		}
		ordered := orderCandidatesForUI(mode, agents, candidates, &lastMessage)
		if len(ordered) == 0 {
			break
		}
		selected := ordered[0]
		sequence = append(sequence, selected.ID)
		history = append(history, domain.Message{
			SessionID:      lastMessage.SessionID,
			ConversationID: lastMessage.ConversationID,
			Sender:         domain.AgentSender(selected.ID),
			Channel:        domain.MessageChannelBroadcast,
			Kind:           domain.MessageKindUtterance,
			ReplyTo:        lastMessage.ID,
			Timestamp:      time.Now().UTC(),
		})
	}
	return sequence
}

func (m attachModel) canSplitConversations() bool {
	return false
}

func (m attachModel) roomConversationScope() domain.ConversationID {
	return ""
}

func (m attachModel) roomConversationLabel() string {
	if scope := m.roomConversationScope(); scope != "" {
		return string(scope)
	}
	return "session"
}
