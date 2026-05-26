# CHECKLIST.md — LSM-Tree Storage Engine

> Priority: 🔴 blocking · 🟡 important · 🟢 enhancement · 🔵 stretch
> **Build phases in strict order — each phase's tests must pass before the next begins.**

---

## Phase 0 — Bootstrap (10 tasks)

- [x] 🔴 `go mod init lsm-engine`; zero external dependencies except `stretchr/testify`
- [x] 🔴 Directory structure: `cmd/server/`, `internal/{wal,memtable,sstable,bloom,compaction,manifest,cache,engine,simulation,events}/`, `gateway/`, `test/`
- [x] 🔴 `internal/sstable/format.go`: `InternalKey{UserKey, SeqNo, Type}`, `BlockHandle{Offset, Size}`, `SSTableMeta{FileID, Level, FirstKey, LastKey, FileSize, NumKeys}`, `EntryType{TypeValue=1, TypeDeletion=2}`
- [x] 🔴 `InternalKey.Less(b)`: sort by UserKey ASC, then SeqNo DESC (newer first)
- [x] 🔴 `internal/events/bus.go`: non-blocking `EventBus.Publish(Event)`, `Subscribe`, `Unsubscribe`
- [x] 🔴 `config.yaml` with all defaults from SPEC §19
- [x] 🔴 `cmd/server/main.go`: parse config, init engine, start gateway
- [x] 🔴 `Makefile` targets: `run`, `test`, `test-race`, `bench`, `lint`
- [x] 🔴 `cd frontend && bun init`; add `elysia`, `@elysiajs/eden`, `d3`, TypeScript
- [x] 🔴 `frontend/server/bff.ts`: Elysia BFF (SPEC §17)

---

## Phase 1 — WAL: Write-Ahead Log (20 tasks)

- [x] 🔴 `internal/wal/record.go`: `RecordFull=1`, `RecordFirst=2`, `RecordMiddle=3`, `RecordLast=4` constants
- [x] 🔴 `WALEntryType`: `EntrySet=1`, `EntryDelete=2`, `EntryFlush=3`
- [x] 🔴 Binary entry encoding: `[type:1][keyLen:4][key][valLen:4][val][seqNo:8]` (little-endian)
- [x] 🔴 `internal/wal/wal.go`: `WAL{file, bufWriter, blockPos int, seqNo atomic.Uint64}`
- [x] 🔴 `WAL.Append(entry WALEntry) error`: encode → fragment into 32KB blocks → write header+payload per fragment
- [x] 🔴 Record header write: `[CRC32:4][Length:2][Type:1]` — CRC computed over payload only
- [x] 🔴 Block boundary handling: pad with zero bytes when less than HeaderSize remains in current block; reset blockPos
- [x] 🔴 `WAL.Sync() error`: `bufWriter.Flush()` then `file.Sync()` (fsync)
- [x] 🔴 `internal/wal/reader.go`: `RecoverWAL(path string) ([]WALEntry, error)`
- [x] 🔴 Recovery: reassemble fragmented records; verify CRC32 per fragment; skip last truncated record
- [x] 🔴 Emit `EvtWALAppend` on every write; `EvtWALSync` on every fsync
- [x] 🔴 Unit test `TestWAL_SingleRecord`: write 1 entry; read back exact bytes
- [x] 🔴 Unit test `TestWAL_MultipleRecords`: write 100 entries; recover all in order
- [x] 🔴 Unit test `TestWAL_LargeRecord`: entry > 32KB; fragments across multiple blocks
- [x] 🔴 Unit test `TestWAL_CRCDetectsCorruption`: corrupt 1 byte of payload; recovery skips corrupt record
- [x] 🔴 Unit test `TestWAL_TruncatedRecord`: truncate last record by 5 bytes; recovery returns N-1 valid records
- [x] 🔴 Unit test `TestWAL_BlockBoundaryPadding`: write entries that exactly fill 32KB block; next entry starts clean block
- [x] 🟡 Unit test `TestWAL_SeqNoMonotonic`: 1000 writes; recovered entries have strictly increasing seqNos
- [x] 🟡 Benchmark `BenchmarkWAL_Append`: target > 1M ops/s without fsync; > 50k ops/s with fsync
- [x] 🔴 `go test ./internal/wal/... -race` zero races

---

