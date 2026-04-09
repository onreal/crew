package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"crew/internal/application"
	"crew/internal/domain"
)

type localStubOrchestrator struct{}

func (localStubOrchestrator) SelectNext(_ context.Context, state application.ConversationState, candidates []domain.Agent) (application.OrchestrationDecision, error) {
	if len(candidates) == 0 {
		return application.OrchestrationDecision{Strategy: state.Mode}, nil
	}

	sortedAll := slices.Clone(candidates)
	if len(state.AllAgents) > 0 {
		sortedAll = slices.Clone(state.AllAgents)
	}
	slices.SortFunc(sortedAll, compareAgents)

	orderedAll := orderCandidates(state, sortedAll)
	candidateByID := make(map[domain.AgentID]domain.Agent, len(candidates))
	for _, agent := range candidates {
		candidateByID[agent.ID] = agent
	}

	ordered := make([]domain.Agent, 0, len(candidates))
	for _, agent := range orderedAll {
		candidate, exists := candidateByID[agent.ID]
		if !exists {
			continue
		}
		ordered = append(ordered, candidate)
	}
	if len(ordered) == 0 {
		ordered = slices.Clone(candidates)
		slices.SortFunc(ordered, compareAgents)
	}

	orderedIDs := make([]domain.AgentID, 0, len(ordered))
	for _, agent := range ordered {
		orderedIDs = append(orderedIDs, agent.ID)
	}

	return application.OrchestrationDecision{
		Selected:            []domain.Agent{ordered[0]},
		OrderedCandidateIDs: orderedIDs,
		Strategy:            normalizeOrchestrationMode(state.Mode),
	}, nil
}

func compareAgents(a, b domain.Agent) int {
	if a.Policies.Priority > b.Policies.Priority {
		return -1
	}
	if a.Policies.Priority < b.Policies.Priority {
		return 1
	}
	if a.Policies.Weight > b.Policies.Weight {
		return -1
	}
	if a.Policies.Weight < b.Policies.Weight {
		return 1
	}
	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}

func orderCandidates(state application.ConversationState, sorted []domain.Agent) []domain.Agent {
	mode := normalizeOrchestrationMode(state.Mode)
	switch mode {
	case application.OrchestrationModeRoundRobin:
		return roundRobinCandidates(state, sorted)
	case application.OrchestrationModeMentionedFirst:
		return mentionedFirstCandidates(state, sorted)
	default:
		return deterministicCandidates(state, sorted)
	}
}

func deterministicCandidates(state application.ConversationState, sorted []domain.Agent) []domain.Agent {
	if state.LastMessage == nil || state.LastMessage.Sender.Type != domain.MessageSenderTypeAgent {
		return sorted
	}

	lastSender := domain.AgentID(state.LastMessage.Sender.ID)
	ordered := slices.Clone(sorted)
	slices.SortStableFunc(ordered, func(a, b domain.Agent) int {
		if a.ID == lastSender && b.ID != lastSender {
			return 1
		}
		if b.ID == lastSender && a.ID != lastSender {
			return -1
		}
		return 0
	})
	return ordered
}

func roundRobinCandidates(state application.ConversationState, sorted []domain.Agent) []domain.Agent {
	if state.LastMessage == nil || state.LastMessage.Sender.Type != domain.MessageSenderTypeAgent {
		return sorted
	}

	lastSender := domain.AgentID(state.LastMessage.Sender.ID)
	start := 0
	for idx, agent := range sorted {
		if agent.ID == lastSender {
			start = (idx + 1) % len(sorted)
			break
		}
	}

	ordered := make([]domain.Agent, 0, len(sorted))
	ordered = append(ordered, sorted[start:]...)
	ordered = append(ordered, sorted[:start]...)
	return ordered
}

func mentionedFirstCandidates(state application.ConversationState, sorted []domain.Agent) []domain.Agent {
	if state.LastMessage == nil {
		return sorted
	}

	mentioned := make([]domain.Agent, 0, len(sorted))
	others := make([]domain.Agent, 0, len(sorted))
	for _, agent := range sorted {
		if messageTargetsAgent(*state.LastMessage, agent.ID) {
			mentioned = append(mentioned, agent)
			continue
		}
		others = append(others, agent)
	}

	if len(mentioned) == 0 {
		return deterministicCandidates(state, sorted)
	}

	ordered := make([]domain.Agent, 0, len(sorted))
	ordered = append(ordered, mentioned...)
	ordered = append(ordered, others...)
	return ordered
}

