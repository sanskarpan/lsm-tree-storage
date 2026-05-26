// Package engine — see config.go for the package doc.
package engine

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lsm-engine/internal/cache"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"
	"lsm-engine/internal/memtable"
	"lsm-engine/internal/sstable"
	"lsm-engine/internal/wal"
)

// ErrNotFound is returned by Get when the requested key does not exist or has been deleted.
var ErrNotFound = errors.New("engine: key not found")

type immutableMemtable struct {
	table     *memtable.MemTable
	walPath   string
	logNumber uint64
}

// RuntimeState describes the engine's current process-managed lifecycle state.
type RuntimeState struct {
	Open            bool
	DataDir         string
	ActiveWALPath   string
	ActiveLogNumber uint64
	SyncWAL         bool
	CompactionStyle string
}

// MemTableEntrySnapshot is a serializable snapshot of one memtable record.
type MemTableEntrySnapshot struct {
	UserKey []byte
	SeqNo   uint64
	Type    sstable.EntryType
	Value   []byte
}

// TableSnapshot is a bounded copy of a mutable or immutable memtable.
type TableSnapshot struct {
	ApproximateSize int64
	WALSeqNo        uint64
	Entries         []MemTableEntrySnapshot
	Truncated       bool
}

// ImmutableMemTableSnapshot captures one immutable memtable plus its WAL ownership.
type ImmutableMemTableSnapshot struct {
	LogNumber uint64
	WALPath   string
	Table     TableSnapshot
}

// MemTablesSnapshot captures the current mutable and immutable memtables.
type MemTablesSnapshot struct {
	Mutable         TableSnapshot
	Immutables      []ImmutableMemTableSnapshot
	ActiveWALPath   string
	ActiveLogNumber uint64
}

// HealthStatus captures process readiness and operational state for probes.
type HealthStatus struct {
	Ready                    bool         `json:"ready"`
	State                    RuntimeState `json:"state"`
	ManifestPath             string       `json:"manifest_path"`
	ManifestExists           bool         `json:"manifest_exists"`
	ActiveWALExists          bool         `json:"active_wal_exists"`
	MutableMemtableSize      int64        `json:"mutable_memtable_size"`
	ImmutableCount           int          `json:"immutable_count"`
	MaxImmutableMemTables    int          `json:"max_immutable_memtables"`
	FlushQueueDepth          int          `json:"flush_queue_depth"`
	FlushQueueCapacity       int          `json:"flush_queue_capacity"`
	CompactionTriggerBacklog int          `json:"compaction_trigger_backlog"`
	Level0Files              int          `json:"level0_files"`
	Level0StopWritesTrigger  int          `json:"level0_stop_writes_trigger"`
	Reasons                  []string     `json:"reasons,omitempty"`
}

// LSMEngine is the main storage engine
type LSMEngine struct {
	mu        sync.RWMutex
	flushCond *sync.Cond // broadcast when an immutable is flushed

	cfg     Config
	dataDir string

	// Write path
	memTable   *memtable.MemTable
	immutables []*immutableMemtable
	wal        *wal.WAL
	walPath    string
	logNumber  uint64
	seqNo      uint64

	// SSTable readers by fileID
	// readersMu is held for the ENTIRE duration of any SSTable read to prevent
	// use-after-free when the compaction worker removes/closes readers.
	readers   map[uint64]*sstable.SSTableReader
	readersMu sync.RWMutex

	// Manifest
	manifest *manifest.Manifest

	// Block cache
	cache *cache.BlockCache

	// Event bus
	bus events.EventPublisher

	// Flush worker
	flushQueue chan *immutableMemtable
	flushWG    sync.WaitGroup

	// Compaction
	compactTrigger chan struct{}
	compactWG      sync.WaitGroup

	nextFileID atomic.Uint64

	closeOnce sync.Once
	closeCh   chan struct{}
}

