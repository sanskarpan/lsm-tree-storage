# Backup and Recovery

---

## Consistent snapshot (backup)

To take a consistent backup, quiesce the engine first by calling the admin snapshot endpoint. This flushes all in-memory MemTable data to SSTables before returning the list of files to copy.

```bash
curl -s -X POST http://localhost:8080/admin/snapshot \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{}'
```

Example response:

```json
{
  "ok": true,
  "data_dir": "./data",
  "snapshot_files": [
    "./data/MANIFEST",
    "./data/CURRENT",
    "./data/000001.sst",
    "./data/000002.sst"
  ],
  "message": "engine flushed; copy all snapshot_files to take a consistent backup"
}
```

The handler (`gateway/rest.go handleSnapshot`) requires the `roleAdmin` role. It:

1. Acquires a flush lock to prevent concurrent compaction during the snapshot window.
2. Flushes the active MemTable to an L0 SSTable.
3. Waits for the flush to complete.
4. Returns the list of live SSTable files plus MANIFEST and CURRENT.

Copy all returned files to your backup destination. The data directory structure is self-contained.

!!! warning "Never copy files without quiescing first"
    Copying SSTables without calling `/admin/snapshot` first may capture mid-compaction state — where some input SSTables have been deleted and output SSTables have not yet been committed to MANIFEST. The result is an unrecoverable backup. Always use `/admin/snapshot`.

### Script-based backup

The repository ships `scripts/backup.sh`:

```bash
./scripts/backup.sh ./data ./backups
# Creates: ./backups/lsm-backup-<timestamp>.tar.gz
#          ./backups/lsm-backup-<timestamp>.tar.gz.sha256
```

The archive includes `data/`, `metadata.json`, and `config.yaml` (if present).

---

## Recovery from crash

On `engine.Open()`, crash recovery runs automatically with no manual intervention:

```text
1. Read CURRENT file → find active MANIFEST filename.
2. Run manifest.Recover() → reconstruct Version (all SSTable metadata for all levels).
   - MaxSeqNo is restored before WAL replay begins.
3. Open WAL file(s) listed in MANIFEST (LogNumber field).
4. Replay WAL records into MemTable:
   a. Verify CRC32 of each record payload.
   b. Apply EntrySet → MemTable.Put(key, value, seqNo).
   c. Apply EntryDelete → MemTable.Delete(key, seqNo).
   d. Skip EntryFlush (already committed to MANIFEST).
   e. Silently skip the final truncated record (partial write before crash).
5. Validate that all SSTable files in MANIFEST exist on disk.
6. Scan for orphaned SSTable files not in MANIFEST → delete them.
   (Orphans are output files from an interrupted compaction or flush.)
7. Open SSTableReader for each live file; load index block and Bloom filter.
8. Start FlushWorker and Compactor background goroutines.
9. Engine is ready to serve requests.
```

**Correctness guarantees after crash recovery:**

- Every acknowledged write has a CRC-valid WAL record; it will be replayed.
- No acknowledged write is lost regardless of crash timing.
- Orphaned SSTables (from an interrupted flush or compaction) are removed.
- SSTable inventory matches MANIFEST exactly before the engine starts serving.

---

## Recovery from backup

```bash
# Restore into an empty target directory
./scripts/restore.sh ./backups/lsm-backup-<timestamp>.tar.gz ./restored-data

# Overwrite an existing data directory (--force required)
./scripts/restore.sh ./backups/lsm-backup-<timestamp>.tar.gz ./data --force
```

The restore script verifies the SHA-256 checksum before unpacking. After restore, start the engine normally — crash recovery will run as usual (no special flags needed).

---

## Recovery from corruption

### WAL CRC failure

If a WAL record has an invalid CRC (detected in `internal/wal/reader.go`):

- The record is **skipped** (treated as a partial write boundary).
- A warning is logged.
- Recovery continues from the next block boundary.
- All prior valid records are still replayed.

This means a single corrupt WAL record may cause the loss of the writes that touched that block, but all earlier acknowledged writes are preserved.

### MANIFEST CRC failure

If a CRC mismatch is detected in a new-format MANIFEST record (detected in `manifest.Recover()`):

- Recovery is **aborted** with an error.
- The engine does not start.
- The error is: `manifest: CRC mismatch on record ...`

This is intentional. A corrupt MANIFEST means the level inventory is unreliable. Recover from the most recent backup and re-apply any WAL that post-dates the backup.

!!! note "Legacy MANIFEST files"
    MANIFEST files written before the CRC format was introduced (no `MNIF` magic header) are read without CRC validation. A single corrupted record in a legacy manifest may be silently skipped. This is a known trade-off of the backward-compatibility path.
