# AGENTS.MD

## Purpose

`internal/adapters/providers/gemini` contains the Gemini named-provider wrapper for the shared chat-completions transport family.

## Business Role

This package gives Gemini a first-class provider boundary in the codebase while reusing the neutral shared transport implementation.

## Rules

- keep Gemini-specific identity in this package and keep shared HTTP shaping in `chatcompletions`
- do not reintroduce provider selection logic into application or domain code
- expose only the config needed to construct the Gemini provider cleanly

## Current Responsibilities

- binding the provider identity `gemini` to the shared chat-completions transport
- preserving Gemini as an explicit runtime-registered provider name

## Update Triggers

Update this file when:

- Gemini-specific defaults or registration behavior change
- Gemini stops using the shared chat-completions transport family
