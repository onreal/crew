package application

import (
	"fmt"
	"slices"
	"strings"

	"crew/internal/domain"
)

const (
	TopicSessionCreated      = "session.created"
	TopicSessionUpdated      = "session.updated"
	TopicMessageDispatched   = "message.dispatched"
	TopicAgentTaskCreated    = "agent_task.created"
	TopicAgentTaskUpdated    = "agent_task.updated"
	TopicAgentHandoffCreated = "agent_handoff.created"
	TopicWorkflowRegistered  = "workflow.registered"
	TopicWorkflowProgressed  = "workflow.progressed"
)

type CreateSessionCommand struct {
	Mode         domain.SessionMode
	ActorCatalog string
}

func (c CreateSessionCommand) Validate() error {
	if c.ActorCatalog != "" && c.ActorCatalog != strings.TrimSpace(c.ActorCatalog) {
		return fmt.Errorf("session actor catalog must not contain surrounding whitespace")
	}
	return c.Mode.Validate()
}

type SessionIDCommand struct {
	SessionID domain.SessionID
}

func (c SessionIDCommand) Validate() error {
	return c.SessionID.Validate()
}

type GetSessionQuery struct {
	SessionID domain.SessionID
}

func (q GetSessionQuery) Validate() error {
	return q.SessionID.Validate()
}

type DispatchMessageCommand struct {
	SessionID      domain.SessionID
	ConversationID domain.ConversationID
	Sender         domain.MessageSender
	ToAgentIDs     []domain.AgentID
	Channel        domain.MessageChannel
	Kind           domain.MessageKind
	Body           string
	ReplyTo        domain.MessageID
	Metadata       map[string]any
	Policy         *domain.ConversationPolicy
}

func (c DispatchMessageCommand) Validate() error {
	if err := c.SessionID.Validate(); err != nil {
		return err
	}

	if err := c.ConversationID.Validate(); err != nil {
		return err
	}

	if err := c.Sender.Validate(); err != nil {
		return err
	}

	if err := c.Channel.Validate(); err != nil {
		return err
	}

	if err := c.Kind.Validate(); err != nil {
		return err
	}

	if strings.TrimSpace(c.Body) == "" {
		return fmt.Errorf("dispatch message body must not be empty")
	}

	for _, recipientID := range c.ToAgentIDs {
		if err := recipientID.Validate(); err != nil {
			return err
		}
	}

	if c.ReplyTo != "" {
		if err := c.ReplyTo.Validate(); err != nil {
			return err
		}
	}

	if c.Policy != nil {
		return c.Policy.Validate()
	}

	return nil
}

type ListSessionMessagesQuery struct {
	SessionID domain.SessionID
}

func (q ListSessionMessagesQuery) Validate() error {
	return q.SessionID.Validate()
}

type StepSessionCommand struct {
	SessionID         domain.SessionID
	ConversationID    domain.ConversationID
	OrchestrationMode OrchestrationMode
	ReplyRoutingMode  ReplyRoutingMode
}

func (c StepSessionCommand) Validate() error {
	if err := c.SessionID.Validate(); err != nil {
		return err
	}

	if c.ConversationID != "" {
		if err := c.ConversationID.Validate(); err != nil {
			return err
		}
	}

	if c.OrchestrationMode != "" {
		if err := c.OrchestrationMode.Validate(); err != nil {
			return err
		}
	}

	if c.ReplyRoutingMode != "" {
		if err := c.ReplyRoutingMode.Validate(); err != nil {
			return err
		}
	}

	return nil
}

type AutoSessionCommand struct {
	SessionID         domain.SessionID
	ConversationID    domain.ConversationID
	MaxSteps          int
	OrchestrationMode OrchestrationMode
	ReplyRoutingMode  ReplyRoutingMode
}

func (c AutoSessionCommand) Validate() error {
	if err := c.SessionID.Validate(); err != nil {
		return err
	}

	if c.ConversationID != "" {
		if err := c.ConversationID.Validate(); err != nil {
			return err
		}
	}
	if c.OrchestrationMode != "" {
		if err := c.OrchestrationMode.Validate(); err != nil {
			return err
		}
	}
	if c.ReplyRoutingMode != "" {
		if err := c.ReplyRoutingMode.Validate(); err != nil {
			return err
		}
	}

	if c.MaxSteps < 1 {
		return fmt.Errorf("auto max steps must be >= 1, got %d", c.MaxSteps)
	}

	return nil
}

