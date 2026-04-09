# crew

`crew` is a Go-based local-first multi-agent CLI runtime.

You can run and play with it now.

Current implemented surface:

- local session lifecycle management through the CLI
- filesystem-backed default agent catalog under `crew_agents/`
- deterministic single-turn free-mode stepping with agents loaded from the `crew_agents/` directory
- bounded multi-turn free-mode auto execution with agents loaded from the `crew_agents/` directory
- optional per-agent provider-backed free-mode generation behind the same CLI/runtime flow
- persisted sandbox task creation and execution through a Codex-backed copied-workspace runtime
- mixed-provider free-mode delegation where a text agent can create a sandbox task and emit sandbox result messages back into the same conversation
- SQLite-backed persistence across separate CLI invocations
- persisted session stream history for inspection
- vector status and rebuild controls over derived SQLite-backed embeddings
- session recall with deterministic fallback behavior
- machine-readable JSON success and error output

Current non-implemented surface:

- full autonomous free-mode and sequential-mode conversation engines
- CLI or TUI features that use vector retrieval

## What Works Today

You can currently:

- create and start sessions
- pause, resume, stop, and inspect sessions
- send user messages into running sessions
- send direct user messages to one or more target agents
- send user messages as explicit replies to earlier message IDs
- execute one deterministic agent turn with `session step`
- execute a bounded multi-turn free-mode run with `session auto`
- choose reply-routing semantics independently from orchestration with `latest_speaker` or `reply_obligations`
- start a free session and enter the live chat room immediately when running on a real terminal
- keep the attached room as one continuous full-session timeline; `tui attach --conversation-id` only changes the initial send target, not what the room shows
- watch the persisted session stream as a live terminal view with `session tail` or `tui attach`
- see transient Codex reasoning/progress in `tui attach` while a turn is still running, including a dedicated reasoning pane once real progress arrives, without mixing that output into the persisted conversation transcript
- keep working with the same session across separate CLI invocations
- inspect the persisted session event stream
- inspect vector backend/index state
- rebuild derived vector ownership rows from canonical messages
- run session recall commands that degrade gracefully when vector search is unavailable
- create, inspect, list, and execute persisted sandbox tasks against the configured sandbox provider
- trigger sandbox delegation from free mode when the local stub agent sees `sandbox:`, `codex:`, or `delegate:` in the latest user message
- inspect the resolved configuration
- inspect the filesystem-backed agent catalog
- print the available command surface from the CLI itself

The live local backing store is SQLite at `storage.path`.

## Requirements

- Go 1.24+
- a working C toolchain because the current SQLite adapter uses `github.com/mattn/go-sqlite3`

Optional Phase 5B work:

- a build with `-tags sqlitevec_cgo` is the intended path for future `sqlite-vec` integration
- enabling that build path still requires adding the upstream optional module `github.com/asg017/sqlite-vec-go-bindings/cgo`
- the default build remains fully operational without vector support

Optional provider-backed free-mode generation:

- text-provider selection is now per agent through `crew_agents/*.yaml`
- shipped/bootstrap planner, reviewer, and writer agents now default to `provider: codex`, `model: gpt-5.4`, and `reasoning_effort: medium`
- provider connection settings live under top-level `providers:` config entries such as `openai`, `gemini`, and `grok`
- `providers.codex.timeout_millis: 0` disables the direct-text timeout and is now the default because Codex turns may legitimately run longer than a fixed short wall clock
- the same session may mix agents from different text providers while keeping the same persistence, orchestration, and audit behavior
- the OpenAI-style text adapter returns a structured response envelope, so providers can request sandbox delegation without leaking provider-specific tool formats into the runtime

Optional sandbox runtime execution:

- sandboxed CLI runtimes are configured under `sandbox.providers.<name>`
- `sandbox.default_provider` selects the default runtime for manual `task create`
- the first supported CLI runtime is `codex`
- operators must have the Codex CLI installed and authenticated before running Codex-backed tasks

## Quick Start

Run the CLI directly:

```bash
go run ./cmd/crew version
```

Initialize a local workspace catalog:

```bash
go run ./cmd/crew init
```

Start a session:

```bash
go run ./cmd/crew session start --mode free
```

On a real terminal, free-mode `session start` now opens the interactive room immediately. Use `tui attach` later when you want to rejoin an existing running session.

Inspect it:

```bash
go run ./cmd/crew session inspect --session-id session-1
```

Tail it live:

```bash
go run ./cmd/crew session tail --session-id session-1 --follow
```

Pause it:

```bash
go run ./cmd/crew session pause --session-id session-1
```

Resume it:

```bash
go run ./cmd/crew session resume --session-id session-1
```

Stop it:

```bash
go run ./cmd/crew session stop --session-id session-1
```

Send a message:

```bash
go run ./cmd/crew session send --session-id session-1 --body "review the runtime recovery path"
```

Reply to an earlier message:

```bash
go run ./cmd/crew session send --session-id session-1 --reply-to message-1 --body "follow up on that point"
```

Step the free-mode engine once:

```bash
go run ./cmd/crew session step --session-id session-1
```

Trigger mixed-provider delegation from free mode:

```bash
go run ./cmd/crew session send --session-id session-1 --body "sandbox: update the README summary"
go run ./cmd/crew session step --session-id session-1
go run ./cmd/crew task list --session-id session-1
go run ./cmd/crew session inspect --session-id session-1
```

Run a bounded free-mode auto cycle:

```bash
go run ./cmd/crew session auto --session-id session-1 --max-steps 3
```

Create and run a sandbox task:

```bash
go run ./cmd/crew task create --session-id session-1 --instruction "update the README summary" --workspace-root .
go run ./cmd/crew task list --session-id session-1
go run ./cmd/crew task run --task-id task-REPLACE_ME
```

## Install

Install `crew` into `~/.local/bin`:

```bash
./install.sh
crew version
```

Install into a different bin directory:

```bash
INSTALL_DIR=/usr/local/bin ./install.sh
```

What `install.sh` provisions:

- installs a small `crew` wrapper into `INSTALL_DIR`
- installs the real compiled binary under `${XDG_DATA_HOME:-$HOME/.local/share}/crew/bin/crew`
- seeds an operator-local fallback agent catalog under `${XDG_DATA_HOME:-$HOME/.local/share}/crew/crew_agents` on first install
- refreshes that installed fallback agent catalog from the current repository state on reinstall
- writes a default home config to `${XDG_CONFIG_HOME:-$HOME/.config}/crew/crew.yaml` on first install
- creates dedicated runtime-state directories under `${XDG_STATE_HOME:-$HOME/.local/state}/crew`
- updates a supported shell startup file so `INSTALL_DIR` is added to `PATH` when it is not already there
- validates that the installed wrapper can read the seeded config and agent catalog immediately

