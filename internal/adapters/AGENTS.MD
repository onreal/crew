# AGENTS.MD

## Purpose

`internal/adapters` contains concrete implementations of the ports defined by the application layer.

## Business Role

Adapters make the product operable in the real world by connecting the core system to terminal interfaces, persistence, messaging, and external intelligence providers.

## Expected Subdomains

- `cli`
- `tui`
- `storage/sqlite`
- `storage/...`
- `bus/memory`
- `llm/...`
- `providers/...`
- `config`
- `memory`
- `runtime`

Each new adapter directory must include its own `AGENTS.MD` on creation.

## Rules

- adapters translate external concerns into application contracts
- adapters must not own business policy
- provider-specific behavior must stay isolated from the domain
- plain-text LLM providers and sandboxed CLI runtimes are separate adapter classes and must not be merged into one convenience abstraction
- plain-text provider routing must happen in adapters/runtime code against canonical agent `provider` values rather than through global application switches
- runtime adapters must preserve transactional outbox semantics when moving durable events into live delivery paths
- canonical persistence adapters must remain the source of truth for runtime durability
- optional derived adapters such as vector indexing must be rebuildable from canonical persisted state and must not become correctness dependencies for session control
- shared sandbox lifecycle, workspace-copy, and subprocess concerns belong in dedicated sandbox adapters rather than provider-specific packages or application code

## Current Responsibilities

- in-memory adapters for local execution and tests
- runtime composition for the local engine
- SQLite canonical storage adapters and migrations
- a separate optional `storage/sqlitevec` adapter family for vector retrieval work
- provider adapters for external model-backed generation
- provider-routing and registry logic for per-agent text generation
- shared sandbox execution foundations for isolated workspace preparation and bounded subprocess execution
- sandboxed CLI runtime adapters for tool-using agent execution

## Update Triggers

Update this file when:

- a new adapter family is introduced
- external dependency strategy changes
- transport, storage, or provider contracts change
- persistence or vector indexing responsibilities change
