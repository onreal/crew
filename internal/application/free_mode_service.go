package application

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"crew/internal/domain"
)

type FreeModeService struct {
	sessions                 SessionRepository
	messages                 MessageRepository
	agents                   AgentRepository
	orchestrator             Orchestrator
	llm                      LLMProvider
	messageService           *MessageService
	coordination             *CoordinationService
	sourceWorkspaceRoot      string
	defaultOrchestrationMode OrchestrationMode
	defaultReplyRoutingMode  ReplyRoutingMode
}

func NewFreeModeService(
	sessions SessionRepository,
	messages MessageRepository,
	agents AgentRepository,
	orchestrator Orchestrator,
	llm LLMProvider,
	messageService *MessageService,
) *FreeModeService {
	return &FreeModeService{
		sessions:                 sessions,
		messages:                 messages,
		agents:                   agents,
		orchestrator:             orchestrator,
		llm:                      llm,
		messageService:           messageService,
		defaultOrchestrationMode: OrchestrationModeDeterministic,
		defaultReplyRoutingMode:  ReplyRoutingModeOutstandingFirst,
	}
}

func (s *FreeModeService) WithSandboxDelegation(coordination *CoordinationService, workspaceRoot string) *FreeModeService {
	s.coordination = coordination
	s.sourceWorkspaceRoot = strings.TrimSpace(workspaceRoot)
	return s
}

func (s *FreeModeService) WithDefaultOrchestrationMode(mode OrchestrationMode) *FreeModeService {
	if mode == "" {
		s.defaultOrchestrationMode = OrchestrationModeDeterministic
		return s
	}
	s.defaultOrchestrationMode = mode
	return s
}

func (s *FreeModeService) WithDefaultReplyRoutingMode(mode ReplyRoutingMode) *FreeModeService {
	if mode == "" {
		s.defaultReplyRoutingMode = ReplyRoutingModeOutstandingFirst
		return s
	}
	s.defaultReplyRoutingMode = mode
	return s
}

const (
	stepReasonNoMessages                    = "no_messages"
	stepReasonNoEligibleAgents              = "no_eligible_agents"
	stepReasonNoSelectedAgents              = "no_selected_agents"
	stepReasonEmptyGeneration               = "empty_generation"
	stepReasonPolicyMaxTurnsReached         = "policy_max_turns_reached"
	stepReasonPolicyMaxConsecutiveTurns     = "policy_max_consecutive_turns_reached"
	autoStopReasonMaxStepsReached           = "max_steps_reached"
	ineligibleReasonConsecutiveTurnsReached = "consecutive_turn_limit"
)