Install behavior intentionally preserves an existing home config, but reinstall now refreshes the installed fallback `crew_agents` catalog and reconciles `providers.codex.timeout_millis` from the old shipped default `30000` to the current shipped default `0` so stale direct-text timeouts do not linger forever.

Supported PATH bootstrap targets:

- automatic shell detection for `zsh`, `bash`, `fish`, and `sh`-style profile fallbacks
- zsh installs update both `~/.zprofile` and `~/.zshrc`; bash installs update both `~/.bash_profile` and `~/.bashrc`
- `PATH_SETUP_TARGET=zsh|zshrc|zprofile|bash|bashrc|bash_profile|profile|fish|none` if you want to force or disable the PATH-profile edit

The installer does not install external runtimes or provider credentials for you:

- Go and a working C toolchain are still required at install time
- bundled Grok-backed agents still need `XAI_API_KEY` at runtime
- the shipped `crew init` agents and any `provider: codex` catalog entries require the `codex` CLI to be installed and authenticated

## Build

Run without building:

```bash
go run ./cmd/crew version
```

Build a local binary:

```bash
go build -o bin/crew ./cmd/crew
./bin/crew version
```

## Configuration

The CLI loads configuration in this order:

1. `--config /path/to/crew.yaml`
2. `./crew.yaml`
3. `$HOME/.config/crew/crew.yaml`
4. `CREW_*` environment variables

Inspect the resolved configuration:

```bash
go run ./cmd/crew config show
```

Sync the active YAML config into the installed default config path:

```bash
go run ./cmd/crew config sync ./crew.yaml
```

Preferred installed-CLI flow:

```bash
crew config sync ./crew.yaml
```

Default resolved config:

```yaml
app:
  name: crew
  environment: development
  log_level: info

session:
  mode: free
  loop_protection: true
  max_turns: 64
  default_agent_mode: reactive
  orchestration_mode: deterministic
  reply_routing_mode: reply_obligations

storage:
  driver: sqlite
  path: ./var/crew.db

vector:
  enabled: false
  dimensions: 16
  embedder: local_stub
  default_recall_limit: 5

providers:
  openai:
    base_url: https://api.openai.com/v1
    api_key: ""
    api_key_env: OPENAI_API_KEY
    timeout_millis: 30000
    temperature: 0.2
  gemini:
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key: ""
    api_key_env: GEMINI_API_KEY
    timeout_millis: 30000
    temperature: 0.2
  grok:
    base_url: https://api.x.ai/v1
    api_key: ""
    api_key_env: XAI_API_KEY
    timeout_millis: 30000
    temperature: 0.2
  codex:
    binary: codex
    working_directory: .
    timeout_millis: 0

sandbox:
  default_provider: disabled
  source_workspace_root: .
  permission_profile: patch
  providers:
    codex:
      binary: codex
      model: ""
      workspace_root: ./var/sandboxes
      timeout_millis: 300000
      additional_write: []

runtime:
  state_path: ./var/crew-runtime.json

ui:
  refresh_interval_millis: 250
  attach_auto_steps: 1
  attach_split_panes: true
  theme: sunrise
  show_timestamps: true
  compact_messages: false
  attach_sidebar: true
  agent_colors:
    operator: "#f97316"
    planner: "#fb7185"
    reviewer: "#38bdf8"
    writer: "#34d399"
    system: "#fbbf24"
    task: "#a78bfa"
```

Notes:

- `storage.path` is the live local runtime backing store
- `storage.driver` must currently be `sqlite`
- the installed wrapper runs with `--config $HOME/.config/crew/crew.yaml` by default unless you override it with `CREW_CONFIG_PATH` or an explicit CLI `--config`
- `config sync` copies the active YAML file into that installed default config path so the installed `crew` command picks up the same config without rerunning `install.sh`
- when you are editing a repo-local config and want the installed `crew` command to use it, prefer `crew config sync ./crew.yaml`
- the active agent catalog root may be pinned with `CREW_AGENTS_DIR`; otherwise `crew` discovers the nearest `./crew_agents` by walking upward from the current working directory and falls back to the installed home catalog only when no local catalog exists
- `providers.<name>` configures transport, auth, or CLI settings for that named text provider
- agent files now carry both `provider:` and `model:`; provider choice is part of canonical agent state, not a process-global switch
- `provider: local_stub` keeps deterministic local generation; `provider: openai`, `provider: gemini`, and `provider: grok` use configured HTTP providers; `provider: codex` talks through the local Codex CLI
- `providers.<name>.api_key` can hold a literal provider API key directly
- `providers.<name>.api_key_env` names an environment variable to use when `api_key` is empty
- `providers.codex.binary` optionally overrides the Codex CLI binary path for direct `provider: codex` chat turns
- `providers.codex.working_directory` tells direct `provider: codex` chat turns which repo/worktree root Codex should inspect in read-only mode
- different agents in the same chat may use different text providers
- `sandbox.default_provider` selects which configured sandbox runtime manual `task create` uses when `--runtime` is omitted; `disabled` turns that default off
- `sandbox.providers.<name>` configures named sandbox runtimes such as `codex` without coupling runtime assembly to one hardcoded CLI provider
- `sandbox.providers.<name>.binary` optionally overrides the CLI binary path; Codex defaults to `codex`
- `sandbox.providers.<name>.model` optionally selects a provider-specific model for sandbox execution
- `sandbox.source_workspace_root` is the source tree copied into per-task sandboxes for autonomous agent delegation
- `sandbox.providers.<name>.workspace_root` is the root under which copied per-task execution workspaces are created for that runtime
- `sandbox.permission_profile` maps to the provider sandbox mode; it does not change canonical runtime policy rules
- `sandbox.providers.<name>.timeout_millis` is the per-runtime execution timeout; `sandbox.providers.codex.timeout_millis: 0` disables the Codex sandbox timeout
- sandbox tasks operate on copied workspaces inside `sandbox.providers.<name>.workspace_root`; they do not mutate the source workspace directly in this phase
- free-mode sandbox delegation no longer chooses a runtime globally; each agent may now name its own `delegation_runtime`
- an agent may also set `sandbox_workspace_root` to force its delegated tasks into its own sandbox root instead of the runtime default
- `provider: codex` and `delegation_runtime: codex` are different controls: the first chooses who speaks, the second chooses who executes delegated sandbox tasks
- copied sandbox preparation rejects symlinked source entries in the source workspace for now, because recreated symlinks would break workspace isolation
- sandbox delegation is policy-gated by the selected agent; tool access alone does not imply the agent may delegate to every runtime
- task IDs must be simple identifiers, not filesystem paths; path separators and absolute-path forms are rejected
- default agents are loaded from `./crew_agents/*.yaml`; editing those files changes the active local catalog on the next free-mode turn and on the next CLI invocation
- `--actors <selector>` still narrows `agents list|validate|sync` to `./crew_agents/<selector>/*.yaml`
- on `session start`, `--actors <selector>` is now persisted on the session itself; later `session send`, `session step`, `session auto`, and `tui attach` use that stored catalog automatically
- agent files may define `policies.priority` and `policies.weight`; higher values are considered earlier by the deterministic base ordering before mode-specific orchestration is applied
- `session.orchestration_mode` sets the default free-mode agent-selection strategy for `session step`, `session auto`, and `tui attach`
- `session.reply_routing_mode` sets the default free-mode reply-addressing strategy independently from orchestration; `latest_speaker` replies to the newest conversational speaker, while `reply_obligations` satisfies queued user and agent obligations in priority order
- `vector.enabled` controls whether the optional `sqlite-vec` build path should be attempted
- `vector.embedder` is currently `local_stub` only
- `runtime.state_path` is deprecated compatibility config from the old JSON bridge and is not used by live session commands
- `ui.attach_auto_steps` controls how many free-mode turns `tui attach` runs automatically after each operator message; set it higher than `1` for more conversational multi-agent rooms
- `ui.attach_split_panes` controls whether the full-screen room may split into active and preview conversation panes when multiple conversations exist
- `ui.theme` controls the full-screen attach palette; supported values are `sunrise` and `graphite`
- `ui.show_timestamps` toggles timestamps in the full-screen room
- `ui.compact_messages` switches the room feed between denser and more spaced message rendering
- `ui.attach_sidebar` toggles the compact participant/status strip rendered below the full-width chat room
- `ui.agent_colors` lets you override sender colors in the full-screen room by agent ID

