# AGENTS.MD

## Purpose

`internal/adapters/storage/sqlite` contains the SQLite-backed canonical persistence adapter for Phase 5A.

## Business Role

This package is responsible for replacing the JSON runtime bridge with durable relational persistence while keeping canonical runtime state restart-safe, inspectable, and migration-driven.

## Rules

- canonical runtime records belong here, not in vector indexes
- repository methods must validate domain objects before persistence and validate decoded rows on the way out
- agent persistence must preserve historical rows needed for auditability while exposing only the active filesystem-backed catalog to operational lookups and list calls
- agent persistence must preserve canonical `provider` and `model` identity so mixed-provider sessions survive restart and catalog resyncs
- agent persistence must also preserve canonical agent `reasoning_effort` so Codex-backed catalogs keep the intended reasoning level after sync, restart, and replay
- agent persistence must preserve canonical `delegation_runtime`, `sandbox_workspace_root`, and `sandbox_workspace_mode` so mixed CLI-runtime rooms survive restart and catalog resyncs
- session persistence must preserve canonical actor-catalog selection so later invocations can reseed the correct roster automatically
- transaction handling must preserve state-and-outbox atomicity
- migrations must be idempotent and ordered
- message persistence must remain append-only and must reject duplicate message IDs rather than rewriting prior records
- persisted message ordering must remain deterministic even when timestamps include sub-second precision
- durable outbox delivery must claim rows before publish so concurrent CLI processes cannot publish the same canonical event twice
- persisted session stream rows derived from outbox delivery must remain idempotent per durable outbox sequence
- persisted sandbox tasks and agent handoffs are canonical runtime records and must remain auditable across restart
- persisted sandbox tasks must preserve the selected runtime name and any sandbox-root override so execution restarts do not silently move work into a different runtime or sandbox namespace
- SQLite vector integration must not be implemented in this package; it belongs in the sibling `sqlitevec` adapter family

## Current Responsibilities

- open and configure SQLite connections
- run embedded schema migrations
- implement SQLite repositories for sessions, messages, workflows, agents, sandbox tasks, agent handoffs, durable outbox rows, and persisted session stream rows
- flush durable outbox rows into the live event bus while persisting session stream rows and publish markers
- coordinate cross-process-safe outbox claiming and publish completion
- use the current `github.com/mattn/go-sqlite3` binding for canonical Phase 5A SQLite persistence

## Update Triggers

Update this file when:

- schema or migration layout changes
- repository behavior changes
- transaction semantics change
- canonical versus vector storage responsibilities change
