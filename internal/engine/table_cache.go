// Package engine — see config.go for the package doc.
package engine

import (
	"container/list"
	"log"
	"sync"

	"lsm-engine/internal/cache"
	"lsm-engine/internal/events"
	"lsm-engine/internal/sstable"
)

// tableCache is a bounded LRU cache of open SSTable file descriptors.
// It enforces MaxOpenFiles by evicting the least-recently-used reader
// whose reference count is zero when the cache is full.
//
// Readers that are currently being accessed (ref > 0) are pinned and
// cannot be evicted. This guarantees that an in-progress read always
// has a valid open file handle for the duration of the operation.
type tableCache struct {
	mu      sync.Mutex
	cap     int
	entries map[uint64]*tcEntry
	lruList *list.List // back = LRU, front = MRU; elements store *tcEntry
	cache   *cache.BlockCache
	bus     events.EventPublisher
}

type tcEntry struct {
	fileID   uint64
	elem     *list.Element // position in lruList; nil when removed from the list
	reader   *sstable.SSTableReader
	refs     int  // number of active get() callers currently pinning this entry
	deleted  bool // true if unregister() was called while refs > 0
}

// newTableCache creates a tableCache that holds at most cap open readers.
// If cap <= 0 the default of 1000 is used.
func newTableCache(cap int, bc *cache.BlockCache, bus events.EventPublisher) *tableCache {
	if cap <= 0 {
		cap = 1000
	}
	return &tableCache{
		cap:     cap,
		entries: make(map[uint64]*tcEntry),
		lruList: list.New(),
		cache:   bc,
		bus:     bus,
	}
}

// register opens a new SSTableReader for fileID and inserts it into the cache.
// If the cache is at capacity, the least-recently-used entry with refs==0 is
// evicted and closed to make room. If every entry is currently pinned (refs>0),
// the new reader is added without eviction and a warning is logged.
func (tc *tableCache) register(fileID uint64, meta sstable.SSTableMeta, filePath string) error {
	// Open the reader outside the lock to avoid holding tc.mu during I/O.
	reader, err := sstable.NewSSTableReader(filePath, meta, tc.cache, tc.bus)
	if err != nil {
		return err
	}

	tc.mu.Lock()
	defer tc.mu.Unlock()

	// If already registered (e.g. duplicate call), discard the new reader.
	if _, ok := tc.entries[fileID]; ok {
		_ = reader.Close()
		return nil
	}

	// Evict the LRU entry with refs==0 if we are at capacity.
	if len(tc.entries) >= tc.cap {
		evicted := false
		for elem := tc.lruList.Back(); elem != nil; elem = elem.Prev() {
			entry := elem.Value.(*tcEntry)
			if entry.refs == 0 && !entry.deleted {
				tc.lruList.Remove(elem)
				delete(tc.entries, entry.fileID)
				_ = entry.reader.Close()
				evicted = true
				break
			}
		}
		if !evicted {
			log.Printf("warn: tableCache: MaxOpenFiles (%d) exceeded; all %d readers are pinned — adding without eviction",
				tc.cap, len(tc.entries))
		}
	}

	entry := &tcEntry{
		fileID: fileID,
		reader: reader,
	}
	entry.elem = tc.lruList.PushFront(entry)
	tc.entries[fileID] = entry
	return nil
}

// get returns the SSTableReader for fileID together with a release function.
// The caller MUST invoke the release function exactly once when it is done
// accessing the reader (or any data that originated from the reader's mmap).
// Returns (nil, nil, false) if fileID is not in the cache.
func (tc *tableCache) get(fileID uint64) (*sstable.SSTableReader, func(), bool) {
	tc.mu.Lock()
	entry, ok := tc.entries[fileID]
	if !ok || entry.deleted {
		tc.mu.Unlock()
		return nil, nil, false
	}
	entry.refs++
	if entry.elem != nil {
		tc.lruList.MoveToFront(entry.elem)
	}
	tc.mu.Unlock()

	release := func() {
		tc.mu.Lock()
		entry.refs--
		shouldClose := entry.deleted && entry.refs == 0
		tc.mu.Unlock()
		if shouldClose {
			_ = entry.reader.Close()
		}
	}
	return entry.reader, release, true
}

// unregister removes fileID from the cache and schedules its reader to be
// closed. If the entry currently has active get() callers (refs > 0), it is
// marked as deleted and the last release() call will close the reader.
// If refs == 0 the reader is closed immediately.
func (tc *tableCache) unregister(fileID uint64) {
	tc.mu.Lock()
	entry, ok := tc.entries[fileID]
	if !ok {
		tc.mu.Unlock()
		return
	}
	delete(tc.entries, fileID)
	if entry.elem != nil {
		tc.lruList.Remove(entry.elem)
		entry.elem = nil
	}
	entry.deleted = true
	shouldClose := entry.refs == 0
	tc.mu.Unlock()

	if shouldClose {
		_ = entry.reader.Close()
	}
	// If refs > 0 the final release() will close the reader.
}

// close closes every open reader in the cache. It should only be called after
// all background workers have stopped (no concurrent get() calls in flight).
func (tc *tableCache) close() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for _, entry := range tc.entries {
		if entry.elem != nil {
			tc.lruList.Remove(entry.elem)
			entry.elem = nil
		}
		_ = entry.reader.Close()
	}
	tc.entries = make(map[uint64]*tcEntry)
}
