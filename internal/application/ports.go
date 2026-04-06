package application

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"crew/internal/domain"
)

type EventBus interface {
	Publish(ctx context.Context, topic string, event any) error
}

type UnitOfWork interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

type EventOutbox interface {
	Add(ctx context.Context, event RecordedEvent) error
}

type SessionRepository interface {
	Save(ctx context.Context, session domain.Session) error
	GetByID(ctx context.Context, id domain.SessionID) (domain.Session, error)
}

type MessageRepository interface {
	Save(ctx context.Context, message domain.Message) error
	ListBySessionID(ctx context.Context, sessionID domain.SessionID) ([]domain.Message, error)
}

type WorkflowRepository interface {
	Save(ctx context.Context, workflow domain.Workflow) error
	GetByID(ctx context.Context, id domain.WorkflowID) (domain.Workflow, error)
}

type AgentRepository interface {
	GetByID(ctx context.Context, id domain.AgentID) (domain.Agent, error)
	List(ctx context.Context) ([]domain.Agent, error)
}

type Orchestrator interface {
	SelectNext(ctx context.Context, state ConversationState, candidates []domain.Agent) (OrchestrationDecision, error)
}

type LLMProvider interface {
	Generate(ctx context.Context, request GenerationRequest) (GenerationResult, error)
}

type SandboxedAgentRuntime interface {
	ExecuteTask(ctx context.Context, task SandboxTask) (SandboxTaskExecutionResult, error)
	SupportsRuntime(name string) bool
	ProviderClass() AgentProviderClass
}

type SandboxTaskRepository interface {
	SaveTask(ctx context.Context, task SandboxTask) error
	GetTaskByID(ctx context.Context, id AgentTaskID) (SandboxTask, error)
	ListTasksBySessionID(ctx context.Context, sessionID domain.SessionID) ([]SandboxTask, error)
	SaveHandoff(ctx context.Context, handoff AgentHandoff) error
	ListHandoffsBySessionID(ctx context.Context, sessionID domain.SessionID) ([]AgentHandoff, error)
}

type VectorIndex interface {
	Status(ctx context.Context) (VectorIndexStatus, error)
	UpsertMessageEmbedding(ctx context.Context, record MessageEmbeddingRecord) error
	DeleteMessageEmbedding(ctx context.Context, messageID domain.MessageID) error
	SearchMessages(ctx context.Context, query VectorSearchQuery) ([]VectorSearchResult, error)
}

type VectorAdmin interface {
	State(ctx context.Context) (VectorIndexState, error)
	StateForSession(ctx context.Context, sessionID domain.SessionID) (VectorIndexState, error)
	MarkSessionStale(ctx context.Context, sessionID domain.SessionID, occurredAt time.Time) error
	RebuildFromCanonicalMessages(ctx context.Context, embedder Embedder, options VectorRebuildOptions) (VectorRebuildStats, error)
}

type Embedder interface {
	EmbedText(ctx context.Context, text string) ([]float32, error)
	EmbeddingIdentity() string
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewSessionID(ctx context.Context) (domain.SessionID, error)
	NewMessageID(ctx context.Context) (domain.MessageID, error)
}

type ConversationState struct {
	SessionID      domain.SessionID
	ConversationID domain.ConversationID
	LastMessage    *domain.Message
	AllAgents      []domain.Agent
	Policy         domain.ConversationPolicy
	Mode           OrchestrationMode
}

type OrchestrationDecision struct {
	Selected            []domain.Agent
	OrderedCandidateIDs []domain.AgentID
	Strategy            OrchestrationMode
}

type GenerationRequest struct {
	Agent        domain.Agent
	Messages     []domain.Message
	ReplyRouting GenerationReplyRouting
}

type GenerationReplyRouting struct {
	Mode          ReplyRoutingMode
	RecipientType string
	RecipientID   string
	ReplyTo       domain.MessageID
}

type SandboxTaskRequest struct {
	Instruction       string
	PermissionProfile SandboxPermissionProfile
}

type GenerationResult struct {
	MessageBody    string
	Metadata       map[string]any
	SandboxRequest *SandboxTaskRequest
}

type AgentProviderClass string
type SandboxPermissionProfile string
type SandboxTaskStatus string

type VectorIndexStatus string

type VectorIndexStateStatus string
type OrchestrationMode string
type ReplyRoutingMode string