Environment variable examples:

```bash
export UPAI_SESSION_MODE=sequential
export UPAI_STORAGE_PATH=/tmp/crew.db
export OPENAI_API_KEY=replace-me
export GEMINI_API_KEY=replace-me
export XAI_API_KEY=replace-me
export UPAI_VECTOR_DIMENSIONS=8
go run ./cmd/crew config show
```

Minimal custom config:

```yaml
storage:
  path: /tmp/crew.db
```

Provider-backed free-mode example:

```yaml
storage:
  path: /tmp/crew.db

providers:
  codex:
    binary: codex
    working_directory: .
    timeout_millis: 0
  openai:
    base_url: https://api.openai.com/v1
    api_key: ""
    api_key_env: OPENAI_API_KEY
    timeout_millis: 30000
    temperature: 0.2
  gemini:
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key: ""
    api_key_env: GEMINI_API_KEY
    timeout_millis: 30000
    temperature: 0.2
  grok:
    base_url: https://api.x.ai/v1
    api_key: ""
    api_key_env: XAI_API_KEY
    timeout_millis: 30000
    temperature: 0.2

sandbox:
  default_provider: codex
  source_workspace_root: .
  permission_profile: patch
  providers:
    codex:
      binary: codex
      model: ""
      workspace_root: ./var/sandboxes
      timeout_millis: 300000
      additional_write: []
```

Run with an explicit config:

```bash
go run ./cmd/crew --config /tmp/crew.yaml config show
```

## Adding Agents

Current state:

- `crew init` creates a local `./crew_agents` catalog with placeholder planner, reviewer, and writer agents
- the default agent catalog now lives in `./crew_agents`
- each `.yaml` or `.yml` file in that directory defines one agent
- runtime startup seeds missing agents from those files and updates existing agents with the same IDs

### Add Agents In Files

The simplest current path is to run `crew init` once, then create a new file under `./crew_agents`, for example `./crew_agents/architect.yaml`.

Each agent must define:

- `id`: stable unique identifier used in transcripts and policy matching
- `name`: display name
- `role`: short purpose such as `planner`, `reviewer`, `coder`, `researcher`
- `system_prompt`: the role instruction
- `provider`: text provider name such as `local_stub`, `openai`, `gemini`, or `grok`
- `provider: codex` means the agent speaks directly through the Codex CLI in read-only mode against `providers.codex.working_directory`
- `model`: provider-specific model name
- `delegation_runtime`: optional per-agent sandbox runtime such as `codex`; required when sandbox delegation is enabled unless there is exactly one allowed runtime to infer from
- `sandbox_workspace_root`: optional per-agent override for where that agent's copied sandbox workspaces are created
- `policies`: conversation and tool/sandbox permissions

The most important policy fields are:

- `RequireDirectMention`: only participate when the latest message mentions the agent ID
- `AllowBroadcast`: whether the agent may answer broadcast messages
- `AllowToolCalls`: whether the agent may request tool/sandbox work
- `AllowSandboxDelegation`: whether the agent may create sandbox tasks
- `AllowedSandboxRuntimes`: which runtimes such as `codex` are allowed
- `Priority`: deterministic selection precedence; higher values are considered first
- `Weight`: deterministic tie-breaker within the same priority band; higher values are considered first
- `MaxConsecutiveTurns`: how many times the same agent may answer in a row before being blocked

Example agent file:

```yaml
id: architect
name: Architect
role: architect
system_prompt: Design the approach and break work into clear implementation steps.
provider: local_stub
model: local-stub
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/architect
tools: []
policies:
  can_initiate: false
  require_direct_mention: false
  allow_broadcast: true
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes:
    - codex
  priority: 50
  weight: 1
  max_consecutive_turns: 1
  max_tool_calls_per_turn: 1
```

Delegation ownership examples:

- Codex speaks directly and also owns its delegated execution:

```yaml
id: implementer
provider: codex
model: codex-mini
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/implementer
policies:
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes: [codex]
```

- Grok speaks, but Codex executes delegated work:

```yaml
id: reviewer
provider: grok
model: grok-4.20-0309-reasoning
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/reviewer
policies:
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes: [codex]
```

- own sandbox per agent:

```yaml
id: planner
provider: openai
model: gpt-4.1-mini
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/planner
policies:
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes: [codex]
```

```yaml
id: reviewer
provider: grok
model: grok-4.20-0309-reasoning
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/reviewer
policies:
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes: [codex]
```

- shared sandbox root across agents:

```yaml
id: planner
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/shared-team
```

```yaml
id: reviewer
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/shared-team
```

In the current slice only `codex` is registered as a CLI runtime, but the agent-owned fields above are the contract that future runtimes such as `claude` or `deepseek` will use as well.

Two separate Codex controls exist on purpose:

- `provider: codex` means Codex is the speaking model for normal room replies
- `delegation_runtime: codex` means Codex executes persisted sandbox tasks when that agent delegates work

Those values may match on the same agent, or they may differ.

The active default operator catalog is the nearest `crew_agents/` directory discovered from the current working directory. If no local catalog exists, `crew` falls back to the installed home catalog.

On runtime startup:

- every agent file is loaded and validated
- agents are seeded if they do not exist yet
- existing persisted agents with the same IDs are updated from the file definitions
- duplicate agent IDs across files are rejected

On free-mode stepping and auto runs:

- the runtime re-reads `./crew_agents/*.yaml` before each run
- attached sessions therefore pick up edited agent definitions on the next turn without requiring a restart
- if the session was started with `--actors team-a`, those same behaviors apply to `./crew_agents/team-a/*.yaml` for that session on later turns and attaches without repeating the flag

Important limitation:

- the current CLI does not expose agent creation, update, or deletion yet
- the filesystem directory is the default source of truth for shipped agents
- programmatic `SeedAgent(...)` is still available if you embed the runtime directly, but the file-backed catalog is now the default operator path

## Agent Communication Lifecycle

Agents do not communicate through hidden sockets or direct subprocess piping. They communicate through persisted conversation messages inside a session.

The lifecycle is:

1. An operator creates a session with `session start --mode free`.
2. The operator adds a user message with `session send`.
3. `session step` or `session auto` loads the messages for exactly one conversation transcript.
4. The runtime filters eligible agents using policy rules.
5. The orchestrator selects the next agent.
6. The selected agent generates one reply.
7. That reply is persisted as a normal message in the same conversation.
8. The next step reads that persisted reply as part of the conversation history.
9. If the agent requests sandbox work, the runtime persists a task, a handoff, and sandbox status/result messages in the same conversation.

That means agent-to-agent communication is transcript-driven:

- agent A writes a persisted message
- agent B reads that message on the next step or auto turn
- agent B responds with another persisted message
- the whole exchange remains auditable in `session inspect`

### What Makes An Agent Eligible To Speak

In the current free-mode loop, an agent may be blocked if:

- the session is not `running`
- the session is not in `free` mode
- the conversation already reached the max-turn limit
- the same agent already spoke too many times in a row
- the agent requires direct mention and the latest message does not mention its ID
- the latest message is broadcast and the agent does not allow broadcast
- the latest message is direct to other recipients and this agent is not targeted

Current orchestration behavior:

- one agent is selected per step
- the default orchestration strategy is `deterministic`
- you can choose orchestration with `deterministic`, `round_robin`, or `mentioned_first`
- you can choose reply routing independently with `latest_speaker` or `reply_obligations`
- candidate ordering starts from agent `priority`, then `weight`, then stable agent ID order
- `session step` and `session auto` now return diagnostics showing eligible agents, blocked agents, ordered candidates, and the applied orchestration mode

### Orchestration Examples

Use `deterministic` when you want the same highest-priority eligible agent to win unless policy blocks it.

```bash
go run ./cmd/crew session auto --session-id session-1 --max-steps 3 --orchestration deterministic --reply-routing latest_speaker
```

Example chat shape:

```text
operator -> room: planner and reviewer, discuss the rollout
planner -> operator: initial rollout plan
reviewer -> planner: risk review on that plan
planner -> reviewer: mitigation update
```

What to expect:

- `planner` speaks first if it has the strongest `priority` and `weight`
- once `planner` has spoken, `max_consecutive_turns` or routing may move the turn to another eligible agent
- rerunning the same transcript with the same agent catalog should keep the same candidate ordering

Use `round_robin` when you want eligible agents to alternate more evenly.

```bash
go run ./cmd/crew session auto --session-id session-1 --max-steps 4 --orchestration round_robin --reply-routing latest_speaker
```

Example chat shape:

```text
operator -> room: writer and reviewer, iterate on the release note
writer -> operator: first draft
reviewer -> writer: tighten the second paragraph
writer -> reviewer: revised draft
reviewer -> writer: approved with one small nit
```

What to expect:

- the runtime still filters by eligibility first
- among the remaining eligible agents, turns rotate instead of always restarting from the top of the deterministic order
- this is the easiest mode to use when you want visible back-and-forth between peers

Use `mentioned_first` when you want direct user mentions to pull a specific agent to the front of the queue.

```bash
go run ./cmd/crew session auto --session-id session-1 --max-steps 3 --orchestration mentioned_first --reply-routing latest_speaker
```

Example chat shape:

```text
operator -> room: @reviewer start with the migration risks, then planner can refine
reviewer -> operator: main migration risks
planner -> reviewer: proposed mitigation plan
reviewer -> planner: accepted with one extra safeguard
```

What to expect:

- if the latest operator message mentions `@reviewer`, `reviewer` is favored ahead of other otherwise-equal candidates
- after the mentioned agent speaks, later turns still depend on eligibility plus the selected reply-routing mode
- this is useful when the operator wants to force who answers first without making the message direct-only

### Reply Routing Examples

Reply routing is separate from orchestration.

- orchestration decides which eligible agent speaks next
- reply routing decides who that generated message is addressed to and which prior message it is satisfying

Use `latest_speaker` when you want each reply to follow the newest conversational turn.

```bash
go run ./cmd/crew session auto --session-id session-1 --max-steps 3 --orchestration round_robin --reply-routing latest_speaker
```

Example chat shape:

```text
operator -> @planner @reviewer: both answer
planner -> operator: here is the first plan, @reviewer check it
reviewer -> planner: review feedback on your plan
planner -> reviewer: acknowledged, refining it
```

What to expect:

- the next agent still comes from orchestration and eligibility
- once an agent speaks, the following reply is addressed to the newest speaker
- this mode produces tighter back-and-forth handoffs between agents

Use `reply_obligations` when you want older queued obligations to be satisfied before newer handoffs.

```bash
go run ./cmd/crew session auto --session-id session-1 --max-steps 4 --reply-routing reply_obligations
```

Example chat shape:

```text
operator -> @agent1 @agent2: both reply
agent1 -> operator: first user-directed reply, @agent2 take this next
agent2 -> operator: second user-directed reply
agent2 -> agent1: follow-up on the handoff from agent1
```

What to expect:

- the user-created obligations are satisfied first
- later agent-to-agent handoffs do not jump ahead of older user-targeted replies
- one message satisfies one obligation
- the same agent may speak twice in a row if it still owes the next queued obligation

### Worked Room Examples

These examples combine orchestration, reply routing, and persisted actor catalogs so you can predict what the room should do.

Single targeted expert:

```bash
go run ./cmd/crew session start --mode free
go run ./cmd/crew session send --session-id session-1 --to-agent reviewer --body "review the rollback plan"
go run ./cmd/crew session step --session-id session-1 --orchestration deterministic --reply-routing latest_speaker
```

Expected chat shape:

```text
operator -> reviewer: review the rollback plan
reviewer -> operator: direct review reply
```

Two directly targeted agents where one hands off to the other:

```bash
go run ./cmd/crew session start --mode free
go run ./cmd/crew session send --session-id session-1 --to-agent planner --to-agent reviewer --body "both of you answer"
go run ./cmd/crew session auto --session-id session-1 --max-steps 4 --reply-routing reply_obligations
```

Expected chat shape:

```text
operator -> planner, reviewer: both of you answer
planner -> operator: my answer, @reviewer add the risk side
reviewer -> operator: my answer to the operator
reviewer -> planner: here is the risk follow-up you asked for
```

