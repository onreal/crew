package application

import "crew/internal/domain"

const (
	replyRecipientTypeUser         = "user"
	replyRecipientTypeAgent        = "agent"
	replyRecipientTypeConversation = "conversation"

	replyRoutingMetadataMode     = "reply_routing_mode"
	replyRoutingMetadataType     = "addressed_to_type"
	replyRoutingMetadataID       = "addressed_to_id"
	replyRoutingMetadataAnchorID = "routing_anchor_message_id"

	ineligibleReasonAwaitingOlderObligation = "awaiting_older_obligation"
)

type generatedReplyRouting struct {
	Channel    domain.MessageChannel
	ToAgentIDs []domain.AgentID
	ReplyTo    domain.MessageID
	Metadata   map[string]any
	Generation GenerationReplyRouting
}

type replyObligation struct {
	Responder     domain.AgentID
	RecipientType string
	RecipientID   string
	AnchorMessage domain.Message
	AnchorIndex   int
}

type outstandingReplyRequirement struct {
	Active                bool
	RequiredByResponderID map[domain.AgentID]replyObligation
}

func resolveReplyRoutingMode(mode ReplyRoutingMode) ReplyRoutingMode {
	if mode != "" {
		return mode
	}
	return ReplyRoutingModeOutstandingFirst
}

func outstandingReplyRequirementForHistory(mode ReplyRoutingMode, history []domain.Message, agents []domain.Agent) outstandingReplyRequirement {
	if mode != ReplyRoutingModeOutstandingFirst {
		return outstandingReplyRequirement{}
	}

	obligations := deriveOutstandingReplyObligations(history, agents)
	if len(obligations) == 0 {
		return outstandingReplyRequirement{}
	}

	earliestAnchorIndex := obligations[0].AnchorIndex
	required := make(map[domain.AgentID]replyObligation)
	for _, obligation := range obligations {
		if obligation.AnchorIndex != earliestAnchorIndex {
			continue
		}
		if _, exists := required[obligation.Responder]; exists {
			continue
		}
		required[obligation.Responder] = obligation
	}

	return outstandingReplyRequirement{
		Active:                len(required) > 0,
		RequiredByResponderID: required,
	}
}

func resolveGeneratedReplyRouting(mode ReplyRoutingMode, history []domain.Message, agent domain.Agent, agents []domain.Agent) generatedReplyRouting {
	if mode == ReplyRoutingModeOutstandingFirst {
		obligations := deriveOutstandingReplyObligations(history, agents)
		for _, obligation := range obligations {
			if obligation.Responder != agent.ID {
				continue
			}
			return routingFromObligation(mode, obligation)
		}
	}

	anchor, ok := latestConversationalMessage(history)
	if !ok {
		return generatedReplyRouting{
			Channel: domain.MessageChannelBroadcast,
			Metadata: map[string]any{
				replyRoutingMetadataMode: string(mode),
				replyRoutingMetadataType: replyRecipientTypeConversation,
			},
			Generation: GenerationReplyRouting{
				Mode:          mode,
				RecipientType: replyRecipientTypeConversation,
			},
		}
	}

	if anchor.Sender.Type == domain.MessageSenderTypeAgent && anchor.Sender.ID != string(agent.ID) {
		return generatedReplyRouting{
			Channel:    domain.MessageChannelDirect,
			ToAgentIDs: []domain.AgentID{domain.AgentID(anchor.Sender.ID)},
			ReplyTo:    anchor.ID,
			Metadata: map[string]any{
				replyRoutingMetadataMode:     string(mode),
				replyRoutingMetadataType:     replyRecipientTypeAgent,
				replyRoutingMetadataID:       anchor.Sender.ID,
				replyRoutingMetadataAnchorID: string(anchor.ID),
			},
			Generation: GenerationReplyRouting{
				Mode:          mode,
				RecipientType: replyRecipientTypeAgent,
				RecipientID:   anchor.Sender.ID,
				ReplyTo:       anchor.ID,
			},
		}
	}

	recipientID := anchor.Sender.ID
	recipientType := replyRecipientTypeConversation
	if anchor.Sender.Type == domain.MessageSenderTypeUser {
		recipientType = replyRecipientTypeUser
	}

	return generatedReplyRouting{
		Channel: domain.MessageChannelBroadcast,
		ReplyTo: anchor.ID,
		Metadata: map[string]any{
			replyRoutingMetadataMode:     string(mode),
			replyRoutingMetadataType:     recipientType,
			replyRoutingMetadataID:       recipientID,
			replyRoutingMetadataAnchorID: string(anchor.ID),
		},
		Generation: GenerationReplyRouting{
			Mode:          mode,
			RecipientType: recipientType,
			RecipientID:   recipientID,
			ReplyTo:       anchor.ID,
		},
	}
}

func routingFromObligation(mode ReplyRoutingMode, obligation replyObligation) generatedReplyRouting {
	routing := generatedReplyRouting{
		ReplyTo: obligation.AnchorMessage.ID,
		Metadata: map[string]any{
			replyRoutingMetadataMode:     string(mode),
			replyRoutingMetadataType:     obligation.RecipientType,
			replyRoutingMetadataID:       obligation.RecipientID,
			replyRoutingMetadataAnchorID: string(obligation.AnchorMessage.ID),
		},
		Generation: GenerationReplyRouting{
			Mode:          mode,
			RecipientType: obligation.RecipientType,
			RecipientID:   obligation.RecipientID,
			ReplyTo:       obligation.AnchorMessage.ID,
		},
	}

	if obligation.RecipientType == replyRecipientTypeAgent {
		routing.Channel = domain.MessageChannelDirect
		routing.ToAgentIDs = []domain.AgentID{domain.AgentID(obligation.RecipientID)}
		return routing
	}

	routing.Channel = domain.MessageChannelBroadcast
	return routing
}

