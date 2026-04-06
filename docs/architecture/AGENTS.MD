# AGENTS.MD

## Purpose

`docs/architecture` contains the implementation plan, architecture decisions, structure guidance, and technical operating constraints.

## Business Role

These docs protect architectural continuity so the system remains operable, testable, and maintainable as more agents contribute.

## Rules

- record important architectural decisions before or with major structural changes
- keep implementation order and package boundaries current
- document tradeoffs when deviating from the original architecture brief
- when persistence strategy changes, document what is canonical state versus what is derived state
- when optional infrastructure such as vector search is introduced, document how core correctness behaves without it
- when new provider classes such as sandboxed CLI runtimes are introduced, document the taxonomy and collaboration rules before implementation
- when provider registration or per-agent provider ownership changes, document the add-a-provider lifecycle and routing rules
- when sandbox runtime ownership moves into canonical agent or task state, document the selection rules and persistence impact before implementation
- when the same provider family supports both direct text turns and sandbox execution, document the separate ports and routing rules before implementation

## Update Triggers

Update this file when:

- architecture decisions change
- implementation phases change
- structural boundaries or dependency rules change