func messageTargetsAgent(message domain.Message, agentID domain.AgentID) bool {
	for _, target := range message.ToAgentIDs {
		if target == agentID {
			return true
		}
	}
	if message.Sender.Type == domain.MessageSenderTypeAgent {
		return bodyMentionsAgent(message.Body, agentID)
	}
	return bodyMentionsAgent(message.Body, agentID)
}

func bodyMentionsAgent(body string, agentID domain.AgentID) bool {
	body = strings.ToLower(body)
	target := "@" + strings.ToLower(string(agentID))
	start := 0
	for {
		idx := strings.Index(body[start:], target)
		if idx < 0 {
			return false
		}
		idx += start
		end := idx + len(target)
		if end == len(body) || !isAgentIdentifierChar(body[end]) {
			return true
		}
		start = end
	}
}

func isAgentIdentifierChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-'
}

func normalizeOrchestrationMode(mode application.OrchestrationMode) application.OrchestrationMode {
	if mode == "" {
		return application.OrchestrationModeDeterministic
	}
	return mode
}

type localStubLLMProvider struct{}

func (localStubLLMProvider) Generate(_ context.Context, request application.GenerationRequest) (application.GenerationResult, error) {
	if len(request.Messages) == 0 {
		return application.GenerationResult{}, fmt.Errorf("local stub generation requires at least one message")
	}
	if result, ok := workflowStubGeneration(request); ok {
		return result, nil
	}

	return defaultStubGeneration(request), nil
}

func defaultStubGeneration(request application.GenerationRequest) application.GenerationResult {
	last := request.Messages[len(request.Messages)-1]
	body := strings.TrimSpace(last.Body)
	if body == "" {
		body = "continue"
	}

	prefix := request.Agent.Name
	if prefix == "" {
		prefix = string(request.Agent.ID)
	}

	result := application.GenerationResult{
		MessageBody: fmt.Sprintf("%s (%s): %s", prefix, request.Agent.Role, body),
		Metadata: map[string]any{
			"generated_by": "local_stub_llm",
			"agent_id":     string(request.Agent.ID),
		},
	}

	if request.Agent.Policies.AllowToolCalls && last.Sender.Type == domain.MessageSenderTypeUser {
		if instruction, ok := extractSandboxInstruction(body); ok {
			result.MessageBody = fmt.Sprintf("%s (%s): delegating sandbox task: %s", prefix, request.Agent.Role, instruction)
			result.SandboxRequest = &application.SandboxTaskRequest{
				Instruction:       instruction,
				PermissionProfile: application.SandboxPermissionPatch,
			}
			result.Metadata["delegation_mode"] = "sandbox_task"
		}
	}

	return result
}

func extractSandboxInstruction(body string) (string, bool) {
	lowered := strings.ToLower(body)
	for _, prefix := range []string{"sandbox:", "codex:", "delegate:"} {
		idx := strings.Index(lowered, prefix)
		if idx < 0 {
			continue
		}
		instruction := strings.TrimSpace(body[idx+len(prefix):])
		if instruction == "" {
			return "", false
		}
		return instruction, true
	}

	return "", false
}

func workflowStubGeneration(request application.GenerationRequest) (application.GenerationResult, bool) {
	if len(request.Agent.Policies.AllowedHandoffs) == 0 &&
		!request.Agent.Policies.CanInitiate &&
		!request.Agent.Policies.RequireDirectMention {
		return application.GenerationResult{}, false
	}
	last := request.Messages[len(request.Messages)-1]
	if request.Agent.Policies.AllowToolCalls && last.Sender.Type == domain.MessageSenderTypeUser {
		if _, ok := extractSandboxInstruction(strings.TrimSpace(last.Body)); ok {
			return application.GenerationResult{}, false
		}
	}
	role := strings.ToLower(strings.TrimSpace(request.Agent.Role))
	id := strings.ToLower(string(request.Agent.ID))
	switch {
	case role == "planner" || id == "planner":
		body, ok := plannerStubMessage(request)
		if !ok {
			return application.GenerationResult{}, false
		}
		return application.GenerationResult{MessageBody: body}, true
	case role == "writer" || id == "writer":
		body, ok := writerStubMessage(request)
		if !ok {
			return application.GenerationResult{}, false
		}
		return application.GenerationResult{MessageBody: body}, true
	case role == "reviewer" || id == "reviewer":
		body, ok := reviewerStubMessage(request)
		if !ok {
			return application.GenerationResult{}, false
		}
		return application.GenerationResult{MessageBody: body}, true
	default:
		return application.GenerationResult{}, false
	}
}

