# Compaction Strategies

The compaction strategy is pluggable and can be changed at runtime via `POST /compaction/style`. Three strategies are implemented in `internal/compaction/`.

---

## Amplification comparison

| Strategy | Write amplification | Read amplification | Space amplification | Best for |
|----------|--------------------|--------------------|---------------------|----------|
| LCS (Leveled) | 10–30× | O(numLevels) ≈ 7 | ~1.1× | Mixed / read-heavy |
| STCS (Size-Tiered) | 4–10× | O(tiers); high | ~4× during merge | Write-heavy |
| TWCS (Time-Window) | Low (STCS-like) | Low (temporal) | Low | Time-series / append-only |

---

## Leveled Compaction Strategy (LCS) — default

Implementation: `internal/compaction/leveled.go`

LCS maintains strict per-level size limits and non-overlapping key ranges at L1 and above. L0 files may have overlapping key ranges (written directly from MemTable flushes).

### Level geometry

| Level | Max size | Key ranges | Source |
|-------|----------|------------|--------|
| L0 | 0–`Level0StopWritesTrigger` files | Overlapping | Direct flush |
| L1 | 10 MB | Non-overlapping | Compacted from L0 |
| L2 | 100 MB | Non-overlapping | Cascaded from L1 |
| L3 | 1 GB | Non-overlapping | Cascaded from L2 |
| L4 | 10 GB | Non-overlapping | Cascaded from L3 |
| L5 | 100 GB | Non-overlapping | Cascaded from L4 |
| L6 | 1 TB | Non-overlapping | Cascaded from L5 |

Size multiplier per level: `LevelSizeMultiplier` (default 10). Base level size derives from `SSTMaxSize` (default 64 MB).

### Trigger

- Compaction begins when the number of L0 files reaches `Level0FileNumCompactionTrigger` (default 4).
- Writes stall when L0 reaches `Level0StopWritesTrigger` (default 12).
- Cascading compaction runs when any Lk exceeds its size budget.

### Algorithm

```text
1. Pick one SSTable from the level most over its size budget
   (round-robin / oldest-first selection).
2. Find all overlapping SSTables in the level below.
3. k-way merge via min-heap (internal/compaction/iterator.go MergeIterator):
   - For each key: keep only the highest seqNo version.
   - Drop tombstones at the bottom level (no older versions exist below).
4. Write new SSTables at the lower level (respecting SSTMaxSize).
5. Fsync all output files.
6. Write EditAddSSTable + EditDeleteSSTable records to MANIFEST atomically.
7. Delete input SSTable files.
8. Repeat if the lower level now exceeds its budget.
```

---

## Size-Tiered Compaction Strategy (STCS)

Implementation: `internal/compaction/stcs.go`

STCS groups SSTables of similar size into tiers and merges them when a tier accumulates enough members.

### Tier grouping

```text
bucketLow  = 0.5   (files smaller than avg × 0.5 form a new bucket)
bucketHigh = 1.5   (files larger than avg × 1.5 form a new bucket)

Files within [avg × 0.5, avg × 1.5] form one tier.
Trigger: tier has >= minThreshold (4) members.
```

### Algorithm

```text
1. Group all SSTables by size bucket.
2. Find a bucket with >= minThreshold (default 4) members.
3. k-way merge all SSTables in the bucket → one larger output SSTable.
4. The output file may fall into the next larger bucket.
5. Update MANIFEST; delete input files.
```

!!! note "Space amplification"
    STCS can accumulate up to `~T×` space amplification during a tier merge, where T is the tier threshold (default 4). This is acceptable for write-heavy workloads that tolerate higher read costs.

---

## Time-Window Compaction Strategy (TWCS)

Implementation: `internal/compaction/twcs.go`

TWCS is designed for time-series data where writes are append-only and queries are time-bounded. SSTables within the same time window are compacted using STCS semantics. Closed windows are never re-compacted.

### Parameters

| Parameter | Config field | Default |
|-----------|-------------|---------|
| Window size | `TimeWindowSize` | `1h` |
| Window key | `SSTableMeta.CreatedAt` | Unix nanoseconds at flush time |

### Algorithm

```text
Active (current) window: apply STCS — merge SSTables of similar size.
Closed (past) windows:   no compaction — data is effectively immutable.

Cross-window merges never happen; this preserves temporal locality
and avoids mixing data from different time periods.
```

!!! tip "When to use TWCS"
    TWCS is optimal when: (1) all writes have monotonically increasing timestamps, (2) reads are time-range queries over recent data, and (3) old time windows can be dropped by TTL. It is a poor choice for random-key workloads where `CreatedAt` is meaningless.

---

## Compaction rollback

If the MANIFEST write fails after output SSTables have been written to disk, the engine rolls back:

1. Output SSTable files are **deleted** (they are not in the MANIFEST, so they are orphaned).
2. Input SSTable files are **preserved** (they remain in the MANIFEST at the old level).
3. On the next engine open, orphaned files discovered by the scan are deleted.

This ensures that a crash at any point during compaction leaves the engine in a valid, recoverable state with no data loss.

---

## Merge iterator

The k-way merge used during compaction (`internal/compaction/iterator.go`) is a min-heap over SSTable iterators. It guarantees:

- Output keys are in ascending `InternalKey` order (UserKey ASC, SeqNo DESC).
- Duplicate user keys are collapsed: only the version with the highest seqNo is emitted.
- Tombstones are propagated upward unless the output level is the bottom level (L6 by default), in which case tombstones are dropped — there are no older versions below them.
