# AGENTS.MD

## Mission

Build `crew` as a professional Go-based multi-agent CLI platform that supports:

- free conversation mode with reactive, event-driven agent interactions
- sequential conversation mode with deterministic workflow execution
- local-first CLI and TUI operation
- durable session and message persistence
- future LLM provider integrations without contaminating domain logic
- future sandboxed CLI agent runtimes that can collaborate safely with plain-text LLMs

The architecture must remain hexagonal: business rules stay inside the domain and application layers; infrastructure concerns stay in adapters and platform code.

## Product Intent

This system is not just a chat shell. It is a controllable multi-agent runtime for:

- coordinating specialized agents
- observing agent behavior live through a CLI/TUI
- replaying and debugging agent conversations
- executing deterministic workflows when repeatability matters
- supporting future enterprise requirements such as auditability, policy enforcement, and reliability controls

Every feature must document the business reason it exists, not only the technical shape.

## Non-Negotiable Architecture Rules

1. The implementation language is Go.
2. The primary architectural style is Hexagonal Architecture (Ports and Adapters).
3. `internal/domain` contains pure domain models, value objects, invariants, and domain services only.
4. `internal/application` contains use cases, orchestration, ports, policies, and workflow coordination.
5. `internal/adapters` contains concrete implementations for CLI, TUI, persistence, event bus, and external providers.
6. `internal/platform` contains shared technical capabilities such as configuration, logging, IDs, clocks, and runtime helpers.
7. `cmd` wires the application together. It must not contain business logic.
8. `pkg` is reserved for intentionally public, stable packages only. Do not move code there for convenience.
9. Free mode and sequential mode must share the same message, session, and policy contracts where possible.
10. Deterministic workflow execution must remain testable independently from live event-driven execution.
11. Plain-text LLM providers and sandboxed CLI runtimes are separate provider classes and must not be collapsed into one interface for convenience.
12. Cross-agent collaboration must flow through persisted runtime contracts, not hidden subprocess piping or direct adapter coupling.
13. Sandboxed CLI execution in the current phase must run against copied workspaces, not directly against canonical source workspaces.
14. Plain-text provider selection belongs to canonical agent state, not process-global config, so one session may mix agents from different text providers safely.
15. Actor-catalog selection belongs to canonical session state once a session is started, not to repeated CLI flags on every later invocation.
16. Free-mode agent selection and free-mode reply routing are separate runtime concerns: orchestration decides which agent speaks next, while reply routing decides who that message is addressed to and which prior message it satisfies.
17. Free-mode reply routing must support both latest-speaker and obligation-queue semantics without coupling either behavior to a specific orchestration mode.
18. Sandboxed delegation runtime selection belongs to canonical agent state and persisted task state, not to one process-global free-mode default.
19. A provider family may participate in both text-generation and sandbox-runtime roles, but those roles must still remain separate ports and separate persisted runtime contracts.

## Required Go Standards

- Prefer small interfaces owned by consumers.
- Keep packages cohesive and narrowly scoped.
- Use explicit constructors and dependency injection.
- Pass `context.Context` through boundaries that perform I/O or long-running work.
- Return typed errors or well-structured sentinel errors where business behavior depends on failure class.
- Avoid framework leakage into domain code.
- Favor table-driven tests for domain and application logic.
- Keep concurrency explicit and controlled. Every goroutine must have lifecycle ownership and stop semantics.

## Initial Target Structure

```text
cmd/
  crew/
internal/
  domain/
  application/
  adapters/
  platform/
pkg/
docs/
  architecture/
  business/
tests/
```

## Implementation Order

Follow [docs/architecture/implementation-plan.md](/Users/another_reality/Projects/upai/docs/architecture/implementation-plan.md).

The expected order is:

