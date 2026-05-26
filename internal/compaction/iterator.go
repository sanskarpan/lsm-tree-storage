// Package compaction — see leveled.go for the package doc.
package compaction

import (
	"bytes"
	"container/heap"

	"lsm-engine/internal/sstable"
)

// Iterator is the interface consumed by the MergeIterator.
type Iterator interface {
	Valid() bool
	Key() sstable.InternalKey
	Value() []byte
	Next()
	SeekToFirst()
}

// entry is one slot in the merge heap.
type entry struct {
	key   sstable.InternalKey
	value []byte
	iter  Iterator
	idx   int // lower idx = higher priority (newer file)
}

type minHeap []*entry

func (h minHeap) Len() int { return len(h) }
func (h minHeap) Less(i, j int) bool {
	ki, kj := h[i].key, h[j].key
	cmp := bytes.Compare(ki.UserKey, kj.UserKey)
	if cmp != 0 {
		return cmp < 0
	}
	if ki.SeqNo != kj.SeqNo {
		return ki.SeqNo > kj.SeqNo // higher seqNo sorts first (newer)
	}
	return h[i].idx < h[j].idx
}
func (h minHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{}) { *h = append(*h, x.(*entry)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// MergeIterator performs a k-way sorted merge over multiple iterators.
// Duplicate user keys are NOT automatically deduplicated here; callers
// must inspect consecutive keys and keep the highest SeqNo version.
type MergeIterator struct {
	h minHeap
}

// NewMergeIterator builds a MergeIterator. Each iter must already be
// seeked (SeekToFirst called externally, or this will call it).
func NewMergeIterator(iters []Iterator) *MergeIterator {
	h := make(minHeap, 0, len(iters))
	heap.Init(&h)
	for idx, it := range iters {
		it.SeekToFirst()
		if it.Valid() {
			heap.Push(&h, &entry{
				key:   it.Key(),
				value: it.Value(),
				iter:  it,
				idx:   idx,
			})
		}
	}
	return &MergeIterator{h: h}
}

// Valid returns whether there are more entries.
func (m *MergeIterator) Valid() bool { return len(m.h) > 0 }

// Key returns the current smallest key.
func (m *MergeIterator) Key() sstable.InternalKey {
	if len(m.h) == 0 {
		return sstable.InternalKey{}
	}
	return m.h[0].key
}

// Value returns the value for the current key.
func (m *MergeIterator) Value() []byte {
	if len(m.h) == 0 {
		return nil
	}
	return m.h[0].value
}

// Next advances past the current entry.
func (m *MergeIterator) Next() {
	if len(m.h) == 0 {
		return
	}
	top := heap.Pop(&m.h).(*entry)
	top.iter.Next()
	if top.iter.Valid() {
		top.key = top.iter.Key()
		top.value = top.iter.Value()
		heap.Push(&m.h, top)
	}
}
