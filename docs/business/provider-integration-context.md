# Provider Integration Context

## Why Provider-Backed Intelligence Exists

The local stub generator is useful for deterministic testing and operator demos, but it does not produce realistic agent output or real tool-driven work. Provider-backed intelligence exists so operators can exercise the same persisted runtime with real model responses and, later, real sandboxed agent execution.

## Operator Value

- switch free-mode generation from local stub output to real model output without changing session persistence or CLI flow
- keep the same `session send`, `session step`, `session auto`, and `session inspect` workflow
- control provider connection settings such as base URL, credentials, and timeout from configuration
- select the text provider per agent in the filesystem catalog so one chat may mix `openai`, `gemini`, `grok`, `codex`, and `local_stub` agents
- keep provider-specific model selection in the agent catalog where the agent identity lives
- allow sandboxed CLI agents to participate in the same session and delegate work safely through a named runtime catalog

## Current Implementation Status

- plain-text provider-backed generation is live through the `LLMProvider` port and now routes per agent
- Phase 9A sandbox collaboration contracts are implemented in the application layer as persisted sandbox tasks and agent handoffs
- Phase 9B and Phase 9C now exist in an initial operational form: copied-workspace sandbox execution, persisted sandbox-task records, and a Codex-backed runtime adapter are wired through the live CLI/runtime path
- `task create`, `task run`, `task get`, and `task list` now expose the first sandbox operator surface
- Phase 9D has started in the free-mode loop: a text agent can now request sandbox delegation through the generation port, persist the resulting task and handoff, and emit sandbox status/result messages back into the same conversation
- the current named text providers are `openai`, `gemini`, `grok`, and `codex`, with `local_stub` preserved for deterministic local execution
- sandboxed CLI runtimes now also use a named provider catalog under `sandbox.providers.<name>`
- free-mode sandbox delegation now resolves runtime ownership from each agent's canonical `delegation_runtime`, with optional per-agent `sandbox_workspace_root`
- `sandbox.default_provider` is now only the default runtime used by manual `task create` when `--runtime` is omitted
- `codex` is the first registered sandbox runtime in that catalog

## Guardrails

- provider-backed generation is optional and agent-scoped; agents that stay on `provider: local_stub` remain deterministic and local
- provider connection settings live under top-level `providers.<name>` config entries
- provider credentials may come either from a literal `providers.<name>.api_key` or an environment variable named by `providers.<name>.api_key_env`
- CLI-backed text providers may instead use provider-local binary and working-directory settings under `providers.<name>`, as `codex` now does
- provider-backed generation changes only message generation; agent selection remains deterministic in the current slice
- the canonical agent contract now stores both `provider` and `model`, so mixed-provider conversations are restart-safe and auditable
- canonical message persistence, outbox semantics, and stream inspection must remain correct even when provider calls fail
- plain-text LLMs and sandboxed CLI runtimes must communicate through persisted session/task contracts, not hidden subprocess coupling
- sandbox executions must stay bound to the runtime/provider that was originally assigned to the task
- sandbox runtime configuration now lives under `sandbox.providers.<name>` instead of one global `sandbox.provider`, so adding a new CLI runtime does not require rewriting application contracts
- sandbox delegation runtime choice now belongs to canonical agent state rather than a global free-mode default, so rooms may mix agents that delegate to different CLI runtimes safely
- sandbox execution in the current slice must occur inside copied per-task workspaces so provider runtimes do not mutate canonical source workspaces directly
- agent-owned sandbox-root overrides must be persisted onto sandbox tasks so restart and replay keep using the same copied-workspace location
- copied per-task sandbox preparation now rejects symlinked source entries in this phase, because recreating symlinks would allow writes to escape the copied workspace boundary
- plain-text providers may request sandbox work, but they still do so only through persisted coordination and persisted conversation messages
- sandbox delegation now requires explicit agent-policy permission plus an allowed-runtime match; tool access alone is not enough

## Failure Impact

- provider misconfiguration now fails the step or auto run for the selected agent instead of requiring one process-wide text-provider choice
- provider transport or API failures fail the current step or auto run and no generated message is persisted
- operators must still be able to fall back to `local_stub` without changing canonical runtime state
- future sandboxed runtime failures must surface as explicit task failures without corrupting canonical session state
- sandbox default-runtime misconfiguration now fails explicit task creation or delegated sandbox work without breaking text-only turns in the same session
- agent-level delegation-runtime misconfiguration now fails only the affected agent's delegated work path instead of forcing one room-wide sandbox runtime
- if terminal event recording fails after sandbox side effects, the canonical task row must still be recovered to a terminal state to prevent accidental re-execution on retry
- provider subprocess metadata, changed-file artifacts, and task status must remain inspectable from persisted task records after restart
- malformed sandbox delegation requests from a text provider must fail before the agent reply is persisted, so a rejected delegation does not leave the conversation advanced without matching coordination state

## Tradeoffs

- provider-backed generation improves realism but adds latency and external dependency risk
- the current slice does not make orchestration provider-backed yet, so selection remains predictable while message content becomes external
- exact output determinism is reduced when an external provider is enabled
- copied-workspace sandbox execution improves safety and auditability, but it means provider-produced file changes are currently isolated artifacts rather than direct edits to the source workspace
- rejecting symlinked source entries is stricter than a full materialization strategy, but it preserves the copied-workspace isolation guarantee until a safer materialization path exists
- free-mode mixed-provider delegation improves realism, but in the current slice it is only structured for providers that explicitly request sandbox delegation; plain text providers that do not return a sandbox request remain text-only
- the current `openai`, `gemini`, and `grok` text adapters share an OpenAI-style transport shape internally, while `codex` is a separate CLI-backed text adapter; provider names still remain explicit in agent state and operator config so the domain does not depend on transport quirks
- the current `codex` sandbox runtime is a named CLI provider behind a routed runtime catalog, and the same provider family can now also back direct text turns, so future `claude` or `deepseek` runtimes can be added without collapsing text and sandbox contracts together
- agent-owned sandbox roots make isolation more flexible, but they also make operator mistakes more visible: if two agents point at the same root they intentionally share a sandbox namespace