1. finalize architecture and documentation rules
2. bootstrap Go module, configuration, and command surface
3. implement core domain contracts
4. implement application ports and use cases
5. implement in-memory event bus and runtime scheduler
6. implement SQLite persistence
7. implement optional vector retrieval foundation behind isolated adapters
8. implement free conversation engine
9. implement sequential workflow engine
10. implement TUI projection and controls
11. implement external LLM adapters
12. implement sandboxed CLI agent runtimes and mixed-provider collaboration
13. harden with tests, replay, loop prevention, and observability

## Strict Documentation Rule For All Agents

The `AGENTS.MD` maintenance rule is mandatory.

1. Every directory in this repository must contain its own `AGENTS.MD`.
2. A new directory is invalid until its `AGENTS.MD` is created in the same change.
3. Any change to behavior, contracts, dependencies, ownership, data flow, or business intent must update the nearest relevant `AGENTS.MD` in the same change.
4. If a change affects cross-directory behavior, update both the local `AGENTS.MD` and any affected parent `AGENTS.MD`.
5. If a feature changes user value, operational semantics, failure modes, or policy constraints, update the business context in the relevant `AGENTS.MD` and supporting docs under `docs/business`.
6. Agents must never defer `AGENTS.MD` updates to a later task.
7. A pull request or task is incomplete if code or docs changed but the relevant `AGENTS.MD` files were not reviewed and updated when needed.

## Required Contents For Every AGENTS.MD

Each `AGENTS.MD` must state, at minimum:

- purpose of the directory
- business role of the code or docs in that directory
- ownership boundaries and what must not be placed there
- important inbound and outbound interfaces
- invariants and constraints
- update triggers that require the file to be revised
- test expectations
- known risks, if material

## Business Context Rule

For every feature, agents must preserve and maintain the full business context, including:

- the user or operator problem being solved
- why the feature exists
- what inputs it accepts and what outputs it must produce
- operational expectations and guardrails
- failure impact and fallback behavior
- auditability, safety, and policy implications
- any cost, latency, determinism, or reliability tradeoffs

If the code changes but the business explanation becomes stale, the work is incomplete.

## Feature Delivery Rule

Before implementing a new feature, agents must ensure the following are clear in the local `AGENTS.MD` or linked business docs:

- feature objective
- domain concepts involved
- lifecycle and execution path
- persistence impact
- observability requirements
- test strategy
- rollout or compatibility considerations

## Decision Policy

- Prefer boring, maintainable Go over clever abstractions.
- Prefer deterministic contracts at the domain edge.
- Prefer append-only event/message thinking where traceability matters.
- Keep canonical persistence authoritative; treat vector indexes and other acceleration structures as derived and rebuildable.
- Treat sandboxed agent execution as a first-class runtime concern with explicit policy, isolation, and auditability.
- Introduce public APIs only with clear reuse justification.
- Write docs as durable operating instructions for future agents, not as temporary notes.

## Root Documentation

- `README.md` is the operator-facing entrypoint for building, running, configuring, and exercising the current CLI/runtime surface
- `install.sh` is the operator-facing local installer for building `crew`, installing the PATH-facing wrapper, seeding or refreshing the operator-local fallback `crew_agents` catalog, creating the default home config and runtime directories, reconciling shipped config defaults that must not stay stale forever such as `providers.codex.timeout_millis` and `sandbox.providers.codex.timeout_millis`, and updating a supported shell profile for PATH bootstrap without silently overwriting unrelated operator config edits
- `crew_agents/` is the default workspace-local agent catalog for repository and operator-owned workspaces; when discovery, initialization, or actor-subcatalog semantics change, update `README.md`, `cmd/crew/AGENTS.MD`, `internal/adapters/runtime/AGENTS.MD`, and relevant business docs in the same change
- when command ergonomics change, `README.md` examples must be updated to show the preferred operator-facing syntax directly rather than only a fallback or implementation-detail form
- when command behavior, setup flow, persisted runtime behavior, or limitations change, update `README.md` in the same change
