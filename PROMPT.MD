# CLAUDE CODE PROMPT — LSM-Tree Storage Engine

## Project Mission

Build a from-scratch LSM-Tree storage engine in Go with:
- WAL (Write-Ahead Log): 32KB block format, CRC32 checksums, record fragmentation
- MemTable: skip-list implementation, tombstone markers, mutable/immutable rotation
- SSTable: data blocks with prefix compression + restart points, Bloom filter block, index block, footer
- Bloom filters: k-hash bit-array, LevelDB-style single-hash derivation, serialization
- Leveled compaction (LCS) + Size-Tiered (STCS) + Time-Window (TWCS)
- MANIFEST file: atomic VersionEdit log, crash recovery
- Block cache: LRU for hot data blocks
- Live D3.js visualization of every component fed over WebSocket

**Read `SPEC.md` and `CHECKLIST.md` before writing any code.**
**Build phases in strict order. Each phase's tests must pass before the next begins.**
**Zero external database dependencies — only stdlib + testify.**

---

## Phase 0 — Bootstrap

```bash
go mod init lsm-engine
go get github.com/gorilla/websocket github.com/stretchr/testify/assert
cd frontend && bun init
bun add elysia @elysiajs/eden d3
bun add -d typescript @types/d3
```

### Internal Key Format (must be right before everything else)

```go
// internal/sstable/format.go
package sstable

import "bytes"

type EntryType uint8
const (
    TypeValue    EntryType = 1
    TypeDeletion EntryType = 2  // tombstone
)

// InternalKey: composite key used everywhere in the engine
// Sort order: UserKey ASC, then SeqNo DESC (newer = lower sort position = found first)
type InternalKey struct {
    UserKey []byte
    SeqNo   uint64
    Type    EntryType
}

func (a InternalKey) Less(b InternalKey) bool {
    cmp := bytes.Compare(a.UserKey, b.UserKey)
    if cmp != 0 { return cmp < 0 }
    return a.SeqNo > b.SeqNo // CRITICAL: higher SeqNo sorts FIRST
}

func (a InternalKey) Equal(b InternalKey) bool {
    return bytes.Equal(a.UserKey, b.UserKey) && a.SeqNo == b.SeqNo
}

type BlockHandle struct {
    Offset uint64
    Size   uint64
}

type SSTableMeta struct {
    FileID   uint64
    Level    int
    FirstKey []byte
    LastKey  []byte
    FileSize uint64
    NumKeys  uint64
    FilePath string
}
```

---

## Phase 1 — WAL

### WAL Writer (exact block format)

