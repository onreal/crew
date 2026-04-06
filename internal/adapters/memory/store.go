package memory

import (
	"context"
	"slices"
	"sync"

	"crew/internal/application"
	"crew/internal/domain"
)

type Store struct {
	mu            sync.RWMutex
	outboxFlushMu sync.Mutex
	state         state
}

type state struct {
	sessions  map[domain.SessionID]domain.Session
	messages  map[domain.SessionID][]domain.Message
	workflows map[domain.WorkflowID]domain.Workflow
	agents    map[domain.AgentID]domain.Agent
	active    map[domain.AgentID]bool
	tasks     map[application.AgentTaskID]application.SandboxTask
	handoffs  map[domain.SessionID][]application.AgentHandoff
	outbox    []application.RecordedEvent
}

type txContextKey struct{}

type txState struct {
	state state
}

func NewStore() *Store {
	return &Store{
		state: newState(),
	}
}

func (s *Store) Sessions() *SessionRepository {
	return &SessionRepository{store: s}
}

func (s *Store) Messages() *MessageRepository {
	return &MessageRepository{store: s}
}

func (s *Store) Workflows() *WorkflowRepository {
	return &WorkflowRepository{store: s}
}

func (s *Store) Agents() *AgentRepository {
	return &AgentRepository{store: s}
}

func (s *Store) SandboxTasks() *SandboxTaskRepository {
	return &SandboxTaskRepository{store: s}
}

func (s *Store) Outbox() *Outbox {
	return &Outbox{store: s}
}

func (s *Store) UnitOfWork() *UnitOfWork {
	return &UnitOfWork{store: s}
}

func (s *Store) SeedAgent(agent domain.Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.agents[agent.ID] = cloneAgent(agent)
	s.state.active[agent.ID] = true
	return nil
}

func (s *Store) loadSession(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	current := s.stateFor(ctx)
	session, exists := current.sessions[id]
	if !exists {
		return domain.Session{}, application.NotFoundError{Entity: "session", ID: string(id)}
	}

	return session, nil
}

func (s *Store) loadWorkflow(ctx context.Context, id domain.WorkflowID) (domain.Workflow, error) {
	current := s.stateFor(ctx)
	workflow, exists := current.workflows[id]
	if !exists {
		return domain.Workflow{}, application.NotFoundError{Entity: "workflow", ID: string(id)}
	}

	return cloneWorkflow(workflow), nil
}

func (s *Store) loadAgent(ctx context.Context, id domain.AgentID) (domain.Agent, error) {
	current := s.stateFor(ctx)
	agent, exists := current.agents[id]
	if !exists || !current.active[id] {
		return domain.Agent{}, application.NotFoundError{Entity: "agent", ID: string(id)}
	}

	return cloneAgent(agent), nil
}

func (s *Store) stateFor(ctx context.Context) *state {
	if tx, ok := s.txFromContext(ctx); ok {
		return &tx.state
	}

	return &s.state
}

func (s *Store) txFromContext(ctx context.Context) (*txState, bool) {
	tx, ok := ctx.Value(txContextKey{}).(*txState)
	return tx, ok
}

func (s *Store) cloneState() state {
	return state{
		sessions:  cloneSessionMap(s.state.sessions),
		messages:  cloneMessagesMap(s.state.messages),
		workflows: cloneWorkflowMap(s.state.workflows),
		agents:    cloneAgentMap(s.state.agents),
		active:    cloneActiveAgentMap(s.state.active),
		tasks:     cloneSandboxTaskMap(s.state.tasks),
		handoffs:  cloneSandboxHandoffsMap(s.state.handoffs),
		outbox:    cloneRecordedEvents(s.state.outbox),
	}
}

func newState() state {
	return state{
		sessions:  make(map[domain.SessionID]domain.Session),
		messages:  make(map[domain.SessionID][]domain.Message),
		workflows: make(map[domain.WorkflowID]domain.Workflow),
		agents:    make(map[domain.AgentID]domain.Agent),
		active:    make(map[domain.AgentID]bool),
		tasks:     make(map[application.AgentTaskID]application.SandboxTask),
		handoffs:  make(map[domain.SessionID][]application.AgentHandoff),
		outbox:    nil,
	}
}

type SessionRepository struct {
	store *Store
}

func (r *SessionRepository) Save(ctx context.Context, session domain.Session) error {
	current := r.store.stateFor(ctx)
	current.sessions[session.ID] = session
	return nil
}

func (r *SessionRepository) GetByID(ctx context.Context, id domain.SessionID) (domain.Session, error) {
	if _, ok := r.store.txFromContext(ctx); ok {
		return r.store.loadSession(ctx, id)
	}

	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	return r.store.loadSession(ctx, id)
}