func (s *FreeModeService) Step(ctx context.Context, cmd StepSessionCommand) (SessionStepResult, error) {
	if err := cmd.Validate(); err != nil {
		return SessionStepResult{}, err
	}

	session, err := s.sessions.GetByID(ctx, cmd.SessionID)
	if err != nil {
		return SessionStepResult{}, err
	}
	if session.Status != domain.SessionStatusRunning {
		return SessionStepResult{}, fmt.Errorf("%w: session %q is %q, must be running to step free mode", ErrInvalidState, session.ID, session.Status)
	}
	if session.Mode != domain.SessionModeFree {
		return SessionStepResult{}, fmt.Errorf("%w: session %q is %q, must be free to step free mode", ErrInvalidState, session.ID, session.Mode)
	}

	messages, err := s.messages.ListBySessionID(ctx, cmd.SessionID)
	if err != nil {
		return SessionStepResult{}, err
	}
	conversationID, messages := resolveStepConversation(cmd.ConversationID, messages)
	mode := s.resolveOrchestrationMode(cmd.OrchestrationMode)
	replyRoutingMode := s.resolveReplyRoutingMode(cmd.ReplyRoutingMode)
	policy := domain.DefaultConversationPolicy()
	if len(messages) == 0 {
		return SessionStepResult{
			SessionID:         cmd.SessionID,
			ConversationID:    conversationID,
			Stepped:           false,
			Reason:            stepReasonNoMessages,
			OrchestrationMode: mode,
			ReplyRoutingMode:  replyRoutingMode,
		}, nil
	}
	if len(messages) >= policy.MaxTurns {
		return SessionStepResult{
			SessionID:         cmd.SessionID,
			ConversationID:    conversationID,
			Stepped:           false,
			Reason:            stepReasonPolicyMaxTurnsReached,
			OrchestrationMode: mode,
			ReplyRoutingMode:  replyRoutingMode,
		}, nil
	}

	lastMessage := messages[len(messages)-1]
	allAgents, candidates, blocked, noCandidateReason, err := s.eligibleAgents(ctx, messages, lastMessage, replyRoutingMode)
	if err != nil {
		return SessionStepResult{}, err
	}
	eligibleIDs := agentIDs(candidates)
	if len(candidates) == 0 {
		reason := stepReasonNoEligibleAgents
		if noCandidateReason != "" {
			reason = noCandidateReason
		}
		return SessionStepResult{
			SessionID:         cmd.SessionID,
			ConversationID:    conversationID,
			Stepped:           false,
			Reason:            reason,
			OrchestrationMode: mode,
			ReplyRoutingMode:  replyRoutingMode,
			EligibleAgentIDs:  eligibleIDs,
			BlockedAgents:     blocked,
		}, nil
	}

	decision, err := s.orchestrator.SelectNext(ctx, ConversationState{
		SessionID:      cmd.SessionID,
		ConversationID: conversationID,
		LastMessage:    &lastMessage,
		AllAgents:      allAgents,
		Policy:         policy,
		Mode:           mode,
	}, candidates)
	if err != nil {
		return SessionStepResult{}, err
	}
	if len(decision.Selected) == 0 {
		return SessionStepResult{
			SessionID:           cmd.SessionID,
			ConversationID:      conversationID,
			Stepped:             false,
			Reason:              stepReasonNoSelectedAgents,
			OrchestrationMode:   mode,
			ReplyRoutingMode:    replyRoutingMode,
			EligibleAgentIDs:    eligibleIDs,
			BlockedAgents:       blocked,
			OrderedCandidateIDs: append([]domain.AgentID(nil), decision.OrderedCandidateIDs...),
		}, nil
	}

	agent := decision.Selected[0]
	replyRouting := resolveGeneratedReplyRouting(replyRoutingMode, messages, agent, allAgents)
	generation, err := s.llm.Generate(ctx, GenerationRequest{
		Agent:        agent,
		Messages:     messages,
		ReplyRouting: replyRouting.Generation,
	})
	if err != nil {
		return SessionStepResult{}, err
	}
	generation.MessageBody = normalizeBareAgentHandoff(generation.MessageBody, agent)
	if strings.TrimSpace(generation.MessageBody) == "" {
		return SessionStepResult{
			SessionID:           cmd.SessionID,
			ConversationID:      conversationID,
			Stepped:             false,
			Reason:              stepReasonEmptyGeneration,
			OrchestrationMode:   mode,
			ReplyRoutingMode:    replyRoutingMode,
			EligibleAgentIDs:    eligibleIDs,
			BlockedAgents:       blocked,
			OrderedCandidateIDs: append([]domain.AgentID(nil), decision.OrderedCandidateIDs...),
			Agent:               &agent,
		}, nil
	}
	if err := validateGeneratedSandboxRequest(generation); err != nil {
		return SessionStepResult{}, err
	}

	message, err := s.messageService.Dispatch(ctx, DispatchMessageCommand{
		SessionID:      cmd.SessionID,
		ConversationID: conversationID,
		Sender:         domain.AgentSender(agent.ID),
		ToAgentIDs:     append([]domain.AgentID(nil), replyRouting.ToAgentIDs...),
		Channel:        replyRouting.Channel,
		Kind:           domain.MessageKindUtterance,
		Body:           generation.MessageBody,
		ReplyTo:        replyRouting.ReplyTo,
		Metadata:       mergeMetadata(cloneMetadata(generation.Metadata), replyRouting.Metadata),
	})
	if err != nil {
		return SessionStepResult{}, err
	}

	result := SessionStepResult{
		SessionID:           cmd.SessionID,
		ConversationID:      conversationID,
		Stepped:             true,
		OrchestrationMode:   decision.Strategy,
		ReplyRoutingMode:    replyRoutingMode,
		EligibleAgentIDs:    eligibleIDs,
		BlockedAgents:       blocked,
		OrderedCandidateIDs: append([]domain.AgentID(nil), decision.OrderedCandidateIDs...),
		Agent:               &agent,
		Message:             &message,
	}
	delegation, err := s.maybeDelegateSandboxTask(ctx, &agent, message, generation)
	if err != nil {
		return SessionStepResult{}, err
	}
	result.SandboxTask = delegation.task
	result.SandboxHandoff = delegation.handoff
	result.TaskMessages = delegation.messages
	return result, nil
}