```go
// internal/wal/wal.go
package wal

import (
    "bufio"
    "encoding/binary"
    "hash/crc32"
    "os"
    "sync"
    "sync/atomic"
)

const (
    BlockSize   = 32 * 1024  // 32KB
    HeaderSize  = 7          // CRC(4) + Length(2) + Type(1)
    RecordFull  = uint8(1)
    RecordFirst = uint8(2)
    RecordMiddle= uint8(3)
    RecordLast  = uint8(4)
)

type EntryType uint8
const (
    EntrySet    EntryType = 1
    EntryDelete EntryType = 2
    EntryFlush  EntryType = 3
)

type WALEntry struct {
    Type  EntryType
    Key   []byte
    Value []byte
    SeqNo uint64
}

// Wire format: [type:1][keyLen:4LE][key][valLen:4LE][val][seqNo:8LE]
func EncodeEntry(e WALEntry) []byte {
    keyLen := len(e.Key)
    valLen := len(e.Value)
    buf := make([]byte, 1+4+keyLen+4+valLen+8)
    buf[0] = byte(e.Type)
    binary.LittleEndian.PutUint32(buf[1:], uint32(keyLen))
    copy(buf[5:], e.Key)
    binary.LittleEndian.PutUint32(buf[5+keyLen:], uint32(valLen))
    copy(buf[9+keyLen:], e.Value)
    binary.LittleEndian.PutUint64(buf[9+keyLen+valLen:], e.SeqNo)
    return buf
}

func DecodeEntry(data []byte) (WALEntry, error) {
    if len(data) < 9 { return WALEntry{}, ErrCorrupt }
    entryType := EntryType(data[0])
    keyLen := int(binary.LittleEndian.Uint32(data[1:]))
    if len(data) < 5+keyLen+4 { return WALEntry{}, ErrCorrupt }
    key := make([]byte, keyLen)
    copy(key, data[5:])
    valLen := int(binary.LittleEndian.Uint32(data[5+keyLen:]))
    if len(data) < 9+keyLen+valLen+8 { return WALEntry{}, ErrCorrupt }
    val := make([]byte, valLen)
    copy(val, data[9+keyLen:])
    seqNo := binary.LittleEndian.Uint64(data[9+keyLen+valLen:])
    return WALEntry{Type: entryType, Key: key, Value: val, SeqNo: seqNo}, nil
}

type WAL struct {
    mu       sync.Mutex
    file     *os.File
    buf      *bufio.Writer
    blockPos int    // current position within current 32KB block
    seqNo    atomic.Uint64
    bus      EventPublisher
}

func OpenWAL(path string, bus EventPublisher) (*WAL, error) {
    f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil { return nil, err }
    return &WAL{file: f, buf: bufio.NewWriterSize(f, BlockSize), bus: bus}, nil
}

func (w *WAL) Append(entry WALEntry) error {
    w.mu.Lock()
    defer w.mu.Unlock()
    entry.SeqNo = w.seqNo.Add(1)
    payload := EncodeEntry(entry)
    if err := w.writeRecord(payload); err != nil { return err }
    w.bus.Publish(Event{Type: EvtWALAppend, Extra: map[string]interface{}{
        "type": entry.Type, "key": string(entry.Key), "seq": entry.SeqNo,
    }})
    return nil
}

func (w *WAL) writeRecord(data []byte) error {
    remaining := len(data)
    isFirst := true
    for remaining > 0 {
        // Available space in current block (excluding header)
        avail := BlockSize - w.blockPos - HeaderSize
        if avail < HeaderSize {
            // Pad remaining bytes in block with zeros
            zeros := make([]byte, BlockSize-w.blockPos)
            w.buf.Write(zeros)
            w.blockPos = 0
            avail = BlockSize - HeaderSize
        }
        chunkLen := remaining
        if chunkLen > avail { chunkLen = avail }
        chunk := data[len(data)-remaining : len(data)-remaining+chunkLen]

        var rType uint8
        isFinal := chunkLen == remaining
        switch {
        case isFirst && isFinal: rType = RecordFull
        case isFirst:            rType = RecordFirst
        case isFinal:            rType = RecordLast
        default:                 rType = RecordMiddle
        }

        crc := crc32.ChecksumIEEE(chunk)
        header := make([]byte, HeaderSize)
        binary.LittleEndian.PutUint32(header[0:], crc)
        binary.LittleEndian.PutUint16(header[4:], uint16(chunkLen))
        header[6] = rType
        w.buf.Write(header)
        w.buf.Write(chunk)
        w.blockPos += HeaderSize + chunkLen
        remaining -= chunkLen
        isFirst = false
    }
    return nil
}

func (w *WAL) Sync() error {
    w.mu.Lock()
    defer w.mu.Unlock()
    if err := w.buf.Flush(); err != nil { return err }
    if err := w.file.Sync(); err != nil { return err }
    w.bus.Publish(Event{Type: EvtWALSync})
    return nil
}
```

### WAL Recovery

```go
// internal/wal/reader.go
func RecoverWAL(path string) ([]WALEntry, error) {
    f, err := os.Open(path)
    if err != nil {
        if os.IsNotExist(err) { return nil, nil }
        return nil, err
    }
    defer f.Close()

    var entries []WALEntry
    buf := make([]byte, BlockSize)
    var pendingFragments []byte

    for {
        n, err := io.ReadFull(f, buf)
        if n == 0 || errors.Is(err, io.EOF) { break }
        block := buf[:n]
        pos := 0

        for pos < len(block)-HeaderSize {
            crcStored := binary.LittleEndian.Uint32(block[pos:])
            length := int(binary.LittleEndian.Uint16(block[pos+4:]))
            rType := block[pos+6]

            if length == 0 && rType == 0 { break } // padding

            payload := block[pos+HeaderSize : pos+HeaderSize+length]

            // Verify CRC
            crcComputed := crc32.ChecksumIEEE(payload)
            if crcComputed != crcStored {
                // Truncated/corrupt record — stop here (likely crash at this point)
                goto done
            }

            switch rType {
            case RecordFull:
                e, err := DecodeEntry(payload)
                if err == nil { entries = append(entries, e) }
                pendingFragments = nil
            case RecordFirst:
                pendingFragments = append([]byte{}, payload...)
            case RecordMiddle:
                pendingFragments = append(pendingFragments, payload...)
            case RecordLast:
                pendingFragments = append(pendingFragments, payload...)
                e, err := DecodeEntry(pendingFragments)
                if err == nil { entries = append(entries, e) }
                pendingFragments = nil
            }
            pos += HeaderSize + length
        }
    }
done:
    return entries, nil
}
```

