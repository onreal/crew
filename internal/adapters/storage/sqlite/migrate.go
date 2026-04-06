package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	files, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("glob sqlite migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		applied, err := migrationApplied(ctx, s.db, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		content, err := migrationFiles.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read sqlite migration %q: %w", file, err)
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin sqlite migration %q: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply sqlite migration %q: %w", version, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			version,
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record sqlite migration %q: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit sqlite migration %q: %w", version, err)
		}
	}

	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var exists int
	if err := db.QueryRowContext(
		ctx,
		`SELECT 1 FROM schema_migrations WHERE version = ? LIMIT 1`,
		version,
	).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("check sqlite migration %q: %w", version, err)
	}

	return true, nil
}