const (
	AgentProviderClassTextLLM          AgentProviderClass       = "text_llm"
	AgentProviderClassSandboxedRuntime AgentProviderClass       = "sandboxed_runtime"
	SandboxPermissionReadOnly          SandboxPermissionProfile = "read_only"
	SandboxPermissionPatch             SandboxPermissionProfile = "patch"
	SandboxPermissionFullTask          SandboxPermissionProfile = "full_task"
	SandboxTaskStatusPending           SandboxTaskStatus        = "pending"
	SandboxTaskStatusRunning           SandboxTaskStatus        = "running"
	SandboxTaskStatusSucceeded         SandboxTaskStatus        = "succeeded"
	SandboxTaskStatusFailed            SandboxTaskStatus        = "failed"

	VectorIndexStatusDisabled VectorIndexStatus = "disabled"
	VectorIndexStatusReady    VectorIndexStatus = "ready"

	VectorIndexStateStatusDisabled   VectorIndexStateStatus = "disabled"
	VectorIndexStateStatusReady      VectorIndexStateStatus = "ready"
	VectorIndexStateStatusDegraded   VectorIndexStateStatus = "degraded"
	VectorIndexStateStatusRebuilding VectorIndexStateStatus = "rebuilding"
	VectorIndexStateStatusStale      VectorIndexStateStatus = "stale"

	OrchestrationModeDeterministic  OrchestrationMode = "deterministic"
	OrchestrationModeRoundRobin     OrchestrationMode = "round_robin"
	OrchestrationModeMentionedFirst OrchestrationMode = "mentioned_first"

	ReplyRoutingModeLatestSpeaker    ReplyRoutingMode = "latest_speaker"
	ReplyRoutingModeOutstandingFirst ReplyRoutingMode = "reply_obligations"
)

type MessageEmbeddingRecord struct {
	MessageID  domain.MessageID
	SessionID  domain.SessionID
	Embedding  []float32
	SourceText string
	Metadata   map[string]string
	UpdatedAt  time.Time
}

type VectorSearchQuery struct {
	SessionID domain.SessionID
	Embedding []float32
	Limit     int
}

type VectorSearchResult struct {
	MessageID domain.MessageID
	Distance  float64
}

type VectorIndexState struct {
	IndexName     string
	Provider      string
	Status        VectorIndexStateStatus
	LastRebuiltAt *time.Time
	LastError     string
	UpdatedAt     time.Time
}

type VectorRebuildOptions struct {
	SessionID domain.SessionID
	Force     bool
}

type VectorRebuildStats struct {
	Scanned    int
	Upserted   int
	Skipped    int
	StartedAt  time.Time
	FinishedAt time.Time
}

type RecordedEvent struct {
	Topic      string
	Payload    any
	OccurredAt time.Time
}

type AgentTaskID string

func (id AgentTaskID) Validate() error {
	value := strings.TrimSpace(string(id))
	if value == "" {
		return fmt.Errorf("agent task id must not be empty")
	}
	if value == "." || value == ".." {
		return fmt.Errorf("agent task id %q must not be a relative path token", id)
	}
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return fmt.Errorf("agent task id %q must not be an absolute path", id)
	}
	if strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return fmt.Errorf("agent task id %q must not contain path separators", id)
	}
	return nil
}

type AgentHandoffID string

func (id AgentHandoffID) Validate() error {
	if id == "" {
		return fmt.Errorf("agent handoff id must not be empty")
	}
	return nil
}

type SandboxTask struct {
	ID                 AgentTaskID
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
	Status             SandboxTaskStatus
	ResultSummary      string
	ErrorMessage       string
	Artifacts          []SandboxTaskArtifact
	Metadata           map[string]any
	CreatedAt          time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
}

type SandboxTaskArtifact struct {
	Path        string
	Description string
}

type AgentHandoff struct {
	ID              AgentHandoffID
	SessionID       domain.SessionID
	ConversationID  domain.ConversationID
	SourceMessageID domain.MessageID
	SourceTaskID    AgentTaskID
	TaskID          AgentTaskID
	FromAgentID     domain.AgentID
	ToAgentID       domain.AgentID
	ToProviderClass AgentProviderClass
	Reason          string
	CreatedAt       time.Time
}

type SandboxTaskExecutionResult struct {
	Summary     string
	ErrorText   string
	Artifacts   []SandboxTaskArtifact
	Metadata    map[string]any
	CompletedAt time.Time
}

func (c AgentProviderClass) Validate() error {
	switch c {
	case AgentProviderClassTextLLM, AgentProviderClassSandboxedRuntime:
		return nil
	default:
		return fmt.Errorf("invalid agent provider class %q", c)
	}
}

func (p SandboxPermissionProfile) Validate() error {
	switch p {
	case SandboxPermissionReadOnly, SandboxPermissionPatch, SandboxPermissionFullTask:
		return nil
	default:
		return fmt.Errorf("invalid sandbox permission profile %q", p)
	}
}