**Tests that must pass before Phase 2:**

```go
func TestWAL_TruncatedRecord(t *testing.T) {
    dir := t.TempDir()
    w, _ := OpenWAL(filepath.Join(dir, "wal"), &noopBus{})
    for i := 0; i < 10; i++ {
        w.Append(WALEntry{Type: EntrySet, Key: []byte(fmt.Sprintf("k%d", i)), Value: []byte("v")})
    }
    w.Sync()
    // Truncate last 5 bytes (simulate crash mid-write)
    path := filepath.Join(dir, "wal")
    info, _ := os.Stat(path)
    os.Truncate(path, info.Size()-5)

    entries, err := RecoverWAL(path)
    require.NoError(t, err)
    assert.Equal(t, 9, len(entries)) // last entry truncated; 9 recovered
}

func TestWAL_CRCDetectsCorruption(t *testing.T) {
    dir := t.TempDir()
    w, _ := OpenWAL(filepath.Join(dir, "wal"), &noopBus{})
    w.Append(WALEntry{Type: EntrySet, Key: []byte("k1"), Value: []byte("v1")})
    w.Append(WALEntry{Type: EntrySet, Key: []byte("k2"), Value: []byte("v2")})
    w.Sync()

    // Corrupt byte 20 of the file (inside first record's payload)
    f, _ := os.OpenFile(filepath.Join(dir, "wal"), os.O_RDWR, 0)
    f.Seek(20, 0)
    f.Write([]byte{0xFF})
    f.Close()

    entries, _ := RecoverWAL(filepath.Join(dir, "wal"))
    // First record corrupt → only second may survive (if not in same block parse)
    assert.True(t, len(entries) <= 2)
}
```

---

## Phase 2 — MemTable (Skip List)

### Skip List (exact implementation)

```go
// internal/memtable/skiplist.go
const (
    MaxLevel    = 12
    Probability = 0.25
)

type SkipListNode struct {
    key     InternalKey
    value   []byte
    forward [MaxLevel]*SkipListNode
    level   int
}

type SkipList struct {
    head    *SkipListNode
    level   int
    length  int
    rng     *rand.Rand
}

func NewSkipList() *SkipList {
    head := &SkipListNode{level: MaxLevel}
    return &SkipList{head: head, level: 1, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (sl *SkipList) randomLevel() int {
    level := 1
    for level < MaxLevel && sl.rng.Float64() < Probability {
        level++
    }
    return level
}

func (sl *SkipList) Insert(key InternalKey, value []byte) {
    update := [MaxLevel]*SkipListNode{}
    x := sl.head
    for i := sl.level - 1; i >= 0; i-- {
        for x.forward[i] != nil && x.forward[i].key.Less(key) {
            x = x.forward[i]
        }
        update[i] = x
    }

    // Overwrite if exact same InternalKey exists
    x = x.forward[0]
    if x != nil && x.key.Equal(key) {
        x.value = value
        return
    }

    newLevel := sl.randomLevel()
    if newLevel > sl.level {
        for i := sl.level; i < newLevel; i++ { update[i] = sl.head }
        sl.level = newLevel
    }
    node := &SkipListNode{key: key, value: value, level: newLevel}
    for i := 0; i < newLevel; i++ {
        node.forward[i] = update[i].forward[i]
        update[i].forward[i] = node
    }
    sl.length++
}

// FindLE: find node with largest key <= searchKey
// For Get: search with key={userKey, MaxUint64, TypeValue} to find most recent version
func (sl *SkipList) FindLE(searchKey InternalKey) *SkipListNode {
    x := sl.head
    for i := sl.level - 1; i >= 0; i-- {
        for x.forward[i] != nil && x.forward[i].key.Less(searchKey) {
            x = x.forward[i]
        }
    }
    x = x.forward[0]
    if x == nil { return nil }
    // Return if same UserKey (regardless of SeqNo)
    if bytes.Equal(x.key.UserKey, searchKey.UserKey) { return x }
    return nil
}
```

**Critical test:**

```go
func TestMemTable_TombstoneVisibility(t *testing.T) {
    m := NewMemTable(64 * 1024 * 1024)
    m.Put([]byte("k1"), []byte("v1"), 1)
    m.Delete([]byte("k1"), 2) // tombstone at higher seqNo

    val, found, deleted := m.Get([]byte("k1"))
    assert.True(t, found)
    assert.True(t, deleted)
    assert.Nil(t, val)

    // Older seqNo should still find the value
    // (snapshot read - seqNo=1 query)
    val, found, deleted = m.GetAtSeqNo([]byte("k1"), 1)
    assert.True(t, found)
    assert.False(t, deleted)
    assert.Equal(t, []byte("v1"), val)
}
```