## Phase 2 — MemTable: Skip List (18 tasks)

- [x] 🔴 `internal/memtable/skiplist.go`: `MaxLevel=12`, `Probability=0.25`, `SkipListNode{key InternalKey, value []byte, forward []*SkipListNode}`
- [x] 🔴 `randomLevel() int`: geometric distribution; each level added with probability P
- [x] 🔴 `SkipList.Insert(key InternalKey, value []byte)`: O(log n) insertion maintaining sort order
- [x] 🔴 `SkipList.Find(key InternalKey) *SkipListNode`: O(log n) search; returns node with highest seqNo <= query seqNo
- [x] 🔴 `SkipList.NewIterator() *SkipListIterator`: forward-only iterator in sorted order
- [x] 🔴 `internal/memtable/memtable.go`: `MemTable{sl, size int64, maxSize int64, walSeqNo uint64}`
- [x] 🔴 `MemTable.Put(key, value []byte, seqNo uint64)`: insert with `TypeValue`; update size
- [x] 🔴 `MemTable.Delete(key []byte, seqNo uint64)`: insert with `TypeDeletion` (tombstone)
- [x] 🔴 `MemTable.Get(key []byte) (value []byte, found bool, deleted bool)`: find highest seqNo entry; return deleted=true if tombstone
- [x] 🔴 `MemTable.ApproximateSize() int64`; `MemTable.IsFull() bool`
- [x] 🔴 `MemTable.NewIterator() Iterator`: yields entries in sorted InternalKey order
- [x] 🔴 Unit test `TestSkipList_Insert`: insert 10k keys in random order; iterate and verify sorted
- [x] 🔴 Unit test `TestSkipList_Find`: exact key lookup, prefix lookup, missing key
- [x] 🔴 Unit test `TestMemTable_Tombstone`: Put "k1"; Delete "k1"; Get → deleted=true; older SSTable version invisible
- [x] 🔴 Unit test `TestMemTable_MultipleVersions`: Put "k1" seqNo=1; Put "k1" seqNo=5; Get → returns seqNo=5 value
- [x] 🔴 Unit test `TestMemTable_IteratorOrder`: 1000 random puts; iterate → verify strictly ascending UserKey order
- [x] 🟡 Unit test `TestMemTable_SizeAccounting`: put N fixed-size keys; verify ApproximateSize ≈ N*(keySize+valueSize)
- [x] 🔴 `go test ./internal/memtable/... -race` zero races

---

## Phase 3 — Bloom Filter (12 tasks)

- [x] 🔴 `internal/bloom/bloom.go`: `BloomFilter{bits []byte, numHashFuncs int}`
- [x] 🔴 `bloomHash(key []byte) uint32`: murmur-style hash (seed=0xbc9f1d34)
- [x] 🔴 `NewBloomFilter(keys [][]byte, bitsPerKey int) *BloomFilter`: compute optimal k; set k bit positions per key using single-hash derivation (`delta = (h >> 17) | (h << 15)`)
- [x] 🔴 Store k value as last byte of `bits` slice (LevelDB convention)
- [x] 🔴 `BloomFilter.MayContain(key []byte) bool`: check k bit positions; any 0 → false; all 1 → true
- [x] 🔴 `BloomFilter.Serialize() []byte` and `DeserializeBloomFilter(data []byte) *BloomFilter`
- [x] 🔴 Unit test `TestBloom_NoFalseNegatives`: add 10k keys; check all → MayContain==true for all
- [x] 🔴 Unit test `TestBloom_FalsePositiveRate`: add 10k keys; check 10k different keys; FP rate < 2% for bpk=10
- [x] 🔴 Unit test `TestBloom_EmptyFilter`: empty filter → MayContain always false
- [x] 🔴 Unit test `TestBloom_SerializeDeserialize`: round-trip; same answers before/after
- [x] 🟡 Unit test `TestBloom_BitsPerKeyScaling`: bpk=6 → FP≈5%; bpk=10 → FP≈1%; bpk=14 → FP≈0.3%
- [x] 🔴 `go test ./internal/bloom/... -race` zero races

---

## Phase 4 — SSTable: Builder + Reader (22 tasks)

