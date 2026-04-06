package sqlitevec

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"crew/internal/application"
	"crew/internal/domain"
)

const sqliteVecTimestampLayout = "2006-01-02T15:04:05.000000000Z"

type vectorProvider interface {
	name() string
	status(ctx context.Context) (application.VectorIndexStatus, error)
	ensureSchema(ctx context.Context, execer sqliteExecutor, dimensions int) error
	upsert(ctx context.Context, execer sqliteExecutor, row storedEmbeddingRow) error
	delete(ctx context.Context, execer sqliteExecutor, storageRowID int64) error
	search(ctx context.Context, db *sql.DB, query application.VectorSearchQuery) ([]application.VectorSearchResult, error)
}

type disabledProvider struct {
	reason string
}

type sqliteExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type messageEmbeddingRow struct {
	MessageID          domain.MessageID
	SessionID          domain.SessionID
	SourceText         string
	SourceSHA          string
	RebuildFingerprint string
	Embedding          []byte
	Dimensions         int
	MetadataJSON       []byte
	UpdatedAt          time.Time
}

type storedEmbeddingRow struct {
	messageEmbeddingRow
	storageRowID int64
}

func upsertOwnershipRow(ctx context.Context, execer sqliteExecutor, row messageEmbeddingRow) (int64, error) {
	_, err := execer.ExecContext(
		ctx,
		`INSERT INTO message_embeddings(
  message_id,
  session_id,
  source_text,
  source_sha256,
  rebuild_fingerprint,
  embedding_blob,
  embedding_dimensions,
  metadata_json,
  updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(message_id) DO UPDATE SET
  session_id = excluded.session_id,
  source_text = excluded.source_text,
  source_sha256 = excluded.source_sha256,
  rebuild_fingerprint = excluded.rebuild_fingerprint,
  embedding_blob = excluded.embedding_blob,
  embedding_dimensions = excluded.embedding_dimensions,
  metadata_json = excluded.metadata_json,
  updated_at = excluded.updated_at`,
		string(row.MessageID),
		string(row.SessionID),
		row.SourceText,
		row.SourceSHA,
		row.RebuildFingerprint,
		row.Embedding,
		row.Dimensions,
		string(row.MetadataJSON),
		formatTimestamp(row.UpdatedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("upsert sqlitevec ownership row for message %q: %w", row.MessageID, err)
	}

	var storageRowID int64
	if err := execer.QueryRowContext(
		ctx,
		`SELECT id FROM message_embeddings WHERE message_id = ?`,
		string(row.MessageID),
	).Scan(&storageRowID); err != nil {
		return 0, fmt.Errorf("load sqlitevec ownership row id for message %q: %w", row.MessageID, err)
	}

	return storageRowID, nil
}

func lookupStorageRowID(ctx context.Context, execer sqliteExecutor, messageID domain.MessageID) (int64, bool, error) {
	var rowID int64
	err := execer.QueryRowContext(
		ctx,
		`SELECT id FROM message_embeddings WHERE message_id = ?`,
		string(messageID),
	).Scan(&rowID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("lookup sqlitevec storage row for message %q: %w", messageID, err)
	}

	return rowID, true, nil
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Format(sqliteVecTimestampLayout)
}

func parseTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(sqliteVecTimestampLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse sqlitevec timestamp %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

func encodeEmbeddingBlob(embedding []float32) ([]byte, error) {
	if len(embedding) == 0 {
		return nil, fmt.Errorf("embedding must not be empty")
	}

	blob := make([]byte, len(embedding)*4)
	for idx, value := range embedding {
		binary.LittleEndian.PutUint32(blob[idx*4:], math.Float32bits(value))
	}
	return blob, nil
}

func decodeEmbeddingBlob(blob []byte) ([]float32, error) {
	if len(blob) == 0 {
		return nil, fmt.Errorf("embedding blob must not be empty")
	}
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("embedding blob length %d is not a multiple of 4", len(blob))
	}

	embedding := make([]float32, len(blob)/4)
	for idx := range embedding {
		embedding[idx] = math.Float32frombits(binary.LittleEndian.Uint32(blob[idx*4:]))
	}
	return embedding, nil
}

func (p disabledProvider) name() string {
	return "disabled"
}

func (p disabledProvider) status(context.Context) (application.VectorIndexStatus, error) {
	return application.VectorIndexStatusDisabled, nil
}

func (p disabledProvider) ensureSchema(context.Context, sqliteExecutor, int) error {
	return nil
}

func (p disabledProvider) upsert(context.Context, sqliteExecutor, storedEmbeddingRow) error {
	return nil
}

func (p disabledProvider) delete(context.Context, sqliteExecutor, int64) error {
	return nil
}

func (p disabledProvider) search(context.Context, *sql.DB, application.VectorSearchQuery) ([]application.VectorSearchResult, error) {
	return nil, fmt.Errorf("%w: %s", ErrVectorIndexDisabled, p.reason)
}
