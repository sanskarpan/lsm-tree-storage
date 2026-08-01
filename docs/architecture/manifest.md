# MANIFEST Deep Dive

The MANIFEST (`internal/manifest/manifest.go`) is an append-only log of `VersionEdit` records. It is the single source of truth for which SSTable files exist at which levels, the active WAL log number, the next file ID to assign, and the maximum sequence number seen.

---

## VersionEdit types

Defined in `internal/manifest/version_edit.go`:

| Constant | Value | Meaning |
|----------|-------|---------|
| `EditAddSSTable` | 1 | Add a new SSTable to a level |
| `EditDeleteSSTable` | 2 | Remove an SSTable from a level |
| `EditLogNumber` | 3 | Record the current WAL file number |
| `EditNextFileID` | 4 | Record the next file ID to be assigned |
| `EditMaxSeqNo` | 5 | Record the maximum sequence number seen (flush persistence) |

---

## Record format

New MANIFEST files (those created after the magic header was introduced) use CRC-protected records. Legacy files use the old length-prefixed format and are still accepted during recovery.

### New format (CRC-protected)

```text
 Field     Size    Encoding        Notes
 ───────   ────    ────────────    ──────────────────────────────────────────
 CRC32     4 B     LE uint32       CRC of body bytes only
 Length    4 B     LE uint32       byte length of body
 Body      N B     binary          encoded VersionEdit fields
```

### Legacy format (no CRC)

```text
 Field     Size    Encoding        Notes
 ───────   ────    ────────────    ──────────────────────────────────────────
 Length    4 B     LE uint32       byte length of body
 Body      N B     binary          encoded VersionEdit fields
```

---

## Magic header

New MANIFEST files begin with a 4-byte magic header:

```text
  0x4D 0x4E 0x49 0x46  →  "MNIF" (ASCII)
```

The value `0x464E494D` in little-endian equals approximately 1.1 GiB as a record length, which is implausibly large under the old format. The reader uses this to unambiguously detect which format is in use: if the first 4 bytes match `manifestMagic`, it is a new-format file with CRC; otherwise it is a legacy file without CRC.

---

## Write-before-disk atomicity

`Manifest.Apply` always writes the VersionEdit to disk **before** updating the in-memory `Version`:

```go
// manifest.go — Apply()
// 1. Validate (L1+ non-overlap check for EditAddSSTable)
// 2. Persist to disk (writeEdit → Flush → Sync)  ← happens first
// 3. Update m.current in memory                   ← only if disk write succeeded
```

If the disk write fails, the in-memory state is not modified and the error is returned. There are no "ghost" entries — entries that exist in memory but not on disk. This is the invariant documented in [ADR-0003](../adr/0003-write-before-disk.md).

---

## seqNo persistence

`EditMaxSeqNo` (type 5) is written after every successful MemTable flush:

```text
Write path on flush completion:
  1. SSTableBuilder.Finish() → file synced
  2. Manifest.Apply(EditAddSSTable{...})
  3. Manifest.Apply(EditMaxSeqNo{MaxSeqNo: currentSeqNo})
  4. Manifest.Apply(EditLogNumber{...})   ← if WAL rotated
  5. Old WAL file deleted
```

On recovery, `Recover()` reads `MaxSeqNo` from the MANIFEST and restores it before WAL replay. Without this, after a clean flush (WAL deleted, no unplayed WAL entries), all SSTable data would appear invisible because `SSTableReader.Get` gates visibility on `SeqNo <= readSeqNo` and `readSeqNo` would start at 0.

See [ADR-0004](../adr/0004-seqno-persistence.md) for the full incident analysis.

---

## Recovery

`manifest.Recover(path)` returns `(*Version, usesCRC bool, error)`:

```text
1. Read first 4 bytes → detect format (magic vs legacy length).
2. Iterate all records:
   a. New format: read CRC(4) + length(4) + body → verify CRC; abort if mismatch.
   b. Legacy format: read length(4) + body → no CRC check (backward compat).
3. Apply each VersionEdit to a local Version struct.
4. Return reconstructed Version (all levels, LogNumber, NextFileID, MaxSeqNo).
```

!!! warning "CRC failure during recovery"
    If a CRC mismatch is detected in a new-format MANIFEST, `Recover` returns an error and recovery is **aborted**. The engine will not start. This prevents operating from a corrupt level manifest. Restore from backup and replay WAL if needed.

!!! note "Backward compatibility"
    MANIFEST files that predate the magic header (no CRC) are silently accepted with no CRC validation. The format is detected at open time; `SetFormat(usesCRC)` then configures `Apply` to continue writing in the same format.

---

## In-memory Version

```go
// manifest.go
type Version struct {
    Levels     [7][]*sstable.SSTableMeta  // L0..L6
    LogNumber  uint64                     // active WAL file number
    NextFileID uint64                     // next file ID to allocate
    MaxSeqNo   uint64                     // highest seqNo flushed to disk
}
```

`Version` is immutable after creation; `Apply` allocates a new version on each edit. Concurrent readers hold a pointer to the current version snapshot and are unaffected by concurrent compaction.
