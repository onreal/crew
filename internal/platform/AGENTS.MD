# AGENTS.MD

## Purpose

`internal/platform` contains technical utilities that support the runtime but do not define business behavior.

## Business Role

This directory improves consistency and operational quality across the system through shared technical services.

## Allowed Here

- logging helpers
- configuration helpers
- clock and ID generation utilities
- concurrency and lifecycle helpers
- process bootstrap helpers

## Not Allowed Here

- domain decisions
- feature-specific workflow behavior
- provider-specific business logic

## Current Bootstrap Responsibilities

- load configuration from YAML and `CREW_*` environment variables
- expose the default installed config path convention used by the wrapper so command-layer sync flows can target the same location deterministically
- validate bootstrap-safe defaults for app, session, storage, and UI settings
- validate session orchestration-mode defaults for free-mode agent selection
- validate session reply-routing defaults for free-mode reply target resolution without embedding reply-routing policy logic in platform code
- validate UI defaults for attached-room auto-turn depth without embedding room behavior in platform code
- validate full-screen attach presentation defaults such as theme, timestamps, compactness, sidebar visibility, and override color maps without embedding TUI behavior in platform code
- validate full-screen attach split-pane defaults without embedding multi-conversation room behavior in platform code
- validate optional named text-provider configuration defaults without forcing one process-global text provider
- expose per-provider connection settings such as base URL, timeout, temperature, API key indirection, binary path, and working directory through `providers.<name>` without teaching platform code provider business logic
- allow `providers.codex.timeout_millis: 0` and `sandbox.providers.codex.timeout_millis: 0` as explicit "no timeout" settings while still requiring positive timeouts for the HTTP-backed providers and non-Codex sandbox runtimes
- validate sandbox runtime configuration defaults while keeping sandbox execution disabled by default until adapters are wired
- validate vector bootstrap defaults for local-stub embedding and optional vector activation
- provide the configured SQLite storage path for the live CLI runtime
- expose text-provider configuration as a reusable provider catalog and allow runtime assembly to resolve API keys from either direct config or environment
- expose sandbox default-provider selection plus named sandbox runtime configuration under `sandbox.providers.<name>` without embedding sandbox execution policy in platform code; the default is for manual task creation and compatibility, not a replacement for agent-owned delegation runtime state
- keep configuration generic enough for multiple sandbox runtimes while leaving per-agent delegation policy in domain/application code
- preserve `runtime.state_path` as deprecated compatibility config during the transition away from the JSON bridge
- provide logger construction
- provide signal-aware root context creation
- treat both `SIGINT` and `SIGTERM` as graceful shutdown signals

## Update Triggers

Update this file when:

- shared runtime utilities are added or changed
- configuration or logging standards change
- lifecycle conventions change
- config schema or env var conventions change
- the storage path or deprecated runtime state path behavior changes
- provider catalog defaults or provider-config ownership changes
