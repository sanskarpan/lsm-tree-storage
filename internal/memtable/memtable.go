// Package memtable — see skiplist.go for the package doc.
package memtable

import (
	"math"
	"sync"

	"lsm-engine/internal/sstable"
)

// Iterator is the common interface for iterators across components
type Iterator interface {
	Valid() bool
	Key() sstable.InternalKey
	Value() []byte
	Next()
}

// Entry is a point-in-time copy of a MemTable record.
type Entry struct {
	Key   sstable.InternalKey
	Value []byte
}

// MemTable is a mutable in-memory sorted table backed by a skip list
type MemTable struct {
	mu      sync.RWMutex
	sl      *SkipList
	size    int64
	maxSize int64
	walSeq  uint64
}

// NewMemTable creates a MemTable with the given max size in bytes
func NewMemTable(maxSize int64) *MemTable {
	return &MemTable{
		sl:      NewSkipList(),
		maxSize: maxSize,
	}
}

// Put inserts a key-value pair with the given sequence number
func (m *MemTable) Put(key, value []byte, seqNo uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keyCopy := append([]byte(nil), key...)
	valueCopy := append([]byte(nil), value...)
	ikey := sstable.InternalKey{UserKey: keyCopy, SeqNo: seqNo, Type: sstable.TypeValue}
	m.sl.Insert(ikey, valueCopy)
	m.size += int64(len(key) + len(value) + 16) // 16 for metadata overhead
	if seqNo > m.walSeq {
		m.walSeq = seqNo
	}
}

// Delete inserts a tombstone for the given key
func (m *MemTable) Delete(key []byte, seqNo uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keyCopy := append([]byte(nil), key...)
	ikey := sstable.InternalKey{UserKey: keyCopy, SeqNo: seqNo, Type: sstable.TypeDeletion}
	m.sl.Insert(ikey, nil)
	m.size += int64(len(key) + 16)
	if seqNo > m.walSeq {
		m.walSeq = seqNo
	}
}

// Get returns the most recent value for key.
// Returns (value, found, deleted) where deleted=true means tombstone.
func (m *MemTable) Get(key []byte) ([]byte, bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Search with MaxUint64 to find highest SeqNo version
	searchKey := sstable.InternalKey{
		UserKey: key,
		SeqNo:   math.MaxUint64,
		Type:    sstable.TypeValue,
	}
	node := m.sl.FindLE(searchKey)
	if node == nil {
		return nil, false, false
	}
	if node.key.Type == sstable.TypeDeletion {
		return nil, true, true
	}
	return node.value, true, false
}

// GetAtSeqNo returns the value for key at a specific sequence number (snapshot read)
func (m *MemTable) GetAtSeqNo(key []byte, seqNo uint64) ([]byte, bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	searchKey := sstable.InternalKey{
		UserKey: key,
		SeqNo:   seqNo,
		Type:    sstable.TypeDeletion, // TypeDeletion > TypeValue, so this finds the exact seqNo or the next lower
	}
	node := m.sl.FindLE(searchKey)
	if node == nil {
		return nil, false, false
	}
	if node.key.Type == sstable.TypeDeletion {
		return nil, true, true
	}
	return node.value, true, false
}

// ApproximateSize returns the approximate memory usage in bytes
func (m *MemTable) ApproximateSize() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size
}

// IsFull returns true if the table has exceeded its max size
func (m *MemTable) IsFull() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size >= m.maxSize
}

// WALSeqNo returns the highest WAL seqNo in this MemTable
func (m *MemTable) WALSeqNo() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.walSeq
}

// NewIterator returns a read-only iterator over the MemTable
func (m *MemTable) NewIterator() Iterator {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sl.NewIterator()
}

// Entries returns a consistent snapshot of all entries in the MemTable.
func (m *MemTable) Entries() []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]Entry, 0, m.sl.Len())
	for it := m.sl.NewIterator(); it.Valid(); it.Next() {
		key := it.Key()
		key.UserKey = append([]byte(nil), key.UserKey...)

		var valueCopy []byte
		if value := it.Value(); value != nil {
			valueCopy = append([]byte(nil), value...)
		}

		entries = append(entries, Entry{
			Key:   key,
			Value: valueCopy,
		})
	}

	return entries
}
