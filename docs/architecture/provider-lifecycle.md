# Provider Lifecycle

## Purpose

This document defines the full lifecycle for adding a new provider to `crew` without coupling domain or application code to that provider.

## Why This Exists

Operators need mixed-provider conversations and mixed sandbox-runtime collaboration in the same session. Future contributors need a repeatable way to add providers without turning the runtime into a pile of provider-specific conditionals.

The business constraint is simple:

- text-provider identity belongs to the agent
- sandbox-runtime identity belongs to the task or selected runtime name
- provider connection settings belong to config
- provider protocol details belong to adapters

## Current Model

The current text-provider flow is:

1. operator config defines transport settings under `providers.<name>`
2. agent YAML defines `provider` and `model`
3. runtime seeds those fields into canonical SQLite agent records
4. free-mode generation routes by `agent.provider`
5. provider adapters return the application-owned `GenerationResult`
6. messages, sandbox requests, and failures still flow through canonical persistence

The current sandbox-runtime flow is:

1. operator config defines named runtimes under `sandbox.providers.<name>`
2. agent YAML may define `delegation_runtime` and optional `sandbox_workspace_root`
3. `sandbox.default_provider` selects the default runtime for manual `task create` only
4. persisted sandbox tasks carry `RuntimeName` and any sandbox-root override
5. free-mode delegation routes by the selected agent's `delegation_runtime`
6. runtime execution routes by `task.RuntimeName`
7. provider adapters return the application-owned `SandboxTaskExecutionResult`
8. handoffs, task status, artifacts, and failures still flow through canonical persistence

## Add A Provider

### 1. Decide The Provider Class And Family

First decide whether the provider is:

- a plain-text provider behind `application.LLMProvider`
- a sandboxed CLI runtime behind `application.SandboxedAgentRuntime`

Use the existing chat-completions transport family when the provider can be represented through the same protocol and the same structured response contract.

Use the existing sandbox-runtime routing family when the provider executes bounded tasks from a copied workspace and returns task artifacts through the current application contract.

Add a new provider family package only when the transport, auth, subprocess contract, or result-capture model is materially different enough that reusing the existing adapter would hide real differences.

Do not add provider-specific branching to domain or application code.

### 2. Add Or Reuse Provider Config

For a plain-text provider, add a new default config entry in [config.go](/Users/another_reality/Projects/upai/internal/platform/config.go) under `providers.<name>` with the fields that match its transport family.

For HTTP chat providers this usually means:

- default `base_url`
- default `api_key_env`
- default `timeout_millis`
- default `temperature`

For CLI-backed text providers this may instead mean:

- default `binary`
- optional `working_directory`
- default `timeout_millis`

For a sandboxed CLI runtime, add or reuse a default config entry under `sandbox.providers.<name>` with:

- default `binary`
- optional default `model`
- default `workspace_root`
- default `timeout_millis`
- optional `additional_write`

Validation must stay generic. Platform code validates config shape, not provider business semantics.

### 3. Register The Provider In The Runtime Router

For plain-text providers, update [provider.go](/Users/another_reality/Projects/upai/internal/adapters/runtime/provider.go):

- add a factory in `textProviderFactories`
- map the provider name to its adapter constructor
- keep `local_stub` as a built-in deterministic path

The router must continue to select by `request.Agent.Provider`, not by a global process setting.

For sandboxed CLI runtimes, update the same file by:

- adding a factory in `sandboxProviderFactories`
- mapping the runtime name to its adapter constructor
- keeping routing keyed by `task.RuntimeName`
- preserving `sandbox.default_provider` as a default selector only, not as the only configured runtime

The sandbox router must continue to route by persisted runtime name rather than one global singleton.

### 4. Implement Or Extend The Adapter

If a plain-text provider fits the existing chat-completions path, extend [client.go](/Users/another_reality/Projects/upai/internal/adapters/providers/chatcompletions/client.go) with:

- default base URL
- auth expectations
- operator-visible metadata labels

If a plain-text provider is CLI-backed instead, implement a separate adapter package or package slice that still returns the same application-owned `GenerationResult`. Reuse the shared structured generation contract in [contract.go](/Users/another_reality/Projects/upai/internal/adapters/providers/structuredgeneration/contract.go) rather than inventing a second reply envelope.

If a new package is required:

1. create `internal/adapters/providers/<provider>/`
2. add its own `AGENTS.MD`
3. implement the relevant application port
4. return only application-owned contracts upstream

Adapters must not return provider-native tool formats into the application layer.

For sandboxed CLI runtimes, the adapter should also:

- use the shared sandbox foundation when the runtime fits the copied-workspace execution model
- capture stdout, stderr, exit metadata, and changed artifacts for auditability
- keep provider-specific flags such as `codex exec --json` isolated to the adapter package

### 5. Preserve Canonical State

Do not move text-provider identity back into process-global config.

Do not collapse sandbox runtime selection back into one hardcoded global runtime.

If a plain-text provider changes agent semantics, ensure:

- agent YAML accepts the new `provider` value
- SQLite persistence still round-trips `provider` and `model`
- runtime catalog sync reseeds edited agent files correctly

If a sandbox runtime changes execution selection semantics, ensure:

- agent YAML accepts the new `delegation_runtime` value when relevant
- SQLite persistence still round-trips agent `delegation_runtime` and `sandbox_workspace_root`
- tasks still persist the assigned `RuntimeName`
- tasks still persist any per-agent sandbox-root override
- task execution still routes by the persisted runtime name
- free-mode delegation resolves from canonical agent state rather than a process-global default
- `task create --runtime <name>` can target a non-default configured runtime

### 6. Add Tests

At minimum add or update:

- platform config tests for default and explicit provider config
- adapter tests using a local HTTP server, fake transport, or fake CLI binary as appropriate
- runtime routing tests proving the selected agent or task uses the intended provider
- SQLite runtime tests proving provider-backed turns or tasks persist correctly
- CLI tests for missing credentials or bad provider setup

When possible, add one mixed-provider test where different agents in the same session use different providers.

For sandbox runtimes, also add:

- a task-creation test that uses `--runtime <name>`
- a runtime-router test proving persisted tasks execute against the correct named runtime
- a compatibility test if legacy config shape is still being normalized

### 7. Update Docs In The Same Change

Update:

- [README.md](/Users/another_reality/Projects/upai/README.md)
- [provider-integration-context.md](/Users/another_reality/Projects/upai/docs/business/provider-integration-context.md)
- relevant `AGENTS.MD` files

Document:

- operator value
- config shape
- agent YAML shape when relevant
- failure impact
- determinism tradeoffs
- runtime-selection rules when the provider is a sandboxed CLI runtime

## Guardrails

- never add provider SDK or transport code to `internal/domain`
- never add provider-specific request formats to `internal/application`
- never make provider selection process-global for text agents again
- never make sandbox runtime selection a hardcoded singleton again
- never bypass persisted contracts for sandbox delegation
- never require one provider to be configured just because another agent uses a different provider

## Failure Expectations

Provider configuration errors should fail explicitly for the selected agent turn or task/runtime selection.

Provider transport, API, or CLI execution failures must not persist a partial generated message or corrupt the persisted terminal task state.

Operators must always be able to switch an agent back to `local_stub` without migrating canonical runtime state.

Operators must always be able to change `sandbox.default_provider` or target `task create --runtime <name>` without changing application contracts.
