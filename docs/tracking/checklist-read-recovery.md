# CHECKLIST Read and Recovery Track

This document groups the mid-stack work that makes reads fast and recovery trustworthy.

## Scope

- [#10](https://github.com/sanskarpan/lsm-tree-storage/issues/10) `CHECKLIST Phase 8 - Read Path + Range Scan (12 tasks)`
- [#11](https://github.com/sanskarpan/lsm-tree-storage/issues/11) `CHECKLIST Phase 9 - Crash Recovery (10 tasks)`
- [#12](https://github.com/sanskarpan/lsm-tree-storage/issues/12) `CHECKLIST Phase 10 - Compaction: Leveled (LCS) (20 tasks)`
- [#13](https://github.com/sanskarpan/lsm-tree-storage/issues/13) `CHECKLIST Phase 11 - Compaction: STCS + TWCS (8 tasks)`

## Delivery Goals

- Keep point lookups and scans stable under concurrency and file growth.
- Make recovery deterministic across process crashes and partial writes.
- Ensure leveled and time/window compaction do not reintroduce ordering bugs.
- Preserve read correctness while compaction rewrites on-disk state.

## Acceptance Criteria

- Snapshot reads return consistent results while writes continue.
- Recovery can replay multiple durability sources in a well-defined order.
- Compaction cannot surface unreadable or partially published SSTables.
- Range scans remain correct after compaction and restart.

## Validation Expectations

- Concurrent read/write regression tests.
- Crash-recovery boot tests with multiple WAL and SSTable files.
- Compaction tests for leveled and tiered/time-window behaviors.

## Notes

- This track is where latent durability bugs usually become user-visible corruption.
- The acceptance bar here is correctness first, then throughput.
