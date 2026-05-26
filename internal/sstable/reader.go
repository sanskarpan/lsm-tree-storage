// Package sstable — see format.go for the package doc.
package sstable

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"

	"lsm-engine/internal/bloom"
	"lsm-engine/internal/cache"
	"lsm-engine/internal/events"
)

// SSTableReader reads data from an SSTable file
type SSTableReader struct {
	file        *os.File
	indexBlock  *Block
	bloomFilter *bloom.BloomFilter
	meta        SSTableMeta
	blockCache  *cache.BlockCache
	bus         events.EventPublisher
}

// NewSSTableReader opens an SSTable and reads its index and filter blocks
func NewSSTableReader(path string, meta SSTableMeta, blockCache *cache.BlockCache, bus events.EventPublisher) (*SSTableReader, error) {
	if bus == nil {
		bus = &events.NoopBus{}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Read footer
	footer := make([]byte, FooterSize)
	if _, err := f.ReadAt(footer, int64(meta.FileSize)-FooterSize); err != nil {
		_ = f.Close()
		return nil, err
	}
	magic := binary.LittleEndian.Uint64(footer[40:])
	if magic != MagicNumber {
		_ = f.Close()
		return nil, ErrCorruptSSTable
	}

	// Decode filter handle and index handle
	filterHandle, n1 := decodeBlockHandle(footer)
	indexHandle, _ := decodeBlockHandle(footer[n1:])

	// Load filter block
	var bf *bloom.BloomFilter
	if filterHandle.Size > 0 {
		filterData, err := readBlockAt(f, filterHandle)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		bf = bloom.DeserializeBloomFilter(filterData)
	}

	// Load index block
	indexData, err := readBlockAt(f, indexHandle)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	indexBlock, err := DecodeBlock(indexData)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	return &SSTableReader{
		file:        f,
		indexBlock:  indexBlock,
		bloomFilter: bf,
		meta:        meta,
		blockCache:  blockCache,
		bus:         bus,
	}, nil
}

// Get looks up a key in the SSTable with the given read seqNo
// Returns (value, found, error). Deleted tombstones return (nil, true, nil).
func (r *SSTableReader) Get(userKey []byte, readSeqNo uint64) ([]byte, bool, error) {
	// 1. Bloom filter check
	if r.bloomFilter != nil {
		r.bus.Publish(events.Event{Type: events.EvtBloomCheck, Extra: map[string]interface{}{
			"file_id": r.meta.FileID, "key": string(userKey),
		}})
		if !r.bloomFilter.MayContain(userKey) {
			r.bus.Publish(events.Event{Type: events.EvtBloomMiss, Extra: map[string]interface{}{
				"file_id": r.meta.FileID,
			}})
			return nil, false, nil
		}
		r.bus.Publish(events.Event{Type: events.EvtBloomHit, Extra: map[string]interface{}{
			"file_id": r.meta.FileID,
		}})
	}

	// 2. Binary search index block for block handle
	handle, found := r.findDataBlock(userKey)
	if !found {
		return nil, false, nil
	}

	// 3. Load data block (from cache or disk)
	block, err := r.loadBlock(handle)
	if err != nil {
		return nil, false, err
	}

	// 4. Linear scan within data block using restart points
	it := NewBlockIterator(block)
	for it.Valid() {
		cmp := bytes.Compare(it.Key().UserKey, userKey)
		if cmp > 0 {
			break // past the key
		}
		if cmp == 0 && it.Key().SeqNo <= readSeqNo {
			if it.Key().Type == TypeDeletion {
				return nil, true, nil // tombstone
			}
			return it.Value(), true, nil
		}
		it.Next()
	}
	return nil, false, nil
}

// findDataBlock searches the index block for the handle of the block
// that could contain userKey.
// Index entries: key = last InternalKey of block, value = BlockHandle
func (r *SSTableReader) findDataBlock(userKey []byte) (BlockHandle, bool) {
	it := NewBlockIterator(r.indexBlock)
	for it.Valid() {
		if bytes.Compare(userKey, it.Key().UserKey) <= 0 {
			handle, _ := decodeBlockHandle(it.Value())
			return handle, true
		}
		it.Next()
	}
	return BlockHandle{}, false
}

// loadBlock fetches a block from cache or reads from disk
func (r *SSTableReader) loadBlock(handle BlockHandle) (*Block, error) {
	cacheKey := cache.CacheKey{FileID: r.meta.FileID, Offset: handle.Offset}

	if r.blockCache != nil {
		if data, ok := r.blockCache.Get(cacheKey); ok {
			return DecodeBlock(data)
		}
	}

	data, err := readBlockAt(r.file, handle)
	if err != nil {
		return nil, err
	}

	if r.blockCache != nil {
		r.blockCache.Insert(cacheKey, data)
	}

	return DecodeBlock(data)
}

// NewIterator returns an iterator over all entries in this SSTable
func (r *SSTableReader) NewIterator() *SSTableIterator {
	return &SSTableIterator{reader: r}
}

// Meta returns the SSTableMeta for this reader
func (r *SSTableReader) Meta() SSTableMeta {
	return r.meta
}

// Close closes the SSTable file
func (r *SSTableReader) Close() error {
	return r.file.Close()
}

// readBlockAt reads block data at the given handle from a file
func readBlockAt(f *os.File, handle BlockHandle) ([]byte, error) {
	if handle.Size == 0 {
		return nil, io.EOF
	}
	data := make([]byte, handle.Size)
	_, err := f.ReadAt(data, int64(handle.Offset))
	return data, err
}

// SSTableIterator iterates over all data blocks in an SSTable
type SSTableIterator struct {
	reader      *SSTableReader
	indexIter   *BlockIterator
	dataIter    *BlockIterator
	initialized bool
}

// SeekToFirst positions the iterator at the first entry of the first data block.
func (it *SSTableIterator) SeekToFirst() {
	it.indexIter = NewBlockIterator(it.reader.indexBlock)
	it.initialized = true
	it.advanceToNextDataBlock()
}

func (it *SSTableIterator) advanceToNextDataBlock() {
	for it.indexIter != nil && it.indexIter.Valid() {
		handle, _ := decodeBlockHandle(it.indexIter.Value())
		block, err := it.reader.loadBlock(handle)
		if err != nil {
			it.indexIter.Next()
			continue
		}
		it.dataIter = NewBlockIterator(block)
		if it.dataIter.Valid() {
			it.indexIter.Next()
			return
		}
		it.indexIter.Next()
	}
	it.dataIter = nil
}

// Valid reports whether the iterator is positioned at a valid entry.
// It calls SeekToFirst on the first invocation.
func (it *SSTableIterator) Valid() bool {
	if !it.initialized {
		it.SeekToFirst()
	}
	return it.dataIter != nil && it.dataIter.Valid()
}

// Key returns the InternalKey at the current iterator position.
func (it *SSTableIterator) Key() InternalKey {
	if it.dataIter == nil {
		return InternalKey{}
	}
	return it.dataIter.Key()
}

// Value returns the value bytes at the current iterator position.
func (it *SSTableIterator) Value() []byte {
	if it.dataIter == nil {
		return nil
	}
	return it.dataIter.Value()
}

// Next advances the iterator to the next entry across all data blocks.
func (it *SSTableIterator) Next() {
	// Ensure iterator is positioned (SeekToFirst initializes it)
	if !it.initialized {
		it.SeekToFirst()
		// Now we're at the first entry; Next() should advance past it
	}
	if it.dataIter == nil {
		return
	}
	it.dataIter.Next()
	if !it.dataIter.Valid() {
		it.advanceToNextDataBlock()
	}
}
