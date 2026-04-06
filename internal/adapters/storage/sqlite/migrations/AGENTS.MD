# AGENTS.MD

## Purpose

`internal/adapters/storage/sqlite/migrations` contains ordered SQL migrations for the canonical SQLite schema.

## Business Role

These migrations define the durable shape of runtime state. They protect restart recovery, auditability, and forward-safe schema evolution.

## Rules

- migration files must be append-only once released
- migration filenames must remain lexically ordered
- canonical tables for sessions, messages, workflows, agents, sandbox tasks, agent handoffs, outbox rows, and stream projections must stay explicit
- agent-table migrations must preserve the distinction between the active operational catalog and historical agent rows needed for replay and foreign-key safety
- additive migrations must preserve canonical agent provider/model identity without rewriting historical agent rows ad hoc
- additive migrations must also preserve canonical agent delegation-runtime and sandbox-root ownership without forcing rooms back onto one global sandbox runtime
- session-table migrations must preserve canonical actor-catalog selection without collapsing it back into ephemeral CLI state
- sandbox-task migrations must preserve assigned runtime names and sandbox-root overrides so restart-safe execution keeps using the same runtime and sandbox namespace
- outbox claim/lease and stream deduplication fields must evolve through additive migrations rather than ad hoc schema edits
- destructive schema changes require a clearly documented compatibility plan

## Update Triggers

Update this file when:

- a migration is added
- migration ordering or conventions change
- canonical table ownership changes