**Builder**
- [x] 🔴 `internal/sstable/block_builder.go`: `BlockBuilder{buf, restarts []int, restartInterval=16}`
- [x] 🔴 Prefix compression: `sharedKeyLen + unsharedKeyLen + valueLen + unsharedKey + value` (varint encoding)
- [x] 🔴 Restart points: every 16 entries, store byte offset as restart point (enables binary search)
- [x] 🔴 `BlockBuilder.Add(key InternalKey, value []byte) error`
- [x] 🔴 `BlockBuilder.Finish() []byte`: append restart offsets + numRestarts (little-endian 4-byte each)
- [x] 🔴 `internal/sstable/builder.go`: `SSTableBuilder{dataBlock, indexBlock, filterBuilder, file, blockSize=4096}`
- [x] 🔴 `SSTableBuilder.Add(key InternalKey, value []byte) error`: add to data block; add key to Bloom; flush data block if >= blockSize; add to index block
- [x] 🔴 Index block entry format: `lastKeyInBlock → BlockHandle` (one entry per data block)
- [x] 🔴 `SSTableBuilder.Finish() (SSTableMeta, error)`: flush final data block; write filter block; write index block; write footer; fsync

**Reader**
- [x] 🔴 `internal/sstable/reader.go`: `SSTableReader{file, indexBlock *Block, bloomFilter *BloomFilter, meta SSTableMeta, cache *BlockCache}`
- [x] 🔴 `NewSSTableReader(path string, meta SSTableMeta, cache *BlockCache) (*SSTableReader, error)`: read footer; load index block; load filter block
- [x] 🔴 `SSTableReader.Get(key []byte, seqNo uint64) ([]byte, bool, error)`:
  1. Bloom filter check (emit EvtBloomCheck/EvtBloomHit/EvtBloomMiss)
  2. Binary search index block → BlockHandle
  3. Check cache; load data block if miss (emit EvtCacheHit/EvtCacheMiss)
  4. Binary search within data block using restart points
  5. Return newest version with seqNo <= query seqNo
- [x] 🔴 `SSTableReader.NewIterator() *SSTableIterator`: iterate data blocks sequentially
- [x] 🟡 Varint encode/decode (`encodeVarint`, `decodeVarint`) utility functions

**Tests**
- [x] 🔴 Unit test `TestSSTable_BuildAndReadBack`: build SSTable with 1000 keys; read each back; verify values
- [x] 🔴 Unit test `TestSSTable_BloomShortCircuit`: query keys NOT in SSTable; all skipped by Bloom (zero disk reads)
- [x] 🔴 Unit test `TestSSTable_PrefixCompression`: write 1000 keys with common prefix "user:"; verify file size < naive encoding
- [x] 🔴 Unit test `TestSSTable_TombstonePreservation`: write key with TypeDeletion; read back → deleted=true
- [x] 🔴 Unit test `TestSSTable_Iterator`: write 500 keys; iterate all; verify sorted order
- [x] 🟡 Unit test `TestSSTable_RestartBinarySearch`: verify binary search on restart points returns correct entries
- [x] 🔴 `go test ./internal/sstable/... -race` zero races

---

## Phase 5 — Block Cache (6 tasks)

- [x] 🔴 `internal/cache/lru.go`: `BlockCache{mu, capacity, used, lru *list.List, index map[CacheKey]*list.Element}`
- [x] 🔴 `BlockCache.Get(key CacheKey) (*Block, bool)`: move to front on hit; emit EvtCacheHit/Miss
- [x] 🔴 `BlockCache.Insert(key CacheKey, block *Block)`: evict LRU if over capacity
- [x] 🔴 `BlockCache.HitRate() float64`: hits/(hits+misses)
- [x] 🔴 Unit test `TestLRU_Eviction`: insert capacity+1 items; verify LRU item evicted
- [x] 🔴 Unit test `TestLRU_HitRate`: 1000 ops, 70% hit expected; assert HitRate() > 0.65
- [x] 🔴 `go test ./internal/cache/... -race` zero races

---

## Phase 6 — MANIFEST File (10 tasks)

