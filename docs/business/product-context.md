# Product Context

## What `crew` Is

`crew` is a local-first multi-agent command-line platform. It lets an operator start sessions, coordinate multiple specialized agents, observe the interaction in real time, and choose between open-ended conversation or deterministic workflows.

## Why It Exists

Single-agent interactions are often insufficient when work needs specialization, review, coordination, or controlled sequencing. `crew` exists to provide:

- controlled collaboration between multiple agents
- clear observability into agent behavior
- reproducible workflows for repeatable tasks
- an architecture that can evolve from local development to more reliable deployments

## Primary User Outcomes

- start and operate multi-agent sessions from a terminal
- inspect who said what, when, and why
- switch between flexible collaboration and strict workflows
- prevent uncontrolled loops or noisy behavior
- replay sessions for debugging, auditing, and improvement
- combine plain-text LLM agents and sandboxed tool-using CLI agents in one auditable runtime

## Bootstrap Phase Operator Outcome

The first implementation slice must already provide operator value by making runtime setup explicit and testable:

- configuration can be loaded from file and environment in a predictable way
- startup validation fails fast on invalid runtime assumptions
- command surfaces for session and TUI operation exist before the engines are implemented
- bootstrap behavior is machine-readable so future orchestration and tooling can rely on it

## Current CLI Operator Outcome

The CLI now drives the local runtime for core session control:

- the repository now ships an `install.sh` path that builds and installs the `crew` binary into an operator-selected bin directory so local setup does not depend on custom build steps
- the installed path now includes a PATH-facing wrapper, supported shell-profile PATH bootstrap across login and interactive startup files where appropriate, a seeded operator-local fallback agent catalog that is refreshed on reinstall, a default home config, and dedicated runtime-state directories so the CLI can start from outside the repository without writing SQLite state into arbitrary working directories
- `init` now lets an operator bootstrap a workspace-local `crew_agents/` catalog in any directory, so the default agent roster travels with the project instead of being trapped in one installed global catalog
- the CLI now exposes `config sync` so operators can intentionally copy the YAML file they are editing into the installed wrapper's default config location instead of guessing which config source the installed command is using, with the direct operator-facing form `crew config sync ./crew.yaml`
- `session start` creates and starts a local session, persists any `--actors <selector>` choice onto that session, and on a real terminal now drops free-mode sessions directly into the live chat room so operators do not need a second attach command just to begin talking
- `session pause`, `session resume`, `session stop`, and `session inspect` work across separate CLI invocations through the configured SQLite database at `storage.path`
- `session tail` now lets operators watch the persisted session stream in real time from the terminal
- `tui attach` now lets operators join the same persisted session room in an interactive terminal UI, send messages, and trigger bounded free-mode advancement from one terminal
- `tui attach` can now auto-run several free-mode turns per operator message, which is the current way to approximate a room where multiple agents answer in sequence
- `tui attach` now has operator-facing presentation controls for theme, timestamps, message density, and per-agent colors without changing canonical runtime behavior
- `tui attach` now renders operator messages immediately and keeps free-mode advancement in background room actions so live chats do not appear frozen while agents are replying
- `tui attach` now exposes in-room `/` command discovery and `@agent` recipient selection from the input box itself, so operators can discover room controls and target one or more agents without leaving the live chat flow
- `tui attach` now keeps a borderless managed UI pinned to the bottom of the terminal while the latest transcript tail stays visible inside the room itself, so conversation is no longer hidden behind an internal viewport and the send target stays explicit in room chrome rather than a separate pane
- `tui attach` now groups consecutive sender messages, shows lightweight per-agent/task activity as room chrome below the compose area instead of fake transcript entries, keeps the participant/message counts in one inline line below the compose box, uses a normal single-line compose placeholder that disappears while typing, restores the `CREW CLI` / `NOSTATE` artwork when a new room is still empty, and treats timestamps plus message/conversation IDs as debug-only transcript metadata instead of default room chrome
- `tui attach` can now optionally surface normalized transient live provider progress inline in the chat body when the operator opts in with `--reasoning`, including reasoning-style summaries when a provider exposes them; for Codex-backed turns the adapter explicitly requests detailed summary events so the room can display live reasoning text more consistently without persisting that transient output into canonical session history
- `tui attach` now prints a one-line reattach hint when the operator exits the room, so leaving an active session does not force the operator to remember the exact resume command by hand
- `tui attach` now supports explicit clipboard workflows inside the interactive room without blocking ordinary terminal selection: mouse reporting stays off so operators can select and copy visible chat text normally, and `Ctrl+Y` copies the current visible transcript snapshot for use outside the room
- `session send` lets operators add user messages to a running session through the same persisted runtime path
- `session send --to-agent` now lets operators target one or more specific agents through canonical direct-message routing
- `session send --reply-to` now lets operators keep one conversation explicitly threaded against prior persisted messages
- `agents list` and `agents validate` let operators inspect and verify the filesystem-backed active agent catalog without touching live session state
- `agents sync` now lets operators explicitly persist the current YAML agent catalog into SQLite when they want prompt/model/policy changes applied immediately, instead of waiting for the next free-mode turn to reseed agents opportunistically
- `session step` lets operators execute one deterministic agent turn in free mode using the filesystem-backed agent catalog without needing an external provider, and can be scoped to a single conversation transcript
- `session step` and `session auto` now expose agent-selection diagnostics and support explicit orchestration-mode selection
- free-mode now also exposes explicit reply-routing selection independent from orchestration, so operators can choose between latest-speaker reply behavior and obligation-queue reply behavior without changing who is eligible to speak next
- `session auto` lets operators run a bounded multi-turn free-mode exchange with explicit stop reasons and per-step visibility
- edits to filesystem-backed agent YAML are now reapplied before each free-mode turn, so attached sessions can pick up changed agent definitions without restarting the runtime
- local workspace catalogs now win over the installed home catalog, while the home catalog remains a fallback when an operator runs `crew` outside any initialized workspace
- alternate actor catalogs chosen at `session start` now remain bound to that session across later `session step`, `session auto`, and `tui attach` invocations
- free-mode generation can now mix per-agent text providers in the same session, with provider connection settings coming from config and provider identity living on each agent
- the same named provider family may now appear in both roles at once, for example an agent can use `provider: codex` for direct chat and `delegation_runtime: codex` for sandbox execution
- `task create`, `task run`, `task get`, and `task list` now expose first-class persisted sandbox-task execution through a named sandbox runtime catalog, with `--runtime` available when the operator does not want to use the default
- free-mode sandbox delegation is now also agent-owned: each agent may choose its own `delegation_runtime` and optional `sandbox_workspace_root`, so one room can mix agents that delegate to different CLI runtimes or different sandbox roots
- the runtime now has first-class persisted coordination contracts for sandbox task delegation and handoff between plain-text agents and sandboxed CLI agents
- free-mode stepping can now translate certain text-agent outputs into sandbox delegation, persist the resulting task/handoff, and emit sandbox status/result messages back into the same session conversation
- `vector status` and `vector rebuild` let operators inspect and refresh derived vector state without changing canonical runtime correctness rules
- `session recall` now provides a session-scoped retrieval surface with deterministic fallback when vector search is unavailable or stale
- persisted session inspection includes durable stream history derived from published runtime events
- `runtime.state_path` is now deprecated compatibility config from the former JSON bridge
- default operator-facing config names now follow the `crew` surface: `crew.yaml`, `$HOME/.config/crew/crew.yaml`, `CREW_*`, and for installed flows the seeded home config points SQLite/runtime state at dedicated home-owned paths instead of `./var/*`

