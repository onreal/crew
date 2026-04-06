# Go Multi-Agent CLI Implementation Plan

## Objective

Implement a production-grade Go CLI for multi-agent sessions that supports both event-driven free conversation and deterministic sequential workflows, with strong observability, replayability, and clean architectural boundaries.

## Delivery Principles

- Keep domain logic independent from infrastructure.
- Build a working vertical slice early.
- Treat message contracts and session state as first-class assets.
- Prefer testable deterministic components before adding external LLM integration.
- Preserve business context and operational intent as part of the implementation, not as an afterthought.

## Target Architecture

```text
cmd/crew
internal/domain
internal/application
internal/adapters
internal/platform
pkg
docs/architecture
docs/business
tests
```

## Phases

### Phase 0: Foundation And Governance

- establish repository structure
- create `AGENTS.MD` files for root and each active directory
- define product context, feature documentation standard, and architectural constraints
- confirm naming, package boundaries, and dependency direction

Exit criteria:

- repository structure exists
- governance docs exist and are internally consistent
- implementation order is documented

### Phase 1: Bootstrap The Go Application

- initialize Go module
- add Cobra-based CLI entrypoint
- add configuration loading via Viper
- add structured logging and runtime bootstrap
- define process lifecycle and graceful shutdown behavior

Exit criteria:

- `crew` binary starts
- configuration is loaded from file and environment
- top-level commands compile and run

### Phase 2: Define Core Domain Contracts

- model `Agent`, `Message`, `Session`, `Workflow`, and related value objects
- define invariants for IDs, statuses, channels, message kinds, and workflow step semantics
- define domain policies for routing, reply, loop prevention, and status transitions

Exit criteria:

- domain packages compile without infrastructure dependencies
- domain invariants are covered by unit tests

### Phase 3: Build Application Layer And Ports

- define ports for event bus, repositories, orchestrator, clock, ID generator, and LLM providers
- implement session lifecycle use cases
- implement message dispatch and workflow progression use cases
- define command and query boundaries

Exit criteria:

- application use cases can run against fakes
- business flows are testable without adapters

### Phase 4: Implement Runtime Core

- implement in-memory event bus
- implement agent runner lifecycle
- implement scheduler and stream projection
- define control commands for pause, resume, stop, and inspect

Exit criteria:

- local runtime can create a session and move messages through the system
- runtime shutdown is clean and testable

### Phase 5: Add Persistence

Phase 5 is split into two tracks. Track A is mandatory and delivers durable product persistence. Track B is optional in the first cut and adds vector retrieval foundations without making the core runtime depend on a pre-v1 vector extension.

#### Phase 5A: Core SQLite Persistence

- implement SQLite repositories for sessions, messages, workflows, agents, durable outbox state, and stream projections
- define schema migration strategy
- support replay and recovery primitives
- preserve the current local-first operational model while replacing the JSON runtime state bridge with SQLite-backed adapters

Exit criteria:

- sessions, messages, workflows, and stream projections persist across restarts
- durable outbox state survives process restart
- repositories are covered by integration tests
- CLI session lifecycle commands operate on SQLite-backed persistence instead of the JSON bridge

#### Phase 5B: Optional Vector Retrieval Foundation

- introduce a vector persistence port that is derived from canonical relational state instead of replacing it
- evaluate and isolate `sqlite-vec` behind a dedicated adapter boundary
- keep vector indexing optional and disabled-by-default until the adapter is production-validated
- support derived embeddings for future session memory, semantic recall, and document retrieval use cases
- define fallback behavior when vector support is unavailable or disabled

Exit criteria:

- canonical runtime state does not depend on vector extension availability
- vector index data can be rebuilt from canonical relational records
- vector-enabled integration tests exist behind explicit adapter coverage
- operator-visible behavior remains correct when vector support is disabled

### Phase 6: Implement Free Conversation Mode

- support peer-to-peer and orchestrated routing
- implement message subscriptions and reaction policies
- add loop detection and noise controls

Exit criteria:

- agents can react to live conversation events
- guardrails prevent runaway loops

### Phase 7: Implement Sequential Workflow Mode

- define workflow step contracts
- implement deterministic fan-in and fan-out semantics
- implement stop conditions, retries, and reproducibility rules

Exit criteria:

- the same workflow and inputs produce reproducible step progression
- workflow state is inspectable and replayable

### Phase 8: Implement CLI And TUI Experience

- add session start, attach, inspect, and control commands
- implement Bubble Tea TUI with session, agents, stream, and controls panes
- surface messages, events, and errors live

Exit criteria:

- operator can observe and control sessions from terminal
- TUI remains responsive under realistic message volume

### Phase 9: External Agent Intelligence

- add plain-text LLM provider adapters behind ports
- add sandboxed CLI runtime adapters behind separate ports
- support provider configuration, model selection, sandbox policy, and tool invocation contracts
- preserve deterministic testing with mocks and recorded transcripts

Current status:

- plain-text generation now routes per agent behind the existing `LLMProvider` port, with named providers `openai`, `gemini`, and `grok` plus deterministic `local_stub`
- deterministic orchestrator selection remains local in the current slice; provider-backed orchestration and tool invocation remain future work
- Phase 9A, 9B, and 9C now exist in an initial vertical slice: persisted sandbox tasks/handoffs, copied-workspace sandbox execution, and a Codex-backed CLI runtime adapter are wired through the live runtime and CLI
- Phase 9D has started: free-mode text agents can now request persisted sandbox tasks and emit sandbox result messages back into the conversation, while deeper policy and richer provider participation remain future work
- see [sandboxed-agent-runtime-plan.md](/Users/another_reality/Projects/upai/docs/architecture/sandboxed-agent-runtime-plan.md) for the ongoing mixed-provider roadmap
- see [provider-lifecycle.md](/Users/another_reality/Projects/upai/docs/architecture/provider-lifecycle.md) for the add-a-provider workflow

Exit criteria:

- at least one plain-text provider works behind an application port
- at least one sandboxed CLI runtime works behind a dedicated application port
- cross-provider collaboration is persisted and auditable
- external integration does not leak into domain packages

### Phase 10: Reliability, Observability, And Hardening

- add structured tracing and metrics
- add golden tests, simulations, and replay-driven debugging
- harden failure recovery, backpressure, and cancellation behavior
- document operational runbooks

Exit criteria:

- core paths have unit, integration, and simulation coverage
- runtime risks are documented with mitigations

## Cross-Cutting Requirements

- Every package must have clear ownership and dependency direction.
- Every directory must maintain its own `AGENTS.MD`.
- Every feature must document its business purpose and failure impact.
- Every runtime component must define lifecycle and cancellation behavior.
- Every storage-facing change must define migration and backward-compatibility expectations.
- Canonical relational state must remain authoritative even when optional derived indexes such as vector search are introduced.

## Priority Order For First Vertical Slice

1. bootstrap CLI
2. core domain entities
3. application ports and use cases
4. in-memory event bus
5. local free-mode session
6. SQLite persistence
7. optional vector retrieval foundation
8. basic TUI attach

## Deferred Until Core Is Stable

- distributed event bus
- remote worker execution
- advanced policy DSL
- multi-tenant access control
- production-grade provider marketplace
