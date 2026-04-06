# SQLite And Vector Plan

## Decision

Phase 5 will use SQLite as the mandatory persistence layer for canonical runtime state.

`sqlite-vec` is a good fit for future local-first semantic retrieval, but it will be treated as an optional derived-index adapter, not as a prerequisite for durable session, message, workflow, or outbox persistence.

## Current Implementation Status

The current implementation slices now exist:

- SQLite store bootstrap and migration execution
- canonical repository implementations for sessions, messages, workflows, agents, outbox rows, and session stream rows
- a separate `internal/adapters/storage/sqlitevec` package owns the vector adapter boundary behind the application vector port
- `sqlitevec` now has its own embedded migrations for derived `message_embeddings` ownership rows and `vector_index_state`
- `sqlitevec` can rebuild derived embeddings from canonical messages even when live vector search remains disabled
- rebuild fingerprints now include embedding configuration identity so non-force rebuilds do not silently keep stale rows after model or dimension changes
- session-scoped rebuilds now persist session-scoped state keys instead of overwriting the global index freshness record
- a build-gated CGO integration path exists for `sqlite-vec` via `-tags sqlitevec_cgo`
- that opt-in path still requires the upstream optional module `github.com/asg017/sqlite-vec-go-bindings/cgo` to be added when the feature is activated
- the current Phase 5A adapter is implemented with `github.com/mattn/go-sqlite3`
- the live CLI/runtime path now uses SQLite instead of the former JSON snapshot bridge

Phase 5A is complete enough for canonical runtime persistence. Phase 5B is now in the isolated adapter stage: derived ownership tables and rebuild hooks exist, but vector search is still optional and not wired into operator workflows.

## Why This Fits `crew`

`crew` is explicitly local-first. SQLite matches the current product direction because it is embedded, easy to ship, and keeps session durability close to the CLI and TUI runtime.

Vector search is also a natural future fit for:

- session memory and recall
- semantic lookup across prior messages
- document retrieval for agents
- future knowledge-base features without introducing a separate always-on vector service

## Why `sqlite-vec` Is Not The Core Persistence Layer

The upstream `sqlite-vec` project is usable from Go today, but it is explicitly pre-v1 and subject to breaking changes. That makes it appropriate as an isolated adapter with a narrow contract, but not as a dependency that the correctness of the whole runtime relies on.

For `crew`, canonical product state must remain:

- relational
- migratable
- replayable
- inspectable without vector tooling
- recoverable even when vector support is missing

## Architectural Rule

Canonical state lives in normal SQLite tables.

Vector data is derived from canonical state and must be rebuildable.

This means:

- session rows stay in relational tables
- message rows stay in relational tables
- workflow state stays in relational tables
- durable outbox state stays in relational tables
- stream projections stay in relational tables
- embeddings and vector indexes are sidecar data derived from canonical records

## Recommended Adapter Shape

### Mandatory adapters in Phase 5A

- `internal/adapters/storage/sqlite` for canonical repositories
- migration runner for schema versioning
- transactional boundary that preserves current application outbox guarantees

### Optional adapters in Phase 5B

- `internal/adapters/storage/sqlitevec` or an equivalent clearly isolated vector adapter
- embedding serialization and vector query implementation
- optional index rebuild tooling

The application layer should depend on ports such as:

- canonical repositories for sessions, messages, workflows, and projections
- an optional vector index port for write, delete, rebuild, and nearest-neighbor lookup

The application layer must not know whether vector support is implemented with `sqlite-vec`, another SQLite extension, or a no-op fallback.

## Go Integration Direction

Upstream `sqlite-vec` documents two Go integration paths:

- a CGO path for drivers such as `github.com/mattn/go-sqlite3`
- a non-CGO path built around `github.com/ncruces/go-sqlite3`

For canonical Phase 5A persistence, that decision is now made: the current SQLite adapter uses the CGO-backed `github.com/mattn/go-sqlite3` driver.

That choice is acceptable for the current phase because:

- it is stable and widely used
- it supports the embedded local-first SQLite path cleanly
- it gives us a concrete basis for real repository and migration tests

The current vector-side binding direction is a build-gated CGO path that matches the existing `github.com/mattn/go-sqlite3` runtime foundation. That keeps the default build stable while allowing an opt-in `sqlite-vec` path for future retrieval work. The source tree now includes that tagged code path, but the optional upstream binding is intentionally not part of the default module graph yet.

Current default posture:

- SQLite persistence is mandatory
- vector indexing is optional and build-gated
- vector-disabled execution must remain correct

## Data Model Direction

Canonical relational tables should cover at least:

- `sessions`
- `messages`
- `workflows`
- `workflow_steps` or equivalent normalized workflow storage
- `agents`
- `outbox_events`
- `session_stream` or equivalent persisted projections
- migration metadata

If vector retrieval is enabled, derived structures should include:

- `message_embeddings`
- `vector_index_state`
- `document_chunks`
- `chunk_embeddings`
- `vec_message_embeddings` virtual table
- `vec_chunk_embeddings` virtual table

The vector tables should reference canonical IDs. They must not become the only source of truth for content or metadata.

## Rollout Plan

### Step 1: Replace the JSON bridge with SQLite persistence

- persist canonical runtime state in SQLite
- keep current CLI semantics unchanged
- preserve transactional outbox behavior
- preserve session inspect behavior across separate invocations

### Step 2: Add migration and recovery support

- version the schema
- support clean startup migration checks
- support replay and rebuild of projections from durable state when needed

### Step 3: Add vector-ready derived structures

- introduce derived embedding ownership tables that reference canonical message IDs
- persist vector rebuild state so operators can distinguish disabled, rebuilding, ready, and degraded modes
- define backfill and rebuild semantics from canonical messages

### Step 4: Add `sqlite-vec` behind an isolated adapter

- load or statically link the extension through the chosen Go binding
- create derived vector indexes
- support insert, update, delete, and rebuild flows

### Step 5: Add retrieval use cases

- nearest-neighbor lookup for message memory
- future document or tool-context retrieval
- deterministic fallback when embeddings or vector search are unavailable

## Business And Operational Guardrails

- no operator-critical feature may fail solely because vector support is disabled
- canonical session durability must not depend on embeddings existing
- vector queries must degrade gracefully when the feature is disabled or unavailable
- migration failures must fail fast before runtime mutation begins
- vector indexes must be rebuildable from canonical rows
- derived ownership rows must stay useful even when the live vector extension is unavailable
- replay and audit workflows must remain possible without vector search

## Testing Expectations

- integration tests for canonical SQLite repositories
- migration tests for forward schema changes
- crash/restart tests for durable outbox and replay behavior
- adapter tests for vector insert/query/delete/rebuild behavior when vector support is enabled
- fallback tests proving the system remains operational when vector support is disabled

## Recommendation Summary

Yes, SQLite with vector capability fits this codebase.

The professional implementation path is:

- use SQLite as the required persistence foundation
- treat `sqlite-vec` as an optional sidecar index
- keep vector behavior isolated behind ports and adapter boundaries
- refuse to let pre-v1 vector tooling become a correctness dependency for the core runtime
