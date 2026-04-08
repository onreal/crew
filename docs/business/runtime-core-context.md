# Runtime Core Context

## Why The Runtime Core Exists

The runtime core turns the application use cases into a live local execution engine. It is responsible for lifecycle ownership, event flow, projection, and operational control without weakening the application and domain contracts.

## Operator-Relevant Behaviors

- start a local session runtime and shut it down cleanly
- create, start, pause, resume, stop, and inspect sessions
- move durable outbox events into a live event stream
- inspect projected session activity through a persisted stream view
- support CLI re-entry through SQLite-backed runtime persistence across separate command invocations
- preserve projected stream history across CLI re-entry through persisted session stream rows rather than transient in-process memory
- repair stale persisted stream state on read-only restart by draining pending durable outbox rows before inspection
- seed and update canonical persisted agent records from the filesystem-backed `crew_agents/` catalog selected for the current workspace during runtime startup

## Business Guardrails

- local runtime control must reuse application use cases rather than bypass them
- durable outbox events must be flushed without discarding unpublished events on failure
- outbox flushing must be serialized so concurrent callers cannot duplicate or skip durable events
- background goroutines must shut down cleanly
- projected stream state must be derived from published events, not mutated directly
- runtime operations that rely on live delivery and projection must only execute while the runtime is actively started
- shutdown must not close the live bus while an in-flight runtime operation is still publishing durable events
- SQLite-backed inspection must read canonical persisted stream rows so separate invocations observe the same history
- concurrent CLI processes must not be able to publish the same durable outbox row twice
