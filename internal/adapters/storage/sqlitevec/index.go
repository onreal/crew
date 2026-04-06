package sqlitevec

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sqliteadapter "crew/internal/adapters/storage/sqlite"
	"crew/internal/application"
	"crew/internal/domain"
)

var ErrVectorIndexDisabled = fmt.Errorf("%w: sqlite vector index is disabled", application.ErrDisabled)

const (
	indexStateReady      = "ready"
	indexStateDisabled   = "disabled"
	indexStateDegraded   = "degraded"
	indexStateRebuilding = "rebuilding"

	defaultIndexName = "messages"
)

type Config struct {
	EnableSQLiteVec bool
	Dimensions      int
}

type MessageEmbeddingSnapshot struct {
	MessageID          domain.MessageID
	SessionID          domain.SessionID
	SourceText         string
	SourceSHA          string
	RebuildFingerprint string
	Dimensions         int
	Metadata           map[string]string
	UpdatedAt          time.Time
	Embedding          []float32
	StorageRowID       int64
}

type Index struct {
	store      *sqliteadapter.Store
	provider   vectorProvider
	dimensions int
	now        func() time.Time
}

type DisabledIndex struct{}

func NewDisabled() *DisabledIndex {
	return &DisabledIndex{}
}

func New(store *sqliteadapter.Store, cfg Config) (*Index, error) {
	if store == nil {
		return nil, fmt.Errorf("sqlitevec store must not be nil")
	}

	provider, err := newProvider(cfg)
	if err != nil {
		return nil, err
	}

	return &Index{
		store:      store,
		provider:   provider,
		dimensions: cfg.Dimensions,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}, nil
}

func (i *Index) Migrate(ctx context.Context) error {
	if i == nil {
		return fmt.Errorf("sqlitevec index must not be nil")
	}

	if err := migrate(ctx, i.store.DB()); err != nil {
		return err
	}

	tx, err := i.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlitevec provider schema transaction: %w", err)
	}

	if err := i.provider.ensureSchema(ctx, tx, i.dimensions); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlitevec provider schema transaction: %w", err)
	}

	stateStatus := indexStateDisabled
	if status, err := i.provider.status(ctx); err == nil && status == application.VectorIndexStatusReady {
		stateStatus = indexStateReady
	}
	if err := i.upsertState(ctx, application.VectorIndexState{
		IndexName: defaultIndexName,
		Provider:  i.provider.name(),
		Status:    application.VectorIndexStateStatus(stateStatus),
		UpdatedAt: i.now(),
	}); err != nil {
		return err
	}

	return nil
}

func (i *Index) Status(ctx context.Context) (application.VectorIndexStatus, error) {
	if i == nil {
		return application.VectorIndexStatusDisabled, fmt.Errorf("sqlitevec index must not be nil")
	}
	return i.provider.status(ctx)
}

func (i *Index) UpsertMessageEmbedding(ctx context.Context, record application.MessageEmbeddingRecord) error {
	if i == nil {
		return fmt.Errorf("sqlitevec index must not be nil")
	}

	row, err := i.embeddingRowFromRecord(record)
	if err != nil {
		return err
	}

	return i.withTransaction(ctx, func(tx *sql.Tx) error {
		rowID, err := upsertOwnershipRow(ctx, tx, row)
		if err != nil {
			return err
		}
		return i.provider.upsert(ctx, tx, storedEmbeddingRow{
			messageEmbeddingRow: row,
			storageRowID:        rowID,
		})
	})
}

