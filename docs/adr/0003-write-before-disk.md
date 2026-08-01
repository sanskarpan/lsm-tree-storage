# ADR-0003: Write-Before-Disk Atomicity in MANIFEST

**Status:** Accepted
**Date:** 2025-03

---

## Context

The original `Manifest.Apply()` implementation updated the in-memory `Version` (the live SSTable level inventory) **before** writing the corresponding `VersionEdit` record to disk.

The sequence was:

```text
Old (buggy) order:
  1. Update m.current in memory  ← applied first
  2. Write VersionEdit to disk
  3. Flush + Sync
```

If the process crashed between step 1 and step 3, the in-memory state had been updated but the disk record was not written. On restart, `Manifest.Recover()` would replay only the records that made it to disk. The in-memory state from the aborted `Apply` was gone, but so was the VersionEdit — resulting in a ghost SSTable entry in memory that did not exist on disk.

Concretely: a new SSTable could appear in `m.current.Levels` after a flush, but after a crash that SSTable entry would vanish from the recovered manifest, making the SSTable's data invisible even though the file was still on disk.

---

## Decision

Invert the order in `Manifest.Apply()` so the disk write always precedes the in-memory update:

```text
New (correct) order:
  1. Validate (L1+ non-overlap check)
  2. Write VersionEdit to disk (writeEdit → Flush → Sync)  ← happens first
  3. Update m.current in memory                             ← only on disk success
```

If the disk write fails, the in-memory state is left unchanged and the error is returned to the caller. The caller (engine flush/compaction) sees the error and can retry or propagate it as a background error.

If the process crashes between step 2 and step 3, the VersionEdit is on disk but the in-memory state is not yet updated. On restart, `Recover()` replays the record and applies it — the in-memory state is reconstructed correctly.

This is the standard "write-ahead" pattern applied to the MANIFEST itself.

---

## Consequences

**Positive:**

- No ghost entries: the in-memory state can only contain SSTables that have a corresponding on-disk VersionEdit.
- Crash-safe at any point: recovery always produces a state consistent with the on-disk MANIFEST.
- Simplifies the correctness argument: the MANIFEST is the source of truth, and it is always written before memory.

**Negative:**

- Slightly higher latency per `Apply` call: each call must fsync before returning (rather than deferring the sync). This is acceptable — `Apply` is called at most once per flush or compaction, not per individual write.
- The in-memory state is momentarily stale between the disk write and the in-memory update. This window is sub-microsecond and the `Apply` method holds the manifest mutex throughout, so no concurrent reader can observe the inconsistency.
