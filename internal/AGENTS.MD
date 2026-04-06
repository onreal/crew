# AGENTS.MD

## Purpose

`internal` contains all non-public application implementation.

## Business Role

This is the protected core of the product where business behavior, execution policy, infrastructure adapters, and runtime support are implemented behind controlled package boundaries.

## Boundary Rule

- dependency direction must flow inward toward domain and application logic
- packages under `internal` must not casually depend on sibling internals without a clear boundary reason
- do not export internals through `pkg` unless they are intentionally stabilized

## Update Triggers

Update this file when:

- internal package boundaries change
- dependency direction changes
- a new major subsystem is introduced
- provider-selection ownership moves between domain, application, runtime, or platform layers