type VectorStatusQuery struct {
	SessionID domain.SessionID
}

func (q VectorStatusQuery) Validate() error {
	if q.SessionID == "" {
		return nil
	}
	return q.SessionID.Validate()
}

type VectorRebuildCommand struct {
	SessionID domain.SessionID
	Force     bool
}

func (c VectorRebuildCommand) Validate() error {
	if c.SessionID == "" {
		return nil
	}
	return c.SessionID.Validate()
}

type CreateSandboxTaskCommand struct {
	TaskID             AgentTaskID
	SessionID          domain.SessionID
	ConversationID     domain.ConversationID
	RequestedByAgentID domain.AgentID
	AssignedAgentID    domain.AgentID
	AssignedProvider   AgentProviderClass
	RuntimeName        string
	WorkspaceRoot      string
	SandboxRoot        string
	PermissionProfile  SandboxPermissionProfile
	Instruction        string
	Metadata           map[string]any
}

func (c CreateSandboxTaskCommand) Validate() error {
	if err := c.TaskID.Validate(); err != nil {
		return err
	}
	if err := c.SessionID.Validate(); err != nil {
		return err
	}
	if err := c.ConversationID.Validate(); err != nil {
		return err
	}
	if c.RequestedByAgentID != "" {
		if err := c.RequestedByAgentID.Validate(); err != nil {
			return err
		}
	}
	if c.AssignedAgentID != "" {
		if err := c.AssignedAgentID.Validate(); err != nil {
			return err
		}
	}
	if err := c.AssignedProvider.Validate(); err != nil {
		return err
	}
	if c.AssignedProvider != AgentProviderClassSandboxedRuntime {
		return fmt.Errorf("sandbox tasks must target provider class %q, got %q", AgentProviderClassSandboxedRuntime, c.AssignedProvider)
	}
	if strings.TrimSpace(c.RuntimeName) == "" {
		return fmt.Errorf("sandbox runtime name must not be empty")
	}
	if strings.TrimSpace(c.WorkspaceRoot) == "" {
		return fmt.Errorf("sandbox workspace root must not be empty")
	}
	if c.SandboxRoot != "" && strings.TrimSpace(c.SandboxRoot) == "" {
		return fmt.Errorf("sandbox root must not be blank when set")
	}
	if err := c.PermissionProfile.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Instruction) == "" {
		return fmt.Errorf("sandbox task instruction must not be empty")
	}
	return nil
}

type CreateAgentHandoffCommand struct {
	HandoffID       AgentHandoffID
	SessionID       domain.SessionID
	ConversationID  domain.ConversationID
	SourceMessageID domain.MessageID
	SourceTaskID    AgentTaskID
	TaskID          AgentTaskID
	FromAgentID     domain.AgentID
	ToAgentID       domain.AgentID
	ToProviderClass AgentProviderClass
	Reason          string
}

func (c CreateAgentHandoffCommand) Validate() error {
	if err := c.HandoffID.Validate(); err != nil {
		return err
	}
	if err := c.SessionID.Validate(); err != nil {
		return err
	}
	if err := c.ConversationID.Validate(); err != nil {
		return err
	}
	if c.SourceMessageID != "" {
		if err := c.SourceMessageID.Validate(); err != nil {
			return err
		}
	}
	if c.SourceTaskID != "" {
		if err := c.SourceTaskID.Validate(); err != nil {
			return err
		}
	}
	if c.SourceMessageID == "" && c.SourceTaskID == "" {
		return fmt.Errorf("agent handoff must reference at least one source message or source task")
	}
	if err := c.TaskID.Validate(); err != nil {
		return err
	}
	if c.FromAgentID != "" {
		if err := c.FromAgentID.Validate(); err != nil {
			return err
		}
	}
	if c.ToAgentID != "" {
		if err := c.ToAgentID.Validate(); err != nil {
			return err
		}
	}
	if err := c.ToProviderClass.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf("agent handoff reason must not be empty")
	}
	return nil
}

type ExecuteSandboxTaskCommand struct {
	TaskID AgentTaskID
}

func (c ExecuteSandboxTaskCommand) Validate() error {
	return c.TaskID.Validate()
}

type GetSandboxTaskQuery struct {
	TaskID AgentTaskID
}

func (q GetSandboxTaskQuery) Validate() error {
	return q.TaskID.Validate()
}

type ListSandboxTasksQuery struct {
	SessionID domain.SessionID
}

func (q ListSandboxTasksQuery) Validate() error {
	return q.SessionID.Validate()
}