Persisted team catalog when free-mode start opens the room immediately:

```bash
go run ./cmd/crew --actors team-a session start --mode free
```

Then inside the room:

```text
@planner @reviewer debate the launch plan
```

Expected room behavior:

- the session keeps using `crew_agents/team-a/*.yaml` even though `tui attach` does not repeat `--actors`
- the first eligible `team-a` agent answers the operator
- if that first reply mentions the other agent, obligation mode still lets the second agent answer the operator before any agent-to-agent follow-up
- the TUI transcript should therefore show the operator-facing replies before the cross-agent continuation

### How Two Agents Talk To Each Other

If you want two agents to discuss a topic, the current path is:

1. Add both agents as separate YAML files under `./crew_agents`.
2. Make sure both allow the kind of messages you want them to answer.
3. Start a free session.
4. Send a user prompt that kicks off the topic.
5. Run `session auto --max-steps N` so the runtime advances several turns.
6. Inspect the transcript with `session inspect`.

Example:

```bash
go run ./cmd/crew session start --mode free
go run ./cmd/crew session send --session-id session-1 --body "architect and implementer: design and refine the runtime recovery plan"
go run ./cmd/crew session auto --session-id session-1 --max-steps 4
go run ./cmd/crew session inspect --session-id session-1
```

What should happen:

- the first eligible agent answers the user
- the next step sees that agent reply in history
- another eligible agent can answer that updated history
- turns continue until `max_steps` is reached or policy stops the run

### Direct Mentions vs Broadcast

Current operator messaging supports both broadcast and direct routing.

- `session send` persists a user message into the conversation
- `session send --to-agent <id>` sends a direct message to one or more specific agents
- `session send --reply-to <message-id>` threads the new message against one persisted prior message
- if your agent uses `RequireDirectMention`, mention the agent ID in the message body, for example `reviewer` or `implementer`

So today:

- broad collaboration: use shared prompts and `session auto`
- narrow collaboration: use one or more `--to-agent` flags
- agent-specific activation: mention the agent ID in the body and use `RequireDirectMention`

### Sandbox Delegation Lifecycle

If one of your agents can use sandbox runtimes, the lifecycle extends like this:

1. The agent generates a normal reply plus a structured sandbox request.
2. The runtime validates that request before persisting the agent reply.
3. The reply is persisted.
4. A sandbox task is persisted.
5. A handoff record is persisted.
6. The sandbox runtime executes inside a copied workspace.
7. Result or failure messages are persisted back into the same conversation.
8. Later text agents can read those sandbox result messages and continue reasoning.

This is how plain text LLM agents and sandboxed CLI agents communicate in the current architecture: through persisted messages, tasks, and handoffs.

Per-agent sandbox example:

```text
operator -> @planner @reviewer: both investigate the failing build
planner -> operator: I will delegate this to Codex
system/sandbox -> room: planner delegated task task-1 to codex
system/sandbox -> room: task-1 completed from sandbox root ./var/sandboxes/planner
reviewer -> operator: I will also delegate this
system/sandbox -> room: reviewer delegated task task-2 to codex
system/sandbox -> room: task-2 completed from sandbox root ./var/sandboxes/reviewer
```

What to expect:

- the room may contain multiple agents that all delegate to `codex`
- each delegated task still uses the selected agent's own `delegation_runtime`
- if the agents set different `sandbox_workspace_root` values, their copied task sandboxes stay separated
- if the agents set the same `sandbox_workspace_root`, they intentionally share the same sandbox namespace root

Direct Codex chat example:

```text
operator -> @implementer: inspect the failing test and tell me what is wrong
implementer -> operator: I checked the repo and the failure is caused by the nil-session guard in runtime startup.
```

Same-Codex talk-and-execute example:

```text
operator -> @implementer: inspect the failing build and patch it if needed
implementer -> operator: I found the problem and I will delegate the patch to Codex.
system/sandbox -> room: implementer delegated task task-1 to codex
system/sandbox -> room: task-1 completed from sandbox root ./var/sandboxes/implementer
implementer -> operator: The patch is ready; the nil-check and regression test were added.
```

### Codex Requirements

Codex now appears in two roles:

- text provider role: `provider: codex`
- sandbox runtime role: `delegation_runtime: codex` and `sandbox.providers.codex`

Operator requirements:

- install the Codex CLI and make `codex` available on `PATH`, or set `providers.codex.binary` and `sandbox.providers.codex.binary` explicitly
- authenticate Codex before using it for direct chat turns or delegated tasks; the CLI supports `codex login` and `codex login --with-api-key`
- keep direct chat configured under `providers.codex`
- keep delegated sandbox execution configured under `sandbox.providers.codex`; `sandbox.default_provider` is only the default for manual task creation, while free-mode delegation uses each agent's `delegation_runtime`

For direct `provider: codex` room replies, `crew` currently runs `codex exec` with:

- `--ephemeral` so each turn is a fresh non-persisted Codex session
- `--output-last-message` to capture the final reply body
- `--sandbox read-only` so direct chat turns can inspect context without mutating files
- `--cd <providers.codex.working_directory>` plus the selected agent model

For delegated sandbox work, `crew` currently runs Codex through `codex exec` with:

- `--json` for structured event capture
- `--output-last-message` for the final summary message
- `--sandbox read-only|workspace-write|danger-full-access` mapped from `read_only|patch|full_task`
- `--skip-git-repo-check` because copied task workspaces may not be Git roots
- `--cd <workspace>` plus optional `--model` and `--add-dir`

This matches the current Codex CLI contract used by the local adapter and `codex exec --help`.

Delegated-work examples:

- one agent owns Codex:

```yaml
id: implementer
provider: codex
model: codex-mini
delegation_runtime: codex
policies:
  allow_tool_calls: true
  allow_sandbox_delegation: true
  allowed_sandbox_runtimes: [codex]
```

- two agents both use Codex but keep separate sandboxes:

```yaml
id: planner
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/planner
```

```yaml
id: reviewer
delegation_runtime: codex
sandbox_workspace_root: ./var/sandboxes/reviewer
```

## Commands

Top-level commands:

- `crew version`
- `crew help`
- `crew init`
- `crew config show`
- `crew config sync [source-config-path] [--target <path>]`
- `crew agents list`
- `crew agents validate`
- `crew agents sync`
- `crew session start [--mode free|sequential]`
- `crew session pause --session-id <id>`
- `crew session resume --session-id <id>`
- `crew session stop --session-id <id>`
- `crew session inspect --session-id <id>`
- `crew session tail --session-id <id> [--conversation-id <id>] [--follow] [--poll-interval-millis <n>]`
- `crew session send --session-id <id> --body <text> [--conversation-id <id>] [--reply-to <message-id>] [--to-agent <id>]...`
- `crew task create --session-id <id> --instruction <text> [--conversation-id <id>] [--workspace-root <dir>] [--runtime <name>] [--permission-profile read_only|patch|full_task] [--task-id <id>]`
- `crew task run --task-id <id>`
- `crew task get --task-id <id>`
- `crew task list --session-id <id>`
- `crew session step --session-id <id> [--conversation-id <id>] [--orchestration deterministic|round_robin|mentioned_first] [--reply-routing latest_speaker|reply_obligations]`
- `crew session auto --session-id <id> --max-steps <n> [--conversation-id <id>] [--orchestration deterministic|round_robin|mentioned_first] [--reply-routing latest_speaker|reply_obligations]`
- `crew session recall --session-id <id> --query <text> [--limit N]`
- `crew vector status [--session-id <id>]`
- `crew vector rebuild [--session-id <id>] [--force]`
- `crew tui attach --session-id <id> [--conversation-id <id>] [--follow=false] [--poll-interval-millis <n>] [--auto-steps <n>] [--orchestration deterministic|round_robin|mentioned_first] [--reply-routing latest_speaker|reply_obligations]`

### `version`

Print build metadata:

```bash
go run ./cmd/crew version
```

Example output:

```json
{
  "version": "dev",
  "commit": "none",
  "date": "unknown"
}
```

### `config show`

Print the resolved config as JSON:

```bash
go run ./cmd/crew config show
```

### `config sync`

Copy a YAML config file into the installed default wrapper config path.

```bash
crew config sync ./crew.yaml
crew config sync /absolute/path/to/crew.yaml
go run ./cmd/crew config sync ./crew.yaml
go run ./cmd/crew config sync ./crew.yaml --target ~/.config/crew/crew.yaml
```

Preferred usage is `crew config sync ./crew.yaml`.

This command is for the case where you are editing one YAML file, but the installed `crew` wrapper is reading another one. By default it syncs into `$XDG_CONFIG_HOME/crew/crew.yaml` or `~/.config/crew/crew.yaml`. If `source-config-path` is omitted, it falls back to the currently active YAML config file, but the positional source-path form is the clearer operator-facing choice.

### `help`

Print the available command surface as JSON:

```bash
go run ./cmd/crew help
```

### `init`

Create a local `./crew_agents` catalog in the current directory.

```bash
crew init
go run ./cmd/crew init
```

This command creates:

- `./crew_agents/AGENTS.MD`
- `./crew_agents/planner.yaml`
- `./crew_agents/reviewer.yaml`
- `./crew_agents/writer.yaml`

`crew init` fails if `./crew_agents` already exists.

### `agents list`

List the agent definitions loaded from the filesystem catalog:

```bash
go run ./cmd/crew agents list
go run ./cmd/crew --actors team-a agents list
```

### `agents validate`

Validate every agent file under `./crew_agents`:

```bash
go run ./cmd/crew agents validate
go run ./cmd/crew --actors team-a agents validate
```

### `agents sync`

Persist the current `crew_agents/*.yaml` catalog into the configured SQLite backing store immediately:

```bash
go run ./cmd/crew agents sync
go run ./cmd/crew --actors team-a agents sync
```

Use this when you want changed agent YAML prompts, providers, models, or policies to be written to persistence on demand instead of waiting for the next `session step` or `session auto` to reseed them.

Important note about prompts:

- `agents sync` persists the YAML `system_prompt` exactly as stored in SQLite
- the shared chat-completions transport still wraps that YAML prompt with a hardcoded instruction envelope in [client.go](/Users/another_reality/Projects/upai/internal/adapters/providers/chatcompletions/client.go)
- the wrapper is built by `systemInstruction(...)`
- that wrapper adds the agent name/role plus the JSON-output contract used by the runtime
- if an agent uses `provider: local_stub`, the stub generator does not use `system_prompt` for actual text generation

### `session start`

Create a new session and transition it to `running`.

```bash
go run ./cmd/crew session start --mode free
go run ./cmd/crew --actors team-a session start --mode free
```

```bash
go run ./cmd/crew session start --mode sequential
```

If `--mode` is omitted, `session.mode` from config is used.

If `--actors <selector>` is supplied here, that selector is persisted on the session and reused automatically by later `session send`, `session step`, `session auto`, and `tui attach` commands for the same session.

When `session start --mode free` runs on a real terminal, it immediately opens the attached room for the newly created session instead of stopping after the JSON payload. The JSON output below still applies to non-interactive invocations, redirected output, and sequential sessions.

Example output:

```json
{
  "session": {
    "ID": "session-1",
    "Mode": "free",
    "Status": "running",
    "CreatedAt": "2026-03-20T16:10:21.25856Z"
  },
  "storage_path": "./var/crew.db"
}
```

### `session pause`

Pause a running session:

```bash
go run ./cmd/crew session pause --session-id session-1
```

### `session resume`

Resume a paused session:

```bash
go run ./cmd/crew session resume --session-id session-1
```

### `session stop`

Stop an active session:

```bash
go run ./cmd/crew session stop --session-id session-1
```

### `session inspect`

Inspect persisted state and the persisted stream for a session:

```bash
go run ./cmd/crew session inspect --session-id session-1
```

Example output:

```json
{
  "snapshot": {
    "Session": {
      "ID": "session-1",
      "Mode": "free",
      "Status": "running",
      "CreatedAt": "2026-03-20T16:10:21.25856Z"
    },
    "Messages": null,
    "Stream": [
      {
        "Topic": "session.created",
        "RecordedAt": "2026-03-20T16:10:21.258582Z",
        "Payload": {
          "Session": {
            "CreatedAt": "2026-03-20T16:10:21.25856Z",
            "ID": "session-1",
            "Mode": "free",
            "Status": "pending"
          }
        }
      },
      {
        "Topic": "session.updated",
        "RecordedAt": "2026-03-20T16:10:21.258585Z",
        "Payload": {
          "Session": {
            "CreatedAt": "2026-03-20T16:10:21.25856Z",
            "ID": "session-1",
            "Mode": "free",
            "Status": "running"
          }
        }
      }
    ]
  },
  "storage_path": "/tmp/crew.db"
}
```

### `session tail`

Print the persisted session stream as a human-readable terminal view:

```bash
go run ./cmd/crew session tail --session-id session-1
go run ./cmd/crew session tail --session-id session-1 --follow
go run ./cmd/crew session tail --session-id session-1 --conversation-id conversation-1 --follow
```

This command reads the same durable stream that backs `session inspect`, but formats it as text so you can watch operators, agents, and sandbox-task events in one terminal.

### `session step`

Execute one free-mode turn in a running free session:

```bash
go run ./cmd/crew session step --session-id session-1
```

Conversation-scoped stepping:

```bash
go run ./cmd/crew session step --session-id session-1 --conversation-id conversation-1
```

Important behavior:

- `session step` rejects sequential sessions
- if a provider returns a malformed sandbox delegation request, the step fails before any agent reply is persisted
- when a valid sandbox delegation request is present, the resulting task, handoff, and status/result messages are persisted in the same conversation

