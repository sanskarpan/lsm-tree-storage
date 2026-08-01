# SSTable Format Deep Dive

SSTables (`internal/sstable/`) are the immutable on-disk files produced by MemTable flushes and compaction. The format is modelled after LevelDB's, simplified for educational clarity.

---

## File layout

```text
SSTable (.sst)
┌───────────────────────────────────┐
│  Data Block 0  (target 4 KB)      │
│  Data Block 1  ...                │
│  Data Block N                     │
├───────────────────────────────────┤
│  Filter Block  (Bloom filter)     │
├───────────────────────────────────┤
│  Meta Index Block                 │  ← handle to filter block
├───────────────────────────────────┤
│  Index Block   (one entry/block)  │
├───────────────────────────────────┤
│  Footer        (48 bytes, fixed)  │
└───────────────────────────────────┘
```

---

## Footer layout

The footer is exactly 48 bytes at the end of every SSTable file (`internal/sstable/format.go`).

```text
 Field                  Encoding           Notes
 ─────────────────────  ─────────────────  ──────────────────────────────────────
 MetaIndex BlockHandle  varint pair        offset + size of the meta index block
 Index BlockHandle      varint pair        offset + size of the index block
 Padding                zero bytes         fills to 40 bytes total before version
 Format version         LE uint32 (4 B)   0 = legacy (accepted), 1 = current
 Magic number           fixed 8 B         0x88e241b785f4cff7 (LevelDB magic)
 ─────────────────────────────────────────────────────────────────────────────────
 Total                                     48 bytes
```

### Format version handling

| Version | Behaviour |
|---------|-----------|
| 0 | Legacy format; accepted during `SSTableReader.Open` |
| 1 | Current format; written by `SSTableBuilder.Finish` |
| Other | Rejected with `ErrUnknownFormat` — do not read |

---

## Data block format

Each data block holds a sorted sequence of key-value entries with prefix compression. The block target size is `BlockSize` (default 4 KB, defined in `engine/config.go`).

```text
 Entry (restart point — full key, shared_len=0):
 ┌─────────────┬───────────────┬──────────┬───────────────┬─────────┐
 │ shared_len  │ unshared_len  │ val_len  │ unshared_key  │  value  │
 │  (varint)   │   (varint)    │ (varint) │  (N bytes)    │(M bytes)│
 └─────────────┴───────────────┴──────────┴───────────────┴─────────┘
  shared_len=0 at every 16th entry (restart point)

 Entry (prefix-compressed against previous key):
 ┌─────────────┬───────────────┬──────────┬───────────────┬─────────┐
 │ shared_len  │ unshared_len  │ val_len  │ unshared_key  │  value  │
 │  (varint)   │   (varint)    │ (varint) │               │         │
 └─────────────┴───────────────┴──────────┴───────────────┴─────────┘
  shared_len = prefix bytes shared with previous key

 Restarts section (end of block):
 ┌────────────┬────────────┬─────┬────────────────────┐
 │ restart[0] │ restart[1] │ ... │ numRestarts (4 B LE)│
 │ (uint32 LE)│ (uint32 LE)│     │                     │
 └────────────┴────────────┴─────┴────────────────────┘
  restart[i] = byte offset within block of the i-th restart point
  Enables binary search: seek to nearest restart, then linear scan.
```

The restart interval is every 16 entries (`internal/sstable/block_builder.go`).

---

## Bloom filter block

The filter block stores one Bloom filter covering all keys in the SSTable (`internal/bloom/bloom.go`).

```text
 Layout:
  [ bit array bytes: m/8 bytes ][ k: 1 byte ]

  m = numKeys × BloomBitsPerKey  (default 10)
  k = floor(BloomBitsPerKey × ln(2))  (clamped to [1, 30])
    = floor(10 × 0.693)  = 6 hash functions at default settings
```

| Bits per key | k (hash functions) | False positive rate |
|--------------|-------------------|---------------------|
| 6 | 4 | ~8% |
| 10 | 6–7 | ~1% |
| 14 | 9–10 | ~0.3% |
| 20 | 13–14 | ~0.04% |

The hash function is a murmur-like single-hash derivation: one base hash `h` is computed, then `k` bit positions are derived via `h += (h>>17)|(h<<15)` rotation. This is the same construction as LevelDB's Bloom filter.

`MayContain(key) == false` guarantees the key is **not** in the SSTable (no false negatives). Only false positives are possible.

---

## Index block

One entry per data block, stored using the same prefix-compressed block format:

```text
 Key:   separator key (≥ last key in block i, < first key in block i+1)
 Value: BlockHandle { Offset: uint64, Size: uint64 }  (16 bytes, varint-encoded)
```

On a point `Get`, the reader binary-searches the index block to find the candidate data block handle, then either fetches from the block cache or reads from disk.

---

## LRU TableCache

The engine keeps at most `MaxOpenFiles` (default 1000) `SSTableReader` instances open simultaneously. Readers are reference-counted; the LRU evicts entries only when `refs == 0`. This bounds the number of open file descriptors at the process level.

---

## Block cache

The LRU block cache (`internal/cache/lru.go`) is keyed by:

```go
type CacheKey struct {
    FileID      uint64
    BlockOffset uint64
}
```

`Level` is not part of the cache key — the same block at the same offset within the same file is always the same block regardless of which level it is conceptually associated with.

Cache capacity is controlled by `BlockCacheSize` (default 128 MB). Each eviction removes the block at the LRU tail only when no active reader holds a reference.

---

## mmap vs ReadAt

| Platform | Strategy | Notes |
|----------|----------|-------|
| Unix (Linux, macOS) | `mmap` + `MADV_SEQUENTIAL` | Advises the kernel to prefetch pages sequentially during iterator scans; `POSIX_FADV_DONTNEED` on Linux releases pages after close |
| Windows, fallback | `os.File.ReadAt` | No memory mapping; reads via pread syscall |

The platform selection is compile-time: `reader_mmap_unix.go`, `reader_mmap_linux.go`, `reader_mmap_windows.go`.

---

## InternalKey sort order

Keys are stored throughout the engine as `InternalKey`:

```text
  Primary sort:    UserKey  ASC  (lexicographic)
  Secondary sort:  SeqNo    DESC (higher = newer = sorts first)
```

This ordering means that for a given user key, the most recent version always precedes older versions in any sorted structure. A `Get` at `readSeqNo` finds the first entry whose `SeqNo <= readSeqNo`.

---

## SSTableMeta (in-memory)

The MANIFEST tracks the following fields per SSTable (`internal/sstable/`):

| Field | Type | Purpose |
|-------|------|---------|
| `FileID` | `uint64` | Monotonically increasing identifier |
| `Level` | `int` | LSM level (0–6) |
| `FirstKey` | `[]byte` | Smallest InternalKey in the file |
| `LastKey` | `[]byte` | Largest InternalKey in the file |
| `FileSize` | `uint64` | Total file size in bytes |
| `NumKeys` | `uint64` | Entry count including tombstones |
| `CreatedAt` | `int64` | Unix nanoseconds (used by TWCS for time-window grouping) |
