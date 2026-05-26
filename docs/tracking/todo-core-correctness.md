# TODO Core Correctness Track

This document covers the first set of audit findings that affected basic engine correctness.

## Scope

- [#22](https://github.com/sanskarpan/lsm-tree-storage/issues/22) `todo.md master index and audit backlog tracker`
- [#23](https://github.com/sanskarpan/lsm-tree-storage/issues/23) `todo.md item 1 - WAL rotation could lose live writes during flush`
- [#24](https://github.com/sanskarpan/lsm-tree-storage/issues/24) `todo.md item 2 - Recovery only replayed a single WAL file`
- [#25](https://github.com/sanskarpan/lsm-tree-storage/issues/25) `todo.md item 3 - Manifest could reference SSTables before readers existed`
- [#26](https://github.com/sanskarpan/lsm-tree-storage/issues/26) `todo.md item 4 - Scan was not snapshot-safe under concurrent writes`
- [#27](https://github.com/sanskarpan/lsm-tree-storage/issues/27) `todo.md item 5 - MaxImmutableMemTables was ignored`

## Delivery Goals

- Eliminate data-loss windows in WAL/flush coordination.
- Make recovery replay every durability source in a deterministic order.
- Keep snapshot reads isolated from concurrent writes.
- Enforce in-memory rotation limits so flush pressure stays bounded.

## Acceptance Criteria

- A crash during flush does not drop acknowledged writes.
- Recovery replays the full WAL set and reconstructs a canonical active log state.
- Scan semantics stay stable while writes continue.
- Immutable memtable limits are enforced in code and in tests.

## Validation Expectations

- WAL replay regression coverage.
- Concurrent scan/write regression coverage.
- Rotation and flush behavior under repeated restart cycles.

## Notes

- This track is the first correctness barrier after bootstrap.
- If these issues regress, later compaction and HA layers inherit the failure.