## Planned Provider Expansion

The provider model is expanding in two directions:

- plain-text LLM providers for message generation
- sandboxed CLI runtimes for file-changing and command-executing work

These provider classes must be able to communicate with each other through persisted runtime contracts so collaboration remains auditable and replayable.

In the current slice, sandboxed CLI runtimes execute inside copied task workspaces under `sandbox.providers.<name>.workspace_root`, so file changes are reported as artifacts and metadata rather than being applied directly to the source workspace.

In the current local-stub slice, operators can trigger mixed-provider delegation by sending a user message that contains `sandbox:`, `codex:`, or `delegate:` and then running `session step`. That causes the selected text agent to delegate work into the configured default sandbox runtime through persisted coordination records.

In the current slice, sandboxed CLI runtimes are configured as a named catalog under `sandbox.providers.<name>`, with `sandbox.default_provider` selecting the runtime used by default only for manual `task create` when `--runtime` is omitted.

In the current slice, agent YAML may define `delegation_runtime` and optional `sandbox_workspace_root`. That makes sandbox delegation an agent-owned behavior instead of a process-global default, while still preserving restart-safe task routing because persisted tasks carry the chosen runtime and any sandbox-root override.

In the current slice, the default text-agent catalog is filesystem-backed under `crew_agents/`, so operators can change workspace agent behavior by editing those YAML files instead of patching hardcoded runtime seed lists. `crew init` seeds that local catalog with planner, reviewer, and writer agents that default to `provider: codex`, `model: gpt-5.4`, and `reasoning_effort: medium`, while the installed home catalog acts only as fallback when no local workspace catalog exists.

In the current slice, agent YAML now carries `provider`, `model`, and optional `reasoning_effort`, so operator edits can change not only prompting and policy but also which text provider handles a given agent turn and, for providers like Codex, the intended reasoning depth, without changing process-global config.

In the current slice, `provider: codex` is now supported as a direct text-provider path through the local Codex CLI. That means an operator can talk to the same Codex family that later executes delegated sandbox work, while the architecture still keeps direct reply generation separate from persisted sandbox-task execution.

In the current slice, agent YAML policies may also bias deterministic free-mode turn order through `priority` and `weight`, so operators can shape which agents are considered first without modifying code.

In the current slice, agent YAML may also define `allowed_handoffs`, `can_initiate`, and `require_direct_mention` to enforce a deliberate collaboration graph. The shipped planner/writer/reviewer roster uses planner as the front-door responder for ordinary operator messages, keeps planner focused on planning/orchestration instead of direct sandbox implementation, gives writer ownership of real implementation work and Codex sandbox delegation when file changes are required, and keeps reviewer dormant until explicitly mentioned or handed review work by an allowed peer.

In the current slice, visible `@agent` handles are treated as real routing actions, not casual prose. Shipped prompts and the shared structured-generation contract must therefore avoid speculative or hypothetical agent mentions, and real handoffs should be emitted on a dedicated final line so incidental mid-paragraph prose does not wake another agent.

In the current slice, free-mode reply routing is a separate operator control from orchestration. Orchestration still decides which eligible agent speaks next, while reply routing decides whether generated replies follow the latest conversational speaker or satisfy the oldest outstanding reply obligations first. This matters when one agent engages another before all earlier user-targeted replies have been satisfied.

## Business Constraints

- operators must be able to understand runtime behavior
- deterministic workflows must remain reproducible
- free-mode sessions must be bounded by guardrails
- persistence must support debugging and audit needs
- provider integrations must remain replaceable

## Feature Documentation Expectation

Every feature must explain:

- operator value
- business risk if it fails
- policy or safety implications
- expected latency, determinism, and reliability characteristics
- observability signals needed to operate it safely
