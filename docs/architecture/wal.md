# WAL Deep Dive

The Write-Ahead Log (`internal/wal/wal.go`) uses the same 32 KB block format as LevelDB and RocksDB. Every write is CRC32-protected and optionally fsynced before the engine acknowledges success.

---

## 32 KB block format

Each WAL file is a sequence of fixed-size 32 KB blocks. Records that are larger than the remaining space in a block are fragmented across blocks.

```text
WAL file on disk
┌──────────────────────┬──────────────────────┬──────────────────────┐
│  Block 0 (32768 B)   │  Block 1 (32768 B)   │  Block 2 ...         │
└──────────────────────┴──────────────────────┴──────────────────────┘

Each 32 KB block — record layout:

 Offset  Size   Field         Notes
 ──────  ────   ──────────    ─────────────────────────────────────────
  0       4     CRC32         CRC of the Payload chunk only (not header)
  4       2     Length        LE uint16; byte length of the Payload chunk
  6       1     Type          Record type (see table below)
  7       N     Payload       N = Length bytes

 HeaderSize = 7 bytes  (defined in internal/wal/wal.go)
 BlockSize  = 32768 bytes

Block padding: when fewer than 7 bytes remain before the 32 KB boundary,
the remainder is zero-padded; a new record starts in the next block.
```

### Record types

| Constant | Value | Meaning |
|----------|-------|---------|
| `RecordFull` | 1 | Record fits entirely within one block |
| `RecordFirst` | 2 | First fragment of a multi-block record |
| `RecordMiddle` | 3 | Interior fragment |
| `RecordLast` | 4 | Final fragment |

---

## WAL entry wire format (payload)

After reassembling fragments, the payload decodes as:

```text
 Offset   Size    Field        Encoding
 ──────   ────    ─────────    ──────────────────────────────────
  0        1      EntryType    uint8
  1        4      KeyLen       LE uint32
  5       KeyLen  Key          raw bytes
  5+KL     4      ValLen       LE uint32
  9+KL    ValLen  Value        raw bytes (empty for DELETE)
  9+KL+VL  8      SeqNo        LE uint64 (monotonically increasing)
```

### Entry types

| Constant | Value | Meaning |
|----------|-------|---------|
| `EntrySet` | 1 | Key + value stored |
| `EntryDelete` | 2 | Tombstone; ValLen=0, Value empty |
| `EntryFlush` | 3 | Marks a successful MemTable flush |

The CRC32 in the record header covers **only** the payload chunk (the fragment bytes), not the 7-byte header itself. This matches the LevelDB convention.

---

## File naming

WAL files are named `wal-<logNumber>.log` and placed in `DataDir` (or `WalDir` if overridden). The active log number is tracked in the MANIFEST (`EditLogNumber`).

---

## Recovery algorithm

On `engine.Open()`, `internal/wal/reader.go` performs:

```text
1. Open the WAL file listed in MANIFEST (LogNumber field).
2. For each 32 KB block:
   a. Read record header (7 bytes).
   b. Verify CRC32 of the payload chunk.
   c. If CRC mismatch: record is corrupt; skip rest of block (crash boundary).
   d. Reassemble fragments (First+Middle*+Last) into a complete entry.
3. Decode WALEntry from reassembled payload.
4. Apply to MemTable in seqNo order:
   - EntrySet    → MemTable.Put(key, value, seqNo)
   - EntryDelete → MemTable.Delete(key, seqNo)
   - EntryFlush  → ignored (flush already committed to MANIFEST)
5. Highest seqNo seen restores the WAL writer's internal counter.
```

The last record in a WAL file may be truncated (partial write before crash). This is expected — the reader silently skips it and logs a warning. All earlier records with valid CRC are safe to replay.

seqNo is restored from `MANIFEST.MaxSeqNo` before WAL replay begins (see [ADR-0004](../adr/0004-seqno-persistence.md)), so WAL replay only needs to advance `seqNo` beyond what the MANIFEST already knows.

---

## `WAL.Close()` and fsync behaviour

```go
func (w *WAL) Close() error {
    w.mu.Lock()
    defer w.mu.Unlock()
    if err := w.buf.Flush(); err != nil { return err }
    return w.file.Sync() // fsync before close — critical for durability
}
```

`WAL.Close()` always fsyncs, regardless of the `SyncWAL` setting. The per-write `Sync()` is gated by `SyncWAL`.

---

## Circuit-breaker: `bgError`

When the FlushWorker or Compactor encounters an unrecoverable error (e.g., disk full, I/O error during SSTable write), it sets `engine.bgError`. Subsequent calls to `WAL.Append` check this field and return the stored error immediately, preventing new writes from entering an inconsistent log.

---

!!! warning "WAL deletion"
    The WAL for a successfully flushed MemTable is deleted **after** the flush MANIFEST record (`EditLogNumber`) is written and fsynced. If the engine crashes between the MANIFEST update and the WAL deletion, the WAL will be replayed again on restart — this is safe because `EntryFlush` markers are idempotent.

    The MANIFEST `MaxSeqNo` (`EditMaxSeqNo`) must be written on every flush. Without it, after a clean flush + WAL deletion + restart, the engine cannot distinguish live SSTable data from data at seqNo=0, causing all rows to appear invisible.
