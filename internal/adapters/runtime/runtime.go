package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"crew/internal/adapters/memory"
	"crew/internal/application"
	"crew/internal/domain"
)

var (
	ErrRuntimeNotStarted       = errors.New("runtime not started")
	ErrRuntimeClosed           = errors.New("runtime closed")
	ErrRuntimeActive           = errors.New("runtime active")
	ErrRuntimeStateUnsupported = errors.New("runtime state snapshot unsupported")
)

type lifecycleState uint8

const (
	lifecycleStateCreated lifecycleState = iota
	lifecycleStateStarted
	lifecycleStateClosed
)

type Config struct {
	ProjectionBuffer           int
	AgentsDir                  string
	DefaultActorsSelector      string
	OrchestrationMode          application.OrchestrationMode
	ReplyRoutingMode           application.ReplyRoutingMode
	VectorEnabled              bool
	VectorDimensions           int
	TextProviders              map[string]TextProviderConfig
	SandboxDefaultProvider     string
	SandboxProviders           map[string]SandboxProviderConfig
	SandboxSourceWorkspaceRoot string
	SandboxPermissionProfile   string
}

type SessionSnapshot struct {
	Session  domain.Session
	Messages []domain.Message
	Stream   []StreamEntry
}

type StateSnapshot struct {
	Store   memory.Snapshot                    `json:"store"`
	Streams map[domain.SessionID][]StreamEntry `json:"streams"`
}

func (s StateSnapshot) Clone() StateSnapshot {
	cloned := StateSnapshot{
		Store:   s.Store.Clone(),
		Streams: make(map[domain.SessionID][]StreamEntry, len(s.Streams)),
	}

	for sessionID, entries := range s.Streams {
		cloned.Streams[sessionID] = append([]StreamEntry(nil), entries...)
	}

	return cloned
}