- [x] 🔴 `internal/manifest/version_edit.go`: binary encoding for `VersionEdit{Type, Level, FileID, FileSize, FirstKey, LastKey, Deleted}`
- [x] 🔴 `EncodeVersionEdit(edit VersionEdit) []byte` + `DecodeVersionEdit(data []byte) (VersionEdit, error)`
- [x] 🔴 `internal/manifest/manifest.go`: `Manifest{mu, file, writer, current *Version}`
- [x] 🔴 `Version{Levels [7][]*SSTableMeta, nextFileID, logNumber}` — in-memory representation
- [x] 🔴 `Manifest.Apply(edit VersionEdit) error`: validate; append to file; fsync; update current Version
- [x] 🔴 L1+ overlap validation in `Apply`: EditAddSSTable at L>=1 must not overlap existing SSTables in same level
- [x] 🔴 `Manifest.Recover(path string) (*Version, error)`: replay all VersionEdits from file; rebuild Version
- [x] 🔴 Unit test `TestManifest_AddAndRecover`: add 10 SSTables across 3 levels; close; recover; verify all present
- [x] 🔴 Unit test `TestManifest_DeleteAndRecover`: add 5; delete 2; recover; verify 3 remain
- [x] 🟡 Unit test `TestManifest_L1OverlapRejected`: adding overlapping SSTable at L1 returns error

---

## Phase 7 — Flush Worker (8 tasks)

- [x] 🔴 `internal/engine/flush.go`: `FlushWorker` goroutine watching `immutableQueue chan *MemTable`
- [x] 🔴 Flush algorithm: iterate MemTable → `SSTableBuilder.Add` each entry → `Finish()` → `Manifest.Apply(EditAddSSTable)`
- [x] 🔴 WAL cleanup: after successful flush, delete WAL file(s) covered by flushed MemTable
- [x] 🔴 Atomicity: crash between SSTable write and MANIFEST update → orphaned SSTable detected on recovery and deleted
- [x] 🔴 Emit `EvtFlushStart`, `EvtFlushComplete`, `EvtSSTableCreated`, `EvtWALAppend(EntryFlush marker)`
- [x] 🔴 Trigger: `engine.Put()` checks `memTable.IsFull()` → rotate (mutable→immutable) → signal FlushWorker
- [x] 🔴 Write stall: if `len(immutableQueue) >= maxImmutable`, Put blocks until queue drains
- [x] 🔴 Integration test `TestFlush_MemTableToL0`: write 5MB of data; verify L0 has >= 1 SSTable; data readable

---

## Phase 8 — Read Path + Range Scan (12 tasks)

- [x] 🔴 `internal/engine/engine.go`: `LSMEngine.Get(key []byte) ([]byte, error)` — full read path (SPEC §9)
- [x] 🔴 Check mutable MemTable → immutable MemTables (newest first) → L0 SSTables (newest first) → L1..L6 (binary search)
- [x] 🔴 Emit `EvtReadStart`, `EvtReadMemTable`, `EvtReadSSTable`, `EvtBloomMiss/Hit/FP`, `EvtReadComplete` with components checked
- [x] 🔴 `internal/compaction/iterator.go`: `MergeIterator{heap []*leveledEntry}` — k-way merge using min-heap
- [x] 🔴 `MergeIterator.Next()`: advance heap; skip duplicate UserKeys (keep highest SeqNo version)
- [x] 🔴 `MergeIterator.Valid() bool`, `Key() InternalKey`, `Value() []byte`, `SeekToFirst()`, `Seek(key []byte)`
- [x] 🔴 `LSMEngine.Scan(start, end []byte, seqNo uint64) Iterator`: create MergeIterator over all components
- [x] 🔴 Scan tombstone handling: when tombstone encountered in merge iterator, skip all older versions of same key
- [x] 🔴 Unit test `TestGet_MemTable`: put in memtable; get → found without disk access
- [x] 🔴 Unit test `TestGet_SSTableWithBloom`: put 1000 keys; flush; get existing → found; get non-existing → bloom skips
- [x] 🔴 Unit test `TestScan_AcrossLevels`: put 10k keys across multiple flush cycles; scan [a, z]; verify all returned sorted
- [x] 🔴 Unit test `TestScan_TombstoneHidden`: put "k1"; delete "k1"; scan → k1 not in results

---

## Phase 9 — Crash Recovery (10 tasks)