// Open creates or opens an LSM engine at the given directory
func Open(cfg Config) (*LSMEngine, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.MemTableSize == 0 {
		cfg.MemTableSize = 64 * 1024 * 1024
	}
	if cfg.BlockSize == 0 {
		cfg.BlockSize = 4096
	}
	if cfg.BloomBitsPerKey == 0 {
		cfg.BloomBitsPerKey = 10
	}
	if cfg.SSTMaxSize == 0 {
		cfg.SSTMaxSize = 64 * 1024 * 1024
	}
	if cfg.BlockCacheSize == 0 {
		cfg.BlockCacheSize = 128 * 1024 * 1024
	}
	if cfg.MaxImmutableMemTables == 0 {
		cfg.MaxImmutableMemTables = 2
	}
	if cfg.TimeWindowSize == 0 {
		cfg.TimeWindowSize = time.Hour
	}
	if cfg.Level0FileNumCompactionTrigger == 0 {
		cfg.Level0FileNumCompactionTrigger = 4
	}
	if cfg.Level0StopWritesTrigger == 0 {
		cfg.Level0StopWritesTrigger = 12
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, err
	}

	bus := events.NewEventBus()
	blockCache := cache.NewBlockCache(cfg.BlockCacheSize, bus)

	e := &LSMEngine{
		cfg:            cfg,
		dataDir:        cfg.DataDir,
		readers:        make(map[uint64]*sstable.SSTableReader),
		bus:            bus,
		cache:          blockCache,
		flushQueue:     make(chan *immutableMemtable, 16),
		compactTrigger: make(chan struct{}, 1),
		closeCh:        make(chan struct{}),
	}
	// flushCond uses e.mu so ForceFlush can Wait while holding the lock
	e.flushCond = sync.NewCond(&e.mu)
	e.memTable = memtable.NewMemTable(cfg.MemTableSize)

	// Open/create MANIFEST
	manifestPath := filepath.Join(cfg.DataDir, "MANIFEST")
	m, err := manifest.OpenManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	e.manifest = m

	// Recover
	if err := e.recover(); err != nil {
		return nil, fmt.Errorf("recovery: %w", err)
	}

	w, err := wal.OpenWAL(e.walPath, bus)
	if err != nil {
		return nil, fmt.Errorf("open wal: %w", err)
	}
	e.wal = w

	// Start background workers
	e.flushWG.Add(1)
	go e.flushWorker()

	e.compactWG.Add(1)
	go e.compactionWorker()

	return e, nil
}

func (e *LSMEngine) recover() error {
	// 1. Replay MANIFEST to get the current version (reads without writing)
	manifestPath := filepath.Join(e.cfg.DataDir, "MANIFEST")
	version, err := manifest.Recover(manifestPath)
	if err != nil {
		return fmt.Errorf("manifest recovery: %w", err)
	}

	// Install recovered version without writing new edits to disk
	e.manifest.SetCurrent(version)

	// 2. Open SSTable readers from recovered version
	for level, ssts := range version.Levels {
		for _, meta := range ssts {
			filePath := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.sst", meta.FileID))
			meta.FilePath = filePath
			meta.Level = level
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				return fmt.Errorf("missing SSTable: %s", filePath)
			}
			reader, err := sstable.NewSSTableReader(filePath, *meta, e.cache, e.bus)
			if err != nil {
				return fmt.Errorf("open sstable %s: %w", filePath, err)
			}
			e.registerReader(meta.FileID, reader)
		}
	}

	// 3. Replay every WAL on disk. Immutable memtables can have their own WAL
	// while flushing, so recovery must not rely on a single log number.
	logPaths, maxLogID, err := listWALFiles(e.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("list wal files: %w", err)
	}
	for _, walPath := range logPaths {
		entries, recoverErr := wal.RecoverWAL(walPath)
		if recoverErr != nil {
			return fmt.Errorf("wal recovery %s: %w", walPath, recoverErr)
		}
		for _, entry := range entries {
			switch entry.Type {
			case wal.EntrySet:
				e.memTable.Put(entry.Key, entry.Value, entry.SeqNo)
			case wal.EntryDelete:
				e.memTable.Delete(entry.Key, entry.SeqNo)
			}
			if entry.SeqNo > e.seqNo {
				e.seqNo = entry.SeqNo
			}
		}
	}

	// 4. Clean orphaned SSTable files (on disk but not in MANIFEST)
	pattern := filepath.Join(e.cfg.DataDir, "*.sst")
	files, _ := filepath.Glob(pattern)
	var maxSSTID uint64
	for _, f := range files {
		fileID := extractFileID(f)
		if fileID > maxSSTID {
			maxSSTID = fileID
		}
		if !version.HasFile(fileID) {
			if err := os.Remove(f); err != nil {
				log.Printf("warn: failed to remove orphaned SSTable %s: %v", f, err)
			}
		}
	}

	nextFileID := version.NextFileID
	if maxSSTID >= nextFileID {
		nextFileID = maxSSTID + 1
	}
	if maxLogID >= nextFileID {
		nextFileID = maxLogID + 1
	}
	if nextFileID == 0 {
		nextFileID = 1
	}

	// Fold any recovered logs into a fresh active WAL so runtime can keep a
	// single current writer while old immutable WALs are removed.
	activeLogNumber := nextFileID
	e.logNumber = activeLogNumber
	e.walPath = filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.log", activeLogNumber))
	if len(logPaths) > 0 {
		if err := rewriteMemtableToWAL(e.memTable, e.walPath); err != nil {
			return fmt.Errorf("rewrite recovered wal: %w", err)
		}
		for _, walPath := range logPaths {
			if err := os.Remove(walPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove recovered wal %s: %w", walPath, err)
			}
		}
		nextFileID = activeLogNumber + 1
	}
	e.nextFileID.Store(nextFileID)

	if version.LogNumber != activeLogNumber {
		if err := e.manifest.Apply(manifest.VersionEdit{
			Type:      manifest.EditLogNumber,
			LogNumber: activeLogNumber,
		}); err != nil {
			return fmt.Errorf("persist recovered log number: %w", err)
		}
	}

	return nil
}

