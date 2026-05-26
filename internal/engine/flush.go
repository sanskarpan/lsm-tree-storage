// Package engine — see config.go for the package doc.
package engine

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"
	"lsm-engine/internal/sstable"
)

func (e *LSMEngine) flushWorker() {
	defer e.flushWG.Done()
	for {
		select {
		case <-e.closeCh:
			// Drain remaining flushes before exiting
			for {
				select {
				case imm := <-e.flushQueue:
					if err := e.flush(imm); err != nil {
						log.Printf("flush error during shutdown: %v", err)
					}
				default:
					return
				}
			}
		case imm := <-e.flushQueue:
			if err := e.flush(imm); err != nil {
				log.Printf("flush error: %v", err)
				// Even on error, remove from immutables and broadcast to unblock ForceFlush
				e.mu.Lock()
				e.removeImmutable(imm)
				e.flushCond.Broadcast()
				e.mu.Unlock()
			}
			// Trigger compaction check after flush
			select {
			case e.compactTrigger <- struct{}{}:
			default:
			}
		}
	}
}

// removeImmutable removes imm from the immutables list (must hold e.mu)
func (e *LSMEngine) removeImmutable(imm *immutableMemtable) {
	newImm := make([]*immutableMemtable, 0, len(e.immutables))
	for _, m := range e.immutables {
		if m != imm {
			newImm = append(newImm, m)
		}
	}
	e.immutables = newImm
}

func (e *LSMEngine) flush(imm *immutableMemtable) error {
	e.bus.Publish(events.Event{Type: events.EvtFlushStart, Extra: map[string]interface{}{
		"size": imm.table.ApproximateSize(),
	}})

	fileID := e.nextFileID.Add(1)
	path := filepath.Join(e.cfg.DataDir, fmt.Sprintf("%06d.sst", fileID))

	builder, err := sstable.NewSSTableBuilder(path, fileID, 0, e.cfg.BlockSize, e.cfg.BloomBitsPerKey)
	if err != nil {
		return fmt.Errorf("create sstable builder: %w", err)
	}

	iter := imm.table.NewIterator()
	for iter.Valid() {
		if err := builder.Add(iter.Key(), iter.Value()); err != nil {
			_ = builder.Close()
			_ = os.Remove(path)
			return fmt.Errorf("add to sstable: %w", err)
		}
		iter.Next()
	}

	if builder.NumEntries() == 0 {
		_ = builder.Close()
		_ = os.Remove(path)
		// Still remove from immutables and broadcast
		e.mu.Lock()
		e.removeImmutable(imm)
		e.flushCond.Broadcast()
		e.mu.Unlock()
		return nil
	}

	meta, err := builder.Finish()
	if err != nil {
		_ = builder.Close()
		_ = os.Remove(path)
		return fmt.Errorf("finish sstable: %w", err)
	}
	_ = builder.Close()

	// Open/register the reader before publishing the SSTable through the manifest.
	meta.FilePath = path
	reader, err := sstable.NewSSTableReader(path, meta, e.cache, e.bus)
	if err != nil {
		return fmt.Errorf("open sstable reader: %w", err)
	}
	e.registerReader(fileID, reader)
	edit := manifest.VersionEdit{
		Type:     manifest.EditAddSSTable,
		Level:    0,
		FileID:   fileID,
		FileSize: meta.FileSize,
		FirstKey: meta.FirstKey,
		LastKey:  meta.LastKey,
	}
	if err := e.manifest.Apply(edit); err != nil {
		e.unregisterReader(fileID)
		_ = os.Remove(path) // orphaned; will be cleaned on next recovery
		return fmt.Errorf("manifest apply: %w", err)
	}

	e.mu.Lock()
	// Remove from immutables and notify ForceFlush waiters
	e.removeImmutable(imm)
	e.flushCond.Broadcast()
	e.mu.Unlock()

	if imm.walPath != "" {
		if err := os.Remove(imm.walPath); err != nil && !os.IsNotExist(err) {
			log.Printf("warn: remove immutable wal %s: %v", imm.walPath, err)
		}
	}

	e.bus.Publish(events.Event{Type: events.EvtFlushComplete, Extra: map[string]interface{}{
		"file_id": fileID, "level": 0, "size": meta.FileSize,
	}})

	return nil
}