type Runtime struct {
	bus    *memory.EventBus
	outbox outboxFlusher

	sessionService      *application.SessionService
	messageService      *application.MessageService
	workflowService     *application.WorkflowService
	vectorService       *application.VectorService
	freeModeService     *application.FreeModeService
	coordinationService *application.CoordinationService

	projector             *StreamProjector
	streams               streamEventLoader
	seedAgent             agentSeeder
	snapshot              snapshotStore
	providers             ProviderCatalog
	agentsDir             string
	defaultActorsSelector string

	mu     sync.RWMutex
	opMu   sync.Mutex
	state  lifecycleState
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type outboxFlusher interface {
	application.EventOutbox
	Flush(ctx context.Context, publisher application.EventBus) ([]application.RecordedEvent, error)
}

type streamEventLoader interface {
	ListRecordedBySessionID(ctx context.Context, sessionID domain.SessionID) ([]application.RecordedEvent, error)
}

type agentSeeder interface {
	SeedAgent(ctx context.Context, agent domain.Agent) error
	SyncCatalog(ctx context.Context, agents []domain.Agent) error
}

type snapshotStore interface {
	Snapshot() memory.Snapshot
	LoadSnapshot(snapshot memory.Snapshot) error
}

type runtimeDeps struct {
	Sessions       application.SessionRepository
	Messages       application.MessageRepository
	Workflows      application.WorkflowRepository
	Agents         application.AgentRepository
	Tasks          application.SandboxTaskRepository
	Outbox         outboxFlusher
	Streams        streamEventLoader
	Tx             application.UnitOfWork
	SeedAgent      agentSeeder
	Snapshotter    snapshotStore
	VectorIndex    application.VectorIndex
	VectorAdmin    application.VectorAdmin
	Embedder       application.Embedder
	Orchestrator   application.Orchestrator
	LLMProvider    application.LLMProvider
	SandboxRuntime application.SandboxedAgentRuntime
}

type ProviderBinding struct {
	Name    string
	Class   application.AgentProviderClass
	Enabled bool
}

type ProviderCatalog struct {
	TextGeneration    []ProviderBinding
	SandboxedRuntimes []ProviderBinding
}

func New(
	store *memory.Store,
	bus *memory.EventBus,
	clock application.Clock,
	ids application.IDGenerator,
	cfg Config,
) *Runtime {
	if store == nil {
		store = memory.NewStore()
	}
	if bus == nil {
		bus = memory.NewEventBus()
	}
	if clock == nil {
		clock = memory.SystemClock{}
	}
	if ids == nil {
		ids = memory.NewSequenceIDGenerator()
	}
	if cfg.ProjectionBuffer < 1 {
		cfg.ProjectionBuffer = 64
	}

	return newRuntime(runtimeDeps{
		Sessions:    store.Sessions(),
		Messages:    store.Messages(),
		Workflows:   store.Workflows(),
		Agents:      store.Agents(),
		Tasks:       store.SandboxTasks(),
		Outbox:      store.Outbox(),
		Tx:          store.UnitOfWork(),
		SeedAgent:   memoryAgentSeeder{store: store},
		Snapshotter: store,
	}, bus, clock, ids, cfg)
}

func newRuntime(
	deps runtimeDeps,
	bus *memory.EventBus,
	clock application.Clock,
	ids application.IDGenerator,
	cfg Config,
) *Runtime {
	if bus == nil {
		bus = memory.NewEventBus()
	}
	if clock == nil {
		clock = memory.SystemClock{}
	}
	if ids == nil {
		ids = memory.NewSequenceIDGenerator()
	}
	if cfg.ProjectionBuffer < 1 {
		cfg.ProjectionBuffer = 64
	}
	if deps.Orchestrator == nil {
		deps.Orchestrator = localStubOrchestrator{}
	}
	if deps.LLMProvider == nil {
		deps.LLMProvider = localStubLLMProvider{}
	}
	if deps.SandboxRuntime == nil {
		if sandboxRuntime, err := newConfiguredSandboxRuntime(cfg); err == nil {
			deps.SandboxRuntime = sandboxRuntime
		}
	}

	messageService := application.NewMessageService(deps.Sessions, deps.Messages, deps.Agents, deps.VectorAdmin, deps.Outbox, deps.Tx, clock, ids)
	coordinationService := application.NewCoordinationService(deps.Sessions, deps.Agents, deps.Tasks, deps.SandboxRuntime, deps.Outbox, deps.Tx, clock)
	freeModeService := application.NewFreeModeService(deps.Sessions, deps.Messages, deps.Agents, deps.Orchestrator, deps.LLMProvider, messageService).
		WithSandboxDelegation(coordinationService, cfg.SandboxSourceWorkspaceRoot).
		WithDefaultOrchestrationMode(cfg.OrchestrationMode).
		WithDefaultReplyRoutingMode(cfg.ReplyRoutingMode)

	return &Runtime{
		bus:                   bus,
		outbox:                deps.Outbox,
		sessionService:        application.NewSessionService(deps.Sessions, deps.Outbox, deps.Tx, clock, ids),
		messageService:        messageService,
		workflowService:       application.NewWorkflowService(deps.Workflows, deps.Outbox, bus, deps.Tx, clock),
		vectorService:         application.NewVectorService(deps.Sessions, deps.Messages, deps.VectorIndex, deps.VectorAdmin, deps.Embedder),
		freeModeService:       freeModeService,
		coordinationService:   coordinationService,
		projector:             NewStreamProjector(),
		streams:               deps.Streams,
		seedAgent:             deps.SeedAgent,
		snapshot:              deps.Snapshotter,
		providers:             deriveProviderCatalog(cfg, deps),
		agentsDir:             cfg.AgentsDir,
		defaultActorsSelector: cfg.DefaultActorsSelector,
	}
}

func deriveProviderCatalog(cfg Config, deps runtimeDeps) ProviderCatalog {
	textProviders := []ProviderBinding{{
		Name:    "local_stub",
		Class:   application.AgentProviderClassTextLLM,
		Enabled: true,
	}}
	for _, name := range sortedTextProviderNames(cfg.TextProviders) {
		providerCfg := cfg.TextProviders[name]
		enabled := providerCfg.APIKey != ""
		if name == "codex" {
			enabled = strings.TrimSpace(providerCfg.BinaryPath) != ""
		}
		textProviders = append(textProviders, ProviderBinding{
			Name:    name,
			Class:   application.AgentProviderClassTextLLM,
			Enabled: enabled,
		})
	}

	sandboxProviders := make([]ProviderBinding, 0, len(cfg.SandboxProviders))
	for _, name := range sortedSandboxProviderNames(cfg.SandboxProviders) {
		sandboxProviders = append(sandboxProviders, ProviderBinding{
			Name:    name,
			Class:   application.AgentProviderClassSandboxedRuntime,
			Enabled: deps.SandboxRuntime != nil,
		})
	}

	if len(sandboxProviders) == 0 {
		sandboxProviders = append(sandboxProviders, ProviderBinding{
			Name:    "disabled",
			Class:   application.AgentProviderClassSandboxedRuntime,
			Enabled: false,
		})
	}

	return ProviderCatalog{
		TextGeneration:    textProviders,
		SandboxedRuntimes: sandboxProviders,
	}
}

func (r *Runtime) ProviderCatalog() ProviderCatalog {
	return r.providers
}

func (r *Runtime) Start(parent context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch r.state {
	case lifecycleStateStarted:
		return nil
	case lifecycleStateClosed:
		return ErrRuntimeClosed
	}

	_, cancel := context.WithCancel(parent)
	r.cancel = cancel
	if err := r.seedConfiguredAgents(parent); err != nil {
		cancel()
		r.cancel = nil
		return err
	}
	r.state = lifecycleStateStarted
	return nil
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	switch r.state {
	case lifecycleStateClosed:
		r.mu.Unlock()
		return nil
	case lifecycleStateCreated:
		r.state = lifecycleStateClosed
		r.mu.Unlock()
		r.bus.Close()
		return nil
	}

	cancel := r.cancel
	r.state = lifecycleStateClosed
	r.cancel = nil
	r.mu.Unlock()

	cancel()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		r.bus.Close()
		return nil
	}
}

