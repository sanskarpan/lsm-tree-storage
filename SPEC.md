# SPEC.md — LSM-Tree Storage Engine

> **Backend:** Go 1.22+ (from-scratch engine; no external DB dependencies)
> **Frontend:** Bun + TypeScript + Elysia (BFF) · Vanilla TypeScript + D3.js v7

---

## §1 Project Overview & Language Decision

**Why Go for this project?** Go is the ideal match for an LSM engine implementation:
- `os.File`, `bufio`, `sync`, `container/heap` cover all I/O, concurrency, and data structure needs
- Goroutines naturally model background compaction + flush workers
- Explicit memory management (vs. GC languages) makes buffer lifecycles clear
- The most-read LSM educational codebases (LevelDB Go ports, mini-lsm-go, goleveldb) are all Go
- Binary encoding with `encoding/binary` directly maps to on-disk formats

**Concepts covered:**

| Area | Concepts |
|------|----------|
| Write path | WAL append + fsync, MemTable skip-list insert, immutable flush trigger |
| WAL | 32KB block format, CRC32 checksums, record fragmentation (Full/First/Middle/Last), crash recovery replay |
| MemTable | Skip list (O(log n)), tombstone markers, range iteration, mutable→immutable rotation |
| SSTable format | Data blocks, Filter block (Bloom), Index block, MetaIndex block, Footer + BlockHandles |
| Bloom filters | k-hash bit array, optimal bits_per_key, false positive math, per-SSTable |
| Read path | MemTable → immutable MemTables → L0 → L1+ with Bloom short-circuit + binary search |
| Compaction | STCS (size-tiered), LCS (leveled), TWCS (time-window); write/read/space amplification tradeoffs |
| MANIFEST | Append-only VersionEdit log; SSTable level tracking, key ranges, generation numbers |
| Crash recovery | WAL replay → MemTable rebuild; MANIFEST replay → SSTable level reconstruction |
| Block cache | LRU cache for data blocks + index blocks; configurable size |
| Iterator | Merged iterator across all levels for range scans |
| Tombstones | Delete marker semantics, GC during compaction |
| Amplification | WA/RA/SA measurement, live metrics during benchmark workloads |

---

## §2 Architecture

```
Client (HTTP/WS)
      |
      v
Elysia BFF  (port 3001, Bun)
      |
      v
LSM Engine HTTP Gateway  (port 8080, Go)
      |
      v
LSMEngine
  +-- WAL                     (append-only, CRC32-protected, 32KB blocks)
  +-- MemTable (skip list)    (mutable, active writes)
  +-- ImmutableQueue          ([]MemTable waiting to flush)
  +-- LevelManager            (L0..L6 SSTable metadata)
  |     +-- Level[0]          (L0: overlapping key ranges OK)
  |     +-- Level[1..6]       (L1+: non-overlapping, sorted)
  +-- BlockCache              (LRU; data blocks + index blocks)
  +-- BloomFilterRegistry     (per-SSTable Bloom filters, in-memory)
  +-- Compactor               (background goroutine; STCS or LCS)
  +-- FlushWorker             (background goroutine; immutable → SSTable)
  +-- ManifestWriter          (append-only VersionEdit log)
  +-- EventBus                (→ WebSocket)

Simulated Workloads
  +-- WriteBenchmark          (sequential, random, zipf)
  +-- ReadBenchmark
  +-- MixedWorkload
  +-- CompactionStressTest
```

---

## §3 Write-Ahead Log (WAL)

### 3.1 Block Format

The WAL is a sequence of 32KB blocks, matching LevelDB/RocksDB:

```
File layout:
+------------+------------+-----+
| Block 0    | Block 1    | ... |
| (32768 B)  | (32768 B)  |     |
+------------+------------+-----+

Each block contains one or more records. Records that exceed 32KB
are fragmented across blocks using record types.

Record format:
+--------+---------+------+------------------+
| CRC32  | Length  | Type | Payload          |
| 4 bytes| 2 bytes | 1 byte| Length bytes    |
+--------+---------+------+------------------+

Type values:
  kFullType   = 1  // record fits entirely in one block
  kFirstType  = 2  // first fragment of a multi-block record
  kMiddleType = 3  // middle fragment
  kLastType   = 4  // final fragment
```

```go
// internal/wal/wal.go
const (
    BlockSize     = 32 * 1024  // 32KB
    HeaderSize    = 7          // CRC(4) + Length(2) + Type(1)
    RecordFull    = 1
    RecordFirst   = 2
    RecordMiddle  = 3
    RecordLast    = 4
)

type WALRecord struct {
    Type    uint8
    Payload []byte
}

type WAL struct {
    mu        sync.Mutex
    file      *os.File
    bufWriter *bufio.Writer
    blockPos  int    // current position within the current block
    seqNo     uint64 // monotonically increasing
}

// Entry types written to WAL
type WALEntryType uint8
const (
    EntrySet    WALEntryType = 1  // key + value
    EntryDelete WALEntryType = 2  // key only (tombstone)
    EntryFlush  WALEntryType = 3  // marks memtable flush complete
)

// Wire format for a WAL entry payload:
// [EntryType:1][KeyLen:4][Key:KeyLen][ValLen:4][Val:ValLen][SeqNo:8]
type WALEntry struct {
    Type  WALEntryType
    Key   []byte
    Value []byte // empty for EntryDelete
    SeqNo uint64
}
```

### 3.2 Write Path

