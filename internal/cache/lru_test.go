package cache

import (
	"fmt"
	"testing"

	"lsm-engine/internal/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU_Eviction(t *testing.T) {
	// Capacity for 3 blocks of 100 bytes each
	c := NewBlockCache(300, &events.NoopBus{})

	for i := 0; i < 3; i++ {
		c.Insert(CacheKey{FileID: 1, Offset: uint64(i * 100)}, make([]byte, 100))
	}
	assert.Equal(t, 3, c.Len())

	// First item (Offset=0) is LRU; inserting a 4th should evict it
	c.Insert(CacheKey{FileID: 1, Offset: 300}, make([]byte, 100))
	assert.Equal(t, 3, c.Len())

	_, found := c.Get(CacheKey{FileID: 1, Offset: 0})
	assert.False(t, found, "LRU item should have been evicted")

	_, found = c.Get(CacheKey{FileID: 1, Offset: 300})
	assert.True(t, found, "newest item should be in cache")
}

func TestLRU_HitRate(t *testing.T) {
	c := NewBlockCache(1024*1024, &events.NoopBus{})

	// Insert 100 blocks
	for i := 0; i < 100; i++ {
		c.Insert(CacheKey{FileID: 1, Offset: uint64(i)}, []byte(fmt.Sprintf("block-%d", i)))
	}

	hits := 0
	for i := 0; i < 1000; i++ {
		// ~70% hits (keys 0-69 exist)
		var key CacheKey
		if i%10 < 7 { // 70% of time, use keys 0-99 (all exist)
			key = CacheKey{FileID: 1, Offset: uint64(i % 100)}
		} else {
			key = CacheKey{FileID: 1, Offset: uint64(i + 1000)} // miss
		}
		_, found := c.Get(key)
		if found {
			hits++
		}
	}

	hr := c.HitRate()
	require.Greater(t, hr, 0.65, "hit rate should be > 65%%")
}

func TestLRU_UpdateExisting(t *testing.T) {
	c := NewBlockCache(1000, &events.NoopBus{})
	key := CacheKey{FileID: 1, Offset: 0}
	c.Insert(key, []byte("v1"))
	c.Insert(key, []byte("v2updated"))

	data, found := c.Get(key)
	assert.True(t, found)
	assert.Equal(t, []byte("v2updated"), data)
	assert.Equal(t, 1, c.Len())
}
