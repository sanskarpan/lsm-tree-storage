// Package engine — see config.go for the package doc.
package engine

import (
	"bytes"
	"container/heap"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"lsm-engine/internal/compaction"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"
	"lsm-engine/internal/sstable"
)

func (e *LSMEngine) compactionWorker() {
	defer e.compactWG.Done()
	for {
		select {
		case <-e.closeCh:
			return
		case <-e.compactTrigger:
			e.runCompaction()
		}
	}
}

func (e *LSMEngine) runCompaction() {
	e.mu.RLock()
	style := e.cfg.CompactionStyle
	e.mu.RUnlock()

	switch style {
	case "size-tiered":
		e.runSTCSCompaction()
	case "time-window":
		e.runTWCSCompaction()
	default:
		e.runLeveledCompaction()
	}
}

func (e *LSMEngine) runLeveledCompaction() {
	version := e.manifest.Current()

	maxLevel := manifest.MaxLevels
	if e.cfg.MaxLevels > 0 {
		maxLevel = e.cfg.MaxLevels
	}

	// L0 trigger
	if len(version.Levels[0]) >= e.cfg.Level0FileNumCompactionTrigger {
		if err := e.compactLevel(version, 0); err != nil {
			log.Printf("L0 compaction error: %v", err)
		}
		version = e.manifest.Current()
	}

	// L1..L(maxLevel-2) triggers — L(maxLevel-1) is bottom
	for level := 1; level < maxLevel-1; level++ {
		maxSize := e.levelMaxSize(level)
		var totalSize uint64
		for _, meta := range version.Levels[level] {
			totalSize += meta.FileSize
		}
		if totalSize > maxSize {
			if err := e.compactLevel(version, level); err != nil {
				log.Printf("L%d compaction error: %v", level, err)
			}
			version = e.manifest.Current()
		}
	}
}

func (e *LSMEngine) runSTCSCompaction() {
	version := e.manifest.Current()
	// Collect all L0 sstables for STCS grouping
	all := make([]*sstable.SSTableMeta, len(version.Levels[0]))
	copy(all, version.Levels[0])

	cfg := compaction.DefaultSTCSConfig()
	inputs := compaction.STCSPickInputs(all, cfg)
	if inputs == nil {
		return
	}
	if err := e.executeCompaction(inputs, 0, 1); err != nil {
		log.Printf("STCS compaction error: %v", err)
	}
}

func (e *LSMEngine) runTWCSCompaction() {
	version := e.manifest.Current()
	all := make([]*sstable.SSTableMeta, len(version.Levels[0]))
	copy(all, version.Levels[0])

	cfg := compaction.DefaultTWCSConfig()
	if e.cfg.TimeWindowSize > 0 {
		cfg.WindowSize = e.cfg.TimeWindowSize
	}
	inputs := compaction.TWCSPickInputs(all, cfg, time.Now())
	if inputs == nil {
		return
	}
	if err := e.executeCompaction(inputs, 0, 1); err != nil {
		log.Printf("TWCS compaction error: %v", err)
	}
}

func (e *LSMEngine) levelMaxSize(level int) uint64 {
	base := uint64(10 * 1024 * 1024) // 10MB for L1
	mult := uint64(10)
	if e.cfg.LevelSizeMultiplier > 0 {
		mult = uint64(e.cfg.LevelSizeMultiplier)
	}
	size := base
	for i := 1; i < level; i++ {
		size *= mult
	}
	return size
}

func (e *LSMEngine) compactLevel(version *manifest.Version, level int) error {
	maxLevel := manifest.MaxLevels
	if e.cfg.MaxLevels > 0 {
		maxLevel = e.cfg.MaxLevels
	}
	outputLevel := level + 1
	if outputLevel >= maxLevel {
		return nil // already at bottom level
	}

	var inputs []*sstable.SSTableMeta

	if level == 0 {
		inputs = make([]*sstable.SSTableMeta, len(version.Levels[0]))
		copy(inputs, version.Levels[0])
	} else {
		if len(version.Levels[level]) == 0 {
			return nil
		}
		inputs = []*sstable.SSTableMeta{version.Levels[level][0]}
	}

	if len(inputs) == 0 {
		return nil
	}

	// Compute key range of inputs
	var minKey, maxKey []byte
	for _, meta := range inputs {
		if minKey == nil || bytes.Compare(meta.FirstKey, minKey) < 0 {
			minKey = meta.FirstKey
		}
		if maxKey == nil || bytes.Compare(meta.LastKey, maxKey) > 0 {
			maxKey = meta.LastKey
		}
	}

	// Add overlapping SSTables from outputLevel
	for _, meta := range version.Levels[outputLevel] {
		if overlapsRange(meta.FirstKey, meta.LastKey, minKey, maxKey) {
			inputs = append(inputs, meta)
		}
	}

	// Deduplicate
	seen := make(map[uint64]bool)
	unique := inputs[:0]
	for _, m := range inputs {
		if !seen[m.FileID] {
			seen[m.FileID] = true
			unique = append(unique, m)
		}
	}
	inputs = unique

	return e.executeCompaction(inputs, level, outputLevel)
}

