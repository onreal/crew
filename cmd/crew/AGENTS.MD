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
- current command surface includes `version`, `help`, `config show`, `agents list`, `agents validate`, `agents sync`, `session start`, `session pause`, `session resume`, `session stop`, `session inspect`, `session tail`, `session send`, `session step`, `session auto`, `session recall`, `task create`, `task run`, `task get`, `task list`, `vector status`, `vector rebuild`, and `tui attach`
- bootstrap command failures must remain machine-readable JSON on stderr
- leaf commands must reject unexpected positional arguments
- local runtime persistence must be wired through configured adapters rather than embedded into command handlers
- `storage.path` is the live backing store for session commands; `runtime.state_path` is deprecated compatibility config only
- `config show` must surface both the resolved config payload and the exact config file path in use, and `config sync [source-config-path]` must let operators copy either an explicit YAML file or the active YAML file into the installed default wrapper config path without re-running the installer; README examples should prefer the positional form `crew config sync ./crew.yaml`
- the active filesystem-backed agent catalog may come from `CREW_AGENTS_DIR` when an installed wrapper pins an operator-local catalog; otherwise commands may still discover `agents/` by walking upward from the current working directory
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
- `tui attach` should use a full-screen TTY-first UI when attached to a real terminal, but it must preserve a non-TTY fallback path so automated tests and scripted invocations do not depend on terminal capabilities
- when `tui attach` is not pinned to one conversation, the main room must render the full session timeline and keep a separate send-target conversation, so older conversation history is still reachable by ordinary scrolling instead of being hidden behind conversation-switch controls
- `tui attach` should enable normal mouse-wheel and trackpad scrolling for the room viewport on real terminals instead of requiring keyboard-only navigation for backlog access
- `tui attach` must keep a fixed-height shell for header, chat viewport, input, and footer so typing or status updates do not resize and shift the visible chatboard
- `tui attach` should surface available in-room commands when the operator starts typing `/` and should keep that assist inside the fixed-height input shell rather than spawning resize-sensitive overlays
- `tui attach` should surface available agent mentions when the operator types `@`, allow selecting one or more recipients in the same prompt, and persist those mentions as canonical direct-message `ToAgentIDs` instead of relying on body text alone
- `tui attach` multi-recipient direct prompts must advance enough free-mode turns for each still-targeted recipient to answer once before falling back to normal orchestration, so mentioning two agents in one prompt does not silently stop after the first reply
- `tui attach` must derive pane sizes from the actual rendered shell and frame dimensions, not hard-coded row budgets, so the room, split preview, and sidebar stay aligned to the live terminal window after resize or text wrapping
- `tui attach` conversation scrolling must be owned by the viewport using width-aware wrapped content and the room pane's inner content dimensions only; the outer room border/padding chrome must be rendered outside the viewport so bottom pinning and visible line counts stay correct
- `tui attach` should avoid rebuilding and re-bottom-pinning the room viewport on unchanged poll refreshes, because operators may be typing while the room is already anchored at the bottom and repeated no-op viewport resets create visible jitter
- `tui attach` sidebar and preview panes must clip after wrapping rather than trimming raw newline counts
- `tui attach` should color agent participants from filesystem catalog metadata when available, allow explicit UI config overrides, and otherwise fall back to session-local generated colors instead of hardcoding specific agent IDs
- `tui attach` should render operator messages immediately in the room and run free-mode step/auto work in background commands so the UI does not appear hung while agent replies are being generated
- `tui attach` must keep the active room pane aligned with the conversation it is sending to, so freshly submitted operator messages and the resulting agent replies do not appear hidden in another conversation pane
- `tui attach` should advance bounded auto runs one persisted step at a time in the UI layer instead of wrapping the whole room in one monolithic runtime auto call, so agent replies can appear incrementally
- `tui attach` may show derived visual status for tasks, vectors, providers, and conversation panes, but that data must come from canonical runtime/session queries rather than hidden UI-only coordination state
- `tui attach` sidebar message metrics, including total session messages and per-participant send counts, must be derived from canonical persisted session messages rather than UI-local counters; participant-status rows and message-count rows are separate concerns, and silent participants must not appear in the message-count list with zeroes
- `tui attach` may show optimistic operator entries in the room, pending agent thinking/queued status in the room header, and reply-thread previews, but those visuals must remain derived from canonical conversation state plus explicit local pending UI state rather than inventing a second message source
- `tui attach` should surface provider/runtime failures as visible local room notices, not only as transient header text, so operators do not mistake execution failure for agent silence
- `session step` must reject sequential sessions and must not contaminate workflow-oriented transcripts with free-mode replies
- `session step --conversation-id` must scope execution to that one conversation; when omitted, stepping defaults to the most recent conversation in the session rather than mixing transcripts
- `session step` and `session auto` may accept orchestration-mode overrides, but those overrides must remain validated and visible in the result payload
- `session step`, `session auto`, and `tui attach` may accept reply-routing overrides independent from orchestration; `latest_speaker` and `reply_obligations` are separate operator-visible modes and must not be conflated with agent-selection strategy
- `session auto` must require an explicit positive `--max-steps` bound, must reuse the same persisted free-mode execution path as `session step`, and must return explicit stop reasons and per-step results
- `session step` and `session auto` may now surface persisted sandbox delegation results in their JSON payloads when a text agent delegates work to a sandbox runtime
- `task create` must require an instruction and session scope, default the conversation to `conversation-1`, and use either the configured default sandbox runtime or an explicit `--runtime` override
- `task run` must execute only persisted pending tasks through the configured sandbox runtime and must not bypass runtime/application coordination
- the CLI runtime assembly must load default agents from the repository `agents/` directory rather than relying on hardcoded in-process agent definitions
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
