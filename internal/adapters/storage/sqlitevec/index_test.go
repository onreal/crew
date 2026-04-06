package sqlitevec

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sqliteadapter "crew/internal/adapters/storage/sqlite"
	"crew/internal/application"
	"crew/internal/domain"
)

func TestDisabledIndex(t *testing.T) {
	t.Parallel()

	index := NewDisabled()

	status, err := index.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status != application.VectorIndexStatusDisabled {
		t.Fatalf("expected disabled status, got %q", status)
	}

	err = index.UpsertMessageEmbedding(context.Background(), application.MessageEmbeddingRecord{
		MessageID:  "message-1",
		SessionID:  "session-1",
		Embedding:  []float32{0.1, 0.2},
		SourceText: "hello",
		UpdatedAt:  time.Date(2026, 3, 20, 18, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, application.ErrDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestIndexMigrateAndRebuildFromCanonicalMessages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	messages := seedCanonicalMessages(t, ctx, store)

	index, err := New(store, Config{})
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	if err := index.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlitevec: %v", err)
	}

	status, err := index.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != application.VectorIndexStatusDisabled {
		t.Fatalf("expected disabled status, got %q", status)
	}

	stats, err := index.RebuildFromCanonicalMessages(ctx, fakeEmbedder{id: "test-embedder-v1", dims: 3}, application.VectorRebuildOptions{})
	if err != nil {
		t.Fatalf("rebuild from canonical messages: %v", err)
	}
	if stats.Scanned != len(messages) {
		t.Fatalf("expected %d scanned messages, got %d", len(messages), stats.Scanned)
	}
	if stats.Upserted != len(messages) {
		t.Fatalf("expected %d upserted messages, got %d", len(messages), stats.Upserted)
	}
	if stats.Skipped != 0 {
		t.Fatalf("expected 0 skipped messages, got %d", stats.Skipped)
	}
	if stats.FinishedAt.IsZero() {
		t.Fatalf("expected rebuild finished_at to be set")
	}

	firstEmbedding, exists, err := index.GetMessageEmbedding(ctx, messages[0].ID)
	if err != nil {
		t.Fatalf("get first embedding: %v", err)
	}
	if !exists {
		t.Fatalf("expected embedding row for %q", messages[0].ID)
	}
	if firstEmbedding.SessionID != messages[0].SessionID {
		t.Fatalf("expected session %q, got %q", messages[0].SessionID, firstEmbedding.SessionID)
	}
	if firstEmbedding.SourceText != messages[0].Body {
		t.Fatalf("expected source text %q, got %q", messages[0].Body, firstEmbedding.SourceText)
	}
	if firstEmbedding.SourceSHA != hashSourceText(messages[0].Body) {
		t.Fatalf("expected source hash %q, got %q", hashSourceText(messages[0].Body), firstEmbedding.SourceSHA)
	}
	if len(firstEmbedding.Embedding) != 3 {
		t.Fatalf("expected embedding size 3, got %d", len(firstEmbedding.Embedding))
	}
	if firstEmbedding.Metadata["channel"] != string(messages[0].Channel) {
		t.Fatalf("expected channel metadata %q, got %q", messages[0].Channel, firstEmbedding.Metadata["channel"])
	}

	state, err := index.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.Status != indexStateDisabled {
		t.Fatalf("expected disabled state after rebuild without sqlite-vec, got %q", state.Status)
	}
	if state.Provider != "disabled" {
		t.Fatalf("expected disabled provider, got %q", state.Provider)
	}
	if state.LastRebuiltAt == nil {
		t.Fatalf("expected last rebuilt timestamp to be set")
	}

	secondStats, err := index.RebuildFromCanonicalMessages(ctx, fakeEmbedder{id: "test-embedder-v1", dims: 3}, application.VectorRebuildOptions{})
	if err != nil {
		t.Fatalf("second rebuild from canonical messages: %v", err)
	}
	if secondStats.Upserted != 0 {
		t.Fatalf("expected 0 upserted messages on second rebuild, got %d", secondStats.Upserted)
	}
	if secondStats.Skipped != len(messages) {
		t.Fatalf("expected %d skipped messages on second rebuild, got %d", len(messages), secondStats.Skipped)
	}
}

func TestIndexRebuildDoesNotSkipWhenEmbeddingFingerprintChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	messages := seedCanonicalMessages(t, ctx, store)

	indexV1, err := New(store, Config{Dimensions: 3})
	if err != nil {
		t.Fatalf("new v1 index: %v", err)
	}
	if err := indexV1.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlitevec v1: %v", err)
	}

	if _, err := indexV1.RebuildFromCanonicalMessages(ctx, fakeEmbedder{id: "model-v1", dims: 3}, application.VectorRebuildOptions{}); err != nil {
		t.Fatalf("rebuild v1: %v", err)
	}

	before, exists, err := indexV1.GetMessageEmbedding(ctx, messages[0].ID)
	if err != nil {
		t.Fatalf("get v1 embedding: %v", err)
	}
	if !exists {
		t.Fatalf("expected existing v1 embedding")
	}
	beforeUpdatedAt := before.UpdatedAt

	indexV2, err := New(store, Config{Dimensions: 4})
	if err != nil {
		t.Fatalf("new v2 index: %v", err)
	}
	indexV2.now = func() time.Time {
		return beforeUpdatedAt.Add(time.Minute)
	}
	if err := indexV2.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlitevec v2: %v", err)
	}

	stats, err := indexV2.RebuildFromCanonicalMessages(ctx, fakeEmbedder{id: "model-v2", dims: 4}, application.VectorRebuildOptions{})
	if err != nil {
		t.Fatalf("rebuild v2: %v", err)
	}
	if stats.Upserted != len(messages) {
		t.Fatalf("expected %d upserted messages after fingerprint change, got %d", len(messages), stats.Upserted)
	}
	if stats.Skipped != 0 {
		t.Fatalf("expected 0 skipped messages after fingerprint change, got %d", stats.Skipped)
	}

	after, exists, err := indexV2.GetMessageEmbedding(ctx, messages[0].ID)
	if err != nil {
		t.Fatalf("get v2 embedding: %v", err)
	}
	if !exists {
		t.Fatalf("expected existing v2 embedding")
	}
	if after.Dimensions != 4 {
		t.Fatalf("expected updated dimensions 4, got %d", after.Dimensions)
	}
	if after.RebuildFingerprint == before.RebuildFingerprint {
		t.Fatalf("expected rebuild fingerprint to change after embedder/config change")
	}
	if !after.UpdatedAt.After(beforeUpdatedAt) {
		t.Fatalf("expected updated timestamp after rebuild, before=%s after=%s", beforeUpdatedAt, after.UpdatedAt)
	}
	if after.Metadata["embedder"] != "model-v2" {
		t.Fatalf("expected embedder metadata %q, got %q", "model-v2", after.Metadata["embedder"])
	}
}

