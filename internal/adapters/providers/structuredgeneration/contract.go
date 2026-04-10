package structuredgeneration

import (
	"encoding/json"
	"strings"

	"crew/internal/application"
	"crew/internal/domain"
)

func SystemInstruction(agent domain.Agent) string {
	prompt := strings.TrimSpace(agent.SystemPrompt)
	if prompt == "" {
		prompt = "Respond with the next single message body for the conversation."
	}

	builder := strings.Builder{}
	builder.WriteString("You are agent ")
	builder.WriteString(agent.Name)
	builder.WriteString(" (")
	builder.WriteString(agent.Role)
	builder.WriteString(").\n")
	builder.WriteString(prompt)
	builder.WriteString("\nTreat any `@agent` mention as a real handoff or routing action.")
	builder.WriteString("\nUse `@agent` only when you are actively handing work to that agent in this reply.")
	builder.WriteString("\nAny exact `@agent` token anywhere in the message body will be treated as a real mention.")
	builder.WriteString("\nIf you intend to hand work to another agent, you must use the exact `@agent` handle. A bare name like `writer` or `reviewer` is not enough.")
	builder.WriteString("\nDo not mention `@agent` handles hypothetically, as examples, as options, or while asking the operator for more information.")
	builder.WriteString("\nIf you still need operator input, ask the operator directly and do not hand off yet.")
	builder.WriteString("\nReturn a JSON object with this exact shape:\n")
	builder.WriteString(`{"message_body":"...", "sandbox_request":null}`)
	if agent.Policies.AllowSandboxDelegation {
		builder.WriteString("\nIf sandbox delegation is required, set sandbox_request to ")
		builder.WriteString(`{"instruction":"...", "permission_profile":"patch"}`)
		if len(agent.Policies.AllowedSandboxRuntimes) > 0 {
			builder.WriteString("\nAllowed sandbox runtimes for this agent: ")
			builder.WriteString(strings.Join(agent.Policies.AllowedSandboxRuntimes, ", "))
		}
	} else {
		builder.WriteString("\nThis agent is not allowed to delegate sandbox work. Always set sandbox_request to null.")
	}
	builder.WriteString("\nDo not wrap the JSON in markdown fences.")
	return builder.String()
}

func TranscriptPrompt(request application.GenerationRequest) string {
	var builder strings.Builder
	builder.WriteString("Conversation transcript:\n")
	for _, message := range request.Messages {
		builder.WriteString("- ")
		builder.WriteString(formatSender(message.Sender))
		if len(message.ToAgentIDs) > 0 {
			builder.WriteString(" -> ")
			for idx, recipient := range message.ToAgentIDs {
				if idx > 0 {
					builder.WriteString(", ")
				}
				builder.WriteString(string(recipient))
			}
		}
		builder.WriteString(": ")
		builder.WriteString(strings.TrimSpace(message.Body))
		builder.WriteByte('\n')
	}
	builder.WriteString("\nWrite the next reply as ")
	builder.WriteString(request.Agent.Name)
	builder.WriteString(".")
	if request.ReplyRouting.RecipientType != "" {
		builder.WriteString("\nRoute this reply to ")
		builder.WriteString(request.ReplyRouting.RecipientType)
		if request.ReplyRouting.RecipientID != "" {
			builder.WriteString(":")
			builder.WriteString(request.ReplyRouting.RecipientID)
		}
		builder.WriteString(".")
	}
	if request.ReplyRouting.ReplyTo != "" {
		builder.WriteString("\nThread this reply to message ")
		builder.WriteString(string(request.ReplyRouting.ReplyTo))
		builder.WriteString(".")
	}

	return builder.String()
}

func ParseResult(content string) application.GenerationResult {
	var envelope generationEnvelope
	if err := json.Unmarshal([]byte(content), &envelope); err == nil {
		result := application.GenerationResult{
			MessageBody: strings.TrimSpace(envelope.MessageBody),
		}
		if envelope.SandboxRequest != nil {
			result.SandboxRequest = &application.SandboxTaskRequest{
				Instruction:       strings.TrimSpace(envelope.SandboxRequest.Instruction),
				PermissionProfile: application.SandboxPermissionProfile(strings.TrimSpace(envelope.SandboxRequest.PermissionProfile)),
			}
		}
		return result
	}

	return application.GenerationResult{
		MessageBody: strings.TrimSpace(content),
	}
}

func formatSender(sender domain.MessageSender) string {
	switch sender.Type {
	case domain.MessageSenderTypeAgent:
		return "agent:" + sender.ID
	case domain.MessageSenderTypeSystem:
		return "system:" + sender.ID
	default:
		return "user:" + sender.ID
	}
}

type generationEnvelope struct {
	MessageBody    string                  `json:"message_body"`
	SandboxRequest *sandboxRequestEnvelope `json:"sandbox_request"`
}

type sandboxRequestEnvelope struct {
	Instruction       string `json:"instruction"`
	PermissionProfile string `json:"permission_profile"`
}