- [x] 🔴 `internal/engine/engine.go`: `LSMEngine.Open()` recovery sequence (SPEC §12)
- [x] 🔴 Read CURRENT file → MANIFEST path → replay MANIFEST → reconstruct Version
- [x] 🔴 Identify WAL file(s) from Version.logNumber; replay via `RecoverWAL()`
- [x] 🔴 Orphaned SSTable cleanup: files on disk not in MANIFEST → `os.Remove()`
- [x] 🔴 `Open()` must handle all crash scenarios: crash during flush, crash during compaction, crash mid-WAL-write
- [x] 🔴 Integration test `TestCrashRecovery_MidWrite`: write 500 keys; `os.Exit(1)` simulation (cancel contexts); reopen; all 500 keys readable
- [x] 🔴 Integration test `TestCrashRecovery_DuringFlush`: crash between SSTable write and MANIFEST update; reopen; data recovered from WAL
- [x] 🔴 Integration test `TestCrashRecovery_DuringCompaction`: crash mid-compaction; reopen; no data loss; no duplicate keys
- [x] 🟡 Integration test `TestCrashRecovery_CorruptWAL`: manually corrupt last 10 bytes of WAL; reopen; all-but-last-record recovered
- [x] 🔴 `go test ./test/integration/... -race` zero races

---

## Phase 10 — Compaction: Leveled (LCS) (20 tasks)

- [x] 🔴 `internal/compaction/leveled.go`: `LeveledCompactor{engine}`
- [x] 🔴 L0 trigger: `len(Levels[0]) >= MaxL0SSTables` → compact all L0 into L1
- [x] 🔴 L1+ trigger: `sum(Levels[i].fileSize) > levelMaxSize(i)` → pick one SSTable, compact with overlapping L(i+1)
- [x] 🔴 `PickCompaction() *CompactionTask`: find most-over-limit level; pick SSTable (round-robin within level)
- [x] 🔴 Find overlapping SSTables in L(i+1) by key range intersection
- [x] 🔴 `CompactionTask{InputLevel, InputSSTables, OutputLevel, OutputSSTables}`
- [x] 🔴 `LeveledCompactor.Execute(task)`: create merge iterator over all input SSTables; write output SSTables
- [x] 🔴 Deduplication: same UserKey in merge iterator → keep highest SeqNo only
- [x] 🔴 Tombstone GC: if `task.OutputLevel == maxLevel AND key is tombstone` → drop (emit `EvtTombstoneDropped`)
- [x] 🔴 Emit `EvtCompactionStart`, `EvtCompactionPick{inputs}`, `EvtCompactionMerge` per key, `EvtCompactionComplete`
- [x] 🔴 Atomic commit: (1) fsync output SSTables; (2) MANIFEST apply; (3) delete input files
- [x] 🔴 Crash safety: if crash between (1) and (2) → orphaned SSTables cleaned on recovery
- [x] 🔴 Concurrent reads during compaction: readers keep references to old SSTable readers; compactor uses MVCC-like handoff
- [x] 🔴 Background compaction goroutine: `internal/engine/background.go`; triggered after every flush; respects context cancellation
- [x] 🔴 Write stall at L0: if `len(L0) >= MaxL0SSTables * 2`, stall writes until L0 compacted
- [x] 🔴 Integration test `TestLCS_CorrectAfterCompaction`: write 100k keys with updates; compact fully; verify only latest version present
- [x] 🔴 Integration test `TestLCS_NonOverlappingL1`: after compaction, L1 SSTables have non-overlapping key ranges
- [x] 🔴 Integration test `TestLCS_TombstoneGC`: write 50k keys; delete all; compact to bottom; verify no data on disk
- [x] 🟡 Integration test `TestLCS_WriteAmplification`: measure WA for sequential workload; verify WA < 35×
- [x] 🔴 `go test ./internal/compaction/... -race` zero races

---

## Phase 11 — Compaction: STCS + TWCS (8 tasks)

- [x] 🟡 `internal/compaction/stcs.go`: `SizeTieredCompactor{minThreshold=4, bucketLow=0.5, bucketHigh=1.5}`
- [x] 🟡 `GroupIntoBuckets(sstables)`: group by size; files within [avg×0.5, avg×1.5] go in same bucket
- [x] 🟡 Trigger: any bucket has >= minThreshold members → merge all into one larger SSTable
- [x] 🟡 `internal/compaction/twcs.go`: `TimeWindowCompactor{windowSize time.Duration}`
- [x] 🟡 Assign SSTable to window by `createdAt` timestamp (stored in SSTableMeta)
- [x] 🟡 Compact within window using STCS; never compact across windows
- [x] 🟡 Integration test `TestSTCS_WriteAmplification`: verify STCS WA < LCS WA for write-heavy workload
- [x] 🟡 `engine.SetCompactionStyle(style string) error`: hot-switch between LCS/STCS without restart