---

## Phase 3 — Bloom Filter

### LevelDB-style single-hash derivation

```go
// internal/bloom/bloom.go
import (
    "encoding/binary"
    "math"
)

func bloomHash(key []byte) uint32 {
    const (
        seed = uint32(0xbc9f1d34)
        m    = uint32(0xc6a4a793)
    )
    h := seed ^ uint32(len(key))*m
    for ; len(key) >= 4; key = key[4:] {
        w := binary.LittleEndian.Uint32(key)
        h += w
        h *= m
        h ^= h >> 16
    }
    switch len(key) {
    case 3: h += uint32(key[2]) << 16; fallthrough
    case 2: h += uint32(key[1]) << 8; fallthrough
    case 1: h += uint32(key[0]); h *= m; h ^= h >> 24
    }
    return h
}

func NewBloomFilter(keys [][]byte, bitsPerKey int) *BloomFilter {
    if bitsPerKey < 1 { bitsPerKey = 1 }
    k := int(float64(bitsPerKey) * math.Ln2)
    if k < 1 { k = 1 }
    if k > 30 { k = 30 }

    nBits := len(keys) * bitsPerKey
    nBits = (nBits + 7) & ^7 // round up to byte boundary
    if nBits < 64 { nBits = 64 }

    bits := make([]byte, nBits/8+1) // +1 for k value at end
    for _, key := range keys {
        h := bloomHash(key)
        delta := (h >> 17) | (h << 15) // rotl by 15
        for j := 0; j < k; j++ {
            bitPos := uint32(h) % uint32(nBits)
            bits[bitPos/8] |= 1 << (bitPos % 8)
            h += delta
        }
    }
    bits[len(bits)-1] = byte(k) // store k as last byte
    return &BloomFilter{bits: bits}
}

func (f *BloomFilter) MayContain(key []byte) bool {
    if len(f.bits) < 2 { return false }
    k := int(f.bits[len(f.bits)-1])
    if k > 30 { return true } // unknown filter
    nBits := (len(f.bits) - 1) * 8

    h := bloomHash(key)
    delta := (h >> 17) | (h << 15)
    for j := 0; j < k; j++ {
        bitPos := uint32(h) % uint32(nBits)
        if f.bits[bitPos/8]&(1<<(bitPos%8)) == 0 {
            return false // definitely not in set
        }
        h += delta
    }
    return true // probably in set
}
```

**Property test that must pass:**

```go
func TestBloom_NoFalseNegatives(t *testing.T) {
    var keys [][]byte
    for i := 0; i < 10000; i++ {
        keys = append(keys, []byte(fmt.Sprintf("key-%06d", i)))
    }
    bf := NewBloomFilter(keys, 10)
    for _, key := range keys {
        assert.True(t, bf.MayContain(key), "must not have false negatives: %s", key)
    }
}

func TestBloom_FalsePositiveRate(t *testing.T) {
    var keys [][]byte
    for i := 0; i < 10000; i++ {
        keys = append(keys, []byte(fmt.Sprintf("key-%06d", i)))
    }
    bf := NewBloomFilter(keys, 10)
    fp := 0
    for i := 10000; i < 20000; i++ {
        if bf.MayContain([]byte(fmt.Sprintf("key-%06d", i))) { fp++ }
    }
    fpr := float64(fp) / 10000.0
    assert.Less(t, fpr, 0.02, "FP rate must be < 2%% for bpk=10, got %.4f", fpr)
}
```

---

## Phase 4 — SSTable

### Block Builder (prefix compression + restart points)

```go
// internal/sstable/block_builder.go
const RestartInterval = 16

type BlockBuilder struct {
    buf             []byte
    restarts        []uint32
    counter         int    // entries since last restart
    lastKey         []byte
    restartInterval int
}

func (b *BlockBuilder) Add(key InternalKey, value []byte) {
    // Encode key as: [TypeByte:1][UserKey]  -- append type to key for storage
    encodedKey := encodeKey(key)

    shared := 0
    if b.counter < b.restartInterval {
        for shared < len(b.lastKey) && shared < len(encodedKey) && b.lastKey[shared] == encodedKey[shared] {
            shared++
        }
    } else {
        // Restart point: no prefix compression, store full key
        b.restarts = append(b.restarts, uint32(len(b.buf)))
        b.counter = 0
    }

    unshared := len(encodedKey) - shared
    // [sharedLen:varint][unsharedLen:varint][valueLen:varint][unsharedKey][value]
    b.buf = appendVarint(b.buf, uint64(shared))
    b.buf = appendVarint(b.buf, uint64(unshared))
    b.buf = appendVarint(b.buf, uint64(len(value)))
    b.buf = append(b.buf, encodedKey[shared:]...)
    b.buf = append(b.buf, value...)
    b.lastKey = append(b.lastKey[:0], encodedKey...)
    b.counter++
}

func (b *BlockBuilder) Finish() []byte {
    // Append restart array: [restart0:4LE][restart1:4LE]...[numRestarts:4LE]
    for _, r := range b.restarts {
        buf4 := [4]byte{}
        binary.LittleEndian.PutUint32(buf4[:], r)
        b.buf = append(b.buf, buf4[:]...)
    }
    buf4 := [4]byte{}
    binary.LittleEndian.PutUint32(buf4[:], uint32(len(b.restarts)))
    b.buf = append(b.buf, buf4[:]...)
    return b.buf
}
```

