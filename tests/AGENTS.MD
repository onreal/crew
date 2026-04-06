# AGENTS.MD

## Purpose

`tests` contains higher-level integration, simulation, replay, and golden test assets that validate system behavior across package boundaries.

## Business Role

This directory proves the system is safe to operate, reproducible where required, and resilient against the known risks of multi-agent runtimes.

## Rules

- prioritize end-to-end behaviors that matter to operators
- validate both free-mode flexibility and sequential-mode determinism
- include scenarios for loop prevention, persistence, replay, and recovery

## Update Triggers

Update this file when:

- test strategy changes
- new cross-cutting risks are introduced
- new runtime guarantees must be validated
