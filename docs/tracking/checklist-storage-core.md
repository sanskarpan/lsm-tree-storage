# CHECKLIST Storage Core Track

This document covers the core on-disk storage layer after bootstrap is stable.

## Scope

- [#6](https://github.com/sanskarpan/lsm-tree-storage/issues/6) `CHECKLIST Phase 4 - SSTable: Builder + Reader (22 tasks)`
- [#7](https://github.com/sanskarpan/lsm-tree-storage/issues/7) `CHECKLIST Phase 5 - Block Cache (6 tasks)`
- [#8](https://github.com/sanskarpan/lsm-tree-storage/issues/8) `CHECKLIST Phase 6 - MANIFEST File (10 tasks)`
- [#9](https://github.com/sanskarpan/lsm-tree-storage/issues/9) `CHECKLIST Phase 7 - Flush Worker (8 tasks)`

## Delivery Goals

- Make SSTable creation and lookup deterministic and recoverable.
- Keep the block cache a pure acceleration layer with no correctness dependency.
- Persist enough manifest metadata to reconstruct table state after a crash.
- Ensure flush scheduling never exposes partially written immutable state.

## Acceptance Criteria

- SSTables can be written, reopened, and read back after process restart.
- Manifest replay reconstructs the latest committed storage state.
- Flush worker behavior is compatible with crash recovery and compaction.
- Cache misses never break point reads or range scans.

## Validation Expectations

- Reader/writer round-trip tests for table data.
- Manifest truncation and recovery tests.
- Flush and compaction regressions with repeated restarts.

## Notes

- This track is the bridge between in-memory correctness and durable storage layout.
- Failures here usually show up as missing rows, stale reads, or unreadable tables after restart.
