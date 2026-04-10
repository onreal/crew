# AGENTS.MD

## Purpose

`cmd/crew` is the concrete binary entrypoint for the `crew` CLI.

## Business Role

It exposes the operational entrypoint for starting, attaching to, and controlling agent sessions.

## Rules

- keep `main.go` and command bootstrap thin
- compose dependencies here, do not implement business behavior here
- validate startup configuration and fail fast on invalid runtime setup
- keep command handlers focused on transport concerns such as flags, output formatting, and delegation into internal packages
- current command surface includes `version`, `help`, `init`, `config show`, `agents list`, `agents validate`, `agents sync`, `session start`, `session pause`, `session resume`, `session stop`, `session inspect`, `session tail`, `session send`, `session step`, `session auto`, `session recall`, `task create`, `task run`, `task get`, `task list`, `vector status`, `vector rebuild`, and `tui attach`
- bootstrap command failures must remain machine-readable JSON on stderr
- leaf commands must reject unexpected positional arguments
- local runtime persistence must be wired through configured adapters rather than embedded into command handlers
- `storage.path` is the live backing store for session commands; `runtime.state_path` is deprecated compatibility config only
- `config show` must surface both the resolved config payload and the exact config file path in use, and `config sync [source-config-path]` must let operators copy either an explicit YAML file or the active YAML file into the installed default wrapper config path without re-running the installer; README examples should prefer the positional form `crew config sync ./crew.yaml`
- the active filesystem-backed agent catalog may come from `CREW_AGENTS_DIR`; otherwise commands must discover the nearest `crew_agents/` by walking upward from the current working directory and fall back to the installed home catalog only when no local catalog exists
- `init` must create `./crew_agents` in the current working directory with `AGENTS.MD` plus the placeholder planner, reviewer, and writer agents, and it must fail instead of overwriting an existing local catalog
- the shipped/default `init` roster is operator-facing product behavior: planner, reviewer, and writer should bootstrap as Codex-backed agents using `provider: codex`, `model: gpt-5.4`, and `reasoning_effort: medium`, with planner owning orchestration and writer owning actual Codex-backed implementation work, not as generic local-stub placeholders
- command handlers must treat text providers as a named config catalog under `providers.<name>` while provider choice itself stays on each agent definition
- command handlers may resolve provider credentials from either `providers.<name>.api_key` or `providers.<name>.api_key_env`, and they may also pass through CLI-backed text-provider fields such as `binary` or `working_directory`, but they must not invent a process-global text-provider selection rule
- sandbox task commands must stay thin wrappers over runtime/application coordination flows and must not invent a separate persistence or execution path
- sandbox task creation must resolve the operator-supplied source workspace and bind the task to either `sandbox.default_provider` or an explicit `--runtime` selected from `sandbox.providers.<name>` without embedding sandbox policy in command handlers
- command handlers must not override the selected agent's canonical `delegation_runtime` during free-mode sandbox delegation; that choice belongs to the agent definition, not to a room-wide CLI default
- sandbox task execution may fail after side effects; command handlers must preserve machine-readable error behavior while still allowing operators to inspect the persisted terminal task record
- vector commands must remain optional-control surfaces over derived state and must not imply core runtime correctness depends on vector support
- `session send` must stay a thin transport wrapper over runtime/application message dispatch and must not invent its own persistence path
- `session send --to-agent` must map to canonical direct message routing rather than an out-of-band addressing mechanism
- `session send --reply-to` must map directly to canonical message threading through `ReplyTo` and must not invent CLI-only reply semantics
- `session start --mode free` should immediately hand an operator on a real terminal into the attached room for that newly created session, while non-interactive invocations still keep the machine-readable JSON payload
- `session step` must execute at most one deterministic free-mode agent turn and must keep using the same persisted message/outbox path as user-dispatched messages
- `session tail` must remain a read-only view over the canonical persisted stream; it must not introduce a second event transport or hidden runtime-local state
- `tui attach` may submit operator messages and free-mode control commands, but those actions must still flow through the same canonical persisted runtime methods as `session send`, `session step`, and `session auto`
- `tui attach` may auto-run a bounded number of free-mode turns after operator input, but that behavior must remain explicit, configurable, and routed through canonical `session auto` semantics rather than a private loop
- `tui attach` should use a real-terminal-first interactive room when attached to a TTY, but it must preserve a non-TTY fallback path so automated tests and scripted invocations do not depend on terminal capabilities
- `tui attach` should print a one-line reattach hint when the operator exits the room, including the exact `crew tui attach --session-id ...` command needed to resume the same session and preserving explicit pinned-conversation or debug flags when they shaped the room view
- `tui attach` must keep the managed UI pinned to the bottom of the terminal while the latest transcript tail stays visible in the room itself; changing the send-target conversation must never hide recent conversation history behind room filters, preview panes, or internal viewport clipping
- `tui attach` must remain borderless and transcript-first without double-printing the same persisted events both above the room and inside the room body
- `tui attach` must keep terminal mouse selection available for ordinary copy workflows on real terminals and must not rely on in-room `PgUp`, `PgDn`, `Home`, or `End` handlers for transcript navigation
- `tui attach` should surface available in-room commands when the operator starts typing `/` and should keep that assist inline with the input flow rather than spawning resize-sensitive overlays
- `tui attach` should surface available agent mentions when the operator types `@`, allow selecting one or more recipients in the same prompt, and persist those mentions as canonical direct-message `ToAgentIDs` instead of relying on body text alone
- `tui attach` multi-recipient direct prompts must advance enough free-mode turns for each still-targeted recipient to answer once before falling back to normal orchestration, so mentioning two agents in one prompt does not silently stop after the first reply
- `tui attach` transcript wrapping must derive from the live terminal width rather than hard-coded pane widths, so the room, inline status, and input assist stay aligned after resize or text wrapping
- `tui attach` compact participant counts should appear once below the compose box rather than reading like transcript content, and that inline status must remain width-aware rather than depending on a separate sidebar or decorative pane
- `tui attach` should keep compose placeholder behavior conventional: show one placeholder only while the input is empty, and remove it entirely as soon as the operator starts typing
- `tui attach` should color agent participants from filesystem catalog metadata when available, allow explicit UI config overrides, and otherwise fall back to session-local generated colors instead of hardcoding specific agent IDs
- `tui attach` should also color exact inline `@agent` mentions inside rendered message bodies with that agent's resolved color, so operators can spot handoffs and references quickly without changing persisted message text
- `tui attach` should render the empty-room `CREW CLI` / `NOSTATE` artwork when a session has no visible transcript yet, so a newly started room still has a recognizable CLI identity without reintroducing pane chrome
- `tui attach` should keep the compose area visibly larger than a bare prompt line by using top/bottom padding, while still remaining borderless
- `tui attach` should render operator messages immediately once persisted and run free-mode step/auto work in background commands so the UI does not appear hung while agent replies are being generated
- `tui attach` must keep the active send target aligned with the transcript it is rendering, so freshly submitted operator messages and the resulting agent replies do not appear hidden behind another pane or filtered room
- `tui attach` should advance bounded auto runs one persisted step at a time in the UI layer instead of wrapping the whole room in one monolithic runtime auto call, so agent replies can appear incrementally
- `tui attach` may show derived visual status for tasks, vectors, providers, and the active send target, but that data must come from canonical runtime/session queries rather than hidden UI-only coordination state
- `tui attach --conversation-id` may choose the initial send-target conversation, but it must not filter the room itself; the room remains a full-session view while send routing stays explicit in the header
- `tui attach` may show pending send/agent/task activity in room chrome below the compose box, but optimistic sends and in-flight sandbox progress must not read like ordinary transcript messages or create duplicate operator lines
- `tui attach` should hide raw sandbox handoff chatter from the main room while still surfacing final sandbox completion summaries, sandbox errors, and a compact derived live progress line for currently pending/running tasks, so long-running implementation work does not look like agent silence
- `tui attach` may show transient provider-side progress while a turn is still running, but that transient output must remain TUI-local and must never be persisted into canonical session messages
- `tui attach` should keep ordinary transcript headers minimal by default; timestamps, conversation IDs, and reply target message IDs belong to `--debug` mode instead of the normal room feed
- `tui attach --reasoning` may surface normalized transient provider progress inline in the chat body while a turn is still running, including reasoning-style summaries when a provider exposes them, but that stream must stay opt-in because different providers expose different progress detail and schemas
- `tui attach` should let operators copy room text without leaving the room: terminal mouse selection and standard terminal copy should work because the room must not capture mouse reporting, while `Ctrl+Y` should still copy the current visible transcript snapshot for use outside the room
- `tui attach` clipboard actions must remain UI-local transport behavior only; they must not persist copy state, alter canonical session data, or depend on provider/runtime adapters beyond the already rendered session snapshot
- `tui attach` should surface provider/runtime failures clearly, but must avoid duplicating the same failure both as room chrome and as ordinary transcript content
- `session step` must reject sequential sessions and must not contaminate workflow-oriented transcripts with free-mode replies
- `session step --conversation-id` must scope execution to that one conversation; when omitted, stepping defaults to the most recent conversation in the session rather than mixing transcripts
- `session step` and `session auto` may accept orchestration-mode overrides, but those overrides must remain validated and visible in the result payload
- `session step`, `session auto`, and `tui attach` may accept reply-routing overrides independent from orchestration; `latest_speaker` and `reply_obligations` are separate operator-visible modes and must not be conflated with agent-selection strategy
- `session auto` must require an explicit positive `--max-steps` bound, must reuse the same persisted free-mode execution path as `session step`, and must return explicit stop reasons and per-step results
- `session step` and `session auto` may now surface persisted sandbox delegation results in their JSON payloads when a text agent delegates work to a sandbox runtime
- `task create` must require an instruction and session scope, default the conversation to `conversation-1`, and use either the configured default sandbox runtime or an explicit `--runtime` override
- `task run` must execute only persisted pending tasks through the configured sandbox runtime and must not bypass runtime/application coordination
- the CLI runtime assembly must load default agents from the discovered `crew_agents/` catalog rather than relying on hardcoded in-process agent definitions
- `agents list|validate|sync` must honor the root `--actors <selector>` flag as an invocation-scoped filesystem catalog selector
- `session start --actors <selector>` must persist that selector onto the canonical session so later `session send`, `session step`, `session auto`, and `tui attach` use the same catalog without requiring the flag again
- `agents list` and `agents validate` must read the filesystem-backed agent catalog directly and must not depend on live SQLite runtime state
- `agents sync` must persist the current filesystem-backed agent catalog into the configured runtime backing store on demand and must not invent a second agent-definition source
- `help` must enumerate the available command surface explicitly so operators can discover commands without reading source or docs
- `tui attach` may be human-readable rather than JSON, but failures must still remain machine-readable on stderr

## Update Triggers

Update this file when:

- the binary startup flow changes
- dependency assembly changes
- operator startup behavior changes
- command names, flags, or output contracts change
- error payload shape or argument validation behavior changes