func listWALFiles(dataDir string) ([]string, uint64, error) {
	paths, err := filepath.Glob(filepath.Join(dataDir, "*.log"))
	if err != nil {
		return nil, 0, err
	}

	sort.Slice(paths, func(i, j int) bool {
		return extractFileID(paths[i]) < extractFileID(paths[j])
	})

	var maxLogID uint64
	for _, path := range paths {
		if id := extractFileID(path); id > maxLogID {
			maxLogID = id
		}
	}

	return paths, maxLogID, nil
}

func rewriteMemtableToWAL(table *memtable.MemTable, walPath string) error {
	w, err := wal.OpenWAL(walPath, &events.NoopBus{})
	if err != nil {
		return err
	}

	for _, entry := range table.Entries() {
		walEntry := wal.WALEntry{
			Key:   entry.Key.UserKey,
			Value: entry.Value,
			SeqNo: entry.Key.SeqNo,
		}
		if entry.Key.Type == sstable.TypeDeletion {
			walEntry.Type = wal.EntryDelete
		} else {
			walEntry.Type = wal.EntrySet
		}
		if err := w.AppendWithSeqNo(walEntry); err != nil {
			_ = w.Close()
			return err
		}
	}

	if err := w.Sync(); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func extractFileID(path string) uint64 {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	id, _ := strconv.ParseUint(name, 10, 64)
	return id
}

// ── WriteBatch ────────────────────────────────────────────────────────────────

// BatchEntry is one operation inside a WriteBatch.
type BatchEntry struct {
	Key    []byte
	Value  []byte // nil means delete
	Delete bool
}

// WriteBatch groups multiple writes into a single atomic WAL record.
type WriteBatch struct {
	entries []BatchEntry
}

// Entries returns a defensive copy of the batch contents.
func (b *WriteBatch) Entries() []BatchEntry {
	if b == nil || len(b.entries) == 0 {
		return nil
	}
	out := make([]BatchEntry, 0, len(b.entries))
	for _, entry := range b.entries {
		item := BatchEntry{
			Key:    append([]byte(nil), entry.Key...),
			Value:  append([]byte(nil), entry.Value...),
			Delete: entry.Delete,
		}
		out = append(out, item)
	}
	return out
}

// Put adds a put operation to the batch.
func (b *WriteBatch) Put(key, value []byte) {
	b.entries = append(b.entries, BatchEntry{Key: key, Value: value})
}

// Delete adds a delete (tombstone) operation to the batch.
func (b *WriteBatch) Delete(key []byte) {
	b.entries = append(b.entries, BatchEntry{Key: key, Delete: true})
}

// Write atomically applies all entries in the batch to the engine.
// All batch entries are durably persisted in a single WAL record before the
// MemTable is mutated, so recovery never observes a partial batch.
func (e *LSMEngine) Write(batch *WriteBatch) error {
	if batch == nil || len(batch.entries) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	walEntries := make([]wal.WALEntry, 0, len(batch.entries))
	for _, entry := range batch.entries {
		seqNo := atomic.AddUint64(&e.seqNo, 1)
		walEntry := wal.WALEntry{
			Key:   entry.Key,
			Value: entry.Value,
			SeqNo: seqNo,
			Type:  wal.EntrySet,
		}
		if entry.Delete {
			walEntry.Type = wal.EntryDelete
			walEntry.Value = nil
		}
		walEntries = append(walEntries, walEntry)
	}
	if err := e.wal.AppendBatch(walEntries); err != nil {
		return err
	}
	if e.cfg.SyncWAL {
		if err := e.wal.Sync(); err != nil {
			return err
		}
	}
	for _, entry := range walEntries {
		if entry.Type == wal.EntryDelete {
			e.memTable.Delete(entry.Key, entry.SeqNo)
		} else {
			e.memTable.Put(entry.Key, entry.Value, entry.SeqNo)
		}
	}
	if e.memTable.IsFull() {
		if err := e.rotateMemTable(); err != nil {
			return err
		}
	}
	return nil
}

// Put writes a key-value pair (WAL first, then MemTable)
func (e *LSMEngine) Put(key, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.putLocked(key, value)
}

func (e *LSMEngine) putLocked(key, value []byte) error {
	seqNo := atomic.AddUint64(&e.seqNo, 1)

	// 1. WAL before MemTable (durability)
	entry := wal.WALEntry{Type: wal.EntrySet, Key: key, Value: value, SeqNo: seqNo}
	if err := e.wal.AppendWithSeqNo(entry); err != nil {
		return err
	}
	if e.cfg.SyncWAL {
		if err := e.wal.Sync(); err != nil {
			return err
		}
	}

	// 2. MemTable insert
	e.memTable.Put(key, value, seqNo)

	// 3. Rotate if full
	if e.memTable.IsFull() {
		if err := e.rotateMemTable(); err != nil {
			return err
		}
	}

	return nil
}

// Delete inserts a tombstone for key
func (e *LSMEngine) Delete(key []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	seqNo := atomic.AddUint64(&e.seqNo, 1)

	entry := wal.WALEntry{Type: wal.EntryDelete, Key: key, SeqNo: seqNo}
	if err := e.wal.AppendWithSeqNo(entry); err != nil {
		return err
	}
	if e.cfg.SyncWAL {
		if err := e.wal.Sync(); err != nil {
			return err
		}
	}

	e.memTable.Delete(key, seqNo)

	if e.memTable.IsFull() {
		if err := e.rotateMemTable(); err != nil {
			return err
		}
	}

	return nil
}

func (e *LSMEngine) rotateMemTable() error {
	for len(e.immutables) >= e.cfg.MaxImmutableMemTables {
		e.flushCond.Wait()
	}

	newLogNumber := e.nextFileID.Add(1)
	newWalPath := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.log", newLogNumber))
	newWAL, err := wal.OpenWAL(newWalPath, e.bus)
	if err != nil {
		return err
	}
	if err := e.manifest.Apply(manifest.VersionEdit{
		Type:      manifest.EditLogNumber,
		LogNumber: newLogNumber,
	}); err != nil {
		_ = newWAL.Close()
		_ = os.Remove(newWalPath)
		return err
	}

	oldMem := e.memTable
	oldWAL := e.wal
	oldWalPath := e.walPath
	oldLogNumber := e.logNumber

	e.memTable = memtable.NewMemTable(e.cfg.MemTableSize)
	imm := &immutableMemtable{
		table:     oldMem,
		walPath:   oldWalPath,
		logNumber: oldLogNumber,
	}
	e.immutables = append(e.immutables, imm)
	e.wal = newWAL
	e.walPath = newWalPath
	e.logNumber = newLogNumber

	if oldWAL != nil {
		if closeErr := oldWAL.Close(); closeErr != nil {
			log.Printf("warn: close rotated wal %s: %v", oldWalPath, closeErr)
		}
	}

	// If the queue is full this becomes the write backpressure mechanism.
	e.flushQueue <- imm
	e.bus.Publish(events.Event{Type: events.EvtMemTableFull})
	e.bus.Publish(events.Event{Type: events.EvtMemTableRotate, Extra: map[string]interface{}{
		"num_immutables": len(e.immutables),
	}})
	return nil
}

// Get looks up a key across all levels (MemTable → immutables → L0 → L1+)
func (e *LSMEngine) Get(key []byte) ([]byte, error) {
	e.mu.RLock()
	mem := e.memTable
	imms := make([]*immutableMemtable, len(e.immutables))
	copy(imms, e.immutables)
	e.mu.RUnlock()

	readSeqNo := atomic.LoadUint64(&e.seqNo)

	e.bus.Publish(events.Event{Type: events.EvtReadStart, Extra: map[string]interface{}{
		"key": string(key),
	}})

	// 1. Mutable MemTable
	if val, found, deleted := mem.Get(key); found {
		e.bus.Publish(events.Event{Type: events.EvtReadMemTable})
		if deleted {
			return nil, ErrNotFound
		}
		return val, nil
	}

	// 2. Immutable MemTables (newest first)
	for i := len(imms) - 1; i >= 0; i-- {
		if val, found, deleted := imms[i].table.Get(key); found {
			e.bus.Publish(events.Event{Type: events.EvtReadMemTable})
			if deleted {
				return nil, ErrNotFound
			}
			return val, nil
		}
	}

	// 3. SSTables — hold readersMu.RLock for the duration to prevent
	//    use-after-free if the compaction worker closes a reader concurrently.
	version := e.manifest.Current()

	// L0: newest first (highest fileID first)
	l0 := make([]*sstable.SSTableMeta, len(version.Levels[0]))
	copy(l0, version.Levels[0])
	sort.Slice(l0, func(i, j int) bool {
		return l0[i].FileID > l0[j].FileID
	})

	e.readersMu.RLock()
	defer e.readersMu.RUnlock()

	for _, meta := range l0 {
		reader, ok := e.readers[meta.FileID]
		if !ok {
			continue
		}
		val, found, err := reader.Get(key, readSeqNo)
		if err != nil {
			return nil, err
		}
		if found {
			e.bus.Publish(events.Event{Type: events.EvtReadSSTable, Extra: map[string]interface{}{
				"file_id": meta.FileID, "level": 0,
			}})
			if val == nil {
				return nil, ErrNotFound // tombstone
			}
			return val, nil
		}
	}

	// L1+: binary comparison using bytes.Compare (correct for binary keys)
	for level := 1; level < manifest.MaxLevels; level++ {
		ssts := version.Levels[level]
		if len(ssts) == 0 {
			continue
		}
		meta := findLevelCandidate(ssts, key)
		if meta == nil {
			continue
		}
		reader, ok := e.readers[meta.FileID]
		if !ok {
			continue
		}
		val, found, err := reader.Get(key, readSeqNo)
		if err != nil {
			return nil, err
		}
		if found {
			e.bus.Publish(events.Event{Type: events.EvtReadSSTable, Extra: map[string]interface{}{
				"file_id": meta.FileID, "level": level,
			}})
			if val == nil {
				return nil, ErrNotFound // tombstone
			}
			return val, nil
		}
	}

	return nil, ErrNotFound
}

func (e *LSMEngine) registerReader(fileID uint64, reader *sstable.SSTableReader) {
	e.readersMu.Lock()
	defer e.readersMu.Unlock()
	e.readers[fileID] = reader
}

// unregisterReader removes a reader from the map and closes it.
// It acquires readersMu.Lock, which blocks until all in-progress reads complete.
func (e *LSMEngine) unregisterReader(fileID uint64) {
	e.readersMu.Lock()
	reader, ok := e.readers[fileID]
	if ok {
		delete(e.readers, fileID)
	}
	e.readersMu.Unlock()
	if ok {
		_ = reader.Close()
	}
}

// ForceFlush rotates the mutable MemTable and blocks until all queued flushes complete.
func (e *LSMEngine) ForceFlush() {
	e.mu.Lock()
	if e.memTable.ApproximateSize() > 0 {
		if err := e.rotateMemTable(); err != nil {
			e.mu.Unlock()
			return
		}
	}
	// Wait until all immutables are flushed (flushCond broadcast by flushWorker)
	for len(e.immutables) > 0 {
		e.flushCond.Wait()
	}
	e.mu.Unlock()
}

// ForceCompaction triggers a compaction check
func (e *LSMEngine) ForceCompaction(_ int) {
	select {
	case e.compactTrigger <- struct{}{}:
	default:
	}
}

// Close gracefully shuts down the engine
func (e *LSMEngine) Close() error {
	var err error
	e.closeOnce.Do(func() {
		close(e.closeCh)

		// Flush remaining MemTable
		e.mu.Lock()
		if e.memTable.ApproximateSize() > 0 {
			if rotateErr := e.rotateMemTable(); rotateErr != nil {
				err = rotateErr
			}
		}
		e.mu.Unlock()

		e.flushWG.Wait()
		e.compactWG.Wait()

		if e.wal != nil {
			err = e.wal.Close()
		}
		if e.manifest != nil {
			_ = e.manifest.Close()
		}

		// Close all readers (safe: no background workers running now)
		e.readersMu.Lock()
		for _, r := range e.readers {
			_ = r.Close()
		}
		e.readersMu.Unlock()
	})
	return err
}

// crashClose simulates a crash (closes without graceful flush, for tests)
func (e *LSMEngine) crashClose() {
	if e.wal != nil {
		_ = e.wal.Close()
	}
}

// Manifest returns the engine's manifest (for gateway/monitoring use)
func (e *LSMEngine) Manifest() *manifest.Manifest {
	return e.manifest
}

// EventBus returns the engine's event bus
func (e *LSMEngine) EventBus() *events.EventBus {
	if eb, ok := e.bus.(*events.EventBus); ok {
		return eb
	}
	return nil
}

// Stats returns engine statistics including SSTable, MemTable, WAL, and cache metrics.
func (e *LSMEngine) Stats() map[string]interface{} {
	version := e.manifest.Current()
	var totalFiles, totalSize int
	for _, level := range version.Levels {
		totalFiles += len(level)
		for _, m := range level {
			totalSize += int(m.FileSize)
		}
	}

	e.mu.RLock()
	memSize := e.memTable.ApproximateSize()
	numImm := len(e.immutables)
	e.mu.RUnlock()

	cs := e.cache.Stats()

	return map[string]interface{}{
		"total_sst_files": totalFiles,
		"total_sst_bytes": totalSize,
		"seq_no":          atomic.LoadUint64(&e.seqNo),
		// MemTable
		"memtable_size":  memSize,
		"num_immutables": numImm,
		// WAL (one active WAL plus one WAL per immutable memtable while flushing)
		"wal_files": 1 + numImm,
		// Block cache
		"cache_hits":     cs.Hits,
		"cache_misses":   cs.Misses,
		"cache_hit_rate": cs.HitRate,
		"cache_size":     cs.NumEntries,
	}
}

// Config returns the engine configuration
func (e *LSMEngine) Config() Config {
	return e.cfg
}

// SetCompactionStyle changes the active compaction strategy at runtime.
func (e *LSMEngine) SetCompactionStyle(style string) {
	e.mu.Lock()
	e.cfg.CompactionStyle = style
	e.mu.Unlock()
}

// State returns the current process-managed runtime state for the engine.
func (e *LSMEngine) State() RuntimeState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return RuntimeState{
		Open:            true,
		DataDir:         e.cfg.DataDir,
		ActiveWALPath:   e.walPath,
		ActiveLogNumber: e.logNumber,
		SyncWAL:         e.cfg.SyncWAL,
		CompactionStyle: e.cfg.CompactionStyle,
	}
}

