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
	body := strings.ToLower(message.Body)
	return strings.Contains(body, strings.ToLower(string(agentID)))
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

	return result, nil
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
