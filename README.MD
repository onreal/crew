# CREW CLI

**CREW CLI** is a local-first multi-agent runtime for the terminal.

It lets you run persistent agent sessions, inspect transcript-driven conversations, execute deterministic turns, and delegate sandboxed work from a SQLite-backed CLI workflow.

![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)
![SQLite](https://img.shields.io/badge/Storage-SQLite-003B57?logo=sqlite&logoColor=white)
![CLI](https://img.shields.io/badge/Interface-CLI%20%2B%20TUI-4B5563)
![Local First](https://img.shields.io/badge/Architecture-Local--First-16A34A)
![Multi-Agent](https://img.shields.io/badge/Runtime-Multi--Agent-7C3AED)
![Sandboxed](https://img.shields.io/badge/Execution-Sandboxed-F59E0B)

## 🧭 What CREW Is For

CREW is for developers who want multi-agent systems that stay inspectable, scriptable, and grounded in local runtime state.

- Persistent sessions and messages are the source of truth.
- Agents communicate through transcript events, not hidden in-process state.
- Free mode supports reactive multi-agent conversation.
- Sequential mode supports deterministic workflow execution.
- CLI and TUI entrypoints operate over the same persisted runtime.
- Sandbox delegation is represented as persisted tasks, handoffs, and result messages.

## ⚙️ Install

CREW builds from source during installation, so install Go first if you do not already have it:

```bash
go version
```

If that command is missing or older than Go 1.24, install Go from the official guide at <https://go.dev/doc/install>. On macOS, Homebrew users can also run:

```bash
brew install go
```

You also need `git` available because the one-line installer clones the repository.

Install CREW from GitHub:

```bash
tmp="$(mktemp -d)" && git clone https://github.com/onreal/crew "$tmp/crew" && (cd "$tmp/crew" && ./install.sh)
```

If you are already inside a local checkout, run the installer directly:

```bash
./install.sh
```

Then confirm the command is available:

```bash
crew version
crew help
```

The installer builds the binary, installs the PATH-facing wrapper, creates the default home config/runtime directories, and refreshes the operator-local fallback `crew_agents` catalog.

## 🗂️ Flow: Initialize A Workspace

Run this inside the project directory where you want a local agent catalog:

```bash
cd /path/to/your/project
crew init
```

This creates:

```text
crew_agents/
```

The generated catalog is copied from the installed fallback catalog. CREW discovers `crew_agents/` by walking upward from your current directory unless you override it with `CREW_AGENTS_DIR` or `--actors`.

Useful checks:

```bash
crew agents list
crew agents validate
```

## 👥 Flow: Sync Agents Into Runtime State

Agent YAML files are filesystem-owned until you sync them into the configured runtime store.

```bash
crew agents validate
crew agents sync
```

Use actor subcatalogs when you keep multiple teams under one `crew_agents/` directory:

```bash
crew --actors team-a agents list
crew --actors team-a agents sync
crew --actors team-a session start --mode free
```

When a session starts with `--actors`, the selected catalog is persisted onto the session. Later commands such as `session send`, `session step`, `session auto`, and `tui attach` reuse that session catalog.

## 🧾 Flow: Sync Configuration

Inspect the active resolved configuration:

```bash
crew config show
```

Copy a repository config into the installed default wrapper config path:

```bash
crew config sync ./crew.yaml
```

Write to a custom target when needed:

```bash
crew config sync ./crew.yaml --target ~/.config/crew/crew.yaml
```

Prefer environment-backed provider credentials such as `OPENAI_API_KEY`, `GEMINI_API_KEY`, or `XAI_API_KEY` instead of committing literal API keys to repository config.

## 💬 Flow: Start Free Mode

Create and start a free-mode session:

```bash
crew session start --mode free
```

On a real terminal, a free-mode start auto-attaches to the room. In scripts or non-interactive shells, the command returns JSON with the session ID.

If you already have a session ID, attach manually:

```bash
crew tui attach --session-id session-1
```

Use terminal-native scrollback when you want the full history in your terminal and mouse-wheel scrolling handled by the terminal:

```bash
crew tui attach --session-id session-1 --terminal-scrollback
```

Show live provider progress while a turn is running:

```bash
crew tui attach --session-id session-1 --reasoning
```

## 🗨️ Flow: Enter A Conversation

Send a message without opening the TUI:

```bash
crew session send --session-id session-1 --body "Review the rollback plan."
```

Send into a specific conversation:

```bash
crew session send --session-id session-1 --conversation-id conversation-2 --body "Continue this thread."
```

Target one or more agents directly:

```bash
crew session send --session-id session-1 --body "Please review this API." --to-agent reviewer
crew session send --session-id session-1 --body "Plan and implement this." --to-agent planner --to-agent backend_engineer
```

Inside `tui attach`, plain text sends a user message. Mention syntax such as `@reviewer check this` maps to direct agent recipients when the mentioned agents exist in the active catalog.

## ▶️ Flow: Step Or Auto-Run Agents

Run one free-mode turn:

```bash
crew session step --session-id session-1
```

Run one turn for a specific conversation:

```bash
crew session step --session-id session-1 --conversation-id conversation-2
```

Run a bounded multi-turn sequence:

```bash
crew session auto --session-id session-1 --max-steps 3
```

Override orchestration and reply routing when you need explicit behavior:

```bash
crew session auto --session-id session-1 --max-steps 5 --orchestration mentioned_first --reply-routing reply_obligations
```

Supported orchestration modes:

- `deterministic`
- `round_robin`
- `mentioned_first`

Supported reply-routing modes:

- `latest_speaker`
- `reply_obligations`

## 🔎 Flow: Inspect And Tail Runtime State

Inspect a session as JSON:

```bash
crew session inspect --session-id session-1
```

Print persisted stream entries once:

```bash
crew session tail --session-id session-1
```

Follow the stream:

```bash
crew session tail --session-id session-1 --follow
```

Filter by conversation:

```bash
crew session tail --session-id session-1 --conversation-id conversation-2 --follow
```

Pause, resume, or stop a session:

```bash
crew session pause --session-id session-1
crew session resume --session-id session-1
crew session stop --session-id session-1
```

## 🧪 Flow: Sandbox Tasks

Create a persisted sandbox task:

```bash
crew task create --session-id session-1 --instruction "Implement the requested CLI fix."
```

Create a task from a specific workspace and runtime:

```bash
crew task create --session-id session-1 --workspace-root . --runtime codex --permission-profile patch --instruction "Update the docs and tests."
```

Run a pending task:

```bash
crew task run --task-id task-1
```

Inspect or list tasks:

```bash
crew task get --task-id task-1
crew task list --session-id session-1
```

Sandbox execution is auditable: tasks, handoffs, status updates, errors, and summaries are persisted into runtime state.

## 🧠 Flow: Recall And Vectors

Recall relevant messages for a query:

```bash
crew session recall --session-id session-1 --query "rollback plan" --limit 5
```

Check vector backend/index state:

```bash
crew vector status
crew vector status --session-id session-1
```

Rebuild derived vector rows from canonical messages:

```bash
crew vector rebuild --session-id session-1 --force
```

Vector state is derived and rebuildable. Canonical correctness comes from persisted sessions, messages, tasks, and events.

## 🛠️ Command Map

Core commands:

- `crew init` creates a local `crew_agents/` catalog.
- `crew config show` prints resolved configuration as JSON.
- `crew config sync [source-config-path]` copies YAML config to the installed default or `--target`.
- `crew agents list` lists the active filesystem catalog.
- `crew agents validate` validates agent YAML files.
- `crew agents sync` persists the active agent catalog into runtime state.
- `crew session start` creates and starts a session.
- `crew session send` persists a user message.
- `crew session step` executes one free-mode turn.
- `crew session auto` executes bounded free-mode turns.
- `crew session tail` prints the persisted stream.
- `crew tui attach` opens an interactive session view.
- `crew task create|run|get|list` manages sandbox tasks.
- `crew vector status|rebuild` inspects or refreshes derived vector state.

Global options:

- `--config <path>` selects a YAML configuration file.
- `--actors <selector>` selects an actor subcatalog under `crew_agents/`.

## 🧱 Architecture Notes

CREW follows hexagonal architecture:

- `internal/domain` owns pure domain models, value objects, invariants, and domain services.
- `internal/application` owns use cases, orchestration, ports, policies, and workflow coordination.
- `internal/adapters` owns concrete CLI, TUI, persistence, event bus, runtime, and provider integrations.
- `internal/platform` owns shared technical capabilities such as config, logging, IDs, clocks, and lifecycle helpers.
- `cmd/crew` wires the runtime together and must not contain business logic.

The transcript is the system of record. Acceleration layers such as vector indexes are derived and rebuildable.

## ⚖️ Comparison

| Project | Best fit | Core abstraction | Runtime style | Local-first CLI runtime | Persistent session flow | Sandbox task delegation |
|---|---|---|---|---|---|---|
| **CREW CLI** | Terminal-native multi-agent operation | Sessions, transcripts, tasks, agent catalog | Runtime-first | **Yes** | **Yes** | **Yes** |
| **AutoGPT** | Agent automation platform workflows | Agents + platform/app builder | Platform-oriented | Partial | Platform dependent | Yes |
| **CrewAI** | Python multi-agent application development | Crews, flows, agents | Framework-first | Not primary | App-defined | Tool/task oriented |
| **LangGraph** | Durable graph orchestration for apps | Graph/state machine | Low-level framework | Not primary | App-defined | App/tool-defined |

CREW is earlier-stage, but it is intentionally centered on local terminal operation, durable transcripts, deterministic control, and auditable sandbox collaboration.