func plannerStubMessage(request application.GenerationRequest) (string, bool) {
	last := request.Messages[len(request.Messages)-1]
	lowerBody := strings.ToLower(strings.TrimSpace(last.Body))
	switch last.Sender.Type {
	case domain.MessageSenderTypeUser:
		if strings.Contains(lowerBody, "wtf") {
			return "I owe you a cleaner workflow. Tell me the product goal and I will route the next step correctly.", true
		}
		if shouldAskPlannerClarifyingQuestion(request.Messages, lowerBody) {
			return "What website do you want me to plan?", true
		}
		if isWebsitePlanningConversation(request.Messages, lowerBody) {
			return "I will create the website plan with structure, pages, booking flow, pricing, and contact details.\n@writer complete it.", true
		}
	case domain.MessageSenderTypeAgent:
		if strings.EqualFold(last.Sender.ID, "reviewer") && messageTargetsAgent(last, request.Agent.ID) {
			return "OK thank you.\n@writer please QA test based on this implementation.", true
		}
	}
	return "", false
}

func writerStubMessage(request application.GenerationRequest) (string, bool) {
	last := request.Messages[len(request.Messages)-1]
	lowerBody := strings.ToLower(strings.TrimSpace(last.Body))
	if last.Sender.Type == domain.MessageSenderTypeAgent {
		switch {
		case strings.EqualFold(last.Sender.ID, "planner") && messageTargetsAgent(last, request.Agent.ID):
			if strings.Contains(lowerBody, "qa") {
				return "I completed QA on the implementation and validated the booking, pricing, and contact flows.", true
			}
			return "I implemented the requested website flow, layout, and content structure. Task completed.\n@reviewer review the latest changes.", true
		case strings.EqualFold(last.Sender.ID, "reviewer") && messageTargetsAgent(last, request.Agent.ID):
			return "I checked the review, fixed the reported issues, and tightened the implementation.\n@reviewer please verify the latest changes.", true
		}
	}
	return "", false
}

func reviewerStubMessage(request application.GenerationRequest) (string, bool) {
	last := request.Messages[len(request.Messages)-1]
	if last.Sender.Type == domain.MessageSenderTypeAgent {
		if !messageTargetsAgent(last, request.Agent.ID) {
			return "", false
		}
		if strings.EqualFold(last.Sender.ID, "writer") {
			switch countAgentMessages(request.Messages, request.Agent.ID) {
			case 0:
				return "I reviewed the latest changes and found issues in copy clarity and form validation.\n@writer please fix them.", true
			case 1:
				return "I found minor issues only: tighten the empty-state text and button labels.\n@writer", true
			default:
				return "Planner, I completed the review and the implementation is in good shape.\n@planner", true
			}
		}
		if strings.EqualFold(last.Sender.ID, "planner") {
			return "I reviewed the latest changes and found issues in copy clarity and form validation.\n@writer please fix them.", true
		}
	}
	return "", false
}

func shouldAskPlannerClarifyingQuestion(history []domain.Message, lowerBody string) bool {
	if !isWebsitePlanningConversation(history, lowerBody) {
		return false
	}
	if strings.Contains(lowerBody, "about ") ||
		strings.Contains(lowerBody, "for ") ||
		strings.Contains(lowerBody, "car wash") ||
		strings.Contains(lowerBody, "car washing") {
		return false
	}
	return countUserMessages(history) < 2
}

func isWebsitePlanningConversation(history []domain.Message, lowerBody string) bool {
	if strings.Contains(lowerBody, "website") || strings.Contains(lowerBody, "web site") {
		return true
	}
	for _, message := range history {
		if message.Sender.Type != domain.MessageSenderTypeUser {
			continue
		}
		body := strings.ToLower(strings.TrimSpace(message.Body))
		if strings.Contains(body, "website") || strings.Contains(body, "web site") {
			return true
		}
	}
	return false
}

func countUserMessages(history []domain.Message) int {
	count := 0
	for _, message := range history {
		if message.Sender.Type == domain.MessageSenderTypeUser {
			count++
		}
	}
	return count
}

func countAgentMessages(history []domain.Message, agentID domain.AgentID) int {
	count := 0
	for _, message := range history {
		if message.Sender.Type == domain.MessageSenderTypeAgent && domain.AgentID(message.Sender.ID) == agentID {
			count++
		}
	}
	return count
}