### SSTable Builder (write full file)

```go
// internal/sstable/builder.go

func (b *SSTableBuilder) Finish() (SSTableMeta, error) {
    // 1. Flush final data block if non-empty
    if b.dataBlock.Size() > 0 { b.flushDataBlock() }

    // 2. Write filter block (Bloom filter)
    filterData := b.filterBuilder.Finish()
    filterHandle, _ := b.writeRawBlock(filterData)

    // 3. Write index block
    indexData := b.indexBlock.Finish()
    indexHandle, _ := b.writeRawBlock(indexData)

    // 4. Write footer
    footer := make([]byte, 48)
    pos := 0
    // Encode filterHandle (as metaindex handle for simplicity)
    pos += encodeBlockHandle(footer[pos:], filterHandle)
    // Encode indexHandle
    pos += encodeBlockHandle(footer[pos:], indexHandle)
    // Pad to 40 bytes
    for pos < 40 { footer[pos] = 0; pos++ }
    // Magic number
    binary.LittleEndian.PutUint64(footer[40:], 0x88e241b785f4cff7)
    b.file.Write(footer)

    // 5. fsync
    b.file.Sync()

    return SSTableMeta{
        FileID:   b.fileID,
        Level:    b.level,
        FirstKey: b.firstKey,
        LastKey:  b.lastKey,
        FileSize: b.fileSize + 48,
        NumKeys:  uint64(b.numEntries),
        FilePath: b.file.Name(),
    }, nil
}
```

---

## Phase 7 — Flush Worker

```go
// internal/engine/flush.go

func (fw *FlushWorker) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case imm := <-fw.queue:
            if err := fw.flush(imm); err != nil {
                // log error; retry or fatal
            }
        }
    }
}

func (fw *FlushWorker) flush(imm *MemTable) error {
    fw.bus.Publish(Event{Type: EvtFlushStart, Extra: map[string]interface{}{
        "size": imm.ApproximateSize(),
    }})

    // 1. Build SSTable from MemTable iterator
    fileID := fw.engine.nextFileID.Add(1)
    path := filepath.Join(fw.engine.cfg.DataDir, fmt.Sprintf("%06d.sst", fileID))
    builder := NewSSTableBuilder(path, fileID, 0, fw.engine.cfg.BlockSize, fw.engine.cfg.BloomBitsPerKey)

    iter := imm.NewIterator()
    for iter.Valid() {
        if err := builder.Add(iter.Key(), iter.Value()); err != nil { return err }
        iter.Next()
    }

    meta, err := builder.Finish()
    if err != nil { return err }

    // 2. Atomic commit: MANIFEST update first, then WAL cleanup
    edit := VersionEdit{Type: EditAddSSTable, Level: 0, FileID: fileID,
        FileSize: meta.FileSize, FirstKey: meta.FirstKey, LastKey: meta.LastKey}
    if err := fw.engine.manifest.Apply(edit); err != nil { return err }

    // 3. Open reader for new SSTable
    reader, err := NewSSTableReader(path, meta, fw.engine.cache)
    if err != nil { return err }
    fw.engine.registerReader(fileID, reader)

    // 4. Delete WAL (now that MANIFEST is updated)
    os.Remove(fw.engine.walPath)

    fw.bus.Publish(Event{Type: EvtFlushComplete, Extra: map[string]interface{}{
        "file_id": fileID, "level": 0, "size": meta.FileSize,
    }})
    return nil
}
```

---

## Phase 9 — Crash Recovery

