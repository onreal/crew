# Sandboxed Agent Runtime Plan

## Objective

Extend `crew` so plain-text LLM providers and sandboxed CLI agent runtimes can both participate in the same session, exchange persisted work, and collaborate safely without breaking the existing hexagonal boundaries.

## Provider Taxonomy

The system now has two provider classes and one mixed-collaboration rule:

1. Plain-text LLM providers
   - generate text responses only
   - do not mutate files or execute commands directly
   - examples: OpenAI-compatible chat generation, future Anthropic-compatible text generation

2. Sandboxed agent runtimes
   - run inside explicit workspaces with controlled permissions
   - may inspect files, edit files, run commands, and return structured results
   - examples: Codex CLI, Claude CLI, DeepSeek CLI, future local agent shells

3. Mixed collaboration
   - plain-text LLMs and sandboxed agent runtimes may communicate with each other
   - that communication must happen through persisted runtime contracts, not hidden shell pipes or direct adapter-to-adapter shortcuts

## Non-Negotiable Rules

- canonical session, message, task, and event state remains in SQLite
- cross-agent communication must be auditable and replayable
- sandbox policies must be explicit per runtime invocation
- provider adapters must remain isolated from domain and application policy
- CLI runtimes must never communicate by ad hoc subprocess piping outside the runtime’s persisted coordination model
- plain-text providers must not gain hidden filesystem or shell privileges just because they can talk to sandboxed agents

## Target Architecture Additions

Add a second outbound capability beside `LLMProvider`:

- `LLMProvider`
  - text generation only
- `SandboxedAgentRuntime`
  - execute a bounded task in a sandbox
  - return structured outputs, artifacts, and status
- `AgentCoordinationService`
  - application-layer use case for handing work from one agent to another
  - mediates communication between text agents and sandboxed agents through persisted records

## Planned Directory Shape

```text
internal/
  application/
    coordination_*.go
    sandbox_*.go
  adapters/
    providers/
      openai/
      codex/
      claude/
      deepseek/
    sandbox/
      workspace/
      policy/
      runner/
```

Each new directory must include its own `AGENTS.MD` in the same change.

## Implementation Phases

### Phase 9A: Provider Taxonomy And Coordination Contracts

- define new application ports for sandboxed runtimes and agent handoff
- define task/request/result contracts for sandbox executions
- define persisted coordination records for agent-to-agent work
- define which message/event types represent handoff, status, result, and failure

Exit criteria:

- plain-text provider and sandboxed-runtime responsibilities are unambiguous
- application ports compile against fakes
- coordination contracts are documented and testable

### Phase 9B: Sandbox Runtime Foundation

- add sandbox workspace adapter(s)
- define workspace lifecycle, mount rules, and command execution policy
- define permission profiles such as read-only, patch, and full-task execution
- add typed runtime errors for timeout, policy denial, sandbox failure, and execution failure

Exit criteria:

- a sandboxed runtime can execute a bounded task in an isolated workspace
- lifecycle, timeout, and cancellation behavior are explicit and tested

### Phase 9C: First CLI Runtime Adapter

- implement `codex` adapter first
- support explicit binary path, working directory, timeout, and permission profile
- support structured capture of stdout, stderr, changed files, and exit status
- expose enough metadata for auditing and replay

Exit criteria:

- one sandboxed CLI provider works behind the new application port
- no domain or application package depends on subprocess protocol details

### Phase 9D: Mixed Collaboration

- allow text LLM agents to delegate tasks to sandboxed agents
- allow sandboxed agents to emit status and result messages back into the session
- persist every delegation, execution state transition, and completion result
- prevent hidden direct communication between adapters

Exit criteria:

- a text agent can request sandboxed work through persisted coordination records
- a sandboxed runtime can report results that other agents can consume
- replay shows the full collaboration chain

### Phase 9E: Additional CLI Providers

- add `claude` and `deepseek` adapters behind the same `SandboxedAgentRuntime` port
- normalize binary discovery, invocation, and result capture behind shared adapter helpers
- keep provider-specific flags isolated inside each adapter package

Exit criteria:

- multiple CLI runtimes can participate without changing application contracts
- provider-specific behavior remains isolated to adapter packages

## Data Model Direction

Planned persisted coordination concepts:

- `agent_tasks`
  - requested work unit
  - requesting agent
  - assigned runtime/provider
  - sandbox policy profile
  - status and timestamps
- `agent_task_artifacts`
  - changed files
  - output summaries
  - structured result payloads
- `agent_handoffs`
  - links between source message/task and delegated task

These remain canonical relational records. Derived views or caches may be added later.

## Safety And Policy Direction

- every sandbox run must declare:
  - workspace root
  - permission profile
  - timeout
  - network policy
  - allowed tools/binaries when applicable
- sandboxed providers must not inherit broad host access by default
- task delegation must be bounded and attributable to the requesting agent

## Observability Requirements

The runtime must expose:

- who delegated work to whom
- when a sandbox task started, completed, failed, or timed out
- which provider/runtime handled the task
- which files changed
- whether a plain-text provider or sandboxed runtime produced each result

## Current Status

- Phase 9A is complete: application ports, persisted sandbox task contracts, and task/handoff events exist
- Phase 9B is implemented in an initial form through a shared sandbox adapter that copies source workspaces into per-task execution directories, enforces timeout boundaries, and captures changed-file artifacts
- Phase 9C is implemented in an initial form through a Codex CLI adapter and live `task` command surface
- Phase 9D is partially implemented: free-mode text agents can now request sandbox delegation, persist the resulting task and handoff, and emit sandbox status/result messages back into the same conversation transcript
- Phase 9E has started in the runtime/config layer: sandboxed CLI runtimes are now configured as a named catalog under `sandbox.providers.<name>`, with Codex registered through a routed sandbox runtime instead of one hardcoded global provider
- free-mode sandbox delegation is now agent-owned: the canonical agent contract may select `delegation_runtime` and an optional agent-specific sandbox root, while persisted sandbox tasks keep the chosen runtime/root for restart-safe execution

## Immediate Next Slice

Continue Phase 9D:

1. add richer policy around which agents may delegate to which sandbox providers
2. support provider-backed text models that return explicit sandbox requests, not just the local stub path
3. surface handoff inspection more directly in the CLI/TUI
4. preserve the same copied-workspace and persisted-audit guarantees while widening mixed-provider collaboration
5. add the next named CLI runtimes such as `claude` and `deepseek` behind the same routed runtime catalog
