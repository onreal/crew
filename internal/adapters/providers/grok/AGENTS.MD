# AGENTS.MD

## Purpose

`internal/adapters/providers/grok` contains the Grok named-provider wrapper for the shared chat-completions transport family.

## Business Role

This package gives Grok a first-class provider boundary in the codebase while reusing the neutral shared transport implementation.

## Rules

- keep Grok-specific identity in this package and keep shared HTTP shaping in `chatcompletions`
- do not reintroduce provider selection logic into application or domain code
- expose only the config needed to construct the Grok provider cleanly

## Current Responsibilities

- binding the provider identity `grok` to the shared chat-completions transport
- preserving Grok as an explicit runtime-registered provider name

## Update Triggers

Update this file when:

- Grok-specific defaults or registration behavior change
- Grok stops using the shared chat-completions transport family