func (s *FreeModeService) Auto(ctx context.Context, cmd AutoSessionCommand) (SessionAutoResult, error) {
	if err := cmd.Validate(); err != nil {
		return SessionAutoResult{}, err
	}

	result := SessionAutoResult{
		SessionID:        cmd.SessionID,
		ConversationID:   cmd.ConversationID,
		ReplyRoutingMode: s.resolveReplyRoutingMode(cmd.ReplyRoutingMode),
		Steps:            make([]SessionStepResult, 0, cmd.MaxSteps),
	}

	for idx := 0; idx < cmd.MaxSteps; idx++ {
		step, err := s.Step(ctx, StepSessionCommand{
			SessionID:         cmd.SessionID,
			ConversationID:    cmd.ConversationID,
			OrchestrationMode: cmd.OrchestrationMode,
			ReplyRoutingMode:  cmd.ReplyRoutingMode,
		})
		if err != nil {
			return SessionAutoResult{}, err
		}

		if result.ConversationID == "" && step.ConversationID != "" {
			result.ConversationID = step.ConversationID
		}

		if !step.Stepped {
			result.CompletedSteps = len(result.Steps)
			result.StopReason = step.Reason
			result.VectorStateMarkedStale = result.CompletedSteps > 0
			return result, nil
		}

		result.Steps = append(result.Steps, step)
		if step.Agent != nil {
			result.SelectedAgentIDs = append(result.SelectedAgentIDs, step.Agent.ID)
		}
		if step.ConversationID != "" {
			result.ConversationID = step.ConversationID
		}
	}

	result.CompletedSteps = len(result.Steps)
	result.StopReason = autoStopReasonMaxStepsReached
	result.VectorStateMarkedStale = result.CompletedSteps > 0
	return result, nil
}

type sandboxDelegationResult struct {
	task     *SandboxTask
	handoff  *AgentHandoff
	messages []domain.Message
}

