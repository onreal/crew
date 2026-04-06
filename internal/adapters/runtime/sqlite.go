package runtime

import (
	"context"
	"fmt"

	"crew/internal/adapters/memory"
	sqliteadapter "crew/internal/adapters/storage/sqlite"
	sqlitevecadapter "crew/internal/adapters/storage/sqlitevec"
	"crew/internal/application"
	"crew/internal/domain"
)

func NewSQLite(
	ctx context.Context,
	store *sqliteadapter.Store,
	bus *memory.EventBus,
	clock application.Clock,
	ids application.IDGenerator,
	cfg Config,
) (*Runtime, error) {
	if store == nil {
		return nil, fmt.Errorf("sqlite runtime store must not be nil")
	}
	if clock == nil {
		clock = memory.SystemClock{}
	}
	if ids == nil {
		sessionCounter, err := store.MaxSessionCounter(ctx)
		if err != nil {
			return nil, err
		}

		messageCounter, err := store.MaxMessageCounter(ctx)
		if err != nil {
			return nil, err
		}

		ids = memory.NewSequenceIDGeneratorWithCounters(sessionCounter, messageCounter)
	}

	vectorIndex, err := sqlitevecadapter.New(store, sqlitevecadapter.Config{
		EnableSQLiteVec: cfg.VectorEnabled,
		Dimensions:      cfg.VectorDimensions,
	})
	if err != nil {
		return nil, err
	}
	if err := vectorIndex.Migrate(ctx); err != nil {
		return nil, err
	}

	embedder := newLocalStubEmbedder(cfg.VectorDimensions)
	llmProvider, err := newConfiguredLLMProvider(cfg)
	if err != nil {
		return nil, err
	}
	sandboxRuntime, err := newConfiguredSandboxRuntime(cfg)
	if err != nil {
		return nil, err
	}

	return newRuntime(runtimeDeps{
		Sessions:       store.Sessions(),
		Messages:       store.Messages(),
		Workflows:      store.Workflows(),
		Agents:         store.Agents(),
		Tasks:          store.SandboxTasks(),
		Outbox:         store.Outbox(),
		Streams:        store.SessionStreams(),
		Tx:             store.UnitOfWork(),
		SeedAgent:      sqliteAgentSeeder{agents: store.Agents()},
		VectorIndex:    vectorIndex,
		VectorAdmin:    vectorIndex,
		Embedder:       embedder,
		LLMProvider:    llmProvider,
		SandboxRuntime: sandboxRuntime,
	}, bus, clock, ids, cfg), nil
}

type sqliteAgentSeeder struct {
	agents *sqliteadapter.AgentRepository
}

func (s sqliteAgentSeeder) SeedAgent(ctx context.Context, agent domain.Agent) error {
	return s.agents.Upsert(ctx, agent)
}

func (s sqliteAgentSeeder) SyncCatalog(ctx context.Context, agents []domain.Agent) error {
	return s.agents.SyncCatalog(ctx, agents)
}
