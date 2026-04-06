# AGENTS.MD

## Purpose

`internal/adapters/storage` contains persistence adapters and related storage-facing support code.

## Business Role

This directory is where durable runtime state becomes real operator value through restart safety, auditability, replay support, and recoverable local-first persistence.

## Rules

- canonical persisted state must remain authoritative
- storage adapters must preserve transactional boundaries required by the application layer
- schema changes must ship with explicit migrations
- derived indexes such as vector search must remain rebuildable from canonical rows
- each storage backend or migration directory must keep its own `AGENTS.MD`
- optional derived adapters must surface their own degraded and rebuild status without weakening canonical durability

## Current Responsibilities

- SQLite persistence adapters for canonical runtime state
- canonical persistence for persisted sandbox tasks and agent handoffs
- migration execution and schema version tracking
- optional derived storage sidecars such as the separate `sqlitevec` vector adapter family
- rebuildable derived embedding ownership and vector-state observability in the `sqlitevec` adapter family

## Update Triggers

Update this file when:

- a new storage backend is introduced
- migration strategy changes
- canonical versus derived persistence responsibilities change
