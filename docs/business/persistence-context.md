# Persistence Context

## Why Persistence Exists

Persistence is not just a technical storage concern. It is the product mechanism that makes `crew` operationally trustworthy.

Operators need to:

- resume work after process restarts
- inspect prior session state and history
- debug failures
- audit what happened and when
- evolve from local experiments to more reliable runtime operation

Without durable persistence, the system cannot meet its debugging, replay, or accountability goals.

## Why Vector Retrieval May Exist

Vector support is a future product capability, not the foundation of durability.

Its business purpose is to improve recall and retrieval for:

- long-running session memory
- semantic lookup across prior conversation
- future document retrieval for agents
- future knowledge-base and tool-context retrieval

That is useful, but it is secondary to the basic requirement that sessions, messages, workflows, and stream history remain durable and inspectable.

## Operator Outcomes

Phase 5 must deliver:

- SQLite-backed durability for core runtime state
- restart-safe session lifecycle continuity
- persisted inspectable session state
- persisted stream or replay information sufficient for debugging and audit

If vector support is enabled later, it should also deliver:

- semantic recall without introducing a separate remote vector database
- local-first retrieval that fits the same operator workflow as the CLI and future TUI

## Current Implementation Status

The first Phase 5A slice now exists:

- a live SQLite-backed runtime path exists for CLI session control
- embedded migrations exist for canonical tables
- repository implementations exist for sessions, messages, workflows, agents, durable outbox rows, and persisted session stream rows
- canonical SQLite repositories now also persist sandbox tasks and agent handoffs for mixed-provider coordination
- vector support has a separate `sqlitevec` adapter boundary behind the application port
- `sqlitevec` now persists derived `message_embeddings` ownership rows and `vector_index_state` rebuild status
- `sqlitevec` can rebuild derived embeddings from canonical messages even when live vector search remains disabled by default
- the CLI now exposes vector status/rebuild controls and session recall behavior on top of those derived records

The former JSON runtime bridge has been replaced for live session commands. Canonical local persistence now lives in SQLite at `storage.path`.

## Business Guardrails

- core runtime correctness must not depend on vector support
- vector indexes must be derived from canonical relational records
- vector-disabled execution must remain fully usable for session lifecycle and inspection
- operators must be able to understand when vector retrieval is enabled, disabled, degraded, or rebuilding
- derived embedding ownership rows must remain rebuildable from canonical messages and must not become a hidden source of truth
- rebuild freshness must consider embedding configuration identity, not just whether canonical message text changed
- a global rebuild must refresh the session-scoped freshness rows that session recall and session-scoped vector status depend on
- any retrieval feature must define fallback behavior when embeddings are missing or stale
- persisted message history must remain append-only so prior interaction records are not silently rewritten
- persisted ordering for auditable event or message history must remain deterministic even when timestamps include sub-second precision
- pending durable outbox rows must be drainable on read-only restart so persisted stream inspection does not go stale after a crash between commit and publish
- concurrent local processes must not be able to publish the same durable outbox row twice
- persisted sandbox tasks and agent handoffs must remain restart-safe so provider side effects, terminal task state, and delegation history stay auditable after process exit

## Failure Impact

If canonical persistence fails:

- sessions may be lost
- runtime inspection becomes unreliable
- replay and audit workflows are weakened
- operator trust in the product is damaged

If vector retrieval fails:

- semantic recall and retrieval quality degrade
- the system should fall back without corrupting canonical state
- session control and inspection must continue working

## Reliability And Determinism Tradeoffs

Canonical runtime persistence should remain deterministic and schema-driven.

Vector retrieval introduces softer behavior:

- embeddings may be model-dependent
- retrieval ranking may change if embeddings are regenerated
- rebuilds may take time

Because of that, vector features must stay clearly separated from the deterministic execution contracts for session lifecycle, workflow control, and auditability.

## Observability Expectations

Persistence work must expose enough signals to answer:

- was canonical state committed
- was an outbox event durably recorded
- was a projection rebuilt or replayed
- is vector indexing enabled
- is vector indexing current, degraded, or rebuilding
- are derived embeddings populated from current canonical messages
- is the reported freshness global or only for a rebuilt session scope

## Product Rule

The product may use vector search to improve recall, but it must never require vector search just to remain correct, durable, or inspectable.
