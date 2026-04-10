# CREW CLI

**CREW CLI** is a local-first multi-agent runtime for the terminal.

It lets you run persistent agent sessions, inspect transcript-driven conversations, execute deterministic turns, and delegate sandboxed work — all from a SQLite-backed CLI workflow.

Built for developers who want **multi-agent systems with control, auditability, and local operability**.
---


<!-- Replace the license badge URL if your repo uses a different license -->
![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)
![SQLite](https://img.shields.io/badge/Storage-SQLite-003B57?logo=sqlite&logoColor=white)
![CLI](https://img.shields.io/badge/Interface-CLI%20%2B%20TUI-4B5563)
![Local First](https://img.shields.io/badge/Architecture-Local--First-16A34A)
![Multi-Agent](https://img.shields.io/badge/Runtime-Multi--Agent-7C3AED)
![Sandboxed](https://img.shields.io/badge/Execution-Sandboxed-F59E0B)
![JSON Output](https://img.shields.io/badge/Output-JSON-0EA5E9)
![Status](https://img.shields.io/badge/Status-Active%20Development-22C55E)

---

## Why CREW Exists

Most agent frameworks optimize for one of two extremes:

- **high-level automation abstractions** that are easy to start with but harder to inspect deeply
- **low-level orchestration primitives** that are powerful but require significant application code around them

**CREW CLI exists to give developers a third path:**

- **local-first by default**  
  Run sessions, inspect state, and iterate from your terminal without depending on a hosted control plane.

- **deterministic where it matters**  
  Step one turn at a time, inspect persisted state, replay behavior, and understand exactly why an agent spoke.

- **multi-agent without hidden magic**  
  Agents communicate through persisted conversation messages, not opaque background coordination.

- **sandboxed execution with auditability**  
  Delegated work becomes persisted tasks with traceable handoffs and results.

- **provider-flexible runtime design**  
  Different agents can use different providers while keeping the same session model, storage layer, and CLI workflow.

- **built for operators and builders**  
  CREW is not just a framework API. It is a runtime you can actually operate from the command line.

In short, **CREW is for developers who want multi-agent systems that stay inspectable, scriptable, and grounded in local runtime state.**

---

## Comparison

> This comparison is directional. It reflects the current public positioning of each project and where CREW fits best.

| Project | Best fit | Core abstraction | Runtime style | Local-first CLI runtime | Built-in persistent session flow | Sandbox task delegation | Main tradeoff |
|---|---|---|---|---|---|---|---|
| **CREW CLI** | Terminal-native multi-agent operation with explicit control | Sessions, transcripts, tasks, agent catalog | Runtime-first | **Yes** | **Yes** | **Yes** | Earlier-stage surface area |
| **AutoGPT** | Agent automation platform and builder workflows | Agents + platform/app builder | Platform-oriented | Partial | Platform dependent | Yes, platform-oriented | Less centered on deterministic CLI stepping |
| **CrewAI** | Python multi-agent application development | Crews, flows, agents | Framework-first | Not primary | App-defined | Tool/task oriented | More code-framework centric than operator-runtime centric |
| **LangGraph** | Durable graph-based orchestration for production apps | Graph/state machine | Low-level orchestration framework | Not primary | App-defined durable execution | Via app/tool implementation | More flexible, but more implementation-heavy |

### **CREW CLI** when you want:
- a **terminal-native** agent runtime
- **persistent local sessions**
- **SQLite-backed state**
- **step-by-step orchestration**
- **inspectable transcript-driven agent communication**
- **sandbox delegation integrated into the same conversation**
---

## How CREW Is Different

CREW is intentionally opinionated in ways many frameworks are not:

### 1. The transcript is the system of record

Agent communication happens through persisted conversation messages inside a session.

That means:

- every turn is inspectable
- every reply is auditable
- delegated work can be traced back to a conversation event

### 2. The CLI is not an afterthought

CREW is designed to be used directly from the shell:

```bash
crew session start --mode free
crew session send --session-id session-1 --body "review the rollback plan"
crew session auto --session-id session-1 --max-steps 3
crew session inspect --session-id session-1
```

### 3. Deterministic stepping comes first

Instead of jumping straight to “autonomous agents,” CREW makes it easy to:

- run one turn
- inspect the result
- adjust the catalog or config
- run again

That makes the runtime easier to debug and safer to evolve.

### 4. Delegation is persisted, not hidden

When an agent requests sandbox work, CREW persists:

- the agent reply
- the task
- the handoff
- the status/result messages

This keeps text-agent reasoning and sandbox execution inside one auditable flow.

When operators use `crew tui attach --reasoning`, those provider reasoning summaries remain room-local: the attach room now keeps each completed turn's reasoning block visible in the live chat view, including delegated Codex sandbox implementation turns, without persisting that transient provider output as canonical session history.

### 5. Providers do not define the runtime

An agent may speak through one provider while delegated execution uses another runtime.

That separation gives CREW a strong long-term architecture for mixed-provider systems.

---
