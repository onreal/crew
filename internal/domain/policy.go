package domain

import "fmt"

type ConversationPolicy struct {
	MaxTurns                    int
	LoopProtectionEnabled       bool
	MaxConsecutiveTurnsPerAgent int
	AllowBroadcastMessages      bool
	RequireReplyTargetForDirect bool
}

func DefaultConversationPolicy() ConversationPolicy {
	return ConversationPolicy{
		MaxTurns:                    64,
		LoopProtectionEnabled:       true,
		MaxConsecutiveTurnsPerAgent: 2,
		AllowBroadcastMessages:      true,
		RequireReplyTargetForDirect: true,
	}
}

func (p ConversationPolicy) Validate() error {
	if p.MaxTurns < 1 {
		return fmt.Errorf("conversation max turns must be >= 1, got %d", p.MaxTurns)
	}

	if p.MaxConsecutiveTurnsPerAgent < 1 {
		return fmt.Errorf("conversation max consecutive turns per agent must be >= 1, got %d", p.MaxConsecutiveTurnsPerAgent)
	}

	return nil
}

func (p ConversationPolicy) ValidateMessage(message Message) error {
	if err := p.Validate(); err != nil {
		return err
	}

	if err := message.Validate(); err != nil {
		return err
	}

	if !p.AllowBroadcastMessages && message.Channel == MessageChannelBroadcast {
		return fmt.Errorf("broadcast messages are disabled by conversation policy")
	}

	if p.RequireReplyTargetForDirect && message.Channel == MessageChannelDirect && message.ReplyTo == "" {
		return fmt.Errorf("direct messages must specify reply_to when direct reply targeting is required")
	}

	return nil
}