func (r *Runtime) SeedAgent(agent domain.Agent) error {
	if r.seedAgent == nil {
		return ErrRuntimeStateUnsupported
	}

	return r.seedAgent.SeedAgent(context.Background(), agent)
}

func (r *Runtime) SyncAgentCatalog(agents []domain.Agent) error {
	if r.seedAgent == nil {
		return ErrRuntimeStateUnsupported
	}

	return r.seedAgent.SyncCatalog(context.Background(), agents)
}

func (r *Runtime) CreateSession(ctx context.Context, mode domain.SessionMode) (domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Session{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	session, err := r.sessionService.Create(ctx, application.CreateSessionCommand{
		Mode:         mode,
		ActorCatalog: r.defaultActorsSelector,
	})
	if err != nil {
		return domain.Session{}, err
	}

	return session, r.flushOutbox(ctx)
}

func (r *Runtime) GetSession(ctx context.Context, sessionID domain.SessionID) (domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Session{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	return r.sessionService.Get(ctx, application.GetSessionQuery{SessionID: sessionID})
}

func (r *Runtime) StartSession(ctx context.Context, sessionID domain.SessionID) (domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Session{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if err := r.seedSessionAgents(ctx, sessionID); err != nil {
		return domain.Session{}, err
	}

	session, err := r.sessionService.Start(ctx, application.SessionIDCommand{SessionID: sessionID})
	if err != nil {
		return domain.Session{}, err
	}

	return session, r.flushOutbox(ctx)
}

func (r *Runtime) PauseSession(ctx context.Context, sessionID domain.SessionID) (domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Session{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	session, err := r.sessionService.Pause(ctx, application.SessionIDCommand{SessionID: sessionID})
	if err != nil {
		return domain.Session{}, err
	}

	return session, r.flushOutbox(ctx)
}

func (r *Runtime) ResumeSession(ctx context.Context, sessionID domain.SessionID) (domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Session{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	session, err := r.sessionService.Resume(ctx, application.SessionIDCommand{SessionID: sessionID})
	if err != nil {
		return domain.Session{}, err
	}

	return session, r.flushOutbox(ctx)
}

func (r *Runtime) StopSession(ctx context.Context, sessionID domain.SessionID) (domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Session{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	session, err := r.sessionService.Stop(ctx, application.SessionIDCommand{SessionID: sessionID})
	if err != nil {
		return domain.Session{}, err
	}

	return session, r.flushOutbox(ctx)
}

func (r *Runtime) DispatchMessage(ctx context.Context, cmd application.DispatchMessageCommand) (domain.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Message{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if err := r.seedSessionAgents(ctx, cmd.SessionID); err != nil {
		return domain.Message{}, err
	}

	message, err := r.messageService.Dispatch(ctx, cmd)
	if err != nil {
		return domain.Message{}, err
	}

	return message, r.flushOutbox(ctx)
}

func (r *Runtime) VectorStatus(ctx context.Context, query application.VectorStatusQuery) (application.VectorIndexState, application.VectorIndexStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.VectorIndexState{}, "", err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	return r.vectorService.Status(ctx, query)
}

func (r *Runtime) RebuildVectors(ctx context.Context, cmd application.VectorRebuildCommand) (application.VectorRebuildStats, application.VectorIndexState, application.VectorIndexStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.VectorRebuildStats{}, application.VectorIndexState{}, "", err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	return r.vectorService.Rebuild(ctx, cmd)
}

func (r *Runtime) RecallSessionMessages(ctx context.Context, query application.RecallSessionMessagesQuery) (application.VectorRecallResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.VectorRecallResponse{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if err := r.flushOutbox(ctx); err != nil {
		return application.VectorRecallResponse{}, err
	}

	return r.vectorService.RecallSessionMessages(ctx, query)
}

func (r *Runtime) StepSession(ctx context.Context, cmd application.StepSessionCommand) (application.SessionStepResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.SessionStepResult{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if err := r.seedSessionAgents(ctx, cmd.SessionID); err != nil {
		return application.SessionStepResult{}, err
	}

	result, err := r.freeModeService.Step(ctx, cmd)
	if err != nil {
		return application.SessionStepResult{}, err
	}
	if result.Stepped {
		if err := r.flushOutbox(ctx); err != nil {
			return application.SessionStepResult{}, err
		}
	}
	return result, nil
}

func (r *Runtime) AutoSession(ctx context.Context, cmd application.AutoSessionCommand) (application.SessionAutoResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.SessionAutoResult{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if err := r.seedSessionAgents(ctx, cmd.SessionID); err != nil {
		return application.SessionAutoResult{}, err
	}

	result, err := r.freeModeService.Auto(ctx, cmd)
	if err != nil {
		return application.SessionAutoResult{}, err
	}
	if result.CompletedSteps > 0 {
		if err := r.flushOutbox(ctx); err != nil {
			return application.SessionAutoResult{}, err
		}
	}
	return result, nil
}

func (r *Runtime) CreateSandboxTask(ctx context.Context, cmd application.CreateSandboxTaskCommand) (application.SandboxTask, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.SandboxTask{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.coordinationService == nil {
		return application.SandboxTask{}, ErrRuntimeStateUnsupported
	}

	task, err := r.coordinationService.CreateSandboxTask(ctx, cmd)
	if err != nil {
		return application.SandboxTask{}, err
	}

	return task, r.flushOutbox(ctx)
}

func (r *Runtime) ExecuteSandboxTask(ctx context.Context, cmd application.ExecuteSandboxTaskCommand) (application.SandboxTask, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.SandboxTask{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.coordinationService == nil {
		return application.SandboxTask{}, ErrRuntimeStateUnsupported
	}

	task, err := r.coordinationService.ExecuteSandboxTask(ctx, cmd)
	if err != nil {
		return task, err
	}

	return task, r.flushOutbox(ctx)
}

func (r *Runtime) GetSandboxTask(ctx context.Context, query application.GetSandboxTaskQuery) (application.SandboxTask, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.SandboxTask{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.coordinationService == nil {
		return application.SandboxTask{}, ErrRuntimeStateUnsupported
	}

	return r.coordinationService.GetSandboxTask(ctx, query)
}

func (r *Runtime) ListSandboxTasksBySession(ctx context.Context, query application.ListSandboxTasksQuery) ([]application.SandboxTask, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return nil, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.coordinationService == nil {
		return nil, ErrRuntimeStateUnsupported
	}

	return r.coordinationService.ListSandboxTasksBySession(ctx, query)
}

func (r *Runtime) ListAgentHandoffsBySession(ctx context.Context, query application.ListAgentHandoffsQuery) ([]application.AgentHandoff, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return nil, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.coordinationService == nil {
		return nil, ErrRuntimeStateUnsupported
	}

	return r.coordinationService.ListAgentHandoffsBySession(ctx, query)
}

func (r *Runtime) RegisterWorkflow(ctx context.Context, workflow domain.Workflow) (domain.Workflow, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return domain.Workflow{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	registered, err := r.workflowService.Register(ctx, application.RegisterWorkflowCommand{Workflow: workflow})
	if err != nil {
		return domain.Workflow{}, err
	}

	return registered, r.flushOutbox(ctx)
}

func (r *Runtime) AdvanceWorkflow(ctx context.Context, cmd application.AdvanceWorkflowCommand) (application.WorkflowProgression, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return application.WorkflowProgression{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	return r.workflowService.Advance(ctx, cmd)
}

func (r *Runtime) InspectSession(ctx context.Context, sessionID domain.SessionID) (SessionSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := r.requireActiveLocked(); err != nil {
		return SessionSnapshot{}, err
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if err := r.flushOutbox(ctx); err != nil {
		return SessionSnapshot{}, err
	}

	session, err := r.sessionService.Get(ctx, application.GetSessionQuery{SessionID: sessionID})
	if err != nil {
		return SessionSnapshot{}, err
	}

	messages, err := r.messageService.ListBySession(ctx, application.ListSessionMessagesQuery{SessionID: sessionID})
	if err != nil {
		return SessionSnapshot{}, err
	}

	stream, err := r.sessionStream(ctx, sessionID)
	if err != nil {
		return SessionSnapshot{}, err
	}

	return SessionSnapshot{
		Session:  session,
		Messages: messages,
		Stream:   stream,
	}, nil
}

func (r *Runtime) flushOutbox(ctx context.Context) error {
	events, err := r.outbox.Flush(ctx, r.bus)
	if err != nil {
		return err
	}

	for _, event := range events {
		r.projector.Apply(event)
	}

	return nil
}

func (r *Runtime) sessionStream(ctx context.Context, sessionID domain.SessionID) ([]StreamEntry, error) {
	if r.streams == nil {
		return r.projector.SessionStream(sessionID), nil
	}

	recorded, err := r.streams.ListRecordedBySessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	stream := make([]StreamEntry, 0, len(recorded))
	for _, event := range recorded {
		stream = append(stream, StreamEntry{
			Topic:      event.Topic,
			RecordedAt: event.OccurredAt,
			Payload:    event.Payload,
		})
	}

	return stream, nil
}

func (r *Runtime) requireActiveLocked() error {
	switch r.state {
	case lifecycleStateStarted:
		return nil
	case lifecycleStateClosed:
		return ErrRuntimeClosed
	default:
		return ErrRuntimeNotStarted
	}
}

func (r *Runtime) Snapshot() (StateSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.state == lifecycleStateClosed {
		return StateSnapshot{}, ErrRuntimeClosed
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.snapshot == nil {
		return StateSnapshot{}, ErrRuntimeStateUnsupported
	}

	return StateSnapshot{
		Store:   r.snapshot.Snapshot(),
		Streams: r.projector.Snapshot(),
	}, nil
}

func (r *Runtime) LoadState(snapshot StateSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch r.state {
	case lifecycleStateStarted:
		return ErrRuntimeActive
	case lifecycleStateClosed:
		return ErrRuntimeClosed
	}
	r.opMu.Lock()
	defer r.opMu.Unlock()

	if r.snapshot == nil {
		return ErrRuntimeStateUnsupported
	}

	if err := r.snapshot.LoadSnapshot(snapshot.Store.Clone()); err != nil {
		return err
	}

	r.projector.LoadSnapshot(snapshot.Streams)
	return nil
}

func (r *Runtime) seedConfiguredAgents(ctx context.Context) error {
	if r.seedAgent == nil || r.agentsDir == "" {
		return nil
	}

	agentsDir, err := resolveRuntimeAgentsDir(r.agentsDir, r.defaultActorsSelector)
	if err != nil {
		return err
	}
	agents, err := loadAgentsDir(agentsDir)
	if err != nil {
		return err
	}
	if err := r.seedAgent.SyncCatalog(ctx, agents); err != nil {
		return err
	}
	return nil
}

func (r *Runtime) seedSessionAgents(ctx context.Context, sessionID domain.SessionID) error {
	if r.seedAgent == nil || r.agentsDir == "" {
		return nil
	}

	session, err := r.sessionService.Get(ctx, application.GetSessionQuery{SessionID: sessionID})
	if err != nil {
		return err
	}

	agentsDir, err := resolveRuntimeAgentsDir(r.agentsDir, session.ActorCatalog)
	if err != nil {
		return err
	}
	agents, err := loadAgentsDir(agentsDir)
	if err != nil {
		return err
	}
	if err := r.seedAgent.SyncCatalog(ctx, agents); err != nil {
		return err
	}
	return nil
}

func resolveRuntimeAgentsDir(rootDir string, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return rootDir, nil
	}
	if filepath.IsAbs(selector) {
		return "", fmt.Errorf("actors selector %q must be relative to %q", selector, rootDir)
	}

	cleaned := filepath.Clean(selector)
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("actors selector %q resolves to the root catalog; omit the selector to use %q", selector, rootDir)
	}

	candidate := filepath.Join(rootDir, cleaned)
	relative, err := filepath.Rel(rootDir, candidate)
	if err != nil {
		return "", fmt.Errorf("resolve actors selector %q under %q: %w", selector, rootDir, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("actors selector %q escapes the root catalog %q", selector, rootDir)
	}
	return candidate, nil
}

type memoryAgentSeeder struct {
	store *memory.Store
}

func (s memoryAgentSeeder) SeedAgent(_ context.Context, agent domain.Agent) error {
	return s.store.SeedAgent(agent)
}

func (s memoryAgentSeeder) SyncCatalog(ctx context.Context, agents []domain.Agent) error {
	return s.store.Agents().SyncCatalog(ctx, agents)
}