// HealthStatus returns a probe-friendly readiness snapshot for the engine.
func (e *LSMEngine) HealthStatus() HealthStatus {
	state := e.State()
	manifestPath := filepath.Join(e.cfg.DataDir, "MANIFEST")

	status := HealthStatus{
		Ready:                    true,
		State:                    state,
		ManifestPath:             manifestPath,
		ManifestExists:           fileExists(manifestPath),
		ActiveWALExists:          state.ActiveWALPath != "" && fileExists(state.ActiveWALPath),
		MaxImmutableMemTables:    e.cfg.MaxImmutableMemTables,
		FlushQueueDepth:          len(e.flushQueue),
		FlushQueueCapacity:       cap(e.flushQueue),
		CompactionTriggerBacklog: len(e.compactTrigger),
		Level0StopWritesTrigger:  e.cfg.Level0StopWritesTrigger,
	}

	e.mu.RLock()
	status.MutableMemtableSize = e.memTable.ApproximateSize()
	status.ImmutableCount = len(e.immutables)
	e.mu.RUnlock()

	version := e.manifest.Current()
	status.Level0Files = len(version.Levels[0])

	if !state.Open {
		status.Ready = false
		status.Reasons = append(status.Reasons, "engine not open")
	}
	if !status.ManifestExists {
		status.Ready = false
		status.Reasons = append(status.Reasons, "manifest missing")
	}
	if !status.ActiveWALExists {
		status.Ready = false
		status.Reasons = append(status.Reasons, "active wal missing")
	}
	if status.ImmutableCount > status.MaxImmutableMemTables {
		status.Ready = false
		status.Reasons = append(status.Reasons, "immutable memtable backlog exceeded")
	}
	if status.Level0StopWritesTrigger > 0 && status.Level0Files >= status.Level0StopWritesTrigger {
		status.Ready = false
		status.Reasons = append(status.Reasons, "level0 stop-writes threshold reached")
	}

	return status
}

