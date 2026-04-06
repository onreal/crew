# AGENTS.MD

## Purpose

`internal/adapters/memory` contains in-memory implementations of the application ports for local runtime execution, development, and tests.

## Business Role

These adapters provide a local-first execution substrate that preserves the same transactional and eventing semantics the production adapters must honor.

## Rules

- do not add business policy here
- preserve atomic state-and-outbox behavior for mutation use cases
- clone mutable state when crossing boundaries so callers cannot mutate stored values accidentally
- keep concurrency explicit and guarded with synchronization
- in-memory agent catalogs must preserve the same active-versus-historical semantics as canonical persistence so tests do not hide stale-agent bugs

## Current Responsibilities

- in-memory repositories for sessions, messages, workflows, agents, sandbox tasks, and agent handoffs
- transactional unit-of-work support
- durable in-memory outbox storage and flush semantics
- in-memory publish/subscribe event bus
- simple clock and ID-generator adapters for local runtime use
- serialized outbox flushing so concurrent callers do not duplicate or drop durable events
- snapshot load/save support for CLI-local runtime state across process invocations
- store snapshot support that can be embedded inside higher-level runtime snapshots
