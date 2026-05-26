package sstable

import (
	"fmt"
	"path/filepath"
	"testing"

	"lsm-engine/internal/cache"
	"lsm-engine/internal/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestSSTable(t *testing.T, n int) (string, SSTableMeta) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")
	builder, err := NewSSTableBuilder(path, 1, 0, 4096, 10)
	require.NoError(t, err)

	for i := 0; i < n; i++ {
		key := InternalKey{
			UserKey: []byte(fmt.Sprintf("key-%06d", i)),
			SeqNo:   uint64(i + 1),
			Type:    TypeValue,
		}
		err := builder.Add(key, []byte(fmt.Sprintf("val-%06d", i)))
		require.NoError(t, err)
	}
	meta, err := builder.Finish()
	require.NoError(t, err)
	require.NoError(t, builder.Close())
	return path, meta
}

func TestSSTable_BuildAndReadBack(t *testing.T) {
	path, meta := buildTestSSTable(t, 1000)
	bc := cache.NewBlockCache(10*1024*1024, &events.NoopBus{})
	reader, err := NewSSTableReader(path, meta, bc, &events.NoopBus{})
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%06d", i))
		val, found, err := reader.Get(key, ^uint64(0))
		require.NoError(t, err)
		require.True(t, found, "key %s not found", key)
		assert.Equal(t, []byte(fmt.Sprintf("val-%06d", i)), val)
	}
}

func TestSSTable_BloomShortCircuit(t *testing.T) {
	path, meta := buildTestSSTable(t, 1000)
	bc := cache.NewBlockCache(10*1024*1024, &events.NoopBus{})
	reader, err := NewSSTableReader(path, meta, bc, &events.NoopBus{})
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	// Query keys that don't exist — most should be short-circuited by bloom
	falsePositives := 0
	for i := 1000; i < 2000; i++ {
		key := []byte(fmt.Sprintf("key-%06d", i))
		_, found, err := reader.Get(key, ^uint64(0))
		require.NoError(t, err)
		if found {
			falsePositives++
		}
	}
	// FP rate < 2% for bpk=10
	assert.Less(t, falsePositives, 20, "too many bloom false positives: %d", falsePositives)
}

func TestSSTable_PrefixCompression(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefix.sst")
	builder, err := NewSSTableBuilder(path, 1, 0, 4096, 10)
	require.NoError(t, err)

	// 1000 keys with common prefix "user:"
	for i := 0; i < 1000; i++ {
		key := InternalKey{
			UserKey: []byte(fmt.Sprintf("user:%06d", i)),
			SeqNo:   uint64(i + 1),
			Type:    TypeValue,
		}
		require.NoError(t, builder.Add(key, []byte("v")))
	}
	meta, err := builder.Finish()
	require.NoError(t, err)
	require.NoError(t, builder.Close())

	// Without prefix compression, each key = 12 bytes + overhead
	// With prefix compression, shared "user:" prefix is amortized
	naiveSize := uint64(1000 * (12 + 1 + 20)) // key + val + overheads
	assert.Less(t, meta.FileSize, naiveSize, "prefix compression should reduce file size")
}

func TestSSTable_TombstonePreservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tomb.sst")
	builder, err := NewSSTableBuilder(path, 1, 0, 4096, 10)
	require.NoError(t, err)

	// Write a live key and a tombstone
	require.NoError(t, builder.Add(InternalKey{UserKey: []byte("alive"), SeqNo: 1, Type: TypeValue}, []byte("v1")))
	require.NoError(t, builder.Add(InternalKey{UserKey: []byte("dead"), SeqNo: 2, Type: TypeDeletion}, nil))
	meta, err := builder.Finish()
	require.NoError(t, err)
	require.NoError(t, builder.Close())

	bc := cache.NewBlockCache(1024*1024, &events.NoopBus{})
	reader, err := NewSSTableReader(path, meta, bc, &events.NoopBus{})
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	val, found, err := reader.Get([]byte("alive"), ^uint64(0))
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("v1"), val)

	val, found, err = reader.Get([]byte("dead"), ^uint64(0))
	require.NoError(t, err)
	assert.True(t, found, "tombstone should be found (deleted=true)")
	assert.Nil(t, val)
}

func TestSSTable_Iterator(t *testing.T) {
	path, meta := buildTestSSTable(t, 500)
	bc := cache.NewBlockCache(10*1024*1024, &events.NoopBus{})
	reader, err := NewSSTableReader(path, meta, bc, &events.NoopBus{})
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	it := reader.NewIterator()
	count := 0
	prev := ""
	for it.Valid() {
		cur := string(it.Key().UserKey)
		assert.True(t, cur > prev, "expected sorted: %q > %q", cur, prev)
		prev = cur
		count++
		it.Next()
	}
	assert.Equal(t, 500, count)
}

func TestSSTable_PreservesSequenceNumbersForSnapshotReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.sst")
	builder, err := NewSSTableBuilder(path, 1, 0, 4096, 10)
	require.NoError(t, err)

	require.NoError(t, builder.Add(InternalKey{UserKey: []byte("snap"), SeqNo: 5, Type: TypeValue}, []byte("v5")))
	require.NoError(t, builder.Add(InternalKey{UserKey: []byte("snap"), SeqNo: 3, Type: TypeDeletion}, nil))
	require.NoError(t, builder.Add(InternalKey{UserKey: []byte("snap"), SeqNo: 1, Type: TypeValue}, []byte("v1")))

	meta, err := builder.Finish()
	require.NoError(t, err)
	require.NoError(t, builder.Close())

	bc := cache.NewBlockCache(1024*1024, &events.NoopBus{})
	reader, err := NewSSTableReader(path, meta, bc, &events.NoopBus{})
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	_, found, err := reader.Get([]byte("snap"), 0)
	require.NoError(t, err)
	assert.False(t, found)

	val, found, err := reader.Get([]byte("snap"), 1)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("v1"), val)

	val, found, err = reader.Get([]byte("snap"), 4)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Nil(t, val)

	val, found, err = reader.Get([]byte("snap"), 5)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("v5"), val)
}
