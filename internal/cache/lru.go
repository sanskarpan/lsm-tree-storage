// Package cache provides an LRU block cache used by SSTable readers to
// reduce redundant disk reads for hot data blocks.
package cache

import (
	"container/list"
	"sync"

	"lsm-engine/internal/events"
)

// CacheKey uniquely identifies a block (fileID + blockOffset)
type CacheKey struct {
	FileID uint64
	Offset uint64
}

// BlockCache is an LRU cache for SSTable data blocks
type BlockCache struct {
	mu       sync.Mutex
	capacity int64
	used     int64
	lru      *list.List
	index    map[CacheKey]*list.Element
	hits     int64
	misses   int64
	bus      events.EventPublisher
}

type cacheEntry struct {
	key   CacheKey
	data  []byte
	size  int64
}

// NewBlockCache creates an LRU block cache with the given capacity in bytes
func NewBlockCache(capacity int64, bus events.EventPublisher) *BlockCache {
	return &BlockCache{
		capacity: capacity,
		lru:      list.New(),
		index:    make(map[CacheKey]*list.Element),
		bus:      bus,
	}
}

// Get retrieves a block from the cache
func (c *BlockCache) Get(key CacheKey) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.index[key]; ok {
		c.lru.MoveToFront(elem)
		c.hits++
		if c.bus != nil {
			c.bus.Publish(events.Event{Type: events.EvtCacheHit, Extra: map[string]interface{}{
				"file_id": key.FileID, "offset": key.Offset,
			}})
		}
		return elem.Value.(*cacheEntry).data, true
	}
	c.misses++
	if c.bus != nil {
		c.bus.Publish(events.Event{Type: events.EvtCacheMiss, Extra: map[string]interface{}{
			"file_id": key.FileID, "offset": key.Offset,
		}})
	}
	return nil, false
}

// Insert adds a block to the cache, evicting LRU entries as needed
func (c *BlockCache) Insert(key CacheKey, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	size := int64(len(data))

	// If already in cache, update
	if elem, ok := c.index[key]; ok {
		old := elem.Value.(*cacheEntry)
		c.used -= old.size
		old.data = data
		old.size = size
		c.used += size
		c.lru.MoveToFront(elem)
		return
	}

	// Evict LRU entries to make room
	for c.used+size > c.capacity && c.lru.Len() > 0 {
		back := c.lru.Back()
		if back == nil {
			break
		}
		entry := back.Value.(*cacheEntry)
		c.lru.Remove(back)
		delete(c.index, entry.key)
		c.used -= entry.size
	}

	entry := &cacheEntry{key: key, data: data, size: size}
	elem := c.lru.PushFront(entry)
	c.index[key] = elem
	c.used += size
}

// HitRate returns the cache hit rate since creation
func (c *BlockCache) HitRate() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}

// Len returns the number of entries in the cache
func (c *BlockCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}

// CacheStats holds a point-in-time snapshot of cache metrics.
type CacheStats struct {
	Hits       int64
	Misses     int64
	HitRate    float64
	UsedBytes  int64
	NumEntries int
}

// Stats returns a snapshot of cache metrics under a single lock acquisition.
func (c *BlockCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := c.hits + c.misses
	var rate float64
	if total > 0 {
		rate = float64(c.hits) / float64(total)
	}
	return CacheStats{
		Hits:       c.hits,
		Misses:     c.misses,
		HitRate:    rate,
		UsedBytes:  c.used,
		NumEntries: c.lru.Len(),
	}
}
