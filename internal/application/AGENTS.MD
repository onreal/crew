# AGENTS.MD

## Purpose

`internal/application` contains use cases, orchestration logic, and ports that connect business rules to adapters.

## Business Role

This directory defines how the product behaves when operators start sessions, send messages, run workflows, and control runtime execution.

## Allowed Here

- use cases
- command and query handlers
- application services
- ports for repositories, buses, orchestrators, LLMs, clocks, and IDs
- ports for sandboxed agent runtimes and persisted cross-agent task coordination
- transaction and policy coordination

## Current Responsibilities

- session lifecycle use cases for create, start, pause, resume, stop, complete, and fail
- message dispatch and message listing use cases
- free-mode single-turn stepping through orchestrator and generation ports
- free-mode bounded multi-turn auto execution through orchestrator and generation ports
- persisted sandbox task creation, handoff recording, and sandbox execution coordination
- sandbox task inspection and session-scoped sandbox task/handoff listing
- workflow registration, retrieval, and progression evaluation
- application-owned ports for repositories, event publication, orchestration, LLM generation, time, and ID creation
- an application-owned vector index port for optional derived semantic retrieval
- vector status, rebuild, and recall use cases with deterministic fallback when vector search is disabled or stale
- application event topics and event payload contracts
- transactional outbox recording for mutation-producing use cases so durable state and durable events succeed or fail together
- readiness validation that prevents workflow callers from jumping directly into unready downstream steps
- message dispatch marks session-scoped vector state stale when derived embeddings may no longer match canonical messages
- session-scoped vector status and rebuild commands must validate that the target session actually exists before reporting or mutating derived state
- free-mode stepping must remain bounded and deterministic for the current phase: one selected agent, one generated reply, no background loop
- free-mode stepping must reject sequential sessions instead of appending ad-hoc agent turns into workflow-oriented transcripts
- free-mode stepping must stay confined to one conversation transcript; when a conversation ID is supplied it is authoritative, and when omitted the most recent conversation in the session is the only context the step may read or extend
- free-mode user dispatch may target one or more specific agents through direct recipient routing; targeted sends must remain auditable and use the same canonical message path as broadcast sends
- when one direct user prompt targets multiple agents, free-mode stepping must keep the still-unanswered recipients targeted across subsequent steps until each has replied once
- free-mode reply routing is a separate policy axis from orchestration: orchestration chooses which eligible agent speaks next, while reply routing decides who that generated message is addressed to and which prior message it threads to
- free-mode reply routing must support both `latest_speaker` and `reply_obligations`; the latter derives and preserves outstanding reply obligations from conversation history in priority order instead of collapsing everything to the latest turn
- when a delegated agent completes a direct reply with no further handoff, reply-obligation fallback must be able to route the next coordinator response back to the originating user thread instead of reflexively bouncing to the latest agent speaker
- when the latest conversational message is from an agent, free-mode stepping must not admit fresh unsolicited agent follow-ons unless that message explicitly handed work off or the next eligible agent is closing a completed reply chain back to the originating user
- reply-obligation routing must preserve one-obligation-per-message semantics, keep older user obligations ahead of later agent handoffs, and allow the obligated agent to satisfy the next queued obligation even when that requires consecutive turns
- free-mode user dispatch may optionally set canonical `ReplyTo` links; reply-threading must stay on the same persisted message contract as every other message
- free-mode auto execution must run only a caller-bounded number of turns, must return explicit stop reasons, and must preserve the same dispatch semantics and conversation scoping as single-step execution
- free-mode stepping and auto execution must expose enough diagnostics to explain agent selection decisions, including eligible agents, blocked agents, ordered candidates, and the applied orchestration mode
- orchestration modes and reply-routing modes must remain explicit and validated; selection strategy changes must not silently change reply-targeting semantics
- external LLM generation must stay behind the application `LLMProvider` port so provider protocols never leak into use-case logic
- sandboxed CLI runtimes must stay behind a separate application port from plain-text LLM generation
- mixed-provider collaboration must be represented as persisted tasks and handoffs so agent-to-agent work stays auditable and replayable
- when a text-generation port requests sandbox delegation, free-mode stepping must translate that into a persisted sandbox task, a persisted handoff, and persisted conversation messages describing task creation and completion/failure
- sandbox delegation must be explicitly allowed by agent policy and restricted to the runtimes listed in that agent's allowed sandbox-runtime set
- free-mode sandbox delegation must resolve the runtime from canonical agent state rather than one process-global default, and any per-agent sandbox-root override must be persisted onto the resulting task
- agent handoffs must reject task references that do not belong to the same session and conversation as the handoff record
- sandbox task execution must verify that the configured runtime matches the task's assigned runtime/provider before any side effects occur
- if sandbox side effects complete but terminal event persistence fails, the canonical task record must still be recovered to a terminal state so retries do not silently re-run completed work
- sandbox task queries must remain session-aware so operator and runtime inspection can rely on canonical coordination records instead of adapter-local state
- malformed sandbox delegation requests from generation providers must be rejected before the agent reply is persisted so a failed step never advances the conversation only halfway

## Not Allowed Here

- direct framework bootstrapping
- raw SQL or provider SDK code
- terminal rendering

## Update Triggers

Update this file when:

- use case flows change
- application ports change
- orchestration responsibilities move
- runtime control semantics change
- command/query contracts or emitted event topics change
- vector or persistence-facing application ports change
