# AGENTS.MD

## Purpose

`internal/adapters/sandbox` contains the shared local sandbox execution foundation used by provider-specific CLI runtimes.

## Business Role

This package creates isolated execution workspaces, runs bounded subprocesses, and reports auditable file-change artifacts without leaking subprocess details into application logic.

## Rules

- keep provider-neutral sandbox lifecycle, workspace copy, diff, timeout, and subprocess concerns here
- treat task `WorkspaceRoot` as canonical source input; provider adapters may execute only inside derived sandbox workspaces
- sandbox execution may now run either in copied workspaces or, when explicitly configured, directly in the source workspace; copied mode remains the default
- filesystem isolation must not rely on raw task IDs; sandbox task directories must be derived from safe internal names rather than direct operator- or provider-supplied path segments
- copied sandbox preparation must reject source-workspace symlinks in this phase rather than recreating links that could write through to host paths outside the copied tree
- artifact collection must describe changed files deterministically and must ignore internal sandbox bookkeeping files
- timeout, setup, and execution failures must remain typed and explicit

## Current Responsibilities

- preparing per-task execution workspaces under the configured sandbox root
- copying source workspaces into isolated task sandboxes while excluding sandbox-internal recursion paths and rejecting symlinked source entries
- capturing changed-file artifacts by diffing the execution workspace before and after a run
- running bounded subprocesses with stdout, stderr, exit code, and timeout capture
- optionally teeing subprocess stdout and stderr into provider-owned live sinks so UIs can render transient progress without changing canonical persistence

## Update Triggers

Update this file when:

- workspace preparation rules change
- task-root isolation or symlink-handling rules change
- sandbox timeout or execution semantics change
- artifact capture behavior changes
- shared subprocess contracts for provider adapters change
