# AGENTS.MD

## Purpose

`crew_agents` is the default workspace-local catalog of agent definitions for this repository.

## Business Role

This directory lets `crew` run directly inside the repository without depending on one installed global catalog, while keeping the repository's default agent roster versioned with the codebase.

## Rules

- each `.yaml` or `.yml` file defines exactly one agent
- filenames are organizational only; the canonical identity is the agent `id` inside the file
- root-level agents are the default catalog for this workspace
- subdirectories may hold additional actor catalogs selected with `--actors <selector>`
- runtime startup must seed or update canonical agent records from these files rather than relying on hardcoded in-process defaults
- agent definitions here must stay valid against the domain `Agent` contract
- the shipped root roster is the same operator-facing bootstrap catalog used by `crew init`, so provider/model/reasoning choices here are product defaults rather than test-only placeholders
- the default repository roster models a deliberate collaboration graph: `planner` is the front-door orchestration agent for ordinary user broadcasts, `writer` owns real implementation work and may use Codex sandbox delegation for file changes, and `reviewer` remains a mention-driven review specialist
- `writer` may still thread completion replies back to `planner` so planner-owned obligations can close cleanly, even though writer's visible specialist handoff remains `@reviewer`
- prompts in this catalog must avoid speculative `@agent` mentions; any exact `@agent` token is treated as a real handoff by the runtime, so handles should appear only when the agent is actually transferring work
- `allowed_handoffs` is business-significant policy, not display metadata; it constrains which agent-to-agent routes may create direct replies or outstanding obligations
- duplicate agent IDs across files in the same selected catalog are invalid

## Update Triggers

Update this file when:

- the on-disk agent schema changes
- workspace-local discovery or initialization behavior changes
- the default repository agent roster or responsibilities change
- actor subcatalog layout under `crew_agents/` changes
