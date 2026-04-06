# AGENTS.MD

## Purpose

`internal/adapters/providers/codex` contains the named Codex provider family adapters.

## Business Role

These adapters let `crew` use Codex in two explicit roles while keeping provider-specific flags and subprocess details out of the application layer:

- direct text generation for `provider: codex`
- bounded sandbox execution for `delegation_runtime: codex`

Current operator prerequisites:

- the Codex CLI must be installed and reachable through `codex` or the configured binary path
- the operator must authenticate Codex before direct chat turns or task execution, for example with `codex login` or `codex login --with-api-key`

## Rules

- keep Codex CLI command shaping isolated here
- use the shared `internal/adapters/sandbox` foundation for workspace lifecycle, subprocess execution, and changed-file capture
- use the shared `internal/adapters/providers/structuredgeneration` contract for direct text replies so Codex text turns behave like other providers at the application boundary
- do not let Codex mutate the canonical source workspace directly in this phase
- capture provider-visible stdout, stderr, exit code, and execution workspace metadata for auditability
- map application permission profiles to Codex sandbox modes explicitly and centrally

## Current Responsibilities

- building non-interactive `codex exec` invocations for both direct chat and delegated sandbox execution
- running direct `provider: codex` turns in read-only mode against a configured working directory
- mapping application sandbox permission profiles to Codex sandbox flags
- reading final response text from Codex output files
- packaging changed files and subprocess metadata into `SandboxTaskExecutionResult`
- advertising support for the named text provider `codex` and the named sandbox runtime `codex` inside the routed runtime catalogs
- honoring any per-task sandbox-root override so different agents may isolate or share Codex sandboxes intentionally

## Update Triggers

Update this file when:

- Codex invocation flags or output capture strategy change
- Codex CLI authentication or operator prerequisite expectations change
- the direct-text versus sandbox-runtime split changes
- permission-profile mapping changes
- captured execution metadata changes
- sandbox isolation strategy changes