---

## Phase 12 — Engine API + Gateway (14 tasks)

- [x] 🔴 `LSMEngine.Put(key, value []byte) error`: (1) WAL.Append; (2) WAL.Sync if sync_wal=true; (3) MemTable.Put; (4) check if full → rotate
- [x] 🔴 `LSMEngine.Delete(key []byte) error`: same as Put but with TypeDeletion
- [x] 🔴 `LSMEngine.Write(batch *WriteBatch) error`: one WAL append for entire batch; then apply all to MemTable atomically
- [x] 🔴 `LSMEngine.Close() error`: flush remaining immutable MemTables; fsync all files; stop background workers
- [x] 🔴 `LSMEngine.Stats() *EngineStats`: collect WA/RA/SA, cache hit rate, level sizes, compaction stats
- [x] 🔴 `gateway/rest.go`: all 25+ REST handlers from SPEC §15
- [x] 🔴 `gateway/websocket.go`: Gorilla WS upgrade; subscribe to EventBus; send events; filter by type
- [x] 🔴 `POST /db/put`, `GET /db/get`, `DELETE /db/delete`, `GET /db/scan`
- [x] 🔴 `POST /bench/run`: run workload in background goroutine; stream events over WS
- [x] 🔴 `GET /levels`: return all levels with SSTable list, key ranges, file sizes
- [x] 🔴 `GET /bloom/:fileID`: return bloom filter stats (bits_per_key, num_keys, estimated FP rate)
- [x] 🔴 `GET /amplification`: current WA/RA/SA live values
- [x] 🟡 `POST /compaction/force`: trigger immediate compaction at specified level
- [x] 🟡 `POST /compaction/style`: change strategy on the fly

---

## Phase 13 — Simulation & Benchmark Engine (10 tasks)

- [x] 🟡 `internal/simulation/workload.go`: `SequentialWrite`, `RandomWrite`, `ZipfRead`, `MixedWorkload`, `CompactionStress`, `PointDelete` generators
- [x] 🟡 `ZipfGenerator`: uses Zipf distribution (skew=1.0 = heavy; skew=0.5 = moderate)
- [x] 🟡 `LatencyHistogram`: p50, p95, p99, p999 nanosecond precision
- [x] 🟡 `internal/simulation/amplification.go`: live WA/RA/SA measurement; emit `EvtAmplification` every second
- [x] 🟡 `internal/simulation/scenarios.go`: 8 named scenarios (SPEC §13)
- [x] 🟡 Scenario `"write_flush"`: 5MB sequential writes; pause at each SSTable flush; emit step events
- [x] 🟡 Scenario `"bloom_demo"`: insert 10k keys; query 10k missing keys; count FP; emit per-query bloom event
- [x] 🟡 Scenario `"crash_recovery"`: write 500 keys; simulate crash; recover; verify all present
- [x] 🟡 Scenario `"amplification"`: run same workload with LCS then STCS; emit WA/RA/SA comparison
- [x] 🟡 Scenario `"tombstone_gc"`: write 1k keys; delete all; compact to bottom; verify zero live keys

---

## Phase 14 — Elysia BFF + TypeScript Types (6 tasks)

- [x] 🔴 `frontend/server/bff.ts`: complete REST proxy + WS fan-out (SPEC §17)
- [x] 🔴 Eden Treaty export: `export type App = typeof app`
- [x] 🔴 `frontend/src/api/client.ts`: `treaty<App>("localhost:3001")`
- [x] 🔴 `frontend/src/api/types.ts`: TypeScript interfaces for all 30 event types + EngineStats + SSTableMeta + LevelInfo + BloomStats
- [x] 🔴 `frontend/src/ws/client.ts`: WS connection + typed event dispatcher + exponential backoff reconnect
- [x] 🟡 `GET /health` route

---

## Phase 15 — Frontend Panel 1: Write Path Visualizer (12 tasks)

