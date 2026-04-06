//go:build sqlitevec_cgo

package sqlitevec

import (
	"context"
	"database/sql"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"crew/internal/application"
)

type cgoProvider struct {
	dimensions int
}

func newProvider(cfg Config) (vectorProvider, error) {
	if !cfg.EnableSQLiteVec {
		return disabledProvider{reason: "sqlite-vec support disabled"}, nil
	}
	if cfg.Dimensions <= 0 {
		return nil, fmt.Errorf("sqlitevec dimensions must be greater than zero when sqlite-vec is enabled")
	}

	sqlite_vec.Auto()

	return &cgoProvider{dimensions: cfg.Dimensions}, nil
}

func (p *cgoProvider) name() string {
	return "sqlite-vec-cgo"
}

func (p *cgoProvider) status(context.Context) (application.VectorIndexStatus, error) {
	return application.VectorIndexStatusReady, nil
}

func (p *cgoProvider) ensureSchema(ctx context.Context, execer sqliteExecutor, dimensions int) error {
	if dimensions <= 0 {
		dimensions = p.dimensions
	}
	if dimensions <= 0 {
		return fmt.Errorf("sqlitevec dimensions must be greater than zero")
	}

	_, err := execer.ExecContext(
		ctx,
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS vec_message_embeddings USING vec0(embedding float[%d])`, dimensions),
	)
	if err != nil {
		return fmt.Errorf("ensure sqlite-vec message index schema: %w", err)
	}

	return nil
}

func (p *cgoProvider) upsert(ctx context.Context, execer sqliteExecutor, row storedEmbeddingRow) error {
	embedding, err := decodeEmbeddingBlob(row.Embedding)
	if err != nil {
		return fmt.Errorf("decode embedding blob for sqlite-vec message %q: %w", row.MessageID, err)
	}

	serialized, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("serialize sqlite-vec embedding for message %q: %w", row.MessageID, err)
	}

	_, err = execer.ExecContext(
		ctx,
		`INSERT INTO vec_message_embeddings(rowid, embedding)
VALUES(?, ?)
ON CONFLICT(rowid) DO UPDATE SET embedding = excluded.embedding`,
		row.storageRowID,
		serialized,
	)
	if err != nil {
		return fmt.Errorf("upsert sqlite-vec row for message %q: %w", row.MessageID, err)
	}

	return nil
}

func (p *cgoProvider) delete(ctx context.Context, execer sqliteExecutor, storageRowID int64) error {
	_, err := execer.ExecContext(ctx, `DELETE FROM vec_message_embeddings WHERE rowid = ?`, storageRowID)
	if err != nil {
		return fmt.Errorf("delete sqlite-vec row %d: %w", storageRowID, err)
	}
	return nil
}

func (p *cgoProvider) search(ctx context.Context, db *sql.DB, query application.VectorSearchQuery) ([]application.VectorSearchResult, error) {
	serialized, err := sqlite_vec.SerializeFloat32(query.Embedding)
	if err != nil {
		return nil, fmt.Errorf("serialize sqlite-vec search embedding: %w", err)
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT me.message_id, vec.distance
FROM vec_message_embeddings AS vec
JOIN message_embeddings AS me ON me.id = vec.rowid
WHERE vec.embedding MATCH ?
  AND k = ?
  AND me.session_id = ?
ORDER BY vec.distance ASC
LIMIT ?`,
		serialized,
		query.Limit,
		string(query.SessionID),
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search sqlite-vec messages for session %q: %w", query.SessionID, err)
	}
	defer rows.Close()

	var results []application.VectorSearchResult
	for rows.Next() {
		var result application.VectorSearchResult
		if err := rows.Scan(&result.MessageID, &result.Distance); err != nil {
			return nil, fmt.Errorf("scan sqlite-vec search result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite-vec search results: %w", err)
	}

	return results, nil
}