func TestIndexSessionScopedRebuildUsesSessionScopedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	seedCanonicalMessagesForSession(t, ctx, store, "session-1", "conversation-1", time.Date(2026, 3, 20, 19, 0, 0, 0, time.UTC))
	seedCanonicalMessagesForSession(t, ctx, store, "session-2", "conversation-2", time.Date(2026, 3, 20, 20, 0, 0, 0, time.UTC))

	index, err := New(store, Config{})
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	if err := index.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlitevec: %v", err)
	}

	stats, err := index.RebuildFromCanonicalMessages(ctx, fakeEmbedder{id: "test-embedder-v1", dims: 3}, application.VectorRebuildOptions{
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatalf("session-scoped rebuild: %v", err)
	}
	if stats.Scanned != 2 {
		t.Fatalf("expected 2 scanned messages for session-scoped rebuild, got %d", stats.Scanned)
	}

	globalState, err := index.State(ctx)
	if err != nil {
		t.Fatalf("global state: %v", err)
	}
	if globalState.LastRebuiltAt != nil {
		t.Fatalf("expected global state last_rebuilt_at to remain nil after session-scoped rebuild")
	}

	sessionState, err := index.StateForSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("session state: %v", err)
	}
	if sessionState.IndexName != "messages/session/session-1" {
		t.Fatalf("expected session-scoped state key, got %q", sessionState.IndexName)
	}
	if sessionState.LastRebuiltAt == nil {
		t.Fatalf("expected session-scoped last_rebuilt_at to be set")
	}

	otherState, err := index.StateForSession(ctx, "session-2")
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("expected not found for untouched session-2 state, got state=%+v err=%v", otherState, err)
	}
}

