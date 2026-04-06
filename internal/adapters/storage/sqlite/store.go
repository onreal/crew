package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const driverName = "sqlite3"

type Store struct {
	db *sql.DB
}

type txContextKey struct{}

type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite path must not be empty")
	}

	dsn := path
	if strings.Contains(path, "?") {
		dsn += "&_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL"
	} else {
		dsn += "?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL"
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", path, err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database %q: %w", path, err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
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

func (s *Store) Outbox() *OutboxRepository {
	return &OutboxRepository{store: s}
}

func (s *Store) SessionStreams() *SessionStreamRepository {
	return &SessionStreamRepository{store: s}
}

func (s *Store) UnitOfWork() *UnitOfWork {
	return &UnitOfWork{store: s}
}

func (s *Store) txFromContext(ctx context.Context) (*sql.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{}).(*sql.Tx)
	return tx, ok
}

func (s *Store) execer(ctx context.Context) Executor {
	if tx, ok := s.txFromContext(ctx); ok {
		return tx
	}

	return s.db
}

func (s *Store) Executor(ctx context.Context) Executor {
	return s.execer(ctx)
}

type UnitOfWork struct {
	store *Store
}

func (u *UnitOfWork) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := u.store.txFromContext(ctx); ok {
		return fn(ctx)
	}

	tx, err := u.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite transaction: %w", err)
	}

	txCtx := context.WithValue(ctx, txContextKey{}, tx)
	if err := fn(txCtx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("rollback sqlite transaction after error %v: %w", err, rollbackErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}

	return nil
}