func overlapsRange(a1, a2, b1, b2 []byte) bool {
	return bytes.Compare(a1, b2) <= 0 && bytes.Compare(b1, a2) <= 0
}

type mergeEntry struct {
	key   sstable.InternalKey
	value []byte
	iter  sstableIter
	idx   int
}

type sstableIter interface {
	Valid() bool
	Key() sstable.InternalKey
	Value() []byte
	Next()
}

type mergeHeap []*mergeEntry

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	ki, kj := h[i].key, h[j].key
	if ki.Less(kj) {
		return true
	}
	if kj.Less(ki) {
		return false
	}
	// Tiebreak: lower idx = newer (for L0 files sorted newest first)
	return h[i].idx < h[j].idx
}
func (h mergeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(x interface{}) { *h = append(*h, x.(*mergeEntry)) }
func (h *mergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func (e *LSMEngine) executeCompaction(inputs []*sstable.SSTableMeta, inputLevel, outputLevel int) error {
	e.bus.Publish(events.Event{Type: events.EvtCompactionStart, Extra: map[string]interface{}{
		"input_level": inputLevel, "output_level": outputLevel, "num_inputs": len(inputs),
	}})

	maxLevel := manifest.MaxLevels
	if e.cfg.MaxLevels > 0 {
		maxLevel = e.cfg.MaxLevels
	}
	isBottomLevel := outputLevel == maxLevel-1

	// Sort L0 inputs newest-first for heap tiebreaking
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Level != inputs[j].Level {
			return inputs[i].Level < inputs[j].Level
		}
		if inputs[i].Level == 0 {
			return inputs[i].FileID > inputs[j].FileID
		}
		return inputs[i].FileID < inputs[j].FileID
	})

	// Build merge heap.
	// Hold readersMu.RLock while accessing readers to prevent concurrent unregister.
	h := &mergeHeap{}
	heap.Init(h)

	e.readersMu.RLock()
	for idx, meta := range inputs {
		reader, ok := e.readers[meta.FileID]
		if !ok {
			log.Printf("warn: compaction input reader missing for file %d", meta.FileID)
			continue
		}
		iter := reader.NewIterator()
		iter.SeekToFirst()
		if iter.Valid() {
			heap.Push(h, &mergeEntry{
				key:   iter.Key(),
				value: iter.Value(),
				iter:  iter,
				idx:   idx,
			})
		}
	}
	e.readersMu.RUnlock()

	var outputs []sstable.SSTableMeta
	var builder *sstable.SSTableBuilder
	var prevUserKey []byte

	newFile := func() error {
		if builder != nil {
			if builder.NumEntries() > 0 {
				meta, err := builder.Finish()
				if err != nil {
					return err
				}
				_ = builder.Close()
				outputs = append(outputs, meta)
			} else {
				_ = builder.Close()
			}
		}
		fileID := e.nextFileID.Add(1)
		path := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.sst", fileID))
		var err error
		builder, err = sstable.NewSSTableBuilder(path, fileID, outputLevel, e.cfg.BlockSize, e.cfg.BloomBitsPerKey)
		return err
	}

	if err := newFile(); err != nil {
		return fmt.Errorf("create first output file: %w", err)
	}

	for h.Len() > 0 {
		top := heap.Pop(h).(*mergeEntry)
		key := top.key
		val := top.value

		// Deduplication: skip older versions of the same user key
		if prevUserKey != nil && bytes.Equal(key.UserKey, prevUserKey) {
			top.iter.Next()
			if top.iter.Valid() {
				top.key = top.iter.Key()
				top.value = top.iter.Value()
				heap.Push(h, top)
			}
			continue
		}

		prevUserKey = append(prevUserKey[:0], key.UserKey...)

		// Drop tombstones at bottom level (no older data can exist below)
		if key.Type == sstable.TypeDeletion && isBottomLevel {
			e.bus.Publish(events.Event{Type: events.EvtTombstoneDropped,
				Extra: map[string]interface{}{"key": string(key.UserKey)}})
			top.iter.Next()
			if top.iter.Valid() {
				top.key = top.iter.Key()
				top.value = top.iter.Value()
				heap.Push(h, top)
			}
			continue
		}

		// Roll to next output file if too large
		if builder.ApproxSize() >= e.cfg.SSTMaxSize {
			if err := newFile(); err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
		}

		if err := builder.Add(key, val); err != nil {
			return fmt.Errorf("add key to compaction output: %w", err)
		}
		e.bus.Publish(events.Event{Type: events.EvtCompactionMerge, Extra: map[string]interface{}{
			"key": string(key.UserKey), "type": key.Type, "level_out": outputLevel,
		}})

		top.iter.Next()
		if top.iter.Valid() {
			top.key = top.iter.Key()
			top.value = top.iter.Value()
			heap.Push(h, top)
		}
	}

	// Finalize last output file
	if builder != nil {
		if builder.NumEntries() > 0 {
			meta, err := builder.Finish()
			if err != nil {
				return fmt.Errorf("finish output file: %w", err)
			}
			_ = builder.Close()
			outputs = append(outputs, meta)
		} else {
			_ = builder.Close()
		}
	}

	// Open/register output readers before publishing manifest changes so the
	// visible version never references an SSTable without an in-memory reader.
	outputReaders := make(map[uint64]*sstable.SSTableReader, len(outputs))
	for i := range outputs {
		outputs[i].FilePath = filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.sst", outputs[i].FileID))
		reader, err := sstable.NewSSTableReader(outputs[i].FilePath, outputs[i], e.cache, e.bus)
		if err != nil {
			for _, opened := range outputReaders {
				_ = opened.Close()
			}
			return fmt.Errorf("open compaction output reader %d: %w", outputs[i].FileID, err)
		}
		outputReaders[outputs[i].FileID] = reader
		e.registerReader(outputs[i].FileID, reader)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		for _, meta := range outputs {
			e.unregisterReader(meta.FileID)
			if meta.FilePath != "" {
				_ = os.Remove(meta.FilePath)
			}
		}
	}()

	// Atomic commit order (crash-safe):
	//   1. Output SSTables already fsynced by builder.Finish()
	//   2. Output readers registered
	//   3. Apply MANIFEST: add outputs
	//   4. Apply MANIFEST: delete inputs
	//   5. Unregister and delete input files
	for i := range outputs {
		if err := e.manifest.Apply(manifest.VersionEdit{
			Type:             manifest.EditAddSSTable,
			Level:            outputLevel,
			FileID:           outputs[i].FileID,
			FileSize:         outputs[i].FileSize,
			FirstKey:         outputs[i].FirstKey,
			LastKey:          outputs[i].LastKey,
			SkipOverlapCheck: true, // inputs not yet deleted; transient overlap is expected
		}); err != nil {
			return fmt.Errorf("manifest add output %d: %w", outputs[i].FileID, err)
		}
	}
	for _, meta := range inputs {
		if err := e.manifest.Apply(manifest.VersionEdit{
			Type:   manifest.EditDeleteSSTable,
			Level:  meta.Level,
			FileID: meta.FileID,
		}); err != nil {
			log.Printf("warn: manifest delete input %d: %v", meta.FileID, err)
		}
	}

	// Unregister and delete input files (unregisterReader waits for in-progress reads)
	for _, meta := range inputs {
		e.unregisterReader(meta.FileID)
		path := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.sst", meta.FileID))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("warn: remove input SSTable %s: %v", path, err)
		}
	}
	committed = true

	e.bus.Publish(events.Event{Type: events.EvtCompactionComplete, Extra: map[string]interface{}{
		"input_level": inputLevel, "output_level": outputLevel,
		"num_inputs": len(inputs), "num_outputs": len(outputs),
	}})

	return nil
}
