# AGENTS.MD

## Purpose

`internal/adapters/storage/sqlitevec` contains the optional SQLite vector-search adapter family for Phase 5B.

## Business Role

This package is where semantic retrieval is built without contaminating canonical runtime persistence. It exists to improve recall and retrieval while preserving the rule that correctness and durability do not depend on vector indexing.

## Rules

- this package is optional and must never become the source of truth for canonical runtime state
- all vector indexes must be derived from canonical relational rows
- disabled or unavailable vector behavior must fail explicitly and safely
- SQLite vector integration details must remain isolated here rather than leaking into the canonical `sqlite` package
- derived embedding ownership rows must remain rebuildable from canonical messages
- vector rebuild state must be durable enough to explain whether indexing is disabled, rebuilding, ready, or degraded
- build-gated `sqlite-vec` integration must not break the default build when the optional binding is absent
- rebuild skip decisions must include embedding configuration identity, not just source text hashes
- session-scoped rebuilds must write session-scoped state keys and must not claim the global index is current
- a global rebuild must also refresh the affected session-scoped state keys, because status and recall consult session-scoped freshness first

## Current Responsibilities

- own vector-side migrations for derived embedding and rebuild-state tables
- implement the application `VectorIndex` port against rebuildable derived rows
- provide rebuild hooks from canonical messages into derived embedding ownership rows
- expose a build-gated CGO integration path for `sqlite-vec` without making it a default correctness dependency
- keep default builds explicitly vector-disabled and searchable failure modes machine-readable through typed errors
- persist rebuild fingerprints that capture source text plus embedding configuration identity
- distinguish full-index state from session-scoped rebuild state in `vector_index_state`

## Update Triggers

Update this file when:

- real vector indexing is introduced
- the chosen SQLite vector binding changes
- fallback or degraded-mode behavior changes
- derived embedding schemas or rebuild-state semantics change