```go
func (w *WAL) Append(entry WALEntry) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    payload := encodeEntry(entry) // binary encoding
    return w.writeRecord(payload)
}

func (w *WAL) writeRecord(data []byte) error {
    for len(data) > 0 {
        available := BlockSize - w.blockPos - HeaderSize
        if available < HeaderSize {
            // Pad current block to boundary
            w.padBlock()
            w.blockPos = 0
        }
        chunkLen := min(available, len(data))
        var rType uint8
        switch {
        case len(data) == chunkLen && w.blockPos == 0:
            rType = RecordFull
        case w.blockPos == 0:
            rType = RecordFirst
        case len(data) == chunkLen:
            rType = RecordLast
        default:
            rType = RecordMiddle
        }
        crc := crc32.ChecksumIEEE(data[:chunkLen])
        w.writeHeader(crc, uint16(chunkLen), rType)
        w.bufWriter.Write(data[:chunkLen])
        data = data[chunkLen:]
        w.blockPos += HeaderSize + chunkLen
    }
    return nil
}

// Sync flushes OS buffer and calls fsync
func (w *WAL) Sync() error {
    if err := w.bufWriter.Flush(); err != nil { return err }
    return w.file.Sync()
}
```

### 3.3 Recovery

```go
func RecoverWAL(path string) ([]WALEntry, error) {
    // Read all valid records, verify CRC32
    // Skip truncated records at end (partial write before crash)
    // Return entries in order; caller replays into MemTable
}
```

**Recovery contract:** The last record may be truncated (partial write before crash). This is expected — skip it, log a warning, and continue. All complete records with valid CRC are safe to replay.

---

## §4 MemTable (Skip List)

### 4.1 Skip List Design

```go
// internal/memtable/skiplist.go
const MaxLevel = 12
const Probability = 0.25

type SkipListNode struct {
    key     InternalKey     // key + sequence number + type
    value   []byte
    forward []*SkipListNode // one pointer per level
}

type InternalKey struct {
    UserKey []byte
    SeqNo   uint64
    Type    EntryType // TypeValue or TypeDeletion
}

// Ordering: UserKey ASC, then SeqNo DESC (newer entries sort first)
func (a InternalKey) Less(b InternalKey) bool {
    cmp := bytes.Compare(a.UserKey, b.UserKey)
    if cmp != 0 { return cmp < 0 }
    return a.SeqNo > b.SeqNo // higher seqno = newer = sorts first
}
```

### 4.2 MemTable API

```go
type MemTable struct {
    sl       *SkipList
    size     int64   // approximate byte size
    maxSize  int64   // flush threshold
    walSeqNo uint64  // tracks which WAL entries are "covered"
}

func (m *MemTable) Put(key, value []byte, seqNo uint64)
func (m *MemTable) Delete(key []byte, seqNo uint64)  // inserts tombstone
func (m *MemTable) Get(key []byte) ([]byte, bool, error)
    // returns (value, found, err); found=true + empty value = tombstone
func (m *MemTable) NewIterator() *MemTableIterator
func (m *MemTable) ApproximateSize() int64
func (m *MemTable) IsFull() bool // size >= maxSize
```

### 4.3 Tombstone Semantics

```go
const TombstoneValue = "" // empty value signals deletion

// Get logic:
func (m *MemTable) Get(key []byte) (value []byte, found bool, deleted bool) {
    node := m.sl.Find(InternalKey{UserKey: key, SeqNo: math.MaxUint64})
    if node == nil || !bytes.Equal(node.key.UserKey, key) {
        return nil, false, false  // not in memtable
    }
    if node.key.Type == TypeDeletion {
        return nil, true, true    // found tombstone — key is deleted
    }
    return node.value, true, false  // found live value
}
```

---

## §5 SSTable File Format

Modeled after LevelDB's format; simplified for educational clarity.

```
SSTable file layout:
+------------------+
| Data Block 0     |  sorted key-value pairs, prefix-compressed
+------------------+
| Data Block 1     |
+------------------+
|   ...            |
+------------------+
| Data Block N     |
+------------------+
| Filter Block     |  Bloom filter bit array
+------------------+
| Index Block      |  one entry per data block: (lastKey -> blockHandle)
+------------------+
| Footer (48 bytes)|  BlockHandle for IndexBlock + magic number
+------------------+

BlockHandle = { offset uint64, size uint64 }

Footer:
+------------------+----------+----------+
| IndexHandle(var) | Padding  | Magic(8B)|
+------------------+----------+----------+
| total = 48 bytes                        |
| Magic = 0x88e241b785f4cff7 (LevelDB)   |
```

### 5.1 Data Block Format

```
Data block layout:
+-------------------+-------------------+-----+-----------+
| Entry 0           | Entry 1           | ... | Restarts  |
+-------------------+-------------------+-----+-----------+

Each entry (prefix-compressed):
[sharedKeyLen:varint][unsharedKeyLen:varint][valueLen:varint][unsharedKey][value]

Restarts section (at end of block):
[restart0:4][restart1:4]...[numRestarts:4]
Restart = byte offset of a non-prefix-compressed entry (every 16 entries)
This enables binary search on restart points.
```

### 5.2 SSTable Builder

```go
// internal/sstable/builder.go
type SSTableBuilder struct {
    dataBlock     *BlockBuilder
    indexBlock    *BlockBuilder
    filterBuilder *BloomFilterBuilder
    file          *os.File
    pendingHandle BlockHandle   // handle of most recently finished block
    firstKey      []byte
    lastKey       []byte
    numEntries    int
    fileSize      uint64
}

func (b *SSTableBuilder) Add(key InternalKey, value []byte) error {
    // 1. Add to data block
    // 2. Add key to Bloom filter
    // 3. If data block >= blockSize: flush, add to index block
}

func (b *SSTableBuilder) Finish() (SSTableMeta, error) {
    // Flush last data block
    // Write filter block
    // Write index block
    // Write footer
    // Sync to disk
    return meta, nil
}

type SSTableMeta struct {
    FileID    uint64
    Level     int
    FirstKey  []byte
    LastKey   []byte
    FileSize  uint64
    NumKeys   uint64
}
```