func (s SandboxTaskStatus) Validate() error {
	switch s {
	case SandboxTaskStatusPending, SandboxTaskStatusRunning, SandboxTaskStatusSucceeded, SandboxTaskStatusFailed:
		return nil
	default:
		return fmt.Errorf("invalid sandbox task status %q", s)
	}
}

func (a SandboxTaskArtifact) Validate() error {
	if strings.TrimSpace(a.Path) == "" {
		return fmt.Errorf("sandbox task artifact path must not be empty")
	}
	if strings.TrimSpace(a.Description) == "" {
		return fmt.Errorf("sandbox task artifact description must not be empty")
	}
	return nil
}

func (t SandboxTask) Validate() error {
	if err := t.ID.Validate(); err != nil {
		return err
	}
	if err := t.SessionID.Validate(); err != nil {
		return err
	}
	if err := t.ConversationID.Validate(); err != nil {
		return err
	}
	if t.RequestedByAgentID != "" {
		if err := t.RequestedByAgentID.Validate(); err != nil {
			return err
		}
	}
	if t.AssignedAgentID != "" {
		if err := t.AssignedAgentID.Validate(); err != nil {
			return err
		}
	}
	if err := t.AssignedProvider.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(t.RuntimeName) == "" {
		return fmt.Errorf("sandbox task runtime name must not be empty")
	}
	if strings.TrimSpace(t.WorkspaceRoot) == "" {
		return fmt.Errorf("sandbox task workspace root must not be empty")
	}
	if t.SandboxRoot != "" && strings.TrimSpace(t.SandboxRoot) == "" {
		return fmt.Errorf("sandbox task sandbox root must not be blank when set")
	}
	if err := t.PermissionProfile.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(t.Instruction) == "" {
		return fmt.Errorf("sandbox task instruction must not be empty")
	}
	if err := t.Status.Validate(); err != nil {
		return err
	}
	for _, artifact := range t.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	if t.CreatedAt.IsZero() {
		return fmt.Errorf("sandbox task created_at must not be zero")
	}
	if t.StartedAt != nil && t.StartedAt.IsZero() {
		return fmt.Errorf("sandbox task started_at must not be zero")
	}
	if t.CompletedAt != nil && t.CompletedAt.IsZero() {
		return fmt.Errorf("sandbox task completed_at must not be zero")
	}
	return nil
}

func (h AgentHandoff) Validate() error {
	if err := h.ID.Validate(); err != nil {
		return err
	}
	if err := h.SessionID.Validate(); err != nil {
		return err
	}
	if err := h.ConversationID.Validate(); err != nil {
		return err
	}
	if h.SourceMessageID != "" {
		if err := h.SourceMessageID.Validate(); err != nil {
			return err
		}
	}
	if h.SourceTaskID != "" {
		if err := h.SourceTaskID.Validate(); err != nil {
			return err
		}
	}
	if h.SourceMessageID == "" && h.SourceTaskID == "" {
		return fmt.Errorf("agent handoff must reference at least one source message or source task")
	}
	if err := h.TaskID.Validate(); err != nil {
		return err
	}
	if h.FromAgentID != "" {
		if err := h.FromAgentID.Validate(); err != nil {
			return err
		}
	}
	if h.ToAgentID != "" {
		if err := h.ToAgentID.Validate(); err != nil {
			return err
		}
	}
	if err := h.ToProviderClass.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(h.Reason) == "" {
		return fmt.Errorf("agent handoff reason must not be empty")
	}
	if h.CreatedAt.IsZero() {
		return fmt.Errorf("agent handoff created_at must not be zero")
	}
	return nil
}

func (r SandboxTaskRequest) Validate() error {
	if strings.TrimSpace(r.Instruction) == "" {
		return fmt.Errorf("sandbox task request instruction must not be empty")
	}
	if err := r.PermissionProfile.Validate(); err != nil {
		return err
	}
	return nil
}

func (m OrchestrationMode) Validate() error {
	switch m {
	case OrchestrationModeDeterministic, OrchestrationModeRoundRobin, OrchestrationModeMentionedFirst:
		return nil
	default:
		return fmt.Errorf("invalid orchestration mode %q", m)
	}
}

func (m ReplyRoutingMode) Validate() error {
	switch m {
	case ReplyRoutingModeLatestSpeaker, ReplyRoutingModeOutstandingFirst:
		return nil
	default:
		return fmt.Errorf("invalid reply routing mode %q", m)
	}
}