```go
// internal/engine/engine.go

func (e *LSMEngine) recover() error {
    // 1. Replay MANIFEST
    version, err := e.manifest.Recover()
    if err != nil { return fmt.Errorf("manifest recovery: %w", err) }
    e.manifest.current = version

    // 2. Open all SSTable readers from version
    for level, ssts := range version.Levels {
        for _, meta := range ssts {
            if _, err := os.Stat(meta.FilePath); os.IsNotExist(err) {
                return fmt.Errorf("missing SSTable: %s", meta.FilePath)
            }
            reader, err := NewSSTableReader(meta.FilePath, meta, e.cache)
            if err != nil { return err }
            e.registerReader(meta.FileID, reader)
            _ = level
        }
    }

    // 3. Replay WAL
    walPath := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.log", version.logNumber))
    entries, err := RecoverWAL(walPath)
    if err != nil { return err }
    for _, entry := range entries {
        switch entry.Type {
        case EntrySet:    e.memTable.Put(entry.Key, entry.Value, entry.SeqNo)
        case EntryDelete: e.memTable.Delete(entry.Key, entry.SeqNo)
        }
        if entry.SeqNo > e.seqNo { e.seqNo = entry.SeqNo }
    }

    // 4. Clean orphaned SSTable files (not in MANIFEST)
    pattern := filepath.Join(e.cfg.DataDir, "*.sst")
    files, _ := filepath.Glob(pattern)
    for _, f := range files {
        fileID := extractFileID(f)
        if !version.HasFile(fileID) {
            os.Remove(f) // orphaned; safe to delete
        }
    }

    return nil
}
```

---

## Phase 10 — Leveled Compaction

### K-way Merge Iterator (critical correctness)

```go
// internal/compaction/iterator.go
// Uses a min-heap over iterators to merge k SSTables efficiently

type mergeEntry struct {
    key   InternalKey
    value []byte
    iter  Iterator
    idx   int // tiebreak by recency (lower idx = newer L0 SSTable)
}

type MergeIterator struct {
    heap []*mergeEntry
    prev InternalKey // last yielded UserKey (for dedup)
}

func (mi *MergeIterator) advance(e *mergeEntry) {
    e.iter.Next()
    if e.iter.Valid() {
        e.key = e.iter.Key()
        e.value = e.iter.Value()
        heap.Push(mi, e)
    }
}

func (mi *MergeIterator) Next() {
    // Pop minimum; advance its iterator; push back if valid
    // Skip entries with same UserKey as previously yielded (keep highest SeqNo)
    for len(mi.heap) > 0 {
        top := heap.Pop(mi).(*mergeEntry)
        if !bytes.Equal(top.key.UserKey, mi.prev.UserKey) || top.key.SeqNo > mi.prev.SeqNo {
            mi.prev = top.key
            // set current
            mi.advance(top)
            return
        }
        mi.advance(top) // skip stale version
    }
}
```

### Leveled Compactor (exact algorithm)

```go
// internal/compaction/leveled.go

func (c *LeveledCompactor) Execute(task *CompactionTask) error {
    // 1. Create merge iterator over all input SSTables
    var iters []Iterator
    for _, meta := range task.Inputs {
        reader := c.engine.getReader(meta.FileID)
        iters = append(iters, reader.NewIterator())
    }
    mergeIter := NewMergeIterator(iters, task.InputLevel == 0)
    mergeIter.SeekToFirst()

    // 2. Write output SSTables
    var outputs []SSTableMeta
    var builder *SSTableBuilder

    newFileForKey := func(key InternalKey) {
        if builder != nil {
            meta, _ := builder.Finish()
            outputs = append(outputs, meta)
        }
        fileID := c.engine.nextFileID.Add(1)
        path := sstPath(c.engine.cfg.DataDir, fileID)
        builder = NewSSTableBuilder(path, fileID, task.OutputLevel,
            c.engine.cfg.BlockSize, c.engine.cfg.BloomBitsPerKey)
    }
    newFileForKey(InternalKey{}) // start first output file

    isBottomLevel := task.OutputLevel == c.engine.maxLevel

    for mergeIter.Valid() {
        key := mergeIter.Key()
        val := mergeIter.Value()

        // Drop tombstones at bottom level (no older versions can exist)
        if key.Type == TypeDeletion && isBottomLevel {
            c.engine.bus.Publish(Event{Type: EvtTombstoneDropped,
                Extra: map[string]interface{}{"key": string(key.UserKey)}})
            mergeIter.Next()
            continue
        }

        // Start new output file if current is too large
        if builder.ApproxSize() >= c.engine.cfg.SSTMaxSize {
            newFileForKey(key)
        }

        builder.Add(key, val)
        c.engine.bus.Publish(Event{Type: EvtCompactionMerge, Extra: map[string]interface{}{
            "key": string(key.UserKey), "type": key.Type, "level_out": task.OutputLevel,
        }})
        mergeIter.Next()
    }

    if builder.NumEntries() > 0 {
        meta, _ := builder.Finish()
        outputs = append(outputs, meta)
    }

    // 3. Atomic commit (SPEC §8.1)
    // fsync all output SSTables
    for _, m := range outputs {
        f, _ := os.Open(m.FilePath)
        f.Sync()
        f.Close()
    }
    // Apply MANIFEST: add new + delete old
    edit := buildVersionEdit(task.Inputs, outputs, task.InputLevel, task.OutputLevel)
    if err := c.engine.manifest.Apply(edit); err != nil { return err }
    // Delete input SSTable files
    for _, meta := range task.Inputs {
        os.Remove(meta.FilePath)
        c.engine.unregisterReader(meta.FileID)
    }
    // Register new readers
    for _, meta := range outputs {
        reader, _ := NewSSTableReader(meta.FilePath, meta, c.engine.cache)
        c.engine.registerReader(meta.FileID, reader)
    }
    return nil
}
```