func TestIndexGlobalRebuildRefreshesSessionScopedStates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	messages := seedCanonicalMessages(t, ctx, store)

	index, err := New(store, Config{})
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	if err := index.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlitevec: %v", err)
	}
	if err := index.MarkSessionStale(ctx, messages[0].SessionID, time.Date(2026, 3, 20, 19, 5, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark session stale: %v", err)
	}

	stats, err := index.RebuildFromCanonicalMessages(ctx, fakeEmbedder{id: "test-embedder-v1", dims: 3}, application.VectorRebuildOptions{})
	if err != nil {
		t.Fatalf("global rebuild: %v", err)
	}
	if stats.Scanned != len(messages) {
		t.Fatalf("expected %d scanned messages, got %d", len(messages), stats.Scanned)
	}

	sessionState, err := index.StateForSession(ctx, messages[0].SessionID)
	if err != nil {
		t.Fatalf("session state after global rebuild: %v", err)
	}
	if sessionState.Status != application.VectorIndexStateStatusDisabled {
		t.Fatalf("expected session-scoped state to be refreshed by global rebuild, got %q", sessionState.Status)
	}
	if sessionState.LastRebuiltAt == nil {
		t.Fatalf("expected session-scoped state to record last_rebuilt_at after global rebuild")
	}
}

func TestIndexSearchDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	index, err := New(store, Config{})
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	if err := index.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlitevec: %v", err)
	}

	_, err = index.SearchMessages(ctx, application.VectorSearchQuery{
		SessionID: "session-1",
		Embedding: []float32{0.1, 0.2, 0.3},
		Limit:     5,
	})
	if !errors.Is(err, application.ErrDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func openTestStore(t *testing.T) *sqliteadapter.Store {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sqlitevec.db")
	store, err := sqliteadapter.Open(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		t.Fatalf("migrate canonical sqlite store: %v", err)
	}

	return store
}

func seedCanonicalMessages(t *testing.T, ctx context.Context, store *sqliteadapter.Store) []domain.Message {
	return seedCanonicalMessagesForSession(t, ctx, store, "session-1", "conversation-1", time.Date(2026, 3, 20, 19, 0, 0, 0, time.UTC))
}

func seedCanonicalMessagesForSession(t *testing.T, ctx context.Context, store *sqliteadapter.Store, sessionID domain.SessionID, conversationID domain.ConversationID, createdAt time.Time) []domain.Message {
	t.Helper()

	session, err := domain.NewSession(sessionID, domain.SessionModeFree, createdAt)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	session, err = session.Start()
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	firstMessage, err := domain.NewMessage(domain.Message{
		ID:             domain.MessageID("message-" + string(sessionID) + "-1"),
		SessionID:      session.ID,
		ConversationID: conversationID,
		Sender:         domain.UserSender("operator"),
		Channel:        domain.MessageChannelUser,
		Kind:           domain.MessageKindUtterance,
		Body:           "Plan the implementation in phases",
		Timestamp:      createdAt.Add(time.Second),
		Metadata:       map[string]any{"source": "review"},
	})
	if err != nil {
		t.Fatalf("new first message: %v", err)
	}

	secondMessage, err := domain.NewMessage(domain.Message{
		ID:             domain.MessageID("message-" + string(sessionID) + "-2"),
		SessionID:      session.ID,
		ConversationID: conversationID,
		Sender:         domain.SystemSender("runtime"),
		Channel:        domain.MessageChannelSystem,
		Kind:           domain.MessageKindEvent,
		Body:           "Workflow registered for deterministic replay",
		Timestamp:      createdAt.Add(2 * time.Second),
		Metadata:       map[string]any{"source": "runtime"},
	})
	if err != nil {
		t.Fatalf("new second message: %v", err)
	}

	if err := store.UnitOfWork().WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := store.Sessions().Save(txCtx, session); err != nil {
			return err
		}
		if err := store.Messages().Save(txCtx, firstMessage); err != nil {
			return err
		}
		if err := store.Messages().Save(txCtx, secondMessage); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("seed canonical messages: %v", err)
	}

	return []domain.Message{firstMessage, secondMessage}
}

type fakeEmbedder struct {
	id   string
	dims int
}

func (f fakeEmbedder) EmbedText(_ context.Context, text string) ([]float32, error) {
	dims := f.dims
	if dims <= 0 {
		dims = 3
	}

	embedding := make([]float32, dims)
	for idx := range embedding {
		embedding[idx] = float32(len(text) + idx)
	}
	return embedding, nil
}

func (f fakeEmbedder) EmbeddingIdentity() string {
	if f.id == "" {
		return "fake-embedder"
	}
	return f.id
}
