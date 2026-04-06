# AGENTS.MD

## Purpose

`pkg` is reserved for packages that are intentionally public and stable for external reuse.

## Business Role

This directory protects the team from accidentally publishing unstable internal implementation details.

## Rule

Do not add code here unless:

- there is a clear external consumer
- the API can be supported as a stable contract
- the package is documented and versioned with care

## Update Triggers

Update this file when:

- a public package is introduced
- stability promises change
- external API guarantees change
