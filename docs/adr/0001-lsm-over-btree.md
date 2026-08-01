# ADR-0001: Log-Structured Merge-Tree over B-Tree

**Status:** Accepted
**Date:** 2025-01

---

## Context

The project needed a storage engine optimised for write-heavy workloads. Two primary data structures were considered: B-Tree (used by most traditional databases) and Log-Structured Merge-Tree (used by LevelDB, RocksDB, Cassandra, and HBase).

B-trees update data in-place on disk. Every write requires a random I/O to find the target page, modify it, and write it back. For workloads with many small writes, this produces high write amplification from repeated random seeks, and page splits add further overhead.

LSM-Trees batch writes into sequential I/O. All writes go to the WAL first (sequential append), then accumulate in-memory in the MemTable (skip list), and are flushed to sorted SSTable files on disk sequentially. Compaction reorganises SSTables in the background, also sequentially.

---

## Decision

Implement a Log-Structured Merge-Tree with:

- WAL append + fsync for durability (`internal/wal/`)
- In-memory MemTable using a skip list (`internal/memtable/skiplist.go`, MaxLevel=12, P=0.25)
- Immutable MemTable queue → SSTable flush (`internal/engine/flush.go`)
- Multi-level SSTable hierarchy (L0–L6) (`internal/manifest/`)
- Pluggable compaction strategies (`internal/compaction/`)
- Per-SSTable Bloom filters to short-circuit read path misses (`internal/bloom/bloom.go`)
- LRU block cache to avoid repeated disk reads (`internal/cache/lru.go`)

---

## Consequences

**Positive:**

- Write throughput is bounded by sequential I/O speed, not random I/O. Design target: > 200K ops/s without fsync, > 20K ops/s with fsync on SSD.
- Sequential I/O is friendly to SSDs (reduces write amplification at the hardware level) and HDDs (avoids seek latency).
- Natural separation of concerns: WAL owns durability, MemTable owns in-memory ordering, SSTables own on-disk permanence, Compaction owns reclamation.
- The multi-level structure naturally supports TTL expiration (tombstone GC at the bottom level).

**Negative:**

- **Read amplification**: a key not in the MemTable may require checking every L0 SSTable plus one SSTable per level (up to 7 disk reads in the worst case). Mitigated by Bloom filters (short-circuit ~99% of missing-key checks at 10 bits/key) and the block cache (warm reads serve from DRAM).
- **Space amplification**: compaction input files occupy disk until they are replaced by output files. LCS space amplification is ~1.1×; STCS can reach ~4× during a merge.
- **Compaction complexity**: three strategies (LCS, STCS, TWCS) with different trade-offs add operational complexity. Mitigated by live switching via `/compaction/style`.
- **Write stalls**: if the compaction worker falls behind, writes stall at `Level0StopWritesTrigger` (default 12 L0 files). Mitigated by monitoring `lsm_engine_l0_sst_files` and the `LSML0FileCountCritical` alert.
