# TODO Atomicity and Format Track

This document covers the issues that affected durability semantics and on-disk compatibility.

## Scope

- [#34](https://github.com/sanskarpan/lsm-tree-storage/issues/34) `todo.md item 12 - Write batches were not truly atomic`
- [#35](https://github.com/sanskarpan/lsm-tree-storage/issues/35) `todo.md item 13 - SSTable format dropped sequence numbers on disk`
- [#36](https://github.com/sanskarpan/lsm-tree-storage/issues/36) `todo.md item 14 - Several REST endpoints were stubs or misleading placeholders`
- [#37](https://github.com/sanskarpan/lsm-tree-storage/issues/37) `todo.md item 15 - Manifest replay could mis-handle truncated reads`

## Delivery Goals

- Make write batching atomic in both the durability log and replay path.
- Persist enough metadata in SSTables to preserve ordering and snapshot semantics.
- Replace stubs with explicit endpoint behavior.
- Harden manifest parsing against partial or truncated storage reads.

## Acceptance Criteria

- Partial batch failure cannot leave the store in a half-applied state.
- Sequence and visibility ordering survives flush and compaction.
- Placeholder endpoints return real semantics or explicit rejections.
- Truncated manifest reads fail safely instead of guessing.

## Validation Expectations

- Batch atomicity regression tests.
- SSTable format compatibility and recovery tests.
- Endpoint behavior tests for the previously stubbed surface.
- Manifest truncation and recovery coverage.

## Notes

- These issues sit on the boundary between correctness and long-term maintainability.
- Format changes should always be paired with compatibility and recovery validation.