func (s *FreeModeService) maybeDelegateSandboxTask(ctx context.Context, agent *domain.Agent, message domain.Message, generation GenerationResult) (sandboxDelegationResult, error) {
	if generation.SandboxRequest == nil {
		return sandboxDelegationResult{}, nil
	}
	if err := generation.SandboxRequest.Validate(); err != nil {
		return sandboxDelegationResult{}, err
	}
	if agent == nil || !agent.Policies.AllowToolCalls {
		systemMessage, dispatchErr := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, domain.MessageKindError, fmt.Sprintf("Sandbox delegation requested but agent %s is not allowed to use tool runtimes.", message.Sender.ID), map[string]any{
			"generated_by": "free_mode_sandbox_guard",
		})
		return sandboxDelegationResult{messages: append([]domain.Message(nil), systemMessage)}, dispatchErr
	}
	if !agent.Policies.AllowSandboxDelegation {
		systemMessage, dispatchErr := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, domain.MessageKindError, fmt.Sprintf("Sandbox delegation requested but agent %s is not allowed to delegate sandbox work.", message.Sender.ID), map[string]any{
			"generated_by": "free_mode_sandbox_policy",
		})
		return sandboxDelegationResult{messages: append([]domain.Message(nil), systemMessage)}, dispatchErr
	}
	runtimeName := strings.TrimSpace(agent.DelegationRuntime)
	if runtimeName == "" {
		systemMessage, dispatchErr := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, domain.MessageKindError, fmt.Sprintf("Sandbox delegation requested by %s but no delegation runtime is configured on that agent.", agent.Name), map[string]any{
			"generated_by": "free_mode_sandbox_policy",
		})
		return sandboxDelegationResult{messages: append([]domain.Message(nil), systemMessage)}, dispatchErr
	}
	if !agentAllowsSandboxRuntime(*agent, runtimeName) {
		systemMessage, dispatchErr := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, domain.MessageKindError, fmt.Sprintf("Sandbox delegation requested by %s but runtime %s is not allowed by agent policy.", agent.Name, runtimeName), map[string]any{
			"generated_by":    "free_mode_sandbox_policy",
			"sandbox_runtime": runtimeName,
		})
		return sandboxDelegationResult{messages: append([]domain.Message(nil), systemMessage)}, dispatchErr
	}
	if s.coordination == nil || s.sourceWorkspaceRoot == "" {
		systemMessage, dispatchErr := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, domain.MessageKindError, fmt.Sprintf("Sandbox delegation requested by %s but sandbox delegation is not wired with a source workspace.", agent.Name), map[string]any{
			"generated_by": "free_mode_sandbox_unavailable",
		})
		return sandboxDelegationResult{messages: append([]domain.Message(nil), systemMessage)}, dispatchErr
	}
	if !s.coordination.SupportsRuntime(runtimeName) {
		systemMessage, dispatchErr := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, domain.MessageKindError, fmt.Sprintf("Sandbox delegation requested by %s but runtime %s is not configured or available.", agent.Name, runtimeName), map[string]any{
			"generated_by":    "free_mode_sandbox_unavailable",
			"sandbox_runtime": runtimeName,
		})
		return sandboxDelegationResult{messages: append([]domain.Message(nil), systemMessage)}, dispatchErr
	}

	taskID := AgentTaskID("task-" + string(message.ID))
	task, err := s.coordination.CreateSandboxTask(ctx, CreateSandboxTaskCommand{
		TaskID:             taskID,
		SessionID:          message.SessionID,
		ConversationID:     message.ConversationID,
		RequestedByAgentID: agent.ID,
		AssignedAgentID:    agent.ID,
		AssignedProvider:   AgentProviderClassSandboxedRuntime,
		RuntimeName:        runtimeName,
		WorkspaceRoot:      s.sourceWorkspaceRoot,
		SandboxRoot:        strings.TrimSpace(agent.SandboxWorkspaceRoot),
		PermissionProfile:  generation.SandboxRequest.PermissionProfile,
		Instruction:        generation.SandboxRequest.Instruction,
		Metadata: map[string]any{
			"requested_from_message_id": string(message.ID),
			"requested_by_agent_id":     string(agent.ID),
			"reasoning_effort":          strings.TrimSpace(agent.ReasoningEffort),
			"delegation_runtime":        runtimeName,
			"sandbox_workspace_mode":    strings.TrimSpace(agent.SandboxWorkspaceMode),
		},
	})
	if err != nil {
		return sandboxDelegationResult{}, err
	}

	handoff, err := s.coordination.RecordAgentHandoff(ctx, CreateAgentHandoffCommand{
		HandoffID:       AgentHandoffID("handoff-" + string(message.ID)),
		SessionID:       message.SessionID,
		ConversationID:  message.ConversationID,
		SourceMessageID: message.ID,
		TaskID:          task.ID,
		FromAgentID:     agent.ID,
		ToProviderClass: AgentProviderClassSandboxedRuntime,
		Reason:          fmt.Sprintf("Delegated sandbox task to %s runtime", runtimeName),
	})
	if err != nil {
		return sandboxDelegationResult{}, err
	}

	taskMessages := make([]domain.Message, 0, 2)

	createdMessage, err := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, domain.MessageKindEvent, fmt.Sprintf("%s delegated sandbox task %s to %s: %s", agent.Name, task.ID, task.RuntimeName, task.Instruction), map[string]any{
		"sandbox_task_id":    string(task.ID),
		"sandbox_handoff_id": string(handoff.ID),
		"sandbox_status":     string(task.Status),
		"sandbox_runtime":    task.RuntimeName,
	})
	if err != nil {
		return sandboxDelegationResult{}, err
	}
	taskMessages = append(taskMessages, createdMessage)

	task, execErr := s.coordination.ExecuteSandboxTask(ctx, ExecuteSandboxTaskCommand{TaskID: task.ID})
	kind := domain.MessageKindEvent
	body := fmt.Sprintf("Sandbox task %s completed on %s: %s", task.ID, task.RuntimeName, sandboxResultSummary(task))
	metadata := map[string]any{
		"sandbox_task_id": string(task.ID),
		"sandbox_status":  string(task.Status),
		"sandbox_runtime": task.RuntimeName,
	}
	if task.Status == SandboxTaskStatusFailed {
		kind = domain.MessageKindError
		body = fmt.Sprintf("Sandbox task %s failed on %s: %s", task.ID, task.RuntimeName, sandboxResultSummary(task))
	}
	completedMessage, err := s.dispatchSandboxStatusMessage(ctx, message.SessionID, message.ConversationID, message.ID, kind, body, metadata)
	if err != nil {
		return sandboxDelegationResult{}, err
	}
	taskMessages = append(taskMessages, completedMessage)

	if execErr != nil {
		return sandboxDelegationResult{
			task:     &task,
			handoff:  &handoff,
			messages: taskMessages,
		}, nil
	}

	return sandboxDelegationResult{
		task:     &task,
		handoff:  &handoff,
		messages: taskMessages,
	}, nil
}

