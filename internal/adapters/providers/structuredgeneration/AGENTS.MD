# AGENTS.MD

## Purpose

`internal/adapters/providers/structuredgeneration` contains the neutral prompt and result-shaping helpers shared by text-generation providers.

## Business Role

This package preserves one canonical `crew` generation contract across different text-provider transports, so HTTP chat providers and CLI-backed providers return the same structured message and optional sandbox delegation envelope.

## Rules

- keep this package transport-neutral; it must not know about HTTP payloads or CLI flags
- preserve canonical agent identity, transcript context, and reply-routing hints in prompt shaping
- preserve the structured `{"message_body":"...", "sandbox_request":...}` contract across providers
- do not move provider registration, authentication, or subprocess execution here

## Current Responsibilities

- building system instructions for structured free-mode generation
- building transcript prompts with canonical routing hints
- parsing structured generation envelopes back into `application.GenerationResult`

## Update Triggers

Update this file when:

- the structured reply envelope changes
- transcript or routing prompt semantics change
- more provider families begin sharing this contract