### 5.3 SSTable Reader

```go
type SSTableReader struct {
    file        *os.File
    indexBlock  *Block         // loaded on open
    bloomFilter *BloomFilter   // loaded on open
    cache       *BlockCache
    meta        SSTableMeta
}

func (r *SSTableReader) Get(key []byte, seqNo uint64) ([]byte, bool, error) {
    // 1. Bloom filter check: if "definitely not here" → return false
    // 2. Binary search index block → find data block handle
    // 3. Check block cache; load from disk if miss
    // 4. Binary search within data block (restart points)
    // 5. Return most recent version <= seqNo
}

func (r *SSTableReader) NewIterator() *SSTableIterator
    // Iterates data blocks in order; needed for compaction merge
```

---

## §6 Bloom Filter

### 6.1 Mathematical Foundation

```
Parameters:
  n = expected number of keys
  m = total bit array size
  k = number of hash functions
  p = false positive probability

Optimal k = (m/n) * ln(2)
Optimal false positive rate: p = (1 - e^(-kn/m))^k

LevelDB default: bits_per_key = 10
  → k = 10 * ln(2) ≈ 7 hash functions
  → false positive rate ≈ 1%

For bits_per_key = 14: p ≈ 0.3%
For bits_per_key = 20: p ≈ 0.04%
```

### 6.2 Implementation (LevelDB-style single-hash derivation)

```go
// internal/bloom/bloom.go

type BloomFilter struct {
    bits        []byte  // bit array
    numHashFuncs int
}

func NewBloomFilter(keys [][]byte, bitsPerKey int) *BloomFilter {
    m := len(keys) * bitsPerKey
    m = roundUp(m, 8) // round to nearest byte
    bits := make([]byte, m/8)
    k := int(float64(bitsPerKey) * math.Ln2) // optimal k
    if k < 1 { k = 1 }
    if k > 30 { k = 30 }

    for _, key := range keys {
        h := bloomHash(key)
        delta := (h >> 17) | (h << 15) // rotate right 17 bits
        for j := 0; j < k; j++ {
            bitPos := uint32(h) % uint32(m)
            bits[bitPos/8] |= 1 << (bitPos % 8)
            h += delta
        }
    }
    return &BloomFilter{bits: append(bits, byte(k)), numHashFuncs: k}
}

func (f *BloomFilter) MayContain(key []byte) bool {
    if len(f.bits) < 2 { return false }
    k := int(f.bits[len(f.bits)-1]) // stored at end
    bits := f.bits[:len(f.bits)-1]
    m := len(bits) * 8

    h := bloomHash(key)
    delta := (h >> 17) | (h << 15)
    for j := 0; j < k; j++ {
        bitPos := uint32(h) % uint32(m)
        if bits[bitPos/8]&(1<<(bitPos%8)) == 0 {
            return false // definitely not in set
        }
        h += delta
    }
    return true // probably in set
}

func bloomHash(key []byte) uint32 {
    // murmur-like hash
    const seed = 0xbc9f1d34
    const m = 0xc6a4a793
    // ... (standard implementation)
}
```

### 6.3 False Positive Demonstration

The project includes an interactive Bloom filter panel that shows:
- Add N keys → bit positions set → visual bit array
- Test false positive: query keys NOT in filter → count false "maybe present"
- Slider for bits_per_key (6–20) → live false positive rate curve

---

## §7 Compaction Strategies

### 7.1 Leveled Compaction (LCS) — default

```
Level structure:
  L0: 0–4 SSTables (overlapping key ranges OK; from direct flushes)
  L1: max 10 MB; non-overlapping SSTables
  L2: max 100 MB
  L3: max 1 GB
  ...
  Ln: max 10^n MB (size multiplier = 10)

Trigger: Level i exceeds its max size

Algorithm:
1. Pick one SSTable from level i (round-robin or "oldest first")
2. Find all overlapping SSTables in level i+1
3. Multi-way merge (k-way merge via heap) → new SSTables at level i+1
4. Delete input SSTables; add new SSTables to MANIFEST
5. Repeat if level i+1 now exceeds its max

Amplification:
  Write amplification: O(10) per level, O(10 * numLevels) total (worst case ~30×)
  Read amplification: O(numLevels) = O(7) for point queries
  Space amplification: O(1) — minimal stale data
```

```go
// internal/compaction/leveled.go
type LeveledCompactor struct {
    engine *LSMEngine
}

func (c *LeveledCompactor) PickCompaction() *CompactionTask {
    // Find the level most over its size limit
    // Return task with input SSTables from level i and overlapping from i+1
}

func (c *LeveledCompactor) Execute(task *CompactionTask) error {
    iter := NewMergeIterator(task.InputIterators())
    builder := NewSSTableBuilder(task.OutputLevel)
    var currentKey []byte
    for iter.Valid() {
        key, value := iter.Key(), iter.Value()
        // Skip if key == currentKey (keep only newest version)
        if bytes.Equal(key.UserKey, currentKey) { iter.Next(); continue }
        // Skip tombstones at bottom level (no older versions exist)
        if key.Type == TypeDeletion && isBottomLevel(task.OutputLevel) {
            iter.Next(); continue
        }
        builder.Add(key, value)
        currentKey = key.UserKey
        iter.Next()
    }
    return c.engine.applyCompaction(task, builder.Finish())
}
```

