# Domain Model Context

## Why The Domain Layer Exists

The domain layer defines the business meaning of a multi-agent runtime before any transport, storage, or provider integration is attached. This protects the system from drifting into framework-led design.

## Business Concepts

### Agent

An agent is a named specialist with a role, a prompt contract, a model selection, and policy limits. The business purpose of the agent model is to make specialization explicit and governable.

### Message

A message is the auditable unit of interaction inside a session. It must identify who sent it, which session and conversation it belongs to, how it is routed, what kind of content it carries, and when it occurred. The sender can be an agent, a human operator, or the system runtime; the model must not require synthetic agent identities for non-agent events.

### Session

A session is the runtime container for a conversation mode. It defines whether the system is operating in free or sequential mode and what lifecycle state it is currently in.

### Workflow

A workflow is the deterministic execution contract for sequential mode. It defines where execution starts, what kind of step is being executed, and how control moves through the graph. Step kinds must carry real semantics: for example, a `fan_in` step is a true merge point with multiple predecessors, not just a label.

### Policy

Policies exist to encode business guardrails such as loop prevention, broadcast control, reply targeting, and agent turn limits. These are not adapter concerns; they are core operating rules.

## Business Risks If The Domain Is Wrong

- sessions may become non-reproducible
- agents may loop or spam without enforceable guardrails
- message history may become non-auditable
- workflow execution may become ambiguous or unrecoverable
- later adapters may bake in inconsistent assumptions that are hard to unwind
