# AGENTS.MD

## Purpose

`internal/adapters/providers/openai` contains the named OpenAI provider wrapper for the shared chat-completions transport family.

## Business Role

This package keeps OpenAI as a first-class provider identity in the codebase without making OpenAI the generic transport name for other providers.

## Rules

- keep OpenAI-specific identity in this package and keep shared HTTP shaping in `chatcompletions`
- do not reintroduce provider selection logic into application or domain code
- expose only the config needed to construct the OpenAI provider cleanly

## Current Responsibilities

- binding the provider identity `openai` to the shared chat-completions transport
- preserving OpenAI as an explicit runtime-registered provider name

## Update Triggers

Update this file when:

- OpenAI-specific defaults or registration behavior change
- OpenAI stops using the shared chat-completions transport family