type ListAgentHandoffsQuery struct {
	SessionID domain.SessionID
}

func (q ListAgentHandoffsQuery) Validate() error {
	return q.SessionID.Validate()
}

type RecallSessionMessagesQuery struct {
	SessionID domain.SessionID
	QueryText string
	Limit     int
}

func (q RecallSessionMessagesQuery) Validate() error {
	if err := q.SessionID.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(q.QueryText) == "" {
		return fmt.Errorf("recall query text must not be empty")
	}
	if q.Limit < 1 {
		return fmt.Errorf("recall limit must be >= 1, got %d", q.Limit)
	}
	return nil
}

type RegisterWorkflowCommand struct {
	Workflow domain.Workflow
}

func (c RegisterWorkflowCommand) Validate() error {
	return c.Workflow.Validate()
}

type GetWorkflowQuery struct {
	WorkflowID domain.WorkflowID
}

func (q GetWorkflowQuery) Validate() error {
	return q.WorkflowID.Validate()
}

type AdvanceWorkflowCommand struct {
	WorkflowID       domain.WorkflowID
	CurrentStepID    domain.WorkflowStepID
	CompletedStepIDs []domain.WorkflowStepID
}

func (c AdvanceWorkflowCommand) Validate() error {
	if err := c.WorkflowID.Validate(); err != nil {
		return err
	}

	if c.CurrentStepID != "" {
		if err := c.CurrentStepID.Validate(); err != nil {
			return err
		}
	}

	seen := make(map[domain.WorkflowStepID]struct{}, len(c.CompletedStepIDs))
	for _, stepID := range c.CompletedStepIDs {
		if err := stepID.Validate(); err != nil {
			return err
		}

		if _, exists := seen[stepID]; exists {
			return fmt.Errorf("completed workflow step IDs must be unique, duplicate %q", stepID)
		}

		seen[stepID] = struct{}{}
	}

	return nil
}

type SessionCreatedEvent struct {
	Session domain.Session
}

type SessionUpdatedEvent struct {
	Session domain.Session
}

type MessageDispatchedEvent struct {
	Message domain.Message
}

type AgentTaskCreatedEvent struct {
	Task SandboxTask
}

type AgentTaskUpdatedEvent struct {
	Task SandboxTask
}

type AgentHandoffCreatedEvent struct {
	Handoff AgentHandoff
}

type VectorRecallResult struct {
	Message  domain.Message
	Distance *float64
	Strategy string
}

type VectorRecallResponse struct {
	SessionID      domain.SessionID
	QueryText      string
	Results        []VectorRecallResult
	FallbackUsed   bool
	FallbackReason string
	BackendStatus  VectorIndexStatus
	IndexState     VectorIndexState
}

type SessionStepResult struct {
	SessionID           domain.SessionID
	ConversationID      domain.ConversationID
	Stepped             bool
	Reason              string
	OrchestrationMode   OrchestrationMode
	ReplyRoutingMode    ReplyRoutingMode
	EligibleAgentIDs    []domain.AgentID
	BlockedAgents       []BlockedAgent
	OrderedCandidateIDs []domain.AgentID
	Agent               *domain.Agent
	Message             *domain.Message
	SandboxTask         *SandboxTask
	SandboxHandoff      *AgentHandoff
	TaskMessages        []domain.Message
}

type BlockedAgent struct {
	AgentID domain.AgentID
	Reason  string
}

type SessionAutoResult struct {
	SessionID              domain.SessionID
	ConversationID         domain.ConversationID
	CompletedSteps         int
	SelectedAgentIDs       []domain.AgentID
	ReplyRoutingMode       ReplyRoutingMode
	Steps                  []SessionStepResult
	StopReason             string
	VectorStateMarkedStale bool
}

type WorkflowRegisteredEvent struct {
	Workflow domain.Workflow
}

type WorkflowProgressedEvent struct {
	WorkflowID       domain.WorkflowID
	CurrentStep      domain.WorkflowStep
	ReadyNextSteps   []domain.WorkflowStep
	BlockedNextSteps []domain.WorkflowStep
	Terminal         bool
}

type WorkflowProgression struct {
	Workflow         domain.Workflow
	CurrentStep      domain.WorkflowStep
	ReadyNextSteps   []domain.WorkflowStep
	BlockedNextSteps []domain.WorkflowStep
	Terminal         bool
}

func cloneAgentIDs(ids []domain.AgentID) []domain.AgentID {
	return slices.Clone(ids)
}
