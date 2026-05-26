package memtable

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"lsm-engine/internal/sstable"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkipList_Insert(t *testing.T) {
	sl := NewSkipList()
	// Insert 10k keys in random order
	keys := make([]string, 10000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%06d", i)
	}
	perm := rand.Perm(10000)
	for _, idx := range perm {
		sl.Insert(sstable.InternalKey{
			UserKey: []byte(keys[idx]),
			SeqNo:   uint64(idx + 1),
			Type:    sstable.TypeValue,
		}, []byte("val"))
	}
	assert.Equal(t, 10000, sl.Len())

	// Iterate and verify sorted order
	it := sl.NewIterator()
	prev := ""
	for it.Valid() {
		cur := string(it.Key().UserKey)
		assert.True(t, cur > prev, "expected sorted: %q > %q", cur, prev)
		prev = cur
		it.Next()
	}
}

func TestSkipList_Find(t *testing.T) {
	sl := NewSkipList()
	for i := 0; i < 100; i++ {
		sl.Insert(sstable.InternalKey{
			UserKey: []byte(fmt.Sprintf("k%03d", i)),
			SeqNo:   uint64(i + 1),
			Type:    sstable.TypeValue,
		}, []byte(fmt.Sprintf("v%d", i)))
	}

	// Exact key lookup
	node := sl.FindLE(sstable.InternalKey{UserKey: []byte("k050"), SeqNo: ^uint64(0)})
	require.NotNil(t, node)
	assert.Equal(t, []byte("k050"), node.key.UserKey)

	// Missing key
	node = sl.FindLE(sstable.InternalKey{UserKey: []byte("zzz"), SeqNo: ^uint64(0)})
	assert.Nil(t, node)
}

func TestMemTable_Tombstone(t *testing.T) {
	m := NewMemTable(64 * 1024 * 1024)
	m.Put([]byte("k1"), []byte("v1"), 1)
	m.Delete([]byte("k1"), 2)

	_, found, deleted := m.Get([]byte("k1"))
	assert.True(t, found)
	assert.True(t, deleted)
}

func TestMemTable_TombstoneVisibility(t *testing.T) {
	m := NewMemTable(64 * 1024 * 1024)
	m.Put([]byte("k1"), []byte("v1"), 1)
	m.Delete([]byte("k1"), 2) // tombstone at higher seqNo

	val, found, deleted := m.Get([]byte("k1"))
	assert.True(t, found)
	assert.True(t, deleted)
	assert.Nil(t, val)

	// Older seqNo should still find the value (snapshot read)
	val, found, deleted = m.GetAtSeqNo([]byte("k1"), 1)
	assert.True(t, found)
	assert.False(t, deleted)
	assert.Equal(t, []byte("v1"), val)
}

func TestMemTable_MultipleVersions(t *testing.T) {
	m := NewMemTable(64 * 1024 * 1024)
	m.Put([]byte("k1"), []byte("v1"), 1)
	m.Put([]byte("k1"), []byte("v5"), 5)

	val, found, deleted := m.Get([]byte("k1"))
	assert.True(t, found)
	assert.False(t, deleted)
	assert.Equal(t, []byte("v5"), val)
}

func TestMemTable_IteratorOrder(t *testing.T) {
	m := NewMemTable(64 * 1024 * 1024)
	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%06d", rand.Intn(100000))
	}
	for i, k := range keys {
		m.Put([]byte(k), []byte("v"), uint64(i+1))
	}

	// Deduplicate for expected sorted order
	seen := make(map[string]struct{})
	unique := []string{}
	for _, k := range keys {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			unique = append(unique, k)
		}
	}
	sort.Strings(unique)

	it := m.NewIterator()
	var result []string
	prevKey := ""
	for it.Valid() {
		cur := string(it.Key().UserKey)
		if cur != prevKey {
			result = append(result, cur)
			prevKey = cur
		}
		it.Next()
	}
	assert.Equal(t, unique, result)
}

func TestMemTable_SizeAccounting(t *testing.T) {
	m := NewMemTable(64 * 1024 * 1024)
	const N = 1000
	keySize := 10
	valSize := 20
	for i := 0; i < N; i++ {
		m.Put([]byte(fmt.Sprintf("%010d", i)), []byte(fmt.Sprintf("%020d", i)), uint64(i+1))
	}
	// Size should be approximately N * (keySize + valSize + 16)
	expected := int64(N * (keySize + valSize + 16))
	actual := m.ApproximateSize()
	// Allow 20% deviation
	assert.InDelta(t, float64(expected), float64(actual), float64(expected)*0.2)
}