### `task create`

Create a persisted sandbox task:

```bash
go run ./cmd/crew task create --session-id session-1 --instruction "update the README summary"
go run ./cmd/crew task create --session-id session-1 --instruction "update the README summary" --runtime codex
```

Optional explicit task ID:

```bash
go run ./cmd/crew task create --session-id session-1 --task-id task-readme-1 --instruction "update the README summary"
```

Task ID rules:

- task IDs must be simple identifiers
- path separators like `/` or `\\` are rejected
- absolute-path forms are rejected
- relative path tokens like `.` and `..` are rejected

Runtime selection:

- when `--runtime` is omitted, `task create` uses `sandbox.default_provider`
- `--runtime <name>` must match a configured entry under `sandbox.providers.<name>`
- `task run` executes the persisted task against the runtime recorded on that task, not against whichever runtime is currently default
- free-mode delegated tasks do not use `sandbox.default_provider`; they use the selected agent's `delegation_runtime`

### `tui attach`

Attach a full-screen interactive session room in the terminal.

```bash
go run ./cmd/crew tui attach --session-id session-1
go run ./cmd/crew tui attach --session-id session-1 --conversation-id conversation-1
go run ./cmd/crew tui attach --session-id session-1 --follow=false
go run ./cmd/crew tui attach --session-id session-1 --auto-steps 3 --orchestration round_robin
go run ./cmd/crew tui attach --session-id session-1 --auto-steps 3 --reply-routing latest_speaker
```

`tui attach` now opens a full-screen Bubble Tea room on a real TTY. It uses the same persisted session stream as `session tail`, but adds styled conversation rendering, a bottom input box, keyboard shortcuts, and a compact participant/status strip below the chat instead of reserving a right-hand sidebar. When stdin/stdout are not real terminal devices, it falls back to the earlier line-oriented attach mode so tests and scripted use still work.

This is also the room that `session start --mode free` opens automatically on a real terminal for a brand-new session. `tui attach` remains the explicit reattach path for existing sessions.

Interactive behavior:

- plain text sends a user message into the attached conversation
- when attach is not pinned with `--conversation-id`, the main room now shows the whole session timeline instead of only one conversation, so older session history remains reachable by scrolling
- typing `/` in the input box now shows the available in-room commands and lets you accept one into the input with `Tab` or `Enter`
- typing `@` now shows the available agent recipients and lets you target one or more agents in the same prompt; those mentions are persisted as canonical direct recipients
- when one prompt targets multiple agents, the attached room now auto-runs enough turns for each mentioned recipient to answer once before ordinary room orchestration resumes
- operator messages now render in the room immediately instead of waiting for the bounded auto-run to finish
- the active room pane now snaps to the conversation you just sent into, so new operator messages and follow-up agent replies do not appear hidden in another pane
- `/step` executes one free-mode turn
- `/auto 3` executes a bounded multi-turn run
- `/help` prints the attach commands
- `/quit` exits the attached room
- by default, plain text input also triggers an automatic bounded free-mode run using `ui.attach_auto_steps`
- `--auto-steps N` overrides `ui.attach_auto_steps` for the attached room
- `--reply-routing latest_speaker|reply_obligations` overrides `session.reply_routing_mode` for the attached room without changing orchestration
- `--orchestration round_robin` is the most practical current setting if you want several agents to respond in sequence instead of the highest-priority agent replying first
- round-robin now advances across the full configured agent roster even when the previous speaker is temporarily blocked by `max_consecutive_turns`, so shared-room auto runs do not bounce between only two agents
- `Ctrl+C` or `Esc` exits the full-screen room
- `Ctrl+L` refreshes the room snapshot immediately
- `Ctrl+Y` copies the current visible TUI snapshot, including the room, compact status strip, and any visible preview pane
- normal terminal mouse selection is enabled again, so you can highlight visible TUI text and copy it with your terminal's usual copy gesture
- `Up` and `Down` navigate input history when no assist is open, or move through `/` and `@` suggestions when an assist is open
- `PgUp` and `PgDn` scroll the room viewport
- `Home` and `End` jump to the top or bottom of the room
- `[` and `]` switch the active conversation when the room is not pinned to one conversation
- `Tab` accepts the active `/` or `@` suggestion; when no assist is open it still toggles split conversation panes when multiple conversations are present
- `Ctrl+D` shows recent inputs in the status line
- `ui.attach_sidebar` controls whether the room shows the compact participant/status strip below the chat viewport
- `ui.attach_split_panes` controls whether the room can show active plus preview conversation panes
- `ui.show_timestamps`, `ui.compact_messages`, and `ui.agent_colors` control the room presentation
- the room now uses a fixed-height shell for header, chat viewport, input, and footer, so typing and live status updates do not make the chatboard jump
- background free-mode step/auto work no longer blocks the whole room; agent replies continue to arrive through the persisted stream while the operator keeps control of the UI
- messages from the same sender are grouped together in the room to reduce chatter
- pending agent activity now renders in the room header as lightweight `thinking` and `queued` status, so live runs stay visible without pushing real messages out of the chat backlog
- reply messages now show a short `in reply to ...` preview when the upstream message is available in the current session snapshot
- provider/runtime failures in the attached room now also appear as local system notices in the conversation instead of only in the header state
- the room now stays full-width even when participant status is enabled; no right-hand sidebar space is reserved
- on wide terminals, the input row now reserves a decorative right-side ASCII panel at roughly 20% width, showing `NOSTATE` and `CREW CLI`; on narrower terminals that panel disappears entirely while the conversation viewport stays full width
- the compact status strip now keeps only total persisted message count plus every configured participant with that participant's message count
- participant names in the compact strip shrink with ellipsis as the terminal narrows so the full participant roster stays visible
- room and preview panes now use flat borders rather than rounded corners so the layout remains dense and predictable in terminal grids

## Full Operational Walkthrough

Create a dedicated config file:

```bash
cat > /tmp/crew.yaml <<'EOF'
storage:
  path: /tmp/crew.db
EOF
```

Start from a clean local database:

```bash
rm -f /tmp/crew.db
```

Create and start a session:

```bash
go run ./cmd/crew --config /tmp/crew.yaml session start --mode free
```

Inspect it from a separate invocation:

```bash
go run ./cmd/crew --config /tmp/crew.yaml session inspect --session-id session-1
```

Pause it:

```bash
go run ./cmd/crew --config /tmp/crew.yaml session pause --session-id session-1
```

Resume it:

```bash
go run ./cmd/crew --config /tmp/crew.yaml session resume --session-id session-1
```

Stop it:

```bash
go run ./cmd/crew --config /tmp/crew.yaml session stop --session-id session-1
```

Inspect final persisted state:

```bash
go run ./cmd/crew --config /tmp/crew.yaml session inspect --session-id session-1
```

