# AGENTS.MD

## Purpose

`docs/business` contains product intent, feature business context, operator value, and non-technical reasoning that must remain synchronized with implementation.

## Business Role

This directory is the source of truth for why features exist and how they are expected to create operator value safely.

## Rules

- every meaningful feature must have business context
- document user outcome, failure impact, and operational constraints
- update business docs whenever feature semantics or operator value change
- keep core domain semantics documented as entities, policies, and failure risks evolve
- keep application-layer behavior documented as use cases, events, and operational guardrails evolve
- keep runtime behavior documented as lifecycle, projection, and event-delivery semantics evolve
- keep persistence behavior documented as durability, replay, recovery, and fallback semantics evolve
- if vector retrieval is introduced or planned, document the operator value separately from canonical durability so the two concerns do not get conflated
- if external provider-backed generation is introduced, document configuration, failure impact, and determinism tradeoffs separately from local-stub execution
- if provider selection moves into canonical agent state, document the operator value and failure impact of mixed-provider sessions explicitly
- if free-mode reply routing changes, document the operator value and tradeoffs of agent-selection strategy separately from reply-addressing strategy so the two controls do not get conflated
- if sandboxed CLI runtimes are introduced, document operator value, isolation policy, delegation semantics, and failure impact separately from plain-text LLM generation
- if sandbox runtime ownership moves into canonical agent state or persisted task state, document sandbox-sharing semantics, per-agent isolation behavior, and default/fallback rules explicitly
- if the same provider family can now appear in both text-generation and sandbox-runtime roles, document that distinction explicitly so operators do not confuse who is speaking with who is executing delegated work

## Update Triggers

Update this file when:

- product scope changes
- a feature gains or loses business importance
- operator expectations or risk posture change
