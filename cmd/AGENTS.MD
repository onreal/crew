# AGENTS.MD

## Purpose

`cmd` contains executable entrypoints and top-level command assembly for the Go CLI.

## Business Role

This directory defines how operators access the system. It must provide a stable, clear operational surface without embedding core business logic.

## Allowed Here

- Cobra command wiring
- process bootstrap
- dependency assembly
- startup and shutdown coordination

## Current Bootstrap Surface

- concrete binary entrypoint lives under `cmd/crew`
- root command assembly for `version`, `config`, `session`, `task`, `vector`, and `tui`
- signal-aware process startup
- machine-readable output contracts for bootstrap-phase commands
- machine-readable JSON error payloads on bootstrap command failures

## Current Session Surface

- `session start` creates and starts a local session backed by SQLite at `storage.path`; when the new session is in free mode and the command is launched from a real terminal, it should immediately enter the same attached chat room operators would otherwise open with `tui attach`
- `config sync [source-config-path]` copies either the named YAML file or the currently active YAML config file into the installed default wrapper config path so operators can intentionally align repository config edits with the installed CLI entrypoint; the preferred documented operator form is `crew config sync ./crew.yaml`
- `session pause`, `session resume`, `session stop`, `session inspect`, `session send`, `session step`, `session auto`, and `session recall` operate against persisted SQLite runtime state across separate invocations
- `session tail` exposes a human-readable live view over the persisted SQLite session stream
- `tui attach` exposes an interactive terminal room over the same persisted SQLite session stream and canonical session-control flows
- `tui attach` must keep its shell, full-width room viewport, optional split preview, optional wide-screen ASCII artwork pane beside the input row, and compact participant/status strip aligned to the live terminal window using measured frame sizes and width-aware clipping rather than hard-coded row budgets, the room viewport must scroll against inner pane dimensions instead of counting border/padding chrome as visible lines, in-room `/` and `@` assists must stay inside that fixed shell rather than introducing floating geometry, and copy behavior must rely on native terminal selection plus explicit snapshot-copy commands rather than a second transcript source
- `tui attach` must expose the full session backlog through the room viewport while still preserving an explicit send target for new operator messages, and it must not sacrifice native terminal text selection by forcing mouse-reporting capture
- `tui attach --terminal-scrollback` must switch to append-only terminal output instead of the managed room so full history stays in native terminal scrollback and mouse-wheel scrolling/selection remain controlled by the terminal rather than Bubble Tea mouse capture
- `task create`, `task run`, `task get`, and `task list` operate against persisted sandbox-task state and the configured sandbox provider
- `vector status` and `vector rebuild` expose optional derived vector-state controls without changing canonical runtime correctness

## Not Allowed Here

- domain rules
- workflow logic
- routing policy
- persistence logic beyond adapter wiring

## Update Triggers

Update this file when:

- command structure changes
- CLI responsibilities move
- bootstrap dependencies change materially
- operator-facing command semantics change
