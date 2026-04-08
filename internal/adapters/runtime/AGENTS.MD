# AGENTS.MD

## Purpose

`internal/adapters/runtime` contains the local runtime that composes application services with concrete adapter-backed persistence and event delivery.

## Business Role

This directory is the executable local engine for starting, controlling, observing, and shutting down multi-agent sessions across both in-memory test mode and SQLite-backed local persistence.

## Rules

- runtime lifecycle ownership must be explicit
- session-control methods must flush durable outbox events into the live event bus
- projection logic must remain read-model oriented and must not mutate business state
- shutdown must stop background goroutines cleanly
- runtime operations that depend on live event delivery must reject calls before `Start` and after `Shutdown`
- shutdown must wait for in-flight runtime operations to finish before closing the live bus
- `LoadState` is a pre-start operation only; `Snapshot` must reject calls after shutdown and serialize with active mutations so it never exposes a partially-applied store/projector view
- when a persisted stream store exists, `InspectSession` must read the durable stream view rather than relying only on in-process projection memory
- `InspectSession` must first drain any pending durable outbox rows so read-only restarts cannot observe stale persisted session streams
- SQLite-backed outbox flushing must publish, persist stream entries, and mark durable events as published without discarding pending events on failure
- runtime message dispatch must mark session-scoped vector state stale when derived embeddings need refresh
- vector recall must fall back deterministically when the vector backend is disabled, stale, rebuilding, or degraded
- free-mode stepping must use the same persisted dispatch path as operator-sent messages so projection, outbox, and vector-staleness behavior stay consistent
- free-mode auto runs must stay bounded, use the same persisted dispatch path as single-step execution, and report explicit stop reasons for operator observability
- runtime provider assembly must default safely to local stub generation and must route external text generation by canonical agent `provider` without changing persistence semantics
- runtime provider assembly must support both HTTP-backed and CLI-backed text providers without collapsing them into sandbox runtime selection
- runtime configuration must explicitly distinguish text-generation providers from sandboxed runtime providers
- runtime must treat text-provider connection settings as transport config and agent `provider` values as canonical behavior config
- runtime must treat sandbox runtime config as a named catalog under `sandbox.providers.<name>` and must route persisted tasks by `RuntimeName` rather than assuming one global CLI runtime
- runtime must persist and honor agent-owned `delegation_runtime` plus optional `sandbox_workspace_root`, so free-mode delegated work does not depend on one room-wide runtime or one room-wide sandbox root
- runtime sandbox task methods must use the same persisted coordination path as application sandbox-task services and must not bypass task identity or terminal-state recovery guarantees
- sandbox runtime execution must preserve copied-workspace isolation; provider adapters must not mutate canonical source workspaces directly in this phase
- free-mode mixed-provider delegation must persist sandbox task creation, handoff, and sandbox result messages into the same conversation stream so replay remains coherent
- default agent definitions must come from the filesystem-backed `crew_agents/` catalog selected for the current workspace or explicit override, not hardcoded in-process seed lists
- runtime must persist a session-scoped actor catalog selector from `session start --actors <selector>` and use that selector for later free-mode reseeding on that session, including direct-message dispatch validation as well as step/auto execution
- optional agent-catalog presentation metadata such as per-agent colors may be loaded for UI consumers, but that metadata must stay outside canonical persisted business state
- runtime startup must treat filesystem agent definitions as seed-or-update inputs for canonical persisted agent records
- free-mode turn execution must resync filesystem-backed agent definitions before each step or auto run so attached sessions can pick up edited agent YAML without restarting the process
- runtime free-mode orchestration must support explicit selectable strategies without changing the underlying eligibility rules that gate whether an agent may speak
- runtime free-mode reply routing must support explicit selectable strategies independently from orchestration, so operator choice of `latest_speaker` versus `reply_obligations` changes addressing behavior without replacing the agent-selection strategy
- runtime base candidate ordering must remain deterministic and explainable when filesystem-backed agent policies specify priority and weight
- round-robin orchestration must preserve the full agent ring even when the last speaker is temporarily ineligible, so bounded auto runs do not skip the next agent and snap back to the start of the roster
- when an operator message directly targets multiple agents, runtime step/auto execution must preserve the remaining targeted recipients across subsequent steps until each has replied once, before ordinary orchestration resumes
- reply-obligation routing must preserve older user obligations ahead of later agent handoffs and may keep the same agent eligible across consecutive turns while queued obligations for that same agent remain unsatisfied

## Current Responsibilities

- local runtime bootstrap and lifecycle management
- session control methods for create, start, pause, resume, stop, and inspect
- free-mode single-turn stepping with deterministic orchestration over agents loaded from the filesystem catalog and either local-stub or configured external generation
- free-mode bounded multi-turn auto execution with configurable orchestration over agents loaded from the filesystem catalog and either local-stub or configured external generation
- free-mode mixed-provider delegation from text agents into persisted sandbox tasks and back into persisted system/result messages
- free-mode agent-owned delegation routing into persisted sandbox tasks using the selected agent's runtime and optional sandbox-root override
- sandbox task creation, listing, inspection, and execution through persisted coordination records
- vector status, rebuild, and recall control methods layered over the SQLite-backed local runtime
- workflow registration and progression execution through application services
- stream projection from published runtime events
- SQLite-backed runtime construction for persisted local execution
- deterministic local stub embedding for Phase 5B operator workflows until a real embedding provider is introduced
- deterministic local stub orchestrator and per-agent text-provider routing across local stub plus configured external providers, including direct Codex CLI turns
- session-scoped actor-catalog reseeding for free-mode runtime operations and attached-room agent metadata
- configured named sandbox-runtime assembly and routing for Codex-backed task execution, with room for additional CLI runtimes behind the same port
- filesystem-backed agent catalog loading, local-first `crew_agents/` discovery, installed fallback resolution, and startup seeding/update into canonical agent persistence
- provider topology reporting for plain-text versus sandboxed runtime classes
- memory-backed runtime state snapshot export/import for tests and transitional tooling
