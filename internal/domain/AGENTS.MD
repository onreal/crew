# AGENTS.MD

## Purpose

`internal/domain` contains the core business concepts and invariants of the multi-agent runtime.

## Business Role

This directory protects the meaning of agents, messages, sessions, workflows, routing policies, and execution constraints. It is the authoritative source for business semantics.

## Allowed Here

- entities
- value objects
- enums and statuses
- domain services
- validation and invariants
- business errors

## Not Allowed Here

- SQL
- CLI or TUI code
- Cobra, Viper, Bubble Tea, Lip Gloss, or provider SDK usage
- transport or storage implementations

## Quality Rules

- keep packages deterministic and easy to unit test
- model invariants explicitly
- avoid interface proliferation unless it serves the domain

## Current Core Contracts

- typed identifiers for agents, sessions, conversations, messages, workflows, and workflow steps
- validated `Agent`, `Message`, `Session`, and `Workflow` entities
- `Agent` now carries canonical `provider`, `model`, and optional `reasoning_effort` identity so mixed-provider sessions remain restart-safe and auditable and Codex-backed agents keep their intended reasoning level across catalog syncs and restart
- `Agent` now also carries canonical sandbox-delegation ownership through `delegation_runtime` and optional `sandbox_workspace_root`, so mixed CLI-runtime rooms do not depend on one process-global free-mode default
- `Session` now carries canonical actor-catalog selection so a started session keeps using the same filesystem-backed agent roster across later invocations
- message senders are modeled explicitly as runtime actors of type `agent`, `user`, or `system`
- explicit session mode and status transitions
- workflow step kinds for deterministic sequencing, fan-out, fan-in, and stop behavior
- conversation and agent policies for loop prevention and routing guardrails
- agent policies now also carry deterministic selection hints such as priority and weight

## Update Triggers

Update this file when:

- domain concepts change
- invariants change
- statuses, policies, or contracts change
- business terminology changes
