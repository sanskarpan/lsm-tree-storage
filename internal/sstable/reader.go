// Package sstable — see format.go for the package doc.
package sstable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"lsm-engine/internal/bloom"
	"lsm-engine/internal/cache"
	"lsm-engine/internal/events"
)

// SSTableReader reads data from an SSTable file.
//
// On platforms where mmap is available the entire file is mapped into memory
// at open time, so block reads are zero-copy slices into the mmap region.
// On other platforms (e.g. Windows) the reader falls back to ReadAt, which is
// functionally identical but incurs a syscall per block.
type SSTableReader struct {
	file        *os.File
	mmapData    []byte
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

	// Validate format version.  Version 0 is the legacy format (old files have
	// zero-padding at offset 32) and is accepted for backward compatibility.
	// Any other unknown version is rejected to prevent silent misreads.
	version := binary.LittleEndian.Uint32(footer[32:])
	if version != 0 && version != FormatVersion {
		_ = f.Close()
		return nil, fmt.Errorf("sstable: unsupported format version %d", version)
	}

	// Decode filter handle and index handle
	filterHandle, n1 := decodeBlockHandle(footer)
	indexHandle, _ := decodeBlockHandle(footer[n1:])

	// Best-effort mmap. If mmap fails we silently fall back to ReadAt; the
	// reader still works, just with an extra syscall per block read.
	mmapData, mmapErr := mmapFile(f)
	if mmapErr != nil {
		mmapData = nil
	}
	if len(mmapData) > 0 {
		_ = madviseSequential(mmapData)
	}

	r := &SSTableReader{
		file:       f,
		mmapData:   mmapData,
		meta:       meta,
		blockCache: blockCache,
		bus:        bus,
	}

	// Load filter block
	if filterHandle.Size > 0 {
		filterData, err := r.readBlockAt(filterHandle)
		if err != nil {
			_ = r.Close()
			return nil, err
		}
		r.bloomFilter = bloom.DeserializeBloomFilter(filterData)
	}

	// Load index block
	indexData, err := r.readBlockAt(indexHandle)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	indexBlock, err := DecodeBlock(indexData)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	r.indexBlock = indexBlock

	return r, nil
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

// loadBlock fetches a block from cache or reads from disk.
// The returned *Block always owns its backing bytes — the block cache may
// reuse or evict the underlying buffer at any time, so callers (in particular
// BlockIterator) must be able to rely on the slice's stability for the
// lifetime of the block.
func (r *SSTableReader) loadBlock(handle BlockHandle) (*Block, error) {
	cacheKey := cache.CacheKey{FileID: r.meta.FileID, Level: r.meta.Level, Offset: handle.Offset}

	if r.blockCache != nil {
		if data, ok := r.blockCache.Get(cacheKey); ok {
			owned := append([]byte(nil), data...)
			return DecodeBlock(owned)
		}
	}

	data, err := r.readBlockAt(handle)
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

// Close closes the SSTable file and releases the mmap region.
func (r *SSTableReader) Close() error {
	// Drop the mmap first; the kernel keeps the file's page cache alive
	// after munmap, so POSIX_FADV_DONTNEED afterwards is safe.
	if err := munmapFile(r.mmapData); err != nil {
		_ = r.file.Close()
		return err
	}
	r.mmapData = nil
	if r.file != nil {
		_ = fadviseDontNeed(r.file, int64(r.meta.FileSize))
		return r.file.Close()
	}
	return nil
}

// readBlockAt returns the raw bytes of the block at handle. If the file is
// mmap'd, the returned slice is a view into the mmap region (no copy). If
// mmap is unavailable, the bytes are read with ReadAt.
func (r *SSTableReader) readBlockAt(handle BlockHandle) ([]byte, error) {
	if handle.Size == 0 {
		return nil, io.EOF
	}
	if end := int64(handle.Offset) + int64(handle.Size); end > int64(len(r.mmapData)) {
		return nil, io.ErrUnexpectedEOF
	}
	if len(r.mmapData) > 0 {
		return r.mmapData[handle.Offset : handle.Offset+handle.Size], nil
	}
	data := make([]byte, handle.Size)
	_, err := r.file.ReadAt(data, int64(handle.Offset))
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