**Critical correctness test:**

```go
func TestLCS_CompactionCorrectness(t *testing.T) {
    dir := t.TempDir()
    engine, _ := Open(Config{DataDir: dir, MemTableSize: 64*1024, CompactionStyle: "leveled"})
    defer engine.Close()

    // Write 10k keys, overwrite each 5 times
    for round := 0; round < 5; round++ {
        for i := 0; i < 10000; i++ {
            key := []byte(fmt.Sprintf("key-%06d", i))
            val := []byte(fmt.Sprintf("value-round-%d-%06d", round, i))
            engine.Put(key, val)
        }
    }

    // Force full compaction
    engine.ForceCompaction(0)
    time.Sleep(5 * time.Second) // wait for background work

    // Every key must have its round-4 (latest) value
    for i := 0; i < 10000; i++ {
        key := []byte(fmt.Sprintf("key-%06d", i))
        expected := []byte(fmt.Sprintf("value-round-4-%06d", i))
        val, err := engine.Get(key)
        require.NoError(t, err, "key %s not found", key)
        assert.Equal(t, expected, val)
    }
}
```

---

## Integration Tests

### Crash Recovery (must pass)

```go
func TestCrashRecovery_MidWrite(t *testing.T) {
    dir := t.TempDir()

    // Phase 1: write 500 keys
    {
        engine, err := Open(Config{DataDir: dir, SyncWAL: true})
        require.NoError(t, err)
        for i := 0; i < 500; i++ {
            engine.Put([]byte(fmt.Sprintf("k%04d", i)), []byte(fmt.Sprintf("v%04d", i)))
        }
        // Simulate crash: close file handles without graceful shutdown
        // (in test: just don't flush immutable memtables)
        engine.crashClose() // closes files but skips flush
    }

    // Phase 2: reopen and verify
    {
        engine, err := Open(Config{DataDir: dir, SyncWAL: true})
        require.NoError(t, err)
        defer engine.Close()
        for i := 0; i < 500; i++ {
            val, err := engine.Get([]byte(fmt.Sprintf("k%04d", i)))
            require.NoError(t, err, "key k%04d not found after recovery", i)
            assert.Equal(t, []byte(fmt.Sprintf("v%04d", i)), val)
        }
    }
}
```

### Tombstone Not Resurrected (must pass)

```go
func TestTombstoneNotResurrected(t *testing.T) {
    dir := t.TempDir()
    engine, _ := Open(Config{DataDir: dir, MemTableSize: 4096})
    defer engine.Close()

    // Write key, then delete it
    engine.Put([]byte("hello"), []byte("world"))
    engine.Delete([]byte("hello"))

    // Force flush + compaction to bottom level
    engine.ForceFlush()
    engine.ForceCompaction(0)
    engine.ForceCompaction(1) // promote to bottom
    time.Sleep(2 * time.Second)

    // Key must be gone (not resurrected from old SSTable)
    _, err := engine.Get([]byte("hello"))
    assert.Equal(t, ErrNotFound, err)
}
```

---

## Frontend Key Patterns

### Level Tree D3 Visualization