func (i *Index) DeleteMessageEmbedding(ctx context.Context, messageID domain.MessageID) error {
	if i == nil {
		return fmt.Errorf("sqlitevec index must not be nil")
	}
	if err := messageID.Validate(); err != nil {
		return err
	}

	return i.withTransaction(ctx, func(tx *sql.Tx) error {
		rowID, exists, err := lookupStorageRowID(ctx, tx, messageID)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}

		if err := i.provider.delete(ctx, tx, rowID); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM message_embeddings WHERE message_id = ?`, string(messageID)); err != nil {
			return fmt.Errorf("delete sqlitevec ownership row for message %q: %w", messageID, err)
		}

		return nil
	})
}

func (i *Index) SearchMessages(ctx context.Context, query application.VectorSearchQuery) ([]application.VectorSearchResult, error) {
	if i == nil {
		return nil, fmt.Errorf("sqlitevec index must not be nil")
	}
	if err := query.SessionID.Validate(); err != nil {
		return nil, err
	}
	if len(query.Embedding) == 0 {
		return nil, fmt.Errorf("vector query embedding must not be empty")
	}
	if query.Limit <= 0 {
		query.Limit = 10
	}

	return i.provider.search(ctx, i.store.DB(), query)
}

func (i *Index) RebuildFromCanonicalMessages(ctx context.Context, embedder application.Embedder, options application.VectorRebuildOptions) (application.VectorRebuildStats, error) {
	var stats application.VectorRebuildStats
	if i == nil {
		return stats, fmt.Errorf("sqlitevec index must not be nil")
	}
	if embedder == nil {
		return stats, fmt.Errorf("sqlitevec embedder must not be nil")
	}
	if options.SessionID != "" {
		if err := options.SessionID.Validate(); err != nil {
			return stats, err
		}
	}

	stateKey := stateKeyForSession(options.SessionID)
	stats.StartedAt = i.now()
	if err := i.upsertState(ctx, application.VectorIndexState{
		IndexName: stateKey,
		Provider:  i.provider.name(),
		Status:    application.VectorIndexStateStatusRebuilding,
		UpdatedAt: stats.StartedAt,
	}); err != nil {
		return stats, err
	}

	messages, err := i.listCanonicalMessages(ctx, options.SessionID)
	if err != nil {
		_ = i.upsertState(ctx, application.VectorIndexState{
			IndexName: stateKey,
			Provider:  i.provider.name(),
			Status:    application.VectorIndexStateStatusDegraded,
			LastError: err.Error(),
			UpdatedAt: i.now(),
		})
		return stats, err
	}

	for _, message := range messages {
		stats.Scanned++

		rebuildFingerprint := buildRebuildFingerprint(embedder, message.Body, i.rebuildDimensionMarker())
		existing, exists, err := i.GetMessageEmbedding(ctx, message.ID)
		if err != nil {
			_ = i.upsertState(ctx, application.VectorIndexState{
				IndexName: stateKey,
				Provider:  i.provider.name(),
				Status:    application.VectorIndexStateStatusDegraded,
				LastError: err.Error(),
				UpdatedAt: i.now(),
			})
			return stats, err
		}
		if exists && !options.Force && existing.RebuildFingerprint == rebuildFingerprint {
			stats.Skipped++
			continue
		}

		embedding, err := embedder.EmbedText(ctx, message.Body)
		if err != nil {
			stateErr := i.upsertState(ctx, application.VectorIndexState{
				IndexName: stateKey,
				Provider:  i.provider.name(),
				Status:    application.VectorIndexStateStatusDegraded,
				LastError: err.Error(),
				UpdatedAt: i.now(),
			})
			if stateErr != nil {
				return stats, fmt.Errorf("embed canonical message %q: %w (also failed to update index state: %v)", message.ID, err, stateErr)
			}
			return stats, fmt.Errorf("embed canonical message %q: %w", message.ID, err)
		}

		if err := i.UpsertMessageEmbedding(ctx, application.MessageEmbeddingRecord{
			MessageID:  message.ID,
			SessionID:  message.SessionID,
			Embedding:  embedding,
			SourceText: message.Body,
			Metadata: map[string]string{
				"channel":     string(message.Channel),
				"kind":        string(message.Kind),
				"embedder":    embedderIdentity(embedder),
				"sender_type": string(message.Sender.Type),
			},
			UpdatedAt: i.now(),
		}); err != nil {
			stateErr := i.upsertState(ctx, application.VectorIndexState{
				IndexName: stateKey,
				Provider:  i.provider.name(),
				Status:    application.VectorIndexStateStatusDegraded,
				LastError: err.Error(),
				UpdatedAt: i.now(),
			})
			if stateErr != nil {
				return stats, fmt.Errorf("rebuild sqlitevec message %q: %w (also failed to update index state: %v)", message.ID, err, stateErr)
			}
			return stats, fmt.Errorf("rebuild sqlitevec message %q: %w", message.ID, err)
		}

		stats.Upserted++
	}

	stats.FinishedAt = i.now()
	finalStatus := application.VectorIndexStateStatusDisabled
	if status, err := i.provider.status(ctx); err == nil && status == application.VectorIndexStatusReady {
		finalStatus = application.VectorIndexStateStatusReady
	}

	if err := i.upsertState(ctx, application.VectorIndexState{
		IndexName:     stateKey,
		Provider:      i.provider.name(),
		Status:        finalStatus,
		LastRebuiltAt: &stats.FinishedAt,
		UpdatedAt:     stats.FinishedAt,
	}); err != nil {
		return stats, err
	}

	if options.SessionID == "" {
		for _, sessionID := range uniqueMessageSessionIDs(messages) {
			if err := i.upsertState(ctx, application.VectorIndexState{
				IndexName:     stateKeyForSession(sessionID),
				Provider:      i.provider.name(),
				Status:        finalStatus,
				LastRebuiltAt: &stats.FinishedAt,
				UpdatedAt:     stats.FinishedAt,
			}); err != nil {
				return stats, err
			}
		}
	}

	return stats, nil
}

func (i *Index) State(ctx context.Context) (application.VectorIndexState, error) {
	return i.loadState(ctx, defaultIndexName)
}

func (i *Index) StateForSession(ctx context.Context, sessionID domain.SessionID) (application.VectorIndexState, error) {
	if i == nil {
		return application.VectorIndexState{}, fmt.Errorf("sqlitevec index must not be nil")
	}
	if err := sessionID.Validate(); err != nil {
		return application.VectorIndexState{}, err
	}

	return i.loadState(ctx, stateKeyForSession(sessionID))
}

func (i *Index) MarkSessionStale(ctx context.Context, sessionID domain.SessionID, occurredAt time.Time) error {
	if i == nil {
		return fmt.Errorf("sqlitevec index must not be nil")
	}
	if err := sessionID.Validate(); err != nil {
		return err
	}
	if occurredAt.IsZero() {
		return fmt.Errorf("sqlitevec stale timestamp must not be zero")
	}

	existing, err := i.loadStateOptional(ctx, stateKeyForSession(sessionID))
	if err != nil {
		return err
	}

	state := application.VectorIndexState{
		IndexName: stateKeyForSession(sessionID),
		Provider:  i.provider.name(),
		Status:    application.VectorIndexStateStatusStale,
		UpdatedAt: occurredAt.UTC(),
	}
	if existing != nil {
		state.LastRebuiltAt = existing.LastRebuiltAt
	}

	return i.upsertState(ctx, state)
}

func (i *Index) loadState(ctx context.Context, stateKey string) (application.VectorIndexState, error) {
	state, err := i.loadStateOptional(ctx, stateKey)
	if err != nil {
		return application.VectorIndexState{}, err
	}
	if state == nil {
		return application.VectorIndexState{}, application.NotFoundError{Entity: "vector_index_state", ID: stateKey}
	}
	return *state, nil
}

func (i *Index) loadStateOptional(ctx context.Context, stateKey string) (*application.VectorIndexState, error) {
	if i == nil {
		return nil, fmt.Errorf("sqlitevec index must not be nil")
	}
	var (
		state          application.VectorIndexState
		lastRebuiltRaw sql.NullString
		lastErrorRaw   sql.NullString
		updatedAtRaw   string
	)
	err := i.store.Executor(ctx).QueryRowContext(
		ctx,
		`SELECT index_name, provider, status, last_rebuilt_at, last_error, updated_at
FROM vector_index_state
WHERE index_name = ?`,
		stateKey,
	).Scan(&state.IndexName, &state.Provider, &state.Status, &lastRebuiltRaw, &lastErrorRaw, &updatedAtRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load sqlitevec state: %w", err)
	}

	if lastRebuiltRaw.Valid {
		parsed, err := parseTimestamp(lastRebuiltRaw.String)
		if err != nil {
			return nil, err
		}
		state.LastRebuiltAt = &parsed
	}
	if lastErrorRaw.Valid {
		state.LastError = lastErrorRaw.String
	}
	state.UpdatedAt, err = parseTimestamp(updatedAtRaw)
	if err != nil {
		return nil, err
	}

	return &state, nil
}

func (i *Index) GetMessageEmbedding(ctx context.Context, messageID domain.MessageID) (MessageEmbeddingSnapshot, bool, error) {
	if i == nil {
		return MessageEmbeddingSnapshot{}, false, fmt.Errorf("sqlitevec index must not be nil")
	}
	if err := messageID.Validate(); err != nil {
		return MessageEmbeddingSnapshot{}, false, err
	}

	var (
		snapshot      MessageEmbeddingSnapshot
		metadataJSON  string
		updatedAtRaw  string
		embeddingBlob []byte
	)
	err := i.store.Executor(ctx).QueryRowContext(
		ctx,
		`SELECT id, session_id, source_text, source_sha256, rebuild_fingerprint, embedding_dimensions, metadata_json, updated_at, embedding_blob
FROM message_embeddings
WHERE message_id = ?`,
		string(messageID),
	).Scan(
		&snapshot.StorageRowID,
		&snapshot.SessionID,
		&snapshot.SourceText,
		&snapshot.SourceSHA,
		&snapshot.RebuildFingerprint,
		&snapshot.Dimensions,
		&metadataJSON,
		&updatedAtRaw,
		&embeddingBlob,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MessageEmbeddingSnapshot{}, false, nil
		}
		return MessageEmbeddingSnapshot{}, false, fmt.Errorf("load sqlitevec embedding for message %q: %w", messageID, err)
	}

	updatedAt, err := parseTimestamp(updatedAtRaw)
	if err != nil {
		return MessageEmbeddingSnapshot{}, false, err
	}
	snapshot.MessageID = messageID
	snapshot.UpdatedAt = updatedAt

	if err := json.Unmarshal([]byte(metadataJSON), &snapshot.Metadata); err != nil {
		return MessageEmbeddingSnapshot{}, false, fmt.Errorf("decode sqlitevec embedding metadata for message %q: %w", messageID, err)
	}

	snapshot.Embedding, err = decodeEmbeddingBlob(embeddingBlob)
	if err != nil {
		return MessageEmbeddingSnapshot{}, false, fmt.Errorf("decode sqlitevec embedding for message %q: %w", messageID, err)
	}

	return snapshot, true, nil
}

func (i *Index) withTransaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := i.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlitevec transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlitevec transaction: %w", err)
	}

	return nil
}

func (i *Index) upsertState(ctx context.Context, state application.VectorIndexState) error {
	if state.IndexName == "" {
		state.IndexName = defaultIndexName
	}
	if state.Provider == "" {
		state.Provider = i.provider.name()
	}
	if state.Status == "" {
		state.Status = application.VectorIndexStateStatusDisabled
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = i.now()
	}

	var lastRebuilt any
	if state.LastRebuiltAt != nil {
		lastRebuilt = formatTimestamp(*state.LastRebuiltAt)
	}

	var lastError any
	if strings.TrimSpace(state.LastError) != "" {
		lastError = state.LastError
	}

	_, err := i.store.Executor(ctx).ExecContext(
		ctx,
		`INSERT INTO vector_index_state(index_name, provider, status, last_rebuilt_at, last_error, updated_at)
VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(index_name) DO UPDATE SET
  provider = excluded.provider,
  status = excluded.status,
  last_rebuilt_at = excluded.last_rebuilt_at,
  last_error = excluded.last_error,
  updated_at = excluded.updated_at`,
		state.IndexName,
		state.Provider,
		string(state.Status),
		lastRebuilt,
		lastError,
		formatTimestamp(state.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert sqlitevec state %q: %w", state.IndexName, err)
	}

	return nil
}

func (i *Index) embeddingRowFromRecord(record application.MessageEmbeddingRecord) (messageEmbeddingRow, error) {
	if err := record.MessageID.Validate(); err != nil {
		return messageEmbeddingRow{}, err
	}
	if err := record.SessionID.Validate(); err != nil {
		return messageEmbeddingRow{}, err
	}
	if strings.TrimSpace(record.SourceText) == "" {
		return messageEmbeddingRow{}, fmt.Errorf("message embedding source text must not be empty")
	}
	if len(record.Embedding) == 0 {
		return messageEmbeddingRow{}, fmt.Errorf("message embedding vector must not be empty")
	}
	if record.UpdatedAt.IsZero() {
		return messageEmbeddingRow{}, fmt.Errorf("message embedding updated_at must not be zero")
	}
	if i.dimensions > 0 && len(record.Embedding) != i.dimensions {
		return messageEmbeddingRow{}, fmt.Errorf("message embedding dimensions %d do not match sqlitevec index dimensions %d", len(record.Embedding), i.dimensions)
	}

	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return messageEmbeddingRow{}, fmt.Errorf("encode message embedding metadata for %q: %w", record.MessageID, err)
	}
	blob, err := encodeEmbeddingBlob(record.Embedding)
	if err != nil {
		return messageEmbeddingRow{}, fmt.Errorf("encode message embedding blob for %q: %w", record.MessageID, err)
	}

	return messageEmbeddingRow{
		MessageID:          record.MessageID,
		SessionID:          record.SessionID,
		SourceText:         record.SourceText,
		SourceSHA:          hashSourceText(record.SourceText),
		RebuildFingerprint: buildRebuildFingerprintFromMetadata(record.SourceText, i.rebuildDimensionMarker(), record.Metadata["embedder"]),
		Embedding:          blob,
		Dimensions:         len(record.Embedding),
		MetadataJSON:       metadataJSON,
		UpdatedAt:          record.UpdatedAt.UTC(),
	}, nil
}

func (i *Index) listCanonicalMessages(ctx context.Context, sessionID domain.SessionID) ([]domain.Message, error) {
	query := `SELECT
  id,
  session_id,
  conversation_id,
  sender_type,
  sender_id,
  recipient_ids_json,
  channel,
  kind,
  body,
  reply_to,
  recorded_at,
  metadata_json
FROM messages`
	args := make([]any, 0, 1)
	if sessionID != "" {
		query += ` WHERE session_id = ?`
		args = append(args, string(sessionID))
	}
	query += ` ORDER BY recorded_at ASC, id ASC`

	rows, err := i.store.Executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list canonical messages for sqlitevec rebuild: %w", err)
	}
	defer rows.Close()

	var messages []domain.Message
	for rows.Next() {
		var (
			id             string
			sessionIDRaw   string
			conversationID string
			senderType     string
			senderID       string
			recipientsJSON string
			channel        string
			kind           string
			body           string
			replyTo        sql.NullString
			recordedAtRaw  string
			metadataJSON   string
			recipients     []domain.AgentID
			metadata       map[string]any
		)

		if err := rows.Scan(
			&id,
			&sessionIDRaw,
			&conversationID,
			&senderType,
			&senderID,
			&recipientsJSON,
			&channel,
			&kind,
			&body,
			&replyTo,
			&recordedAtRaw,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan canonical message for sqlitevec rebuild: %w", err)
		}

		if err := json.Unmarshal([]byte(recipientsJSON), &recipients); err != nil {
			return nil, fmt.Errorf("decode canonical message recipients %q for sqlitevec rebuild: %w", id, err)
		}
		if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
			return nil, fmt.Errorf("decode canonical message metadata %q for sqlitevec rebuild: %w", id, err)
		}

		recordedAt, err := parseTimestamp(recordedAtRaw)
		if err != nil {
			return nil, err
		}

		message, err := domain.NewMessage(domain.Message{
			ID:             domain.MessageID(id),
			SessionID:      domain.SessionID(sessionIDRaw),
			ConversationID: domain.ConversationID(conversationID),
			Sender: domain.MessageSender{
				Type: domain.MessageSenderType(senderType),
				ID:   senderID,
			},
			ToAgentIDs: recipients,
			Channel:    domain.MessageChannel(channel),
			Kind:       domain.MessageKind(kind),
			Body:       body,
			ReplyTo:    domain.MessageID(replyTo.String),
			Timestamp:  recordedAt,
			Metadata:   metadata,
		})
		if err != nil {
			return nil, fmt.Errorf("validate canonical message %q for sqlitevec rebuild: %w", id, err)
		}

		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canonical messages for sqlitevec rebuild: %w", err)
	}

	return messages, nil
}

func (d *DisabledIndex) Status(context.Context) (application.VectorIndexStatus, error) {
	return application.VectorIndexStatusDisabled, nil
}

func (d *DisabledIndex) UpsertMessageEmbedding(context.Context, application.MessageEmbeddingRecord) error {
	return ErrVectorIndexDisabled
}

func (d *DisabledIndex) DeleteMessageEmbedding(context.Context, domain.MessageID) error {
	return ErrVectorIndexDisabled
}

func (d *DisabledIndex) SearchMessages(context.Context, application.VectorSearchQuery) ([]application.VectorSearchResult, error) {
	return nil, ErrVectorIndexDisabled
}

func hashSourceText(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func buildRebuildFingerprint(embedder application.Embedder, sourceText string, dimensions int) string {
	return buildRebuildFingerprintFromMetadata(sourceText, dimensions, embedderIdentity(embedder))
}

func buildRebuildFingerprintFromMetadata(sourceText string, dimensions int, embedderID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", sourceText, dimensions, embedderID)))
	return hex.EncodeToString(sum[:])
}

func embedderIdentity(embedder application.Embedder) string {
	if embedder == nil {
		return "<nil>"
	}
	identity := strings.TrimSpace(embedder.EmbeddingIdentity())
	if identity != "" {
		return identity
	}
	return fmt.Sprintf("%T", embedder)
}

func stateKeyForSession(sessionID domain.SessionID) string {
	if sessionID == "" {
		return defaultIndexName
	}
	return fmt.Sprintf("%s/session/%s", defaultIndexName, sessionID)
}

func (i *Index) rebuildDimensionMarker() int {
	if i == nil {
		return 0
	}
	if i.dimensions > 0 {
		return i.dimensions
	}
	return 0
}

func uniqueMessageSessionIDs(messages []domain.Message) []domain.SessionID {
	seen := make(map[domain.SessionID]struct{}, len(messages))
	sessionIDs := make([]domain.SessionID, 0, len(messages))
	for _, message := range messages {
		if _, exists := seen[message.SessionID]; exists {
			continue
		}
		seen[message.SessionID] = struct{}{}
		sessionIDs = append(sessionIDs, message.SessionID)
	}
	return sessionIDs
}
