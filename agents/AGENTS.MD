# AGENTS.MD

## Purpose

`agents` is the filesystem-backed catalog of default agent definitions for the local runtime.

## Business Role

This directory is the operator-editable source of truth for which default text agents exist, how they behave, and which sandbox runtimes they may delegate to.

The catalog may now include both HTTP-backed persona agents and direct `provider: codex` agents that speak through the local Codex CLI while still delegating sandbox work through canonical task contracts.

## Rules

- each `.yaml` or `.yml` file defines exactly one agent
- filenames are organizational only; the canonical identity is the agent `id` inside the file
- runtime startup must seed or update canonical agent records from these files rather than relying on hardcoded in-process default agents
- `install.sh` seeds an operator-local copy of this catalog on first install so the installed CLI can run outside the repository tree; that seeding must not silently overwrite later operator edits in the installed catalog
- agent files may include optional presentation metadata such as `color`; that metadata must remain rebuildable from disk and must not become canonical runtime business state
- agent definitions here must stay valid against the domain `Agent` contract
- agent policies here may include deterministic orchestration hints such as `priority` and `weight`; changes to those values change who is considered first in free mode
- sandbox permissions here are business-significant and must be explicit; do not grant sandbox delegation casually
- when a provider family appears in both speaking and sandbox roles, keep `provider` and `delegation_runtime` explicit so the room contract stays understandable from disk alone
- duplicate agent IDs across files are invalid

## Update Triggers

Update this file when:

- the on-disk agent schema changes
- startup seeding/update behavior changes
- install-time catalog seeding behavior changes
- default agent responsibilities or permissions change
- direct Codex-speaking agents are introduced or their provider/runtime split changes
- the relationship between filesystem agents and canonical persisted agent records changes
