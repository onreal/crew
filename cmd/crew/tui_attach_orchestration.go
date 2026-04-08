package main

import (
	"strings"

	"crew/internal/application"
	"crew/internal/domain"
)

func attachDispatchRouting(recipients []domain.AgentID) (domain.MessageChannel, *domain.ConversationPolicy) {
	if len(recipients) == 0 {
		return domain.MessageChannelUser, nil
	}
	policy := domain.DefaultConversationPolicy()
	policy.RequireReplyTargetForDirect = false
	return domain.MessageChannelDirect, &policy
}

func effectiveAttachAutoSteps(configured int, recipients []domain.AgentID) int {
	return max(configured, len(recipients))
}

func mentionedAgentIDs(body string, agents []domain.Agent) []domain.AgentID {
	matches := mentionedAgentSet(body, agents)
	if len(matches) == 0 {
		return nil
	}
	runes := []rune(body)
	ids := make([]domain.AgentID, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for idx := 0; idx < len(runes); idx++ {
		if runes[idx] != '@' {
			continue
		}
		start, end := idx+1, idx+1
		for end < len(runes) && !isInputAssistWhitespace(runes[end]) {
			end++
		}
		if start == end {
			continue
		}
		token := strings.ToLower(string(runes[start:end]))
		agentID, exists := matches[token]
		if !exists {
			continue
		}
		if _, exists := seen[string(agentID)]; exists {
			continue
		}
		seen[string(agentID)] = struct{}{}
		ids = append(ids, agentID)
	}
	return ids
}

func mentionedAgentSet(body string, agents []domain.Agent) map[string]domain.AgentID {
	lookup := make(map[string]domain.AgentID, len(agents))
	for _, agent := range agents {
		lookup[strings.ToLower(string(agent.ID))] = agent.ID
	}
	matches := make(map[string]domain.AgentID)
	for _, field := range strings.Fields(body) {
		token := sanitizeMentionLookupToken(field)
		if token == "" {
			continue
		}
		if id, exists := lookup[strings.ToLower(token)]; exists {
			matches[strings.ToLower(token)] = id
		}
	}
	return matches
}

func sanitizeMentionLookupToken(field string) string {
	if !strings.HasPrefix(field, "@") || len(field) < 2 {
		return ""
	}
	token := strings.TrimLeft(field, "@")
	token = strings.TrimFunc(token, func(r rune) bool { return !isMentionTokenRune(r) })
	if token == "" {
		return ""
	}
	for _, r := range token {
		if !isMentionTokenRune(r) {
			return ""
		}
	}
	return token
}

func isMentionTokenRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

func eligibleAgentsForUI(history []domain.Message, lastMessage domain.Message, agents []domain.Agent) []domain.Agent {
	candidates := make([]domain.Agent, 0, len(agents))
	for _, agent := range agents {
		if agentIneligibleForUI(agent, history, lastMessage) {
			continue
		}
		candidates = append(candidates, agent)
	}
	return candidates
}

func agentIneligibleForUI(agent domain.Agent, history []domain.Message, lastMessage domain.Message) bool {
	if lastMessage.Sender.Type == domain.MessageSenderTypeAgent && lastMessage.Sender.ID == string(agent.ID) {
		limit := agent.Policies.MaxConsecutiveTurns
		if policyLimit := domain.DefaultConversationPolicy().MaxConsecutiveTurnsPerAgent; policyLimit < limit {
			limit = policyLimit
		}
		if consecutiveAgentTurnsForUI(history, agent.ID) >= limit {
			return true
		}
	}
	if agent.Policies.RequireDirectMention && !messageTargetsAgentForUI(lastMessage, agent.ID) {
		return true
	}
	if lastMessage.Channel == domain.MessageChannelBroadcast && !agent.Policies.AllowBroadcast {
		return true
	}
	if lastMessage.Channel == domain.MessageChannelDirect && len(lastMessage.ToAgentIDs) > 0 && !containsAgentIDForUI(lastMessage.ToAgentIDs, agent.ID) {
		return true
	}
	return false
}

func consecutiveAgentTurnsForUI(history []domain.Message, agentID domain.AgentID) int {
	count := 0
	for idx := len(history) - 1; idx >= 0; idx-- {
		message := history[idx]
		if message.Sender.Type != domain.MessageSenderTypeAgent || message.Sender.ID != string(agentID) {
			break
		}
		count++
	}
	return count
}

func containsAgentIDForUI(ids []domain.AgentID, target domain.AgentID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func messageTargetsAgentForUI(message domain.Message, agentID domain.AgentID) bool {
	if containsAgentIDForUI(message.ToAgentIDs, agentID) {
		return true
	}
	return strings.Contains(strings.ToLower(message.Body), strings.ToLower(string(agentID)))
}

func orderCandidatesForUI(
	mode application.OrchestrationMode,
	allAgents []domain.Agent,
	candidates []domain.Agent,
	lastMessage *domain.Message,
) []domain.Agent {
	all := append([]domain.Agent(nil), allAgents...)
	if len(all) == 0 {
		all = append([]domain.Agent(nil), candidates...)
	}
	sortAgentsForUI(all)
	orderedAll := reorderAgentsForModeForUI(mode, all, lastMessage)
	candidateMap := make(map[domain.AgentID]domain.Agent, len(candidates))
	for _, candidate := range candidates {
		candidateMap[candidate.ID] = candidate
	}

	ordered := make([]domain.Agent, 0, len(candidates))
	for _, agent := range orderedAll {
		if candidate, exists := candidateMap[agent.ID]; exists {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}

func sortAgentsForUI(agents []domain.Agent) {
	for i := 0; i < len(agents); i++ {
		for j := i + 1; j < len(agents); j++ {
			if compareAgentsForUI(agents[j], agents[i]) < 0 {
				agents[i], agents[j] = agents[j], agents[i]
			}
		}
	}
}

func compareAgentsForUI(a, b domain.Agent) int {
	switch {
	case a.Policies.Priority > b.Policies.Priority:
		return -1
	case a.Policies.Priority < b.Policies.Priority:
		return 1
	case a.Policies.Weight > b.Policies.Weight:
		return -1
	case a.Policies.Weight < b.Policies.Weight:
		return 1
	case a.ID < b.ID:
		return -1
	case a.ID > b.ID:
		return 1
	default:
		return 0
	}
}

func reorderAgentsForModeForUI(mode application.OrchestrationMode, agents []domain.Agent, lastMessage *domain.Message) []domain.Agent {
	ordered := append([]domain.Agent(nil), agents...)
	if lastMessage == nil {
		return ordered
	}
	switch normalizeUIMode(mode) {
	case application.OrchestrationModeRoundRobin:
		if lastMessage.Sender.Type != domain.MessageSenderTypeAgent {
			return ordered
		}
		start := 0
		for idx, agent := range ordered {
			if agent.ID == domain.AgentID(lastMessage.Sender.ID) {
				start = (idx + 1) % len(ordered)
				break
			}
		}
		return append(append([]domain.Agent(nil), ordered[start:]...), ordered[:start]...)
	case application.OrchestrationModeMentionedFirst:
		mentioned := make([]domain.Agent, 0, len(ordered))
		others := make([]domain.Agent, 0, len(ordered))
		for _, agent := range ordered {
			if messageTargetsAgentForUI(*lastMessage, agent.ID) {
				mentioned = append(mentioned, agent)
			} else {
				others = append(others, agent)
			}
		}
		if len(mentioned) == 0 {
			return reorderAgentsForModeForUI(application.OrchestrationModeDeterministic, ordered, lastMessage)
		}
		return append(mentioned, others...)
	default:
		if lastMessage.Sender.Type != domain.MessageSenderTypeAgent {
			return ordered
		}
		lastSender := domain.AgentID(lastMessage.Sender.ID)
		head := make([]domain.Agent, 0, len(ordered))
		tail := make([]domain.Agent, 0, 1)
		for _, agent := range ordered {
			if agent.ID == lastSender {
				tail = append(tail, agent)
			} else {
				head = append(head, agent)
			}
		}
		return append(head, tail...)
	}
}

func normalizeUIMode(mode application.OrchestrationMode) application.OrchestrationMode {
	if mode == "" {
		return application.OrchestrationModeDeterministic
	}
	return mode
}
