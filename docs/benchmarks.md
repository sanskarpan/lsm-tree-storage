# Performance Characteristics

---

## Test environment

Numbers below were observed on Apple M-series hardware, local NVMe SSD, Go 1.26.3 with the race detector **disabled**. Results will vary significantly by hardware, OS page cache state, and workload parameters.

!!! note
    These numbers are indicative. Always benchmark under your own workload before capacity planning. Use `make bench` to measure on your hardware.

---

## Write throughput

| Mode | Throughput | Config |
|------|-----------|--------|
| Sequential, no fsync | ~100K ops/s | `SyncWAL=false`, `MemTableSize=64MB` |
| Random, no fsync | ~70K ops/s | `SyncWAL=false`, `MemTableSize=64MB` |
| Sequential, with fsync | ~20K ops/s | `SyncWAL=true` (default) |

Write throughput is dominated by WAL fsync latency when `SyncWAL=true`. On NVMe, fsync typically costs 50–200 µs; the engine can issue one fsync per write, so throughput is bounded by `1 / fsync_latency`.

---

## Read latency

| Scenario | P50 | P99 |
|----------|-----|-----|
| Block cache hit | ~50 µs | ~200 µs |
| L1+ SSTable read (cache miss) | ~200 µs | ~500 µs |
| Key not found (Bloom short-circuit) | ~10 µs | ~50 µs |

The dominant factor for cache-miss reads is the disk read latency for the data block. Bloom filters eliminate unnecessary disk reads for non-existent keys at a rate of ~99% (at `BloomBitsPerKey=10`).

---

## Compaction

| Strategy | Write amplification observed |
|----------|------------------------------|
| Leveled (LCS) | ~3× over a sustained sequential write run |
| Size-Tiered (STCS) | ~1.5× over the same run |

LCS write amplification increases with database size and grows roughly as `O(numLevels)`. STCS write amplification is lower but space amplification is higher (~4× during a tier merge).

---

## Bloom filter

| Bits per key | False positive rate (empirical) |
|--------------|--------------------------------|
| 6 | ~8% |
| 10 (default) | ~1% |
| 14 | ~0.3% |

Empirical rates match the theoretical formula `p ≈ (1 - e^(-kn/m))^k` closely for n > 1000 keys.

---

## Block cache hit rate

| Workload | Hit rate |
|----------|---------|
| Zipf (hot-key, skew=1.0) | > 90% |
| Uniform random | > 70% |

Hot-key distributions benefit strongly from LRU caching. For uniform random key distributions, the hit rate depends on the ratio of `BlockCacheSize` to the working set size.

---

## Running benchmarks

```bash
# All benchmarks with memory allocation stats
make bench

# Specific package
go test ./internal/engine/... -bench=. -benchmem -run=^$
go test ./internal/wal/... -bench=. -benchmem -run=^$
go test ./internal/sstable/... -bench=. -benchmem -run=^$
go test ./internal/compaction/... -bench=. -benchmem -run=^$

# With race detector (slower; catches concurrency bugs)
make test-race

# Fuzz WAL entry round-trip
go test ./internal/wal/... -fuzz=FuzzWALEntryRoundtrip -fuzztime=30s
```

---

## Design targets

These are the published design targets from `README.md` and `SPEC.md`:

| Metric | Target |
|--------|--------|
| Write throughput (sequential, no fsync) | > 200K ops/s |
| Write throughput (with fsync, SSD) | > 20K ops/s |
| Point read — key in L1, warm cache | < 100 µs |
| Point read — key not found (Bloom) | < 50 µs |
| Bloom FP rate at `BloomBitsPerKey=10` | < 1% |
| MemTable flush (64 MB) | < 200 ms |
| L0 → L1 compaction (4 L0 files) | < 2 s |
| Block cache hit rate (random workload) | > 70% |
| Range scan (1000 keys) | < 10 ms |
