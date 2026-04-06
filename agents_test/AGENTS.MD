# AGENTS.MD

## Purpose

`agents_test` is the filesystem-backed catalog of test-only agent definitions for repository tests.

## Business Role

This directory gives tests a stable agent roster so CLI, runtime, and TUI checks do not depend on whatever operator-facing agents happen to live under `agents/`.

## Rules

- each `.yaml` or `.yml` file defines exactly one agent
- filenames are organizational only; the canonical identity is the agent `id` inside the file
- tests that need the canonical planner/reviewer/writer roster must copy or resolve from these files rather than hardcoding inline YAML in test bodies
- agent definitions here must stay valid against the domain `Agent` contract
- optional presentation metadata such as `color` must stay deterministic so TUI tests can rely on a stable fixture catalog
- agent policies here may include deterministic orchestration hints such as `priority` and `weight`; changes to those values change who is considered first in free mode
- sandbox permissions here are business-significant and must be explicit; do not grant sandbox delegation casually
- duplicate agent IDs across files are invalid

## Update Triggers

Update this file when:

- the on-disk agent schema changes
- test agent responsibilities, prompts, or permissions change
- test fixture loading behavior changes
- the relationship between `agents_test/` fixtures and test-time runtime/catalog resolution changes