func deriveOutstandingReplyObligations(history []domain.Message, agents []domain.Agent) []replyObligation {
	pending := make([]replyObligation, 0)
	for idx, message := range history {
		if message.Sender.Type == domain.MessageSenderTypeAgent {
			if matched := firstSatisfiedObligationIndex(pending, message); matched >= 0 {
				pending = append(pending[:matched], pending[matched+1:]...)
			}
		}
		pending = append(pending, obligationsFromMessage(message, idx, agents)...)
	}
	return pending
}

func obligationsFromMessage(message domain.Message, index int, agents []domain.Agent) []replyObligation {
	switch message.Sender.Type {
	case domain.MessageSenderTypeUser:
		if message.Channel != domain.MessageChannelDirect || len(message.ToAgentIDs) == 0 {
			return nil
		}

		obligations := make([]replyObligation, 0, len(message.ToAgentIDs))
		for _, recipient := range message.ToAgentIDs {
			obligations = append(obligations, replyObligation{
				Responder:     recipient,
				RecipientType: replyRecipientTypeUser,
				RecipientID:   message.Sender.ID,
				AnchorMessage: message,
				AnchorIndex:   index,
			})
		}
		return obligations
	case domain.MessageSenderTypeAgent:
		recipients := uniqueTargetedAgents(message, agents)
		if message.Channel == domain.MessageChannelDirect &&
			message.ReplyTo != "" &&
			routingMetadataString(message, replyRoutingMetadataType) == replyRecipientTypeAgent {
			filtered := recipients[:0]
			for _, recipient := range recipients {
				if containsAgentID(message.ToAgentIDs, recipient) {
					continue
				}
				filtered = append(filtered, recipient)
			}
			recipients = filtered
		}
		if len(recipients) == 0 {
			return nil
		}

		obligations := make([]replyObligation, 0, len(recipients))
		for _, recipient := range recipients {
			if recipient == domain.AgentID(message.Sender.ID) {
				continue
			}
			obligations = append(obligations, replyObligation{
				Responder:     recipient,
				RecipientType: replyRecipientTypeAgent,
				RecipientID:   message.Sender.ID,
				AnchorMessage: message,
				AnchorIndex:   index,
			})
		}
		return obligations
	default:
		return nil
	}
}

func uniqueTargetedAgents(message domain.Message, agents []domain.Agent) []domain.AgentID {
	var senderAgent domain.Agent
	senderFound := false
	for _, agent := range agents {
		if agent.ID == domain.AgentID(message.Sender.ID) {
			senderAgent = agent
			senderFound = true
			break
		}
	}

	seen := make(map[domain.AgentID]struct{})
	targets := make([]domain.AgentID, 0, len(message.ToAgentIDs))
	replyRecipient := domain.AgentID("")
	if routingMetadataString(message, replyRoutingMetadataType) == replyRecipientTypeAgent {
		replyRecipient = domain.AgentID(routingMetadataString(message, replyRoutingMetadataID))
	}
	for _, target := range message.ToAgentIDs {
		if replyRecipient != "" && target == replyRecipient {
			continue
		}
		if senderFound && !senderAgent.AllowsHandoffTo(target) {
			continue
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	for _, agent := range agents {
		if agent.ID == domain.AgentID(message.Sender.ID) {
			continue
		}
		if !messageHandsOffToAgent(message, agent.ID) {
			continue
		}
		if senderFound && !senderAgent.AllowsHandoffTo(agent.ID) {
			continue
		}
		if _, exists := seen[agent.ID]; exists {
			continue
		}
		seen[agent.ID] = struct{}{}
		targets = append(targets, agent.ID)
	}
	return targets
}

func firstSatisfiedObligationIndex(obligations []replyObligation, message domain.Message) int {
	for idx, obligation := range obligations {
		if obligation.Responder != domain.AgentID(message.Sender.ID) {
			continue
		}
		if message.ReplyTo != obligation.AnchorMessage.ID {
			continue
		}
		switch obligation.RecipientType {
		case replyRecipientTypeUser:
			if routingMetadataString(message, replyRoutingMetadataType) != replyRecipientTypeUser {
				continue
			}
			if routingMetadataString(message, replyRoutingMetadataID) != obligation.RecipientID {
				continue
			}
			return idx
		case replyRecipientTypeAgent:
			if message.Channel != domain.MessageChannelDirect {
				continue
			}
			if !containsAgentID(message.ToAgentIDs, domain.AgentID(obligation.RecipientID)) {
				continue
			}
			return idx
		}
	}
	return -1
}

func latestConversationalMessage(history []domain.Message) (domain.Message, bool) {
	for idx := len(history) - 1; idx >= 0; idx-- {
		switch history[idx].Sender.Type {
		case domain.MessageSenderTypeUser, domain.MessageSenderTypeAgent:
			return history[idx], true
		}
	}
	return domain.Message{}, false
}

func routingMetadataString(message domain.Message, key string) string {
	if message.Metadata == nil {
		return ""
	}
	value, ok := message.Metadata[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

func mergeMetadata(base map[string]any, extra map[string]any) map[string]any {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}