What you should observe:

- the same session ID remains available across separate commands
- session status changes persist in the SQLite database
- the `snapshot.Stream` array grows as lifecycle transitions are recorded

## Sandbox Safety Constraints

Current sandbox execution is intentionally strict:

- each task runs in a copied workspace rooted under the selected `sandbox.providers.<name>.workspace_root`
- the source workspace is not mutated directly in this phase
- copied-workspace preparation rejects symlinked source entries instead of reproducing them in the sandbox
- sandbox task directories are derived from internal safe names, not raw task IDs
- changed artifacts describe files inside the copied workspace only

If your source tree contains symlinks, sandbox task preparation currently fails. Replace those symlinks or point `sandbox.source_workspace_root` / `--workspace-root` at a symlink-free subtree for now.

## Output Conventions

Success output is written as JSON to stdout.

Error output is written as JSON to stderr.

Leaf commands reject unexpected positional arguments.

Examples:

- `session start`, `pause`, `resume`, and `stop` return `session` and `storage_path`
- `session inspect` returns `snapshot` and `storage_path`
- `config show` returns `config` and `config_path`

## Machine-Readable Errors

Missing required flag:

```bash
go run ./cmd/crew session pause
```

Example stderr:

```json
{
  "error": {
    "code": "invalid_arguments",
    "message": "missing required --session-id"
  }
}
```

Invalid mode:

```bash
go run ./cmd/crew session start --mode invalid
```

Example stderr:

```json
{
  "error": {
    "code": "invalid_arguments",
    "message": "invalid session mode \"invalid\": must be free or sequential"
  }
}
```

### `session send`

Persist a user utterance into a running session.

```bash
go run ./cmd/crew session send --session-id session-1 --body "review the runtime recovery path"
```

Thread the message as a reply:

```bash
go run ./cmd/crew session send --session-id session-1 --reply-to message-1 --body "follow up on that point"
```

Direct routing to specific agents:

```bash
go run ./cmd/crew session send --session-id session-1 --to-agent reviewer --to-agent writer --body "review and draft this response"
```

If `--conversation-id` is omitted, the CLI uses `conversation-1`.

### `session step`

Execute one deterministic free-mode agent turn using the agents currently loaded from the active `crew_agents` catalog.

```bash
go run ./cmd/crew session step --session-id session-1
go run ./cmd/crew session step --session-id session-1 --conversation-id conversation-a
go run ./cmd/crew session step --session-id session-1 --orchestration mentioned_first
go run ./cmd/crew session step --session-id session-1 --reply-routing latest_speaker
```

This is a bounded one-turn operation. It is not yet an autonomous loop.

`session step` is free-mode only. It rejects sequential sessions. When `--conversation-id` is omitted, the command steps only the most recent conversation in the session; it does not mix multiple conversation transcripts together.

If the selected agent uses `provider: local_stub`, generation is deterministic and local. If the selected agent uses `provider: openai`, `provider: gemini`, or `provider: grok`, the same command uses that configured external provider. If the selected agent uses `provider: codex`, the same command runs a direct read-only `codex exec` turn against `providers.codex.working_directory` while preserving the same persistence and outbox behavior.

The JSON response now includes:

- `OrchestrationMode`
- `ReplyRoutingMode`
- `EligibleAgentIDs`
- `BlockedAgents`
- `OrderedCandidateIDs`

### `session auto`

Execute a bounded multi-turn free-mode run using the agents currently loaded from the active `crew_agents` catalog.

```bash
go run ./cmd/crew session auto --session-id session-1 --max-steps 3
go run ./cmd/crew session auto --session-id session-1 --conversation-id conversation-a --max-steps 2
go run ./cmd/crew session auto --session-id session-1 --max-steps 4 --orchestration round_robin
go run ./cmd/crew session auto --session-id session-1 --max-steps 4 --reply-routing reply_obligations
```

`session auto` is free-mode only. It requires a positive `--max-steps` bound, preserves the same conversation scoping rules as `session step`, and returns per-step results plus a final stop reason such as `max_steps_reached`, `policy_max_turns_reached`, or `policy_max_consecutive_turns_reached`.

### `session recall`

Recall relevant session messages. When vector search is disabled, stale, rebuilding, or degraded, the CLI falls back deterministically to lexical or recent-message ordering.

```bash
go run ./cmd/crew session recall --session-id session-1 --query "runtime recovery"
```

End-to-end example:

```bash
go run ./cmd/crew session start --mode free
go run ./cmd/crew session send --session-id session-1 --body "review the runtime recovery path"
go run ./cmd/crew session step --session-id session-1
go run ./cmd/crew vector rebuild --session-id session-1
go run ./cmd/crew session recall --session-id session-1 --query "runtime recovery"
```

### `vector status`

Inspect vector backend status and persisted index state:

```bash
go run ./cmd/crew vector status
go run ./cmd/crew vector status --session-id session-1
```

### `vector rebuild`

Rebuild derived vector ownership rows from canonical messages:

```bash
go run ./cmd/crew vector rebuild
go run ./cmd/crew vector rebuild --session-id session-1
go run ./cmd/crew vector rebuild --session-id session-1 --force
```

## SQLite Backing Store

The current CLI persists runtime state into the configured SQLite database at `storage.path`.

That database currently stores:

- sessions
- messages
- workflows
- agents
- durable outbox rows
- persisted session stream rows
- schema migration metadata

This is now the live local persistence layer for session commands.

An isolated optional Phase 5B adapter also exists under `internal/adapters/storage/sqlitevec`:

- it owns derived `message_embeddings` and `vector_index_state` tables
- it can rebuild embeddings from canonical messages
- it is now exposed through `vector status`, `vector rebuild`, and fallback-aware `session recall`
- live nearest-neighbor search still requires an opt-in `sqlite-vec` build path

## Current Limitations

- `crew init` only bootstraps a new local catalog; later agent creation and updates still happen by editing `./crew_agents/*.yaml`
- free-mode orchestration is now configurable, but it still selects only one agent per step
- provider-backed generation requires the selected agent to name a configured external provider and for that provider to have valid credentials
- sandbox task commands exist, but sandboxed agent orchestration is still limited to the current persisted task/handoff flow rather than a richer autonomous tool-use loop
- `tui attach` is now full-screen only on real TTYs; non-terminal piping and test harnesses still use the fallback line-oriented attach path
- true nearest-neighbor vector search still requires the optional `sqlite-vec` build path and dependency activation
- sequential mode is still a lifecycle/workflow shell rather than a full execution engine
- `runtime.state_path` remains in config only as deprecated compatibility baggage

## Development

Run the test suite:

```bash
go test ./...
```

Useful smoke checks:

```bash
go run ./cmd/crew version
go run ./cmd/crew config show
go run ./cmd/crew session start --mode free
go run ./cmd/crew session inspect --session-id session-1
```
