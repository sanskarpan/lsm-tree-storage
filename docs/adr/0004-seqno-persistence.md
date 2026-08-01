# ADR-0004: Persist MaxSeqNo to MANIFEST on Every Flush

**Status:** Accepted
**Date:** 2025-03

---

## Context

The global sequence number (`seqNo`) is used everywhere in the engine to implement snapshot isolation. Every `WALEntry` carries a `SeqNo`. Every `InternalKey` stores a `SeqNo`. `SSTableReader.Get` uses the condition `SeqNo <= readSeqNo` to gate visibility: versions newer than the read snapshot are invisible.

Before this fix, `seqNo` was tracked only in memory — specifically in the `WAL` struct's `atomic.Uint64`:

```go
// wal/wal.go
seqNo atomic.Uint64
```

On startup, the engine replays the WAL to restore `seqNo` to the highest value seen during replay. This works correctly when a WAL file is present.

However, after a **clean flush**:

1. MemTable is flushed to L0 SSTable.
2. `EditLogNumber` VersionEdit is written to MANIFEST (WAL rotation).
3. The old WAL file is deleted (it is no longer needed — all its entries are in the SSTable).
4. Process restarts.

On restart:
- There is no WAL to replay (it was deleted after the flush).
- `seqNo` resets to 0.
- `engine.Get` uses `readSeqNo = 0`.
- `SSTableReader.Get` requires `SeqNo <= 0`.
- All SSTable entries have `SeqNo >= 1` (they were written before the flush).
- All reads return `not found` — the entire SSTable data is invisible.

This was a silent data-loss bug: the data was on disk, but the engine could not see it.

---

## Decision

Introduce `EditMaxSeqNo` (type 5 in `internal/manifest/version_edit.go`) as a new `VersionEdit` type. After every successful MemTable flush, write `EditMaxSeqNo{MaxSeqNo: currentSeqNo}` to the MANIFEST before deleting the WAL:

```text
Flush completion sequence (correct):
  1. SSTableBuilder.Finish() — fsync output SSTable
  2. Manifest.Apply(EditAddSSTable{...})
  3. Manifest.Apply(EditMaxSeqNo{MaxSeqNo: w.seqNo.Load()})  ← new
  4. Manifest.Apply(EditLogNumber{...})  — if WAL rotated
  5. Delete old WAL file
```

On recovery, `manifest.Recover()` accumulates the `MaxSeqNo` field from all `EditMaxSeqNo` records:

```go
case EditMaxSeqNo:
    if edit.MaxSeqNo > version.MaxSeqNo {
        version.MaxSeqNo = edit.MaxSeqNo
    }
```

The engine restores `seqNo` from `version.MaxSeqNo` before starting WAL replay. WAL replay then advances `seqNo` further if the WAL contains entries beyond what was flushed.

---

## Consequences

**Positive:**

- Data written before a flush is visible after a clean flush + WAL deletion + restart. The bug is eliminated.
- No special-case handling needed for the "no WAL" boot path — `MaxSeqNo` from the MANIFEST covers it.
- The fix is backward-compatible: manifests without `EditMaxSeqNo` records are read as `MaxSeqNo=0`, which preserves the previous behaviour for engines that have never performed a clean flush.

**Negative:**

- One additional MANIFEST write per flush (`EditMaxSeqNo`). This is negligible — flushes already write at least two VersionEdits (`EditAddSSTable` + `EditLogNumber`) and each is individually fsynced.
- The `MaxSeqNo` value in the MANIFEST slightly exceeds the highest seqNo that made it into an SSTable (it reflects the WAL's counter at flush time, not the last entry written). This is safe: the next startup will use this as the starting point for new writes, which is correct.