func validateGeneratedSandboxRequest(generation GenerationResult) error {
	if generation.SandboxRequest == nil {
		return nil
	}
	return generation.SandboxRequest.Validate()
}

func agentAllowsSandboxRuntime(agent domain.Agent, runtimeName string) bool {
	if runtimeName == "" {
		return false
	}
	for _, allowed := range agent.Policies.AllowedSandboxRuntimes {
		if allowed == "*" || strings.EqualFold(strings.TrimSpace(allowed), runtimeName) {
			return true
		}
	}
	return false
}

func (s *FreeModeService) dispatchSandboxStatusMessage(ctx context.Context, sessionID domain.SessionID, conversationID domain.ConversationID, replyTo domain.MessageID, kind domain.MessageKind, body string, metadata map[string]any) (domain.Message, error) {
	return s.messageService.Dispatch(ctx, DispatchMessageCommand{
		SessionID:      sessionID,
		ConversationID: conversationID,
		Sender:         domain.SystemSender("sandbox"),
		Channel:        domain.MessageChannelSystem,
		Kind:           kind,
		Body:           body,
		ReplyTo:        replyTo,
		Metadata:       cloneMetadata(metadata),
	})
}

func sandboxResultSummary(task SandboxTask) string {
	if strings.TrimSpace(task.ResultSummary) != "" {
		return strings.TrimSpace(task.ResultSummary)
	}
	if strings.TrimSpace(task.ErrorMessage) != "" {
		return strings.TrimSpace(task.ErrorMessage)
	}
	if len(task.Artifacts) == 0 {
		return "no result summary"
	}

	paths := make([]string, 0, len(task.Artifacts))
	for _, artifact := range task.Artifacts {
		paths = append(paths, artifact.Path)
	}
	return fmt.Sprintf("%d changed files: %s", len(task.Artifacts), strings.Join(paths, ", "))
}

