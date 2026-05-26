# CHECKLIST API and Gateway Track

This document tracks the service boundary where the engine becomes observable and callable.

## Scope

- [#14](https://github.com/sanskarpan/lsm-tree-storage/issues/14) `CHECKLIST Phase 12 - Engine API + Gateway (14 tasks)`
- [#15](https://github.com/sanskarpan/lsm-tree-storage/issues/15) `CHECKLIST Phase 13 - Simulation & Benchmark Engine (10 tasks)`
- [#16](https://github.com/sanskarpan/lsm-tree-storage/issues/16) `CHECKLIST Phase 14 - Elysia BFF + TypeScript Types (6 tasks)`

## Delivery Goals

- Expose a stable engine API surface for reads, writes, scans, and diagnostics.
- Keep the gateway thin and explicit about request/response contracts.
- Preserve simulation and benchmark tools as first-class validation assets.
- Make the BFF and type layer reflect the actual server behavior instead of drift.

## Acceptance Criteria

- API endpoints map cleanly to engine capabilities.
- Gateway behavior is deterministic under normal and failure scenarios.
- Benchmark and simulation paths validate the same semantics as the production API.
- BFF typing and runtime behavior stay in sync.

## Validation Expectations

- Endpoint smoke tests for write, read, scan, and diagnostic paths.
- Simulation and benchmark runs against the live service.
- TypeScript build checks for the BFF surface.

## Notes

- This track is the contract boundary between the storage engine and external callers.
- Errors here tend to show up as broken integrations rather than internal correctness bugs.
