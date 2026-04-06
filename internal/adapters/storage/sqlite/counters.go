package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) MaxSessionCounter(ctx context.Context) (int, error) {
	return maxNumericSuffix(ctx, s.db, "sessions", "id", "session-", 9)
}

func (s *Store) MaxMessageCounter(ctx context.Context) (int, error) {
	return maxNumericSuffix(ctx, s.db, "messages", "id", "message-", 9)
}

func maxNumericSuffix(ctx context.Context, db *sql.DB, table, column, prefix string, start int) (int, error) {
	query := fmt.Sprintf(
		`SELECT COALESCE(MAX(CAST(SUBSTR(%s, %d) AS INTEGER)), 0)
FROM %s
WHERE %s GLOB ?`,
		column,
		start,
		table,
		column,
	)

	var value int
	if err := db.QueryRowContext(ctx, query, prefix+"[0-9]*").Scan(&value); err != nil {
		return 0, fmt.Errorf("read max numeric suffix from %s.%s: %w", table, column, err)
	}

	return value, nil
}