func resolveStepConversation(requested domain.ConversationID, messages []domain.Message) (domain.ConversationID, []domain.Message) {
	if len(messages) == 0 {
		return requested, nil
	}

	conversationID := requested
	if conversationID == "" {
		conversationID = messages[len(messages)-1].ConversationID
	}

	filtered := make([]domain.Message, 0, len(messages))
	for _, message := range messages {
		if message.ConversationID != conversationID {
			continue
		}
		filtered = append(filtered, message)
	}

	return conversationID, filtered
}

func (s *FreeModeService) eligibleAgents(ctx context.Context, history []domain.Message, lastMessage domain.Message, replyRoutingMode ReplyRoutingMode) ([]domain.Agent, []domain.Agent, []BlockedAgent, string, error) {
	agents, err := s.agents.List(ctx)
	if err != nil {
		return nil, nil, nil, "", err
	}

	directWindow := activeDirectReplyWindow(history)
	obligationRequirement := outstandingReplyRequirementForHistory(replyRoutingMode, history, agents)
	candidates := make([]domain.Agent, 0, len(agents))
	blocked := make([]BlockedAgent, 0)
	blockedByConsecutive := false
	blockedByOtherRule := false
	for _, agent := range agents {
		reason := agentIneligibleReason(agent, history, lastMessage, directWindow, obligationRequirement)
		if reason != "" {
			blocked = append(blocked, BlockedAgent{AgentID: agent.ID, Reason: reason})
			if reason == ineligibleReasonConsecutiveTurnsReached {
				blockedByConsecutive = true
			} else {
				blockedByOtherRule = true
			}
			continue
		}
		candidates = append(candidates, agent)
	}

	slices.SortFunc(agents, func(a, b domain.Agent) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	slices.SortFunc(candidates, func(a, b domain.Agent) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})

	stopReason := ""
	if len(candidates) == 0 && blockedByConsecutive && !blockedByOtherRule {
		stopReason = stepReasonPolicyMaxConsecutiveTurns
	}

	return agents, candidates, blocked, stopReason, nil
}

type directReplyWindow struct {
	AnchorMessage domain.Message
	Remaining     []domain.AgentID
	Active        bool
}

func (s *FreeModeService) resolveOrchestrationMode(mode OrchestrationMode) OrchestrationMode {
	if mode != "" {
		return mode
	}
	if s.defaultOrchestrationMode != "" {
		return s.defaultOrchestrationMode
	}
	return OrchestrationModeDeterministic
}

func (s *FreeModeService) resolveReplyRoutingMode(mode ReplyRoutingMode) ReplyRoutingMode {
	if mode != "" {
		return mode
	}
	if s.defaultReplyRoutingMode != "" {
		return s.defaultReplyRoutingMode
	}
	return ReplyRoutingModeOutstandingFirst
}