### 7.2 Size-Tiered Compaction (STCS)

```
Tier grouping: SSTables within 2× of each other in size form a tier
Trigger: Tier has >= minThreshold (default 4) SSTables

Algorithm:
1. Group SSTables into tiers by approximate size
2. When tier has >= minThreshold members: merge all into one larger SSTable
3. The result may join the next larger tier

Amplification:
  Write amplification: O(log_T(N)) ≈ 5× for T=4 — lower than LCS
  Read amplification: O(tiers) — can be high; many SSTables may overlap
  Space amplification: O(T) — can be ~4× during tier compaction

Best for: write-heavy workloads; rare reads
```

```go
// internal/compaction/stcs.go
type SizeTieredCompactor struct {
    engine       *LSMEngine
    minThreshold int // default 4
    maxThreshold int // default 32
    bucketLow    float64 // 0.5
    bucketHigh   float64 // 1.5
}

func (c *SizeTieredCompactor) GroupIntoBuckets(sstables []*SSTableMeta) [][]* SSTableMeta {
    // Group by size: files within [avg * bucketLow, avg * bucketHigh] together
}
```

### 7.3 Time-Window Compaction (TWCS)

```
For time-series workloads. SSTables from the same time window (e.g., 1 day)
are compacted using STCS within the window. Different windows never mix.

Parameters:
  windowSize: 24h (configurable)
  compactionWindow: which window is currently "active"

Algorithm:
  Within current window: use STCS
  Old (closed) windows: no further compaction (data is immutable time-series)
```

### 7.4 Amplification Metrics

```go
type AmplificationStats struct {
    WriteAmplification float64 // bytes written to disk / bytes written by client
    ReadAmplification  float64 // disk reads per point query (avg)
    SpaceAmplification float64 // disk bytes / actual data bytes
    // Live tracking:
    ClientBytesWritten uint64
    DiskBytesWritten   uint64
    CompactionBytesWritten uint64
    TotalQueries uint64
    TotalDiskReadsPerQuery []float64 // histogram
}
```

---

## §8 MANIFEST File

The MANIFEST is an append-only log of `VersionEdit` records. It tracks the complete SSTable inventory across all levels.

```go
// internal/manifest/manifest.go
type VersionEditType uint8
const (
    EditAddSSTable    VersionEditType = 1
    EditDeleteSSTable VersionEditType = 2
    EditSetCompactPtr VersionEditType = 3  // "compaction pointer" per level
    EditNewDB         VersionEditType = 4
)

type VersionEdit struct {
    Type       VersionEditType
    Level      int
    FileID     uint64
    FileSize   uint64
    FirstKey   []byte
    LastKey    []byte
    Deleted    []DeletedFile // (level, fileID) pairs to remove
}

type Manifest struct {
    mu     sync.Mutex
    file   *os.File
    writer *bufio.Writer
    // In-memory version (rebuilt from MANIFEST on open):
    current *Version
}

type Version struct {
    Levels [7][]*SSTableMeta // L0..L6; L0 may overlap
    nextFileID uint64
    logNumber  uint64 // WAL file being used
}

func (m *Manifest) Apply(edit VersionEdit) error {
    // 1. Validate: new SSTable's key range doesn't overlap (L1+)
    // 2. Append to MANIFEST file; fsync
    // 3. Update in-memory Version
}
```

### 8.1 Atomic Compaction Commit