type MessageRepository struct {
	store *Store
}

func (r *MessageRepository) Save(ctx context.Context, message domain.Message) error {
	current := r.store.stateFor(ctx)
	current.messages[message.SessionID] = append(current.messages[message.SessionID], cloneMessage(message))
	return nil
}

func (r *MessageRepository) ListBySessionID(ctx context.Context, sessionID domain.SessionID) ([]domain.Message, error) {
	if _, ok := r.store.txFromContext(ctx); ok {
		messages := r.store.stateFor(ctx).messages[sessionID]
		return cloneMessages(messages), nil
	}

	r.store.mu.RLock()
	defer r.store.mu.RUnlock()

	messages := r.store.stateFor(ctx).messages[sessionID]
	return cloneMessages(messages), nil
}

type WorkflowRepository struct {
	store *Store
}

func (r *WorkflowRepository) Save(ctx context.Context, workflow domain.Workflow) error {
	current := r.store.stateFor(ctx)
	current.workflows[workflow.ID] = cloneWorkflow(workflow)
	return nil
}

func (r *WorkflowRepository) GetByID(ctx context.Context, id domain.WorkflowID) (domain.Workflow, error) {
	if _, ok := r.store.txFromContext(ctx); ok {
		return r.store.loadWorkflow(ctx, id)
	}

	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	return r.store.loadWorkflow(ctx, id)
}

type AgentRepository struct {
	store *Store
}

func (r *AgentRepository) GetByID(ctx context.Context, id domain.AgentID) (domain.Agent, error) {
	if _, ok := r.store.txFromContext(ctx); ok {
		return r.store.loadAgent(ctx, id)
	}

	r.store.mu.RLock()
	defer r.store.mu.RUnlock()
	return r.store.loadAgent(ctx, id)
}

func (r *AgentRepository) List(ctx context.Context) ([]domain.Agent, error) {
	var agents []domain.Agent

	appendAgents := func(current *state) {
		agents = make([]domain.Agent, 0, len(current.active))
		for id, agent := range current.agents {
			if !current.active[id] {
				continue
			}
			agents = append(agents, cloneAgent(agent))
		}
	}

	if tx, ok := r.store.txFromContext(ctx); ok {
		appendAgents(&tx.state)
	} else {
		r.store.mu.RLock()
		appendAgents(&r.store.state)
		r.store.mu.RUnlock()
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

	return agents, nil
}

func (r *AgentRepository) SyncCatalog(_ context.Context, agents []domain.Agent) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()

	nextActive := make(map[domain.AgentID]bool, len(agents))
	for _, agent := range agents {
		if err := agent.Validate(); err != nil {
			return err
		}
		r.store.state.agents[agent.ID] = cloneAgent(agent)
		nextActive[agent.ID] = true
	}
	r.store.state.active = nextActive
	return nil
}

func cloneActiveAgentMap(values map[domain.AgentID]bool) map[domain.AgentID]bool {
	cloned := make(map[domain.AgentID]bool, len(values))
	for id, active := range values {
		cloned[id] = active
	}
	return cloned
}

type Outbox struct {
	store *Store
}

func (o *Outbox) Add(ctx context.Context, event application.RecordedEvent) error {
	current := o.store.stateFor(ctx)
	current.outbox = append(current.outbox, event)
	return nil
}

func (o *Outbox) Flush(ctx context.Context, publisher application.EventBus) ([]application.RecordedEvent, error) {
	o.store.outboxFlushMu.Lock()
	defer o.store.outboxFlushMu.Unlock()

	var published []application.RecordedEvent
	for {
		o.store.mu.RLock()
		if len(o.store.state.outbox) == 0 {
			o.store.mu.RUnlock()
			return published, nil
		}
		event := o.store.state.outbox[0]
		o.store.mu.RUnlock()

		if err := publisher.Publish(ctx, event.Topic, event.Payload); err != nil {
			return published, err
		}

		o.store.mu.Lock()
		o.store.state.outbox = append([]application.RecordedEvent(nil), o.store.state.outbox[1:]...)
		o.store.mu.Unlock()
		published = append(published, event)
	}
}

type UnitOfWork struct {
	store *Store
}

func (u *UnitOfWork) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	u.store.mu.Lock()
	defer u.store.mu.Unlock()

	tx := &txState{state: u.store.cloneState()}
	txCtx := context.WithValue(ctx, txContextKey{}, tx)

	if err := fn(txCtx); err != nil {
		return err
	}

	u.store.state = tx.state
	return nil
}
