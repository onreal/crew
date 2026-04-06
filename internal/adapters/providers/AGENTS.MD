# AGENTS.MD

## Purpose

`internal/adapters/providers` contains concrete outbound adapters for external intelligence providers.

## Business Role

This directory lets the runtime use external intelligence providers without leaking provider-specific protocols, authentication, or execution semantics into the application and domain layers.

## Rules

- keep provider-specific HTTP payloads, auth, and error handling isolated here
- provider adapters must implement application-owned ports and must not introduce business policy
- provider adapters must fail explicitly on misconfiguration and transport errors
- provider adapters must preserve deterministic local testability through mocks or local test servers
- new provider families must live in their own subdirectory with their own `AGENTS.MD`
- shared transport families may live in neutral subdirectories, but named provider wrappers must remain explicit so a provider name never doubles as the generic transport identity
- distinguish plain-text providers from sandboxed CLI runtimes at the adapter boundary
- sandboxed CLI runtimes must not bypass persisted coordination or policy enforcement by talking to each other directly outside runtime-managed contracts
- reusable transport helpers may exist here, but operator-facing provider names must remain explicit in agent state and runtime registration

## Current Responsibilities

- outbound plain-text LLM provider adapters used by free-mode runtime execution
- reusable transport implementations that can back multiple named providers without leaking those transport details upstream
- thin named-provider wrappers such as `openai`, `gemini`, and `grok` that bind provider identity onto a neutral shared transport where appropriate
- shared structured-generation helpers that keep provider families aligned on one reply envelope
- sandboxed CLI provider adapters such as Codex, with future Claude and DeepSeek adapters expected behind the same routed sandbox-runtime port and named config catalog
- provider families such as Codex that may now expose both a direct text adapter and a sandbox runtime adapter without collapsing those contracts together

## Update Triggers

Update this file when:

- a new provider family is introduced
- provider authentication or transport strategy changes
- outbound provider contracts or guarantees change
- provider taxonomy or cross-provider coordination rules change
