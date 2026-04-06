# AGENTS.MD

## Purpose

`internal/adapters/storage/sqlitevec/migrations` contains schema changes for the optional derived SQLite vector adapter.

## Business Role

These migrations define rebuildable embedding ownership state and vector index observability without turning vector search into canonical product state.

## Rules

- migrations here must only create or evolve derived vector-side tables
- canonical session, message, workflow, outbox, and stream tables must remain outside this directory
- every schema change must preserve rebuildability from canonical relational rows
- migration names must stay ordered and deterministic

## Current Responsibilities

- create embedding ownership tables keyed to canonical message IDs
- create vector index state tables for rebuild and degraded-mode observability
- evolve derived schemas when rebuild fingerprinting or scoped-state tracking changes

## Update Triggers

Update this file when:

- a new derived vector table is added
- rebuild bookkeeping changes
- vector observability or fallback semantics change