```typescript
// Panel 2: each level as a horizontal bar; SSTables as rectangles on key-range axis

interface SSTableRect {
  fileID: number;
  level: number;
  firstKey: string;
  lastKey: string;
  fileSize: number;
  x: number;  // computed from key hash
  width: number;
  color: string; // age-based
}

function keyToX(key: string, width: number): number {
  // Hash key to [0, width]; use first byte as proxy for 0-255 mapping
  const firstByte = key.charCodeAt(0) || 0;
  return (firstByte / 255) * width;
}

// On EvtCompactionStart: highlight input SSTables
// On EvtCompactionComplete: animate deletions + additions
function animateCompaction(svg: SVGElement, inputs: number[], outputs: SSTableRect[]) {
  // Flash input rects yellow
  inputs.forEach(id => {
    d3.select(`#sst-${id}`).transition().duration(200).style("opacity", 0.3);
  });
  // After 500ms: inputs fade out; outputs slide in
  setTimeout(() => {
    inputs.forEach(id => d3.select(`#sst-${id}`).remove());
    outputs.forEach(rect => drawSSTable(svg, rect));
  }, 500);
}
```

### Bloom Filter Bit Array

```typescript
// Panel 3: visualize 512-bit array as 32x16 grid
function renderBitArray(container: SVGElement, bits: boolean[], changedPositions: number[]) {
  const cells = d3.select(container).selectAll<SVGRectElement, boolean>(".bit-cell")
    .data(bits);

  cells.enter().append("rect").attr("class", "bit-cell")
    .merge(cells)
    .attr("x", (_, i) => (i % 32) * 10)
    .attr("y", (_, i) => Math.floor(i / 32) * 10)
    .attr("width", 9).attr("height", 9)
    .attr("rx", 1)
    .attr("fill", d => d ? "#ef4444" : "#1e293b")
    .attr("stroke", (_, i) => changedPositions.includes(i) ? "#fbbf24" : "none")
    .attr("stroke-width", (_, i) => changedPositions.includes(i) ? 2 : 0);
}
```

---

## Critical Correctness Invariants

After all phases complete, verify:

1. **WAL durability:** `TestWAL_TruncatedRecord` and `TestWAL_CRCDetectsCorruption` pass
2. **Bloom no false negatives:** `TestBloom_NoFalseNegatives` — 100% of inserted keys return MayContain=true
3. **Compaction dedup:** `TestLCS_CompactionCorrectness` — only latest version per key after full compaction
4. **Tombstone GC:** `TestTombstoneNotResurrected` — deleted keys gone after reaching bottom level
5. **InternalKey ordering:** `TestMemTable_MultipleVersions` — get always returns highest SeqNo version
6. **L1+ non-overlap:** `TestLCS_NonOverlappingL1` — after compaction, no key-range overlap in L1+
7. **Crash recovery completeness:** `TestCrashRecovery_MidWrite` — all WAL-persisted writes survive crash
8. **Race free:** `go test ./... -race -count=5` — zero data races

---

## Code Standards

- **No external DB libraries** — only stdlib (os, bufio, encoding/binary, hash/crc32, sync, container/heap) + testify
- **WAL write before MemTable insert** — `WAL.Append()` called before `MemTable.Put()` in every write path
- **fsync before MANIFEST** — output SSTables fsynced before `Manifest.Apply()` on compaction
- **MANIFEST before delete** — old SSTables deleted only AFTER `Manifest.Apply()` succeeds
- **InternalKey sort order** — `Less()` must be: UserKey ASC, then SeqNo DESC. Test this explicitly.
- **Bloom filter: no false negatives** — this is the fundamental correctness property; any false negative is a bug
- **`go test ./... -race` zero races** — sync.RWMutex for engine state; sync.Mutex for WAL; atomic for SeqNo and FileID
- **TypeScript strict mode** — no `any`; all event payloads typed
- **EventBus non-blocking** — `select { case ch <- evt: default: }` in Publish; never block storage goroutines

---

## Startup

```bash
# Terminal 1: Go backend
make run
# or: go run ./cmd/server

# Terminal 2: Frontend
cd frontend && bun run server/bff.ts &
bun run src/main.ts

# Terminal 3: Tests
make test-race

# Run benchmark to see live visualization
curl -X POST localhost:8080/api/v1/scenarios/write_flush/run
```

Open `http://localhost:3001`. Run `"write_flush"` scenario first — you'll see:
1. WAL entries appearing in Panel 7 (scrolling log)
2. MemTable skip-list growing in Panel 1
3. Flush animation: MemTable → new SSTable rectangle in L0 (Panel 2)
4. Bloom filter panel (Panel 3) shows filter being built for the new SSTable
5. Then trigger `"compaction_lcs"` to watch L0→L1 compaction animation

Run `"bloom_demo"` scenario to see the false positive demo in Panel 3 — 1000 non-member queries animated against the bit array, with FP count shown live.

Run `"crash_recovery"` scenario in Panel 7 for the most dramatic demo: writes stop mid-stream, memory cleared, WAL replayed step by step — every key restored in order.
