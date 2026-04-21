# AGENTS.MD

## Purpose

`internal/adapters/providers/structuredgeneration` contains the neutral prompt and result-shaping helpers shared by text-generation providers.

## Business Role

This package preserves one canonical `crew` generation contract across different text-provider transports, so HTTP chat providers and CLI-backed providers return the same structured message and optional sandbox delegation envelope.

## Rules

- keep this package transport-neutral; it must not know about HTTP payloads or CLI flags
- preserve canonical agent identity, transcript context, and reply-routing hints in prompt shaping
- preserve the structured `{"message_body":"...", "sandbox_request":...}` contract across providers
- treat visible `@agent` handles as canonical routing semantics in the prompt contract: shared instructions must tell providers that mentioning an agent handle is a real handoff, not a hypothetical example, and that real handoffs belong on a dedicated final line
- when sandbox delegation is allowed, the shared prompt must make clear that direct provider turns stay read-only while `sandbox_request.instruction` must contain the actual delegated work rather than a meta request for access or permissions
- do not move provider registration, authentication, or subprocess execution here

## Current Responsibilities

- building system instructions for structured free-mode generation
- building transcript prompts with canonical routing hints
- parsing structured generation envelopes back into `application.GenerationResult`

## Update Triggers

Update this file when:

- the structured reply envelope changes
- transcript or routing prompt semantics change
- sandbox-delegation prompt semantics change
- more provider families begin sharing this contract
