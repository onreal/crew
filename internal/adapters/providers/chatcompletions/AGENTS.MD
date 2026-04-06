# AGENTS.MD

## Purpose

`internal/adapters/providers/chatcompletions` contains the neutral chat-completions transport shared by multiple named text providers.

## Business Role

This package keeps shared OpenAI-compatible request/response shaping in one place without making any one provider name look like the canonical runtime transport.

## Rules

- treat this package as a reusable transport family adapter, not as a provider identity
- keep provider-specific naming, defaults, and registration outside this package unless they are required to satisfy shared transport behavior
- keep the shared structured generation contract in `structuredgeneration` rather than re-owning prompt or envelope logic here
- request shaping must preserve canonical agent identity, routing hints, and transcript context without mutating persisted messages
- the adapter must preserve the application-owned structured generation contract, including optional sandbox task requests
- tests must use local HTTP servers and must not depend on live network access

## Current Responsibilities

- translating `application.GenerationRequest` into chat-completions HTTP requests
- extracting structured generation results, including optional sandbox requests, from provider responses
- attaching provider/model metadata to generated messages
- providing shared default-base-url behavior for named wrappers using this transport family

## Update Triggers

Update this file when:

- request or response shaping changes
- the shared structured-generation helper contract moves or changes
- shared auth or endpoint assumptions change
- operator-visible generation metadata changes
- failure handling or retry expectations change
- the set of named wrappers using this transport family changes