// MemTableSnapshot returns a bounded copy of the mutable and immutable memtables.
func (e *LSMEngine) MemTableSnapshot(limit int) MemTablesSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snapshot := MemTablesSnapshot{
		Mutable:         snapshotTable(e.memTable, limit),
		Immutables:      make([]ImmutableMemTableSnapshot, 0, len(e.immutables)),
		ActiveWALPath:   e.walPath,
		ActiveLogNumber: e.logNumber,
	}
	for _, imm := range e.immutables {
		snapshot.Immutables = append(snapshot.Immutables, ImmutableMemTableSnapshot{
			LogNumber: imm.logNumber,
			WALPath:   imm.walPath,
			Table:     snapshotTable(imm.table, limit),
		})
	}
	return snapshot
}

func snapshotTable(table *memtable.MemTable, limit int) TableSnapshot {
	if table == nil {
		return TableSnapshot{}
	}

	entries := table.Entries()
	truncated := false
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
		truncated = true
	}

	out := TableSnapshot{
		ApproximateSize: table.ApproximateSize(),
		WALSeqNo:        table.WALSeqNo(),
		Entries:         make([]MemTableEntrySnapshot, 0, len(entries)),
		Truncated:       truncated,
	}
	for _, entry := range entries {
		out.Entries = append(out.Entries, MemTableEntrySnapshot{
			UserKey: append([]byte(nil), entry.Key.UserKey...),
			SeqNo:   entry.Key.SeqNo,
			Type:    entry.Key.Type,
			Value:   append([]byte(nil), entry.Value...),
		})
	}
	return out
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// Scan returns up to limit key-value pairs whose keys fall in [start, end).
// Empty start means "from beginning"; empty end means "to end".
// Performs a snapshot read, deduplicating across all levels.
func (e *LSMEngine) Scan(start, end []byte, limit int) [][2]string {
	e.mu.RLock()
	memEntries := e.memTable.Entries()
	immEntries := make([][]memtable.Entry, len(e.immutables))
	for i, imm := range e.immutables {
		immEntries[i] = imm.table.Entries()
	}
	e.mu.RUnlock()

	version := e.manifest.Current()

	type winEntry struct {
		value   []byte
		seqNo   uint64
		deleted bool
		fromMem bool // memtable entries always beat SSTable entries
	}
	winners := make(map[string]winEntry)

	// addSST: first SSTable entry for a key wins (L0 processed newest-first,
	// so the first entry is the most recent SSTable version).
	addSST := func(userKey, value []byte, deleted bool) {
		k := string(userKey)
		if _, ok := winners[k]; ok {
			return // already have a higher-priority entry
		}
		var v []byte
		if !deleted && len(value) > 0 {
			v = append([]byte{}, value...)
		}
		winners[k] = winEntry{value: v, seqNo: 0, deleted: deleted, fromMem: false}
	}

	// addMem: memtable entries always override SST entries; between
	// memtable entries use SeqNo to keep the most recent one.
	addMem := func(userKey, value []byte, seqNo uint64, deleted bool) {
		k := string(userKey)
		if ex, ok := winners[k]; ok && ex.fromMem && ex.seqNo >= seqNo {
			return
		}
		var v []byte
		if !deleted && len(value) > 0 {
			v = append([]byte{}, value...)
		}
		winners[k] = winEntry{value: v, seqNo: seqNo, deleted: deleted, fromMem: true}
	}

	// Hold readersMu for the entire SSTable scan.
	e.readersMu.RLock()
	defer e.readersMu.RUnlock()

	// 1. L1+ SSTables (lowest priority; no overlapping ranges at each level)
	for lvl := 1; lvl < manifest.MaxLevels; lvl++ {
		for _, meta := range version.Levels[lvl] {
			if !tableOverlapsRange(meta.FirstKey, meta.LastKey, start, end) {
				continue
			}
			reader, ok := e.readers[meta.FileID]
			if !ok {
				continue
			}
			it := reader.NewIterator()
			for it.Valid() {
				ik := it.Key()
				if len(start) > 0 && bytes.Compare(ik.UserKey, start) < 0 {
					it.Next()
					continue
				}
				if len(end) > 0 && bytes.Compare(ik.UserKey, end) >= 0 {
					break
				}
				addSST(ik.UserKey, it.Value(), ik.Type == sstable.TypeDeletion)
				it.Next()
			}
		}
	}

	// 2. L0 SSTables newest-first (higher priority than L1+)
	l0 := make([]*sstable.SSTableMeta, len(version.Levels[0]))
	copy(l0, version.Levels[0])
	sort.Slice(l0, func(i, j int) bool { return l0[i].FileID > l0[j].FileID })
	for _, meta := range l0 {
		if !tableOverlapsRange(meta.FirstKey, meta.LastKey, start, end) {
			continue
		}
		reader, ok := e.readers[meta.FileID]
		if !ok {
			continue
		}
		it := reader.NewIterator()
		for it.Valid() {
			ik := it.Key()
			if len(start) > 0 && bytes.Compare(ik.UserKey, start) < 0 {
				it.Next()
				continue
			}
			if len(end) > 0 && bytes.Compare(ik.UserKey, end) >= 0 {
				break
			}
			addSST(ik.UserKey, it.Value(), ik.Type == sstable.TypeDeletion)
			it.Next()
		}
	}

	// 3. Immutable MemTables oldest-first (higher priority than SSTs)
	for i := 0; i < len(immEntries); i++ {
		for _, entry := range immEntries[i] {
			if len(start) > 0 && bytes.Compare(entry.Key.UserKey, start) < 0 {
				continue
			}
			if len(end) > 0 && bytes.Compare(entry.Key.UserKey, end) >= 0 {
				break
			}
			addMem(entry.Key.UserKey, entry.Value, entry.Key.SeqNo, entry.Key.Type == sstable.TypeDeletion)
		}
	}

	// 4. Mutable MemTable (highest priority)
	for _, entry := range memEntries {
		if len(start) > 0 && bytes.Compare(entry.Key.UserKey, start) < 0 {
			continue
		}
		if len(end) > 0 && bytes.Compare(entry.Key.UserKey, end) >= 0 {
			break
		}
		addMem(entry.Key.UserKey, entry.Value, entry.Key.SeqNo, entry.Key.Type == sstable.TypeDeletion)
	}

	// Collect live keys in range, sort, and limit
	keys := make([]string, 0, len(winners))
	for k, w := range winners {
		if w.deleted {
			continue
		}
		if len(start) > 0 && bytes.Compare([]byte(k), start) < 0 {
			continue
		}
		if len(end) > 0 && bytes.Compare([]byte(k), end) >= 0 {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	results := make([][2]string, len(keys))
	for i, k := range keys {
		results[i] = [2]string{k, string(winners[k].value)}
	}
	return results
}

func findLevelCandidate(level []*sstable.SSTableMeta, key []byte) *sstable.SSTableMeta {
	if len(level) == 0 {
		return nil
	}
	idx := sort.Search(len(level), func(i int) bool {
		return bytes.Compare(level[i].FirstKey, key) > 0
	})
	if idx == 0 {
		return nil
	}
	candidate := level[idx-1]
	if len(candidate.LastKey) > 0 && bytes.Compare(key, candidate.LastKey) > 0 {
		return nil
	}
	return candidate
}

func tableOverlapsRange(firstKey, lastKey, start, end []byte) bool {
	if len(end) > 0 && len(firstKey) > 0 && bytes.Compare(firstKey, end) >= 0 {
		return false
	}
	if len(start) > 0 && len(lastKey) > 0 && bytes.Compare(lastKey, start) < 0 {
		return false
	}
	return true
}