- [x] 🔴 `frontend/src/store/engine.ts`: reactive state; update on WAL/MemTable/Flush events
- [x] 🔴 `components/write_path/diagram.ts`: animated SVG flow: Client → WAL → MemTable → Flush → L0
- [x] 🔴 WAL block: scrolling last-10 entries log; each row shows `[type][key][seqNo][CRC]`
- [x] 🔴 MemTable skip-list visualization: D3 force or manual layout; nodes = skip list nodes; horizontal lines = key positions; vertical pointers = level connections
- [x] 🔴 MemTable size bar: current/max; fill animation on each write
- [x] 🟡 Flush animation: MemTable box → animated arrow → new SSTable box appears in L0
- [x] 🟡 Write stats: ops/sec, bytes/sec, current seqNo counter
- [x] 🟡 On `EvtMemTableFull`: flash red border; "rotating…" animation
- [x] 🟡 On `EvtFlushStart`: MemTable box dims; copy floats toward disk
- [x] 🟢 Prefix compression visualization in data block: shared prefix shown in gray; unique suffix in color
- [x] 🟢 Block boundary indicator: show when 4KB threshold triggers new data block
- [x] 🔴 Live: all state driven by WebSocket events; no polling

---

## Phase 16 — Frontend Panel 2: LSM Level Tree (14 tasks)

- [x] 🔴 `components/level_tree/tree.ts`: D3 layered diagram; L0–L6 horizontal rows
- [x] 🔴 Each SSTable = colored rectangle; width proportional to file size; positioned on key-range axis (0–255 for byte keys)
- [x] 🔴 L0: rectangles may overlap (shown as stacked/overlapping); L1+: tiled non-overlapping on axis
- [x] 🔴 SSTable color: hue = age (newer = brighter blue; older = darker gray)
- [x] 🔴 Level size bar: actual size / max size per level; turns orange at 80%, red at 100%
- [x] 🟡 Compaction animation: input SSTables flash → animated arrows merge into single → output SSTable grows
- [x] 🟡 On `EvtSSTableCreated`: new rectangle slides in from left
- [x] 🟡 On `EvtSSTableDeleted`: rectangle fades out and collapses
- [x] 🟡 Click SSTable: tooltip shows `{FileID, Level, KeyRange, FileSize, NumKeys, Age}`
- [x] 🟡 Bloom filter indicator: small colored dot on SSTable; green = filter loaded, gray = not loaded
- [x] 🟡 Compaction pick highlight: selected input SSTable(s) outlined in yellow; overlapping targets outlined in orange
- [x] 🟢 Zoom on individual level for large SSTable counts
- [x] 🟢 Key range tooltip: hover shows first/last key in human-readable form
- [x] 🔴 Live: driven by `EvtSSTableCreated`, `EvtSSTableDeleted`, `EvtCompactionStart/Complete`

---

## Phase 17 — Frontend Panel 3: Bloom Filter (10 tasks)

- [x] 🔴 `components/bloom/filter.ts`: bit array visualization (512 cells max; color by set/unset)
- [x] 🔴 "Add Key" input → animate k=7 hash positions being set (flash cells sequentially)
- [x] 🔴 "Query Key" input → animate checking k positions → show "MAYBE" (green) or "DEFINITELY NOT" (red)
- [x] 🟡 False positive demo: run 1000 non-member queries automatically; show FP count + rate
- [x] 🟡 Bits-per-key slider (6–20) → live formula: `k = bpk × ln(2)`, `p = (1 - e^(-kn/m))^k`; update display
- [x] 🟡 Hash function visualization: show 7 independent hash values and which bit positions they map to
- [x] 🟡 Per-SSTable bloom stats table: FileID, NumKeys, BitsPerKey, FalsePositiveRate, SerializedSize
- [x] 🟢 Animate real queries from engine: on `EvtBloomCheck` event, light up the relevant SSTable's filter animation
- [x] 🟢 Side-by-side comparison: no bloom vs with bloom; count disk reads saved per 1000 queries
- [x] 🔴 All animations driven by WebSocket events + user input

---

## Phase 18 — Frontend Panels 4–7 (22 tasks)

**Panel 4 — Read Path Tracer**
- [x] 🔴 On `EvtReadStart`: show key being queried
- [x] 🔴 Per component checked: animated check mark (found) or X (not found); bloom skips shown in gray with "🚫"
- [x] 🔴 Show "Disk reads: N", "Bloom skips: M", "Levels checked: K"
- [x] 🟡 Scroll trace for last 10 queries; click any to replay animation
- [x] 🟢 Side-by-side: same key with vs without bloom filters → disk read comparison

