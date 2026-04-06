# Application Layer Context

## Why The Application Layer Exists

The application layer turns domain contracts into executable product behavior. It coordinates repositories, event publication, policies, and time/ID dependencies without allowing infrastructure details to leak into the business core.

## Operator-Relevant Behaviors

- creating and transitioning sessions through a controlled lifecycle
- dispatching messages only when the session state allows it
- dispatching both broadcast and direct user messages through the same durable path
- dispatching reply-threaded user messages through the same durable path
- preserving auditable message persistence and publication contracts
- registering workflows and evaluating deterministic step progression
- exposing vector status, rebuild, and session recall behavior without making vector support a correctness dependency
- executing one bounded free-mode agent turn through orchestrator and generation ports
- supporting pluggable LLM-backed generation behind a stable application port
- defining persisted sandbox task and handoff coordination so plain-text and sandboxed agents can collaborate through auditable contracts
- translating text-agent sandbox requests into persisted tasks, persisted handoffs, and persisted system/result messages inside the active conversation
- exposing sandbox task inspection and session-scoped sandbox coordination queries without leaking adapter details into operator flows

## Business Guardrails

- session state changes must remain explicit and evented
- mutation-producing use cases must record durable events atomically with durable state changes
- message dispatch must fail when the session is not actively running
- actor existence checks must happen before agent-targeted message dispatch succeeds
- direct user messages must be able to target explicit agent recipients without inventing a parallel transport path outside canonical message persistence
- reply-threaded user messages must preserve canonical `ReplyTo` links rather than introducing CLI-only thread metadata
- workflow progression must distinguish between ready and blocked steps, especially for merge points such as `fan_in`
- callers must not be able to jump directly into workflow steps whose predecessors have not completed
- a `fan_in` merge is ready only when all of its incoming branches are complete
- message dispatch must mark session-scoped vector state stale when canonical messages change
- session recall must fall back deterministically when vector retrieval is disabled, stale, rebuilding, or degraded
- session-scoped vector status and rebuild operations must reject nonexistent session IDs instead of creating orphan derived-state rows
- free-mode stepping must stay single-turn and deterministic in the current phase; autonomous loops belong to a later hardening slice
- free-mode stepping must reject sequential sessions so deterministic workflow transcripts are not polluted by ad-hoc agent turns
- free-mode stepping must operate on exactly one conversation transcript at a time; unrelated session conversations must not influence agent selection, prompt context, or reply placement
- free-mode orchestration strategy must be explicit, validated, and observable; operators must be able to see which agents were eligible, which were blocked, and in what order candidates were considered
- round-robin free-mode orchestration must advance across the full agent roster even when the previous speaker is temporarily blocked by consecutive-turn policy, otherwise bounded shared-room runs will silently skip agents
- free-mode auto execution must remain explicitly bounded by the caller, apply conversation max-turn and consecutive-turn policy checks across the run, and return explicit stop reasons for operators
- provider-specific request formats, auth, and transport failures must stay outside the application layer and surface only as port-level generation failures
- sandbox delegation and sandbox execution state must be persisted as first-class coordination records rather than hidden adapter chatter
- handoff records must remain session-local and conversation-local so replay and audit views cannot be polluted by cross-session task references
- sandbox execution must not proceed under a different runtime/provider than the task was assigned to
- after sandbox side effects, the canonical task record must prefer terminal-state recovery over leaving the task stuck in `running`
- malformed sandbox delegation requests must be rejected before the agent reply is persisted so a failed step cannot leave a partially recorded turn in the transcript
- sandbox task inspection and listing must read canonical coordination records, not adapter-local process state
- when a text agent delegates sandbox work, the resulting coordination and completion/failure summaries must appear in the same conversation transcript so later agents can consume them through normal message history
- sandbox delegation must be policy-gated per agent and per runtime so a model cannot silently escalate from "can use tools" to "can run any sandbox provider"