Compaction atomicity: new SSTables written → MANIFEST updated → old SSTables deleted (in that order). If a crash occurs before MANIFEST update, the new SSTables are orphaned (cleaned up on restart). If crash occurs before deletion, old SSTables are kept (safe: they're still valid).

```go
func (e *LSMEngine) applyCompaction(task *CompactionTask, outputs []SSTableMeta) error {
    // 1. fsync all output SSTables
    for _, sst := range outputs {
        if err := sst.Sync(); err != nil { return err }
    }
    // 2. Write VersionEdit to MANIFEST (atomic: add new + delete old)
    edit := buildVersionEdit(task.InputSSTables, outputs)
    if err := e.manifest.Apply(edit); err != nil { return err }
    // 3. Delete old SSTable files
    for _, sst := range task.InputSSTables {
        os.Remove(sst.FilePath())
    }
    // 4. Update bloom filter registry
    return nil
}
```

---

## §9 Read Path

```go
func (e *LSMEngine) Get(key []byte) ([]byte, error) {
    e.mu.RLock()
    defer e.mu.RUnlock()

    seqNo := e.latestSeqNo // read at consistent snapshot

    // 1. Check active MemTable
    if v, found, deleted := e.memTable.Get(key); found {
        if deleted { return nil, ErrNotFound }
        return v, nil
    }

    // 2. Check immutable MemTables (newest first)
    for i := len(e.immutable) - 1; i >= 0; i-- {
        if v, found, deleted := e.immutable[i].Get(key); found {
            if deleted { return nil, ErrNotFound }
            return v, nil
        }
    }

    // 3. Check L0 SSTables (newest first; ALL must be checked; may overlap)
    l0 := e.manifest.current.Levels[0]
    for i := len(l0) - 1; i >= 0; i-- {
        sst := e.sstReaders[l0[i].FileID]
        if !sst.bloomFilter.MayContain(key) { continue } // Bloom short-circuit
        if v, found, err := sst.Get(key, seqNo); err != nil { return nil, err
        } else if found { return v, nil }
    }

    // 4. Check L1..L6 (binary search by key range; only ONE SSTable per level)
    for level := 1; level <= maxLevel; level++ {
        ssts := e.manifest.current.Levels[level]
        idx := sort.Search(len(ssts), func(i int) bool {
            return bytes.Compare(ssts[i].LastKey, key) >= 0
        })
        if idx >= len(ssts) || bytes.Compare(ssts[idx].FirstKey, key) > 0 {
            continue // key is outside this level's range
        }
        sst := e.sstReaders[ssts[idx].FileID]
        if !sst.bloomFilter.MayContain(key) { continue }
        if v, found, err := sst.Get(key, seqNo); err != nil { return nil, err
        } else if found { return v, nil }
    }

    return nil, ErrNotFound
}
```

### 9.1 Range Scan

```go
type Iterator interface {
    Valid() bool
    Next()
    Key() InternalKey
    Value() []byte
    SeekToFirst()
    Seek(key []byte)
}

// MergeIterator: k-way merge using a min-heap
// Merges: active MemTable + all immutable + L0 SSTables + L1+ SSTables
// Handles: deduplication (same user key → keep highest seqNo)
// Handles: tombstone propagation (skip all older versions of deleted key)
```

---

## §10 Block Cache

```go
// internal/cache/lru.go
type BlockCache struct {
    mu       sync.Mutex
    capacity int64           // bytes
    used     int64
    lru      *list.List      // front=MRU, back=LRU
    index    map[CacheKey]*list.Element
}

type CacheKey struct {
    FileID     uint64
    BlockOffset uint64
}

type CacheEntry struct {
    key   CacheKey
    block *Block // decoded, decompressed data block
    size  int64
}

func (c *BlockCache) Get(key CacheKey) (*Block, bool)
func (c *BlockCache) Insert(key CacheKey, block *Block) (evicted int64)
func (c *BlockCache) HitRate() float64
```

---

## §11 Engine API

```go
// internal/engine/engine.go

type Config struct {
    DataDir          string
    MemTableSize     int64         // default 4MB
    MaxL0SSTables    int           // default 4; trigger compaction
    LevelSizeMultiplier int        // default 10
    BlockSize        int           // default 4KB
    BloomBitsPerKey  int           // default 10
    BlockCacheSize   int64         // default 8MB
    CompactionStyle  string        // "leveled" | "stcs" | "twcs"
    SyncWAL          bool          // default true
    MaxOpenFiles     int           // default 500
}

type LSMEngine struct {
    cfg       Config
    // ... (all components above)
}

// Core operations
func (e *LSMEngine) Open(cfg Config) (*LSMEngine, error)
func (e *LSMEngine) Close() error
func (e *LSMEngine) Put(key, value []byte) error
func (e *LSMEngine) Delete(key []byte) error
func (e *LSMEngine) Get(key []byte) ([]byte, error)
func (e *LSMEngine) Scan(start, end []byte) (Iterator, error)
func (e *LSMEngine) Stats() *EngineStats

// Batch operations
type WriteBatch struct { entries []BatchEntry }
func (e *LSMEngine) Write(batch *WriteBatch) error

// Compaction control
func (e *LSMEngine) ForceCompaction(level int) error
func (e *LSMEngine) SetCompactionStyle(style string) error

// Snapshot
func (e *LSMEngine) GetSnapshot() uint64  // returns seqNo
func (e *LSMEngine) GetAtSnapshot(key []byte, seqNo uint64) ([]byte, error)
```

---

## §12 Crash Recovery Procedure

On `Open()`:

```
1. Read CURRENT file → find MANIFEST filename
2. Replay MANIFEST → reconstruct Version (all SSTable levels + metadata)
3. Open WAL file(s) listed in MANIFEST
4. Replay WAL records into MemTable:
   a. Verify CRC32 of each record
   b. Skip truncated last record (partial write before crash)
   c. Apply entries in order (Put/Delete with their sequence numbers)
5. Validate: all SSTable files referenced in MANIFEST exist on disk
6. Open SSTable readers for all files; load index blocks + Bloom filters
7. Initialize BlockCache
8. Start background FlushWorker + Compactor goroutines
9. Ready to serve requests

Recovery edge cases:
  - Crash during flush: orphaned SSTable not in MANIFEST → delete on startup
  - Crash during compaction: new SSTables not in MANIFEST → delete on startup
  - Truncated WAL: replay up to last complete record
  - Missing SSTable: fatal error (data loss — should not happen with correct MANIFEST writes)
```

```go
func (e *LSMEngine) recover() error {
    // Read and replay MANIFEST
    version, maxFileID, logNumber := e.manifest.Recover()
    e.manifest.current = version

    // Replay WAL
    walPath := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.log", logNumber))
    entries, err := RecoverWAL(walPath)
    for _, entry := range entries {
        switch entry.Type {
        case EntrySet:    e.memTable.Put(entry.Key, entry.Value, entry.SeqNo)
        case EntryDelete: e.memTable.Delete(entry.Key, entry.SeqNo)
        }
    }

    // Clean up orphaned SSTable files
    e.removeOrphanedFiles(version)
    return nil
}
```

---

## §13 Simulation & Benchmark Engine

```go
type WorkloadConfig struct {
    Type          string  // "sequential_write", "random_write", "zipf_read",
                          // "mixed", "compaction_stress", "point_delete"
    NumKeys       int
    ValueSize     int     // bytes
    KeySize       int
    ReadWriteRatio float64 // for mixed workload
    ZipfSkew      float64  // for zipf (1.0 = heavy skew)
    BatchSize     int      // for batched writes
}

type BenchmarkResult struct {
    TotalOps       uint64
    Duration       time.Duration
    OpsPerSec      float64
    WriteLatency   LatencyHistogram  // p50, p99, p999
    ReadLatency    LatencyHistogram
    WALBytesWritten uint64
    MemTableFlushes uint64
    Compactions    map[int]int  // level → count
    BloomFP        uint64       // bloom filter false positives
    CacheHitRate   float64
    WA             float64  // write amplification
    RA             float64  // read amplification (avg disk reads/query)
    SA             float64  // space amplification
}

type SimulationOrchestrator struct {
    engine  *LSMEngine
    events  *EventBus
}

// Pre-built demonstration scenarios:
// 1. "write_flush"       — fill memtable → watch flush → new SSTable appears in L0
// 2. "bloom_demo"        — insert 10k keys; query missing keys; count false positives
// 3. "compaction_lcl"    — fill L0 to threshold; watch L0→L1 compaction
// 4. "compaction_stcs"   — demonstrate tiered compaction with size grouping
// 5. "crash_recovery"    — simulate crash mid-write; verify recovery
// 6. "tombstone_gc"      — delete keys; watch tombstones GC'd during compaction
// 7. "range_scan"        — demonstrate merge iterator across levels
// 8. "amplification"     — measure WA/RA/SA for LCS vs STCS vs baseline
```

---

## §14 Event System (→ WebSocket)

```go
type EventType string
const (
    // Write path
    EvtWALAppend         EventType = "wal_append"
    EvtWALSync           EventType = "wal_sync"
    EvtMemTableInsert    EventType = "memtable_insert"
    EvtMemTableFull      EventType = "memtable_full"
    EvtMemTableRotate    EventType = "memtable_rotate"
    EvtFlushStart        EventType = "flush_start"
    EvtFlushComplete     EventType = "flush_complete"
    // SSTable
    EvtSSTableCreated    EventType = "sstable_created"
    EvtSSTableDeleted    EventType = "sstable_deleted"
    EvtBlockRead         EventType = "block_read"
    EvtBloomCheck        EventType = "bloom_check"
    EvtBloomHit          EventType = "bloom_hit"   // MayContain=true
    EvtBloomMiss         EventType = "bloom_miss"  // MayContain=false → skip
    EvtBloomFalsePositive EventType = "bloom_fp"   // MayContain=true but key absent
    // Read path
    EvtReadStart         EventType = "read_start"
    EvtReadComplete      EventType = "read_complete"
    EvtReadMemTable      EventType = "read_memtable"
    EvtReadSSTable       EventType = "read_sstable"
    EvtCacheHit          EventType = "cache_hit"
    EvtCacheMiss         EventType = "cache_miss"
    // Compaction
    EvtCompactionStart   EventType = "compaction_start"
    EvtCompactionPick    EventType = "compaction_pick"   // which SSTables chosen
    EvtCompactionMerge   EventType = "compaction_merge"  // per-key merge decision
    EvtCompactionComplete EventType = "compaction_complete"
    EvtTombstoneDropped  EventType = "tombstone_dropped"
    // MANIFEST
    EvtManifestApply     EventType = "manifest_apply"
    // Amplification
    EvtAmplification     EventType = "amplification"     // periodic stats update
    // Scenarios
    EvtScenarioStep      EventType = "scenario_step"
)

type Event struct {
    Type      EventType              `json:"type"`
    Timestamp int64                  `json:"ts"`
    Extra     map[string]interface{} `json:"extra,omitempty"`
}
```

---

## §15 REST API

Base: `http://localhost:8080/api/v1`

### Engine Operations
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/db/open` | Open/initialize engine with config |
| `POST` | `/db/close` | Graceful close |
| `GET`  | `/db/stats` | EngineStats snapshot |
| `PUT`  | `/db/put` | `{key, value}` |
| `DELETE` | `/db/delete` | `{key}` |
| `GET`  | `/db/get` | `?key=...` |
| `GET`  | `/db/scan` | `?start=...&end=...&limit=100` |
| `POST` | `/db/batch` | WriteBatch `{entries:[{key,value,delete}]}` |

### Compaction
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/compaction/force` | Force compaction at level `{level}` |
| `POST` | `/compaction/style` | Change strategy `{style}` |
| `GET`  | `/compaction/stats` | Per-level compaction statistics |

### Benchmark / Scenarios
| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/bench/run` | Run workload `{type, numKeys, valueSize, ...}` |
| `GET`  | `/bench/result` | Latest benchmark result |
| `POST` | `/scenarios/:name/run` | Run pre-built scenario |
| `GET`  | `/scenarios` | List available scenarios |

### Internals (for visualization)
| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/levels` | All levels + SSTable list with key ranges |
| `GET`  | `/wal/entries` | Last N WAL entries |
| `GET`  | `/memtable/snapshot` | MemTable contents (small; for demo only) |
| `GET`  | `/bloom/:fileID` | Bloom filter stats for an SSTable |
| `GET`  | `/cache/stats` | Block cache hit/miss rates |
| `GET`  | `/amplification` | Live WA/RA/SA values |

**WebSocket:** `ws://localhost:8080/ws`

---

## §16 Frontend — Seven Live Panels

### Panel 1: Write Path Visualizer

Animated flow diagram showing a write moving through the system:

```
[Client PUT "key=value"]
        ↓  (animated arrow)
[WAL] → [MemTable (skip list)]
           ↓ (when full)
[Immutable MemTable] → [SSTable L0]
```

- WAL block: shows last 10 append operations as scrolling log with CRC values
- MemTable: animated skip-list visualization (nodes, level pointers, current size/max bar)
- On flush: animated "waterfall" from MemTable box to L0 box
- Live counter: writes/sec, bytes written/sec

### Panel 2: LSM Level Tree

Main structural visualization — shows the entire LSM as a layered diagram:

- L0 box: SSTables as colored rectangles; overlap shown by overlapping boxes
- L1–L6 boxes: non-overlapping SSTables shown as tiling rectangles on a key-range axis
- Each SSTable rectangle colored by age (newer = brighter); size proportional to file size
- Key range axis across the bottom of each level
- On compaction: animate selected input SSTables → merge animation → new output SSTables
- Click SSTable: show metadata (key range, bloom filter size, num entries, file size, age)
- Compaction pick highlight: selected SSTable flashes; overlapping sstables at next level highlighted

### Panel 3: Bloom Filter Interactive Panel

Split panel:
- Left: bit array visualization (rows of colored cells; 1=red, 0=white, max 512 bits shown)
- Right: hash function visualization (show k=7 positions being set for each key added)
- Controls: Add Key input → animate setting k bit positions
- Query Key input → animate checking k positions → "MAYBE" (green) or "DEFINITELY NOT" (red)
- False positive demonstration: add 1000 keys → query 1000 different keys → count FP rate
- Slider: bits_per_key (6–20) → live false positive rate formula shows updated probability

### Panel 4: Read Path Tracer

For a given key query, shows which components were checked:

```
Trace for GET "mykey":
  ✗ MemTable     (not found)
  ✗ Immutable #1 (not found)
  ✓ Bloom L0[3]  (MAYBE — check data)  ← bloom saved us from L0[0],[1],[2]
  ✗ L0[3]       (present but older version)
  ✓ L1[idx=42]  (FOUND: "myvalue")
  
  Disk reads: 2 (index block + data block)
  Bloom skips: 4 SSTables skipped
```

Animated: each check animates left-to-right; bloom skips shown in gray with "🚫" marker; actual reads in green.

### Panel 5: Compaction Simulation

Full interactive compaction viewer:
- Select compaction style: LCS / STCS / TWCS via radio buttons
- "Step through compaction" button: advance one step at a time
- Each step shows:
  - Which SSTable(s) selected for input
  - Merge iterator output (key by key merge with duplicate elimination)
  - Tombstone GC decisions: "dropped at bottom level" vs "kept for propagation"
  - Output SSTable(s) being built
  - MANIFEST edit being written
- After compaction: level sizes update; amplification stats update

### Panel 6: Amplification Dashboard

Real-time metrics fed from EvtAmplification events:

- **Write Amplification gauge:** WA = compaction_bytes_written / client_bytes_written
  - LCS: expect 10–30× | STCS: expect 4–10× | Baseline (no compaction): 1×
- **Read Amplification gauge:** avg disk reads per point query
- **Space Amplification gauge:** total_disk_size / actual_data_size
- Line charts: WA/RA/SA over time as benchmark runs
- Side-by-side comparison: run same workload with LCS then STCS → overlay graphs

### Panel 7: Scenario Control + WAL Viewer

Top: Scenario dropdown + Run + Speed + Step Mode + Reset + Live write counter

Bottom split:
- Left: WAL viewer — scrolling list of WAL entries (type, key, seqNo, crc, size)
  - Color-coded: PUT=green, DELETE=orange, FLUSH_MARKER=blue, CORRUPT=red
- Right: Crash Recovery simulator:
  - "Simulate Crash" button: stops engine mid-write, clears in-memory state
  - "Recover" button: re-opens from WAL + MANIFEST; shows step-by-step replay
  - Recovery progress: "Replaying WAL: 423/500 entries…" with each key being restored

---

## §17 Elysia BFF

```typescript
// frontend/server/bff.ts
import { Elysia } from "elysia";
const BACKEND = "http://localhost:8080";
const app = new Elysia()
  .get("/api/*",    ({ params }) => fetch(`${BACKEND}/api/${params["*"]}`).then(r => r.json()))
  .post("/api/*",   ({ params, body }) => fetch(`${BACKEND}/api/${params["*"]}`, {
      method: "POST", body: JSON.stringify(body),
      headers: { "Content-Type": "application/json" }}).then(r => r.json()))
  .delete("/api/*", ({ params }) => fetch(`${BACKEND}/api/${params["*"]}`, { method: "DELETE" }).then(r => r.json()))
  .put("/api/*",    ({ params, body }) => fetch(`${BACKEND}/api/${params["*"]}`, {
      method: "PUT", body: JSON.stringify(body),
      headers: { "Content-Type": "application/json" }}).then(r => r.json()))
  .ws("/ws", {
      open(ws) {
          const bk = new WebSocket("ws://localhost:8080/ws");
          bk.onmessage = m => ws.send(m.data);
          (ws as any)._bk = bk;
      },
      close(ws) { (ws as any)._bk?.close(); },
  })
  .listen(3001);
export type App = typeof app;
```

---

## §18 File Structure

```
lsm-engine/
+-- cmd/server/main.go
+-- internal/
|   +-- wal/
|   |   +-- wal.go             # WAL writer: block format, CRC32, record fragmentation
|   |   +-- reader.go          # WAL reader: recovery, CRC verification
|   |   +-- record.go          # Record types + entry binary encoding
|   +-- memtable/
|   |   +-- skiplist.go        # Skip list: MaxLevel=12, P=0.25
|   |   +-- memtable.go        # MemTable wrapper: Put/Delete/Get/Iterator
|   |   +-- iterator.go        # MemTable iterator for flush
|   +-- sstable/
|   |   +-- builder.go         # SSTableBuilder: data blocks, index, filter, footer
|   |   +-- reader.go          # SSTableReader: Get, NewIterator, index loading
|   |   +-- block.go           # Block: prefix compression, restart points, binary search
|   |   +-- block_builder.go   # BlockBuilder: accumulates entries
|   |   +-- format.go          # BlockHandle, Footer, InternalKey definitions
|   +-- bloom/
|   |   +-- bloom.go           # Bloom filter: add, MayContain, serialization
|   +-- compaction/
|   |   +-- leveled.go         # LCS compactor: pick, execute, merge iterator
|   |   +-- stcs.go            # STCS compactor: bucket grouping, tiered merge
|   |   +-- twcs.go            # TWCS compactor: time-window grouping
|   |   +-- iterator.go        # MergeIterator (k-way heap), TombstoneIterator
|   +-- manifest/
|   |   +-- manifest.go        # MANIFEST writer + Version in-memory representation
|   |   +-- version_edit.go    # VersionEdit binary encoding/decoding
|   +-- cache/
|   |   +-- lru.go             # LRU BlockCache
|   +-- engine/
|   |   +-- engine.go          # LSMEngine: Open, Put, Get, Scan, recovery
|   |   +-- flush.go           # FlushWorker goroutine
|   |   +-- background.go      # Background compaction scheduler
|   |   +-- config.go          # Config struct
|   +-- simulation/
|   |   +-- workload.go        # Workload generators (sequential, random, zipf)
|   |   +-- scenarios.go       # 8 pre-built demonstration scenarios
|   |   +-- amplification.go   # WA/RA/SA measurement
|   +-- events/
|       +-- bus.go             # EventBus (non-blocking fan-out)
+-- gateway/
|   +-- server.go
|   +-- rest.go
|   +-- websocket.go
+-- frontend/
|   +-- server/bff.ts
|   +-- src/
|       +-- api/{client,types}.ts
|       +-- store/engine.ts       # Reactive engine state
|       +-- components/
|       |   +-- write_path/       # Panel 1
|       |   +-- level_tree/       # Panel 2
|       |   +-- bloom/            # Panel 3
|       |   +-- read_trace/       # Panel 4
|       |   +-- compaction/       # Panel 5
|       |   +-- amplification/    # Panel 6
|       |   +-- scenarios/        # Panel 7
|       +-- ws/client.ts
+-- test/
|   +-- unit/
|   |   +-- wal_test.go
|   |   +-- skiplist_test.go
|   |   +-- sstable_test.go
|   |   +-- bloom_test.go
|   |   +-- compaction_test.go
|   |   +-- manifest_test.go
|   +-- integration/
|       +-- crash_recovery_test.go
|       +-- compaction_correctness_test.go
|       +-- read_write_correctness_test.go
+-- config.yaml
+-- go.mod
+-- Makefile
```

---

## §19 Configuration

```yaml
engine:
  data_dir: "./data"
  memtable_size: 4194304        # 4MB
  max_l0_sstables: 4            # trigger L0 compaction
  level_size_multiplier: 10     # L1=10MB, L2=100MB, L3=1GB...
  block_size: 4096              # 4KB
  bloom_bits_per_key: 10        # ~1% false positive rate
  block_cache_size: 8388608     # 8MB
  compaction_style: "leveled"   # leveled | stcs | twcs
  sync_wal: true                # fsync on every write
  max_open_files: 500

simulation:
  speed_multiplier: 1.0         # 0.1 to 10.0

gateway:
  port: 8080
  ws_buffer: 512

frontend:
  bff_port: 3001
```

---

## §20 Key Correctness Properties

1. **WAL durability:** Every acknowledged write has a CRC-valid record in the WAL before returning to caller.
2. **Recovery completeness:** After crash recovery, all WAL entries with valid CRC are in the MemTable; the MANIFEST accurately reflects all SSTable files on disk.
3. **Monotonic sequence numbers:** `seqNo` never decreases; newer writes have strictly higher seqNos than older writes.
4. **Compaction correctness:** After compaction, for every key, the highest seqNo version is preserved; no live data is lost.
5. **Tombstone semantics:** A tombstone for key K at seqNo S causes all versions of K with seqNo < S to be invisible on read; the tombstone itself is GC'd only when it has reached the bottom level (no older versions can exist).
6. **Level non-overlap (L1+):** At any level L >= 1, no two SSTables have overlapping key ranges. Bloom filter checks at L1+ are binary-search guided to exactly one SSTable.
7. **Bloom filter soundness:** `MayContain(key) == false` ⟹ key is NOT in the SSTable. (No false negatives.)
8. **MANIFEST atomicity:** The MANIFEST is only updated after all output SSTables are synced. Old SSTables are only deleted after MANIFEST is updated.
9. **Read isolation:** Reads see a consistent snapshot (by seqNo); concurrent writes do not corrupt reads.

---

## §21 Performance Targets

| Metric | Target |
|--------|--------|
| Write throughput (seq, no sync) | > 200k ops/s |
| Write throughput (sync, SSD) | > 20k ops/s |
| Point read (key in L1, warm cache) | < 100µs |
| Point read (key not found, bloom filters) | < 50µs |
| Bloom false positive rate (10 bpk) | < 1% |
| MemTable flush (4MB) | < 200ms |
| L0→L1 compaction (trigger: 4 L0 files) | < 2s |
| Block cache hit rate (random workload) | > 70% |
| Range scan (1000 keys) | < 10ms |
