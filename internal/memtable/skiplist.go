// Package memtable provides an in-memory sorted table backed by a probabilistic skip list.
package memtable

import (
	"bytes"
	"math/rand"
	"time"

	"lsm-engine/internal/sstable"
)

const (
	// MaxLevel is the maximum number of levels in the skip list (controls memory/performance trade-off).
	MaxLevel = 12
	// Probability is the probability that a new node is promoted to the next skip list level.
	Probability = 0.25
)

// SkipListNode is a single node in the skip list, holding an InternalKey and its associated value.
type SkipListNode struct {
	key     sstable.InternalKey
	value   []byte
	forward [MaxLevel]*SkipListNode
	level   int
}

// SkipList is a concurrent-safe probabilistic sorted data structure for InternalKey/value pairs.
type SkipList struct {
	head   *SkipListNode
	level  int
	length int
	rng    *rand.Rand
}

// NewSkipList creates an empty SkipList seeded from the current time.
func NewSkipList() *SkipList {
	head := &SkipListNode{level: MaxLevel}
	return &SkipList{
		head:  head,
		level: 1,
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (sl *SkipList) randomLevel() int {
	level := 1
	for level < MaxLevel && sl.rng.Float64() < Probability {
		level++
	}
	return level
}

// Insert inserts or updates a key in the skip list
func (sl *SkipList) Insert(key sstable.InternalKey, value []byte) {
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
		for i := sl.level; i < newLevel; i++ {
			update[i] = sl.head
		}
		sl.level = newLevel
	}
	node := &SkipListNode{key: key, value: value, level: newLevel}
	for i := 0; i < newLevel; i++ {
		node.forward[i] = update[i].forward[i]
		update[i].forward[i] = node
	}
	sl.length++
}

// FindLE finds the node with the largest key <= searchKey
// For Get: search with key={userKey, MaxUint64, TypeValue} to find most recent version
func (sl *SkipList) FindLE(searchKey sstable.InternalKey) *SkipListNode {
	x := sl.head
	for i := sl.level - 1; i >= 0; i-- {
		for x.forward[i] != nil && x.forward[i].key.Less(searchKey) {
			x = x.forward[i]
		}
	}
	x = x.forward[0]
	if x == nil {
		return nil
	}
	// Return if same UserKey (regardless of SeqNo)
	if bytes.Equal(x.key.UserKey, searchKey.UserKey) {
		return x
	}
	return nil
}

// Len returns the number of entries
func (sl *SkipList) Len() int {
	return sl.length
}

// SkipListIterator is a forward-only iterator over the skip list
type SkipListIterator struct {
	current *SkipListNode
}

// NewIterator returns a forward-only iterator positioned at the first node.
func (sl *SkipList) NewIterator() *SkipListIterator {
	return &SkipListIterator{current: sl.head.forward[0]}
}

// Valid reports whether the iterator is positioned at a valid node.
func (it *SkipListIterator) Valid() bool {
	return it.current != nil
}

// Key returns the InternalKey at the current iterator position.
func (it *SkipListIterator) Key() sstable.InternalKey {
	return it.current.key
}

// Value returns the value at the current iterator position.
func (it *SkipListIterator) Value() []byte {
	return it.current.value
}

// Next advances the iterator to the next node in ascending key order.
func (it *SkipListIterator) Next() {
	if it.current != nil {
		it.current = it.current.forward[0]
	}
}
