# CHECKLIST Bootstrap Track

This document tracks the foundational bootstrap work and the first storage primitives.

## Scope

- [#1](https://github.com/sanskarpan/lsm-tree-storage/issues/1) `CHECKLIST.MD master index and phase tracker`
- [#2](https://github.com/sanskarpan/lsm-tree-storage/issues/2) `CHECKLIST Phase 0 - Bootstrap (10 tasks)`
- [#3](https://github.com/sanskarpan/lsm-tree-storage/issues/3) `CHECKLIST Phase 1 - WAL: Write-Ahead Log (20 tasks)`
- [#4](https://github.com/sanskarpan/lsm-tree-storage/issues/4) `CHECKLIST Phase 2 - MemTable: Skip List (18 tasks)`
- [#5](https://github.com/sanskarpan/lsm-tree-storage/issues/5) `CHECKLIST Phase 3 - Bloom Filter (12 tasks)`

## Delivery Goals

- Establish the project bootstrap path with deterministic local startup.
- Ensure WAL durability, recovery ordering, and flush safety.
- Keep memtable mutation semantics isolated from caller-owned buffers.
- Preserve bloom-filter behavior as a read-path accelerator rather than a correctness dependency.

## Acceptance Criteria

- Startup is repeatable from a clean checkout.
- Write-path durability is explicit and covered by regression tests.
- Concurrent reads and writes do not corrupt state.
- WAL and memtable behavior remain stable under crash/recovery scenarios.

## Validation Expectations

- `go test ./...`
- `go test ./... -race`
- Recovery tests that replay multiple WAL files and truncated records.
- Regression coverage for bloom filter false-positive handling.

## Notes

- This track is intentionally narrow: it covers the engine bootstrap layer and the minimum primitives required for the rest of the storage stack.
- Later tracks extend this foundation into SSTables, compaction, APIs, and the dashboard.