func agentIDs(agents []domain.Agent) []domain.AgentID {
	ids := make([]domain.AgentID, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	return ids
}

func agentIneligibleReason(agent domain.Agent, history []domain.Message, lastMessage domain.Message, directWindow directReplyWindow, obligationRequirement outstandingReplyRequirement) string {
	if obligationRequirement.Active {
		if _, exists := obligationRequirement.RequiredByResponderID[agent.ID]; !exists {
			return ineligibleReasonAwaitingOlderObligation
		}
		return ""
	}

	if lastMessage.Sender.Type == domain.MessageSenderTypeAgent && lastMessage.Sender.ID == string(agent.ID) {
		limit := agent.Policies.MaxConsecutiveTurns
		if policyLimit := domain.DefaultConversationPolicy().MaxConsecutiveTurnsPerAgent; policyLimit < limit {
			limit = policyLimit
		}
		if consecutiveAgentTurns(history, agent.ID) >= limit {
			return ineligibleReasonConsecutiveTurnsReached
		}
	}
	if !directWindow.Active && lastMessage.Sender.Type == domain.MessageSenderTypeAgent {
		if !messageHandsOffToAgent(lastMessage, agent.ID) && !canCloseCompletedDirectReplyToUser(history, agent) {
			return "no_handoff"
		}
	}

	eligibilityMessage := lastMessage
	if directWindow.Active {
		if !containsAgentID(directWindow.Remaining, agent.ID) {
			return "not_targeted"
		}
		eligibilityMessage = directWindow.AnchorMessage
	}

	if eligibilityMessage.Sender.Type == domain.MessageSenderTypeUser &&
		eligibilityMessage.Channel != domain.MessageChannelDirect &&
		!agent.Policies.CanInitiate &&
		!messageMentionsAgent(eligibilityMessage, agent.ID) {
		return "initiation_not_allowed"
	}

	if agent.Policies.RequireDirectMention && !messageMentionsAgent(eligibilityMessage, agent.ID) {
		return "mention_required"
	}

	if eligibilityMessage.Channel == domain.MessageChannelBroadcast && !agent.Policies.AllowBroadcast {
		return "broadcast_not_allowed"
	}

	if eligibilityMessage.Channel == domain.MessageChannelDirect && len(eligibilityMessage.ToAgentIDs) > 0 && !containsAgentID(eligibilityMessage.ToAgentIDs, agent.ID) {
		return "not_targeted"
	}

	return ""
}

func canCloseCompletedDirectReplyToUser(history []domain.Message, agent domain.Agent) bool {
	_, ok := upstreamUserRoutingForCompletedDirectReply(ReplyRoutingModeOutstandingFirst, history, agent)
	return ok
}

func activeDirectReplyWindow(history []domain.Message) directReplyWindow {
	lastUserIndex := -1
	for idx := len(history) - 1; idx >= 0; idx-- {
		if history[idx].Sender.Type == domain.MessageSenderTypeUser {
			lastUserIndex = idx
			break
		}
	}
	if lastUserIndex < 0 {
		return directReplyWindow{}
	}

	anchor := history[lastUserIndex]
	if anchor.Channel != domain.MessageChannelDirect || len(anchor.ToAgentIDs) == 0 {
		return directReplyWindow{}
	}

	replied := make(map[domain.AgentID]struct{}, len(anchor.ToAgentIDs))
	for _, message := range history[lastUserIndex+1:] {
		if message.Sender.Type != domain.MessageSenderTypeAgent {
			continue
		}
		replied[domain.AgentID(message.Sender.ID)] = struct{}{}
	}

	remaining := make([]domain.AgentID, 0, len(anchor.ToAgentIDs))
	for _, recipient := range anchor.ToAgentIDs {
		if _, exists := replied[recipient]; exists {
			continue
		}
		remaining = append(remaining, recipient)
	}
	if len(remaining) == 0 {
		return directReplyWindow{}
	}

	return directReplyWindow{
		AnchorMessage: anchor,
		Remaining:     remaining,
		Active:        true,
	}
}

func consecutiveAgentTurns(history []domain.Message, agentID domain.AgentID) int {
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

func messageMentionsAgent(message domain.Message, agentID domain.AgentID) bool {
	if containsAgentID(message.ToAgentIDs, agentID) {
		return true
	}
	return bodyMentionsAgent(message.Body, agentID)
}

func messageHandsOffToAgent(message domain.Message, agentID domain.AgentID) bool {
	if containsAgentID(message.ToAgentIDs, agentID) {
		return true
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

func normalizeBareAgentHandoff(body string, agent domain.Agent) string {
	if strings.TrimSpace(body) == "" {
		return body
	}
	for _, target := range agent.Policies.AllowedHandoffs {
		handle := "@" + string(target)
		if bodyMentionsAgent(body, target) {
			continue
		}
		pattern := regexp.MustCompile(`(^|[\n.!?]\s+)` + regexp.QuoteMeta(string(target)) + `(\s+[A-Z])`)
		body = pattern.ReplaceAllString(body, `${1}`+handle+`${2}`)
	}
	return body
}

func containsAgentID(ids []domain.AgentID, target domain.AgentID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