**Panel 5 — Compaction Simulation**
- [x] 🟡 Compaction style radio: LCS / STCS / TWCS; changing style calls `POST /compaction/style`
- [x] 🟡 "Step through compaction" mode: one Raft tick per click; shows current merge state
- [x] 🟡 Merge iterator view: running list of key-value pairs as they emerge from k-way merge
- [x] 🟡 Tombstone GC indicator: tombstone entries highlighted in red; "DROPPED" vs "KEPT" label
- [x] 🟡 MANIFEST edit display: shows exact VersionEdit being written
- [x] 🟢 After compaction: animate level size bars updating; amplification stats update

**Panel 6 — Amplification Dashboard**
- [x] 🟡 Three gauge charts: WA / RA / SA (using D3 arc); live-updated from `EvtAmplification`
- [x] 🟡 Line chart: WA/RA/SA over time (last 60 seconds)
- [x] 🟡 LCS vs STCS comparison toggle: run same workload with both; overlay graphs
- [x] 🟡 Per-operation read count histogram: p50/p99/p999 disk reads per query

**Panel 7 — Scenario Control + WAL Viewer**
- [x] 🔴 Scenario dropdown from `GET /scenarios`; Run + Speed + Step Mode + Reset
- [x] 🔴 WAL viewer: scrolling log; PUT=green, DELETE=orange, FLUSH=blue, CORRUPT=red
- [x] 🔴 Crash recovery simulator: "Simulate Crash" → clears memory state; "Recover" → shows step-by-step replay
- [x] 🟡 Recovery progress bar: "Replaying WAL: N/M entries" with current key being restored shown

---

## Phase 19 — Integration Tests + Polish (14 tasks)

**Correctness**
- [x] 🔴 `TestReadWriteCorrectness_1M`: 1M sequential puts; 1M random gets; all values correct
- [x] 🔴 `TestConcurrentReadWrite`: 8 writers + 8 readers; no panics, no stale reads
- [x] 🔴 `TestCompactionCorrectness`: 10 rounds of write+compact; verify only latest version per key
- [x] 🔴 `TestTombstoneNotResurrected`: delete 10k keys; compact; get each → ErrNotFound

**Performance**
- [x] 🟡 `BenchmarkSequentialWrite`: target > 200k ops/s (no fsync)
- [x] 🟡 `BenchmarkPointRead_WarmCache`: target < 100µs avg
- [x] 🟡 `BenchmarkPointRead_NoBloom_vs_Bloom`: bloom filter reduces disk reads by > 60%

**Race detection**
- [x] 🔴 `go test ./... -race -count=3` zero data races

**Code quality**
- [x] 🟡 `golangci-lint run` zero errors
- [x] 🟡 TypeScript strict mode; `bun run tsc --noEmit` zero errors
- [x] 🟡 All exported functions have godoc comments

**Documentation**
- [x] 🟢 `README.md`: setup (Go 1.22), `make run`, panel descriptions, config parameters
- [x] 🟢 WAL block format diagram (ASCII art)
- [x] 🟢 SSTable file format diagram (ASCII art)

---

## Summary Table

| Phase | Tasks |
|-------|-------|
| 0. Bootstrap | 10 |
| 1. WAL | 20 |
| 2. MemTable (Skip List) | 18 |
| 3. Bloom Filter | 12 |
| 4. SSTable Builder + Reader | 22 |
| 5. Block Cache | 6 |
| 6. MANIFEST File | 10 |
| 7. Flush Worker | 8 |
| 8. Read Path + Range Scan | 12 |
| 9. Crash Recovery | 10 |
| 10. Compaction: LCS | 20 |
| 11. Compaction: STCS + TWCS | 8 |
| 12. Engine API + Gateway | 14 |
| 13. Simulation & Benchmarks | 10 |
| 14. Elysia BFF + TypeScript | 6 |
| 15. Frontend Panel 1 (Write Path) | 12 |
| 16. Frontend Panel 2 (Level Tree) | 14 |
| 17. Frontend Panel 3 (Bloom) | 10 |
| 18. Frontend Panels 4–7 | 22 |
| 19. Integration Tests + Polish | 14 |
| **TOTAL** | **238** |
