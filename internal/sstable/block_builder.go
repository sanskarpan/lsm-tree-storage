// Package sstable — see format.go for the package doc.
package sstable

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// RestartInterval is the number of entries between full-key restart points in a data block.
const RestartInterval = 16

// keysAreInOrder reports whether encodedNext may follow encodedPrev in a block.
// The two accepted orderings are:
//   - InternalKey.Less order: UserKey ASC, SeqNo DESC. The natural flush /
//     compaction order. For different UserKeys this matches raw byte order.
//   - Raw byte ASC: UserKey ASC, SeqNo ASC. The fallback used by callers that
//     feed keys in lexicographic byte order (e.g., some external bulk loads).
//
// The same-UserKey case is intentionally permissive because both directions
// produce a self-consistent prefix-compressed block.
func keysAreInOrder(encodedPrev, encodedNext []byte, nextKey InternalKey) bool {
	if bytes.Equal(encodedPrev, encodedNext) {
		return false
	}
	prev := decodeKey(encodedPrev)
	if !bytes.Equal(prev.UserKey, nextKey.UserKey) {
		return bytes.Compare(prev.UserKey, nextKey.UserKey) < 0
	}
	return true
}

// BlockBuilder builds a data block with prefix compression and restart points
type BlockBuilder struct {
	buf             []byte
	restarts        []uint32
	counter         int // entries since last restart
	lastKey         []byte
	restartInterval int
}

// NewBlockBuilder creates a new BlockBuilder
func NewBlockBuilder(restartInterval int) *BlockBuilder {
	if restartInterval <= 0 {
		restartInterval = RestartInterval
	}
	return &BlockBuilder{restartInterval: restartInterval}
}

// Add appends a key-value pair with prefix compression.
// Callers must feed keys in InternalKey.Less order (UserKey ASC, then SeqNo DESC).
// For the same UserKey the raw-byte and Less orderings diverge; the prefix
// compression stays correct in both directions, so this builder accepts either.
func (b *BlockBuilder) Add(key InternalKey, value []byte) {
	encodedKey := encodeKey(key)

	if b.lastKey != nil && !keysAreInOrder(b.lastKey, encodedKey, key) {
		panic(fmt.Sprintf("sstable: BlockBuilder.Add received out-of-order key %q (UserKey=%q, SeqNo=%d, Type=%d) after %q",
			encodedKey, key.UserKey, key.SeqNo, key.Type, b.lastKey))
	}

	shared := 0
	if b.counter < b.restartInterval {
		// Prefix compression against last key
		for shared < len(b.lastKey) && shared < len(encodedKey) && b.lastKey[shared] == encodedKey[shared] {
			shared++
		}
	} else {
		// Restart point: no prefix compression, store full key
		b.restarts = append(b.restarts, uint32(len(b.buf)))
		b.counter = 0
	}

	unshared := len(encodedKey) - shared
	// [sharedLen:varint][unsharedLen:varint][valueLen:varint][unsharedKey][value]
	b.buf = appendVarint(b.buf, uint64(shared))
	b.buf = appendVarint(b.buf, uint64(unshared))
	b.buf = appendVarint(b.buf, uint64(len(value)))
	b.buf = append(b.buf, encodedKey[shared:]...)
	b.buf = append(b.buf, value...)
	b.lastKey = append(b.lastKey[:0], encodedKey...)
	b.counter++
}

// Finish finalizes the block and returns the encoded bytes
func (b *BlockBuilder) Finish() []byte {
	// Append restart array: [restart0:4LE]...[numRestarts:4LE]
	buf4 := [4]byte{}
	for _, r := range b.restarts {
		binary.LittleEndian.PutUint32(buf4[:], r)
		b.buf = append(b.buf, buf4[:]...)
	}
	binary.LittleEndian.PutUint32(buf4[:], uint32(len(b.restarts)))
	b.buf = append(b.buf, buf4[:]...)
	return b.buf
}

// Size returns the current size of the block buffer
func (b *BlockBuilder) Size() int {
	return len(b.buf)
}

// Reset clears the builder for reuse
func (b *BlockBuilder) Reset() {
	b.buf = b.buf[:0]
	b.restarts = b.restarts[:0]
	b.counter = 0
	b.lastKey = b.lastKey[:0]
}

// Block holds a decoded data block for reading
type Block struct {
	data     []byte
	restarts []uint32
}

// DecodeBlock parses raw block bytes into a Block
func DecodeBlock(data []byte) (*Block, error) {
	if len(data) < 4 {
		return nil, ErrCorruptSSTable
	}
	numRestarts := int(binary.LittleEndian.Uint32(data[len(data)-4:]))
	restartsStart := len(data) - 4 - numRestarts*4
	if restartsStart < 0 {
		return nil, ErrCorruptSSTable
	}
	restarts := make([]uint32, numRestarts)
	for i := 0; i < numRestarts; i++ {
		restarts[i] = binary.LittleEndian.Uint32(data[restartsStart+i*4:])
	}
	return &Block{data: data[:restartsStart], restarts: restarts}, nil
}

// BlockIterator iterates over entries in a data block
type BlockIterator struct {
	block   *Block
	pos     int
	lastKey []byte
	key     InternalKey
	value   []byte
	valid   bool
}

// NewBlockIterator creates a BlockIterator positioned at the first entry of block.
func NewBlockIterator(block *Block) *BlockIterator {
	it := &BlockIterator{block: block}
	it.Next() // position at first entry
	return it
}

// Valid reports whether the iterator is positioned at a valid entry.
func (it *BlockIterator) Valid() bool { return it.valid }

// Key returns a copy of the InternalKey at the current iterator position.
func (it *BlockIterator) Key() InternalKey {
	return InternalKey{
		UserKey: append([]byte(nil), it.key.UserKey...),
		SeqNo:   it.key.SeqNo,
		Type:    it.key.Type,
	}
}

// Value returns a copy of the value bytes at the current iterator position.
// The slice is defensively copied because the underlying block buffer may
// be reused by the block cache after the iterator's parent block is dropped.
func (it *BlockIterator) Value() []byte {
	if it.value == nil {
		return nil
	}
	return append([]byte(nil), it.value...)
}

func (it *BlockIterator) Next() {
	if it.pos >= len(it.block.data) {
		it.valid = false
		return
	}
	data := it.block.data[it.pos:]

	shared, n1 := decodeVarint(data)
	if n1 <= 0 {
		it.valid = false
		return
	}
	data = data[n1:]
	unshared, n2 := decodeVarint(data)
	if n2 <= 0 {
		it.valid = false
		return
	}
	data = data[n2:]
	valueLen, n3 := decodeVarint(data)
	if n3 <= 0 {
		it.valid = false
		return
	}
	data = data[n3:]

	if int(shared) > len(it.lastKey) || int(unshared)+int(valueLen) > len(data) {
		it.valid = false
		return
	}

	// Reconstruct full key
	fullKey := make([]byte, shared+unshared)
	copy(fullKey, it.lastKey[:shared])
	copy(fullKey[shared:], data[:unshared])
	it.lastKey = fullKey

	it.key = decodeKey(fullKey)
	it.value = data[unshared : unshared+valueLen]
	it.pos += n1 + n2 + n3 + int(unshared) + int(valueLen)
	it.valid = true
}

// SeekToRestart positions at a restart point
func (it *BlockIterator) SeekToRestart(restartIdx int) {
	it.pos = int(it.block.restarts[restartIdx])
	it.lastKey = it.lastKey[:0]
	it.Next()
}
