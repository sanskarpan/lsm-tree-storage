// Package sstable — see format.go for the package doc.
package sstable

import (
	"encoding/binary"
	"os"
	"time"

	"lsm-engine/internal/bloom"
)

const (
	// MagicNumber is the 8-byte sentinel written at byte 40 of the SSTable footer to detect corruption.
	MagicNumber = uint64(0x88e241b785f4cff7)
	// FooterSize is the fixed size in bytes of an SSTable footer (48 bytes).
	FooterSize = 48
)

// SSTableBuilder writes an SSTable to disk
type SSTableBuilder struct {
	file         *os.File
	fileSize     uint64
	dataBlock    *BlockBuilder
	indexBlock   *BlockBuilder
	bloomKeys    [][]byte
	blockSize    int
	bitsPerKey   int
	firstKey     []byte
	lastKey      []byte
	lastIKey     InternalKey // full last InternalKey added
	numEntries   int
	fileID       uint64
	level        int
	pendingIndex bool
	lastHandle   BlockHandle
	lastBlockIKey InternalKey // last InternalKey of the most recently flushed data block
}

// NewSSTableBuilder creates a new builder writing to path
func NewSSTableBuilder(path string, fileID uint64, level, blockSize, bitsPerKey int) (*SSTableBuilder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if blockSize <= 0 {
		blockSize = 4096
	}
	if bitsPerKey <= 0 {
		bitsPerKey = 10
	}
	return &SSTableBuilder{
		file:       f,
		dataBlock:  NewBlockBuilder(RestartInterval),
		indexBlock: NewBlockBuilder(1), // restart every 1 = full key for each index entry
		blockSize:  blockSize,
		bitsPerKey: bitsPerKey,
		fileID:     fileID,
		level:      level,
	}, nil
}

// Add adds a key-value pair. Keys must be added in sorted order.
func (b *SSTableBuilder) Add(key InternalKey, value []byte) error {
	if b.firstKey == nil {
		b.firstKey = append([]byte{}, key.UserKey...)
	}
	b.lastKey = append(b.lastKey[:0], key.UserKey...)
	b.lastIKey = key

	// Add to bloom filter
	b.bloomKeys = append(b.bloomKeys, append([]byte{}, key.UserKey...))

	// Write pending index entry for previous data block
	if b.pendingIndex {
		b.addIndexEntry(b.lastBlockIKey, b.lastHandle)
		b.pendingIndex = false
	}

	b.dataBlock.Add(key, value)
	b.numEntries++

	// Flush data block if too large
	if b.dataBlock.Size() >= b.blockSize {
		b.lastBlockIKey = key
		if err := b.flushDataBlock(); err != nil {
			return err
		}
	}

	return nil
}

func (b *SSTableBuilder) flushDataBlock() error {
	data := b.dataBlock.Finish()
	handle, err := b.writeRawBlock(data)
	if err != nil {
		return err
	}
	b.lastHandle = handle
	b.pendingIndex = true
	b.dataBlock.Reset()
	return nil
}

// addIndexEntry writes an index entry: lastKeyOfBlock -> blockHandle
// The index key is the full InternalKey of the last entry in the block.
func (b *SSTableBuilder) addIndexEntry(lastKey InternalKey, handle BlockHandle) {
	var handleBuf [20]byte
	n := encodeBlockHandle(handleBuf[:], handle)
	b.indexBlock.Add(lastKey, handleBuf[:n])
}

func (b *SSTableBuilder) writeRawBlock(data []byte) (BlockHandle, error) {
	handle := BlockHandle{
		Offset: b.fileSize,
		Size:   uint64(len(data)),
	}
	if _, err := b.file.Write(data); err != nil {
		return BlockHandle{}, err
	}
	b.fileSize += uint64(len(data))
	return handle, nil
}

// Finish flushes remaining data and writes the footer. Returns SSTableMeta.
func (b *SSTableBuilder) Finish() (SSTableMeta, error) {
	// 1. Flush final data block if non-empty
	if b.dataBlock.Size() > 0 {
		b.lastBlockIKey = b.lastIKey
		if err := b.flushDataBlock(); err != nil {
			return SSTableMeta{}, err
		}
	}
	// Write any pending index entry
	if b.pendingIndex {
		b.addIndexEntry(b.lastBlockIKey, b.lastHandle)
		b.pendingIndex = false
	}

	// 2. Write filter block (Bloom filter)
	var filterHandle BlockHandle
	if len(b.bloomKeys) > 0 {
		bf := bloom.NewBloomFilter(b.bloomKeys, b.bitsPerKey)
		filterData := bf.Serialize()
		var err error
		filterHandle, err = b.writeRawBlock(filterData)
		if err != nil {
			return SSTableMeta{}, err
		}
	}

	// 3. Write index block
	indexData := b.indexBlock.Finish()
	indexHandle, err := b.writeRawBlock(indexData)
	if err != nil {
		return SSTableMeta{}, err
	}

	// 4. Write footer (48 bytes)
	footer := make([]byte, FooterSize)
	pos := 0
	pos += encodeBlockHandle(footer[pos:], filterHandle)
	pos += encodeBlockHandle(footer[pos:], indexHandle)
	for pos < 40 {
		footer[pos] = 0
		pos++
	}
	binary.LittleEndian.PutUint64(footer[40:], MagicNumber)
	if _, err := b.file.Write(footer); err != nil {
		return SSTableMeta{}, err
	}
	b.fileSize += FooterSize

	// 5. fsync
	if err := b.file.Sync(); err != nil {
		return SSTableMeta{}, err
	}

	return SSTableMeta{
		FileID:    b.fileID,
		Level:     b.level,
		FirstKey:  b.firstKey,
		LastKey:   b.lastKey,
		FileSize:  b.fileSize,
		NumKeys:   uint64(b.numEntries),
		FilePath:  b.file.Name(),
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

// NumEntries returns the number of entries added
func (b *SSTableBuilder) NumEntries() int {
	return b.numEntries
}

// ApproxSize returns the approximate current file size
func (b *SSTableBuilder) ApproxSize() uint64 {
	return b.fileSize + uint64(b.dataBlock.Size())
}

// Close closes the underlying file
func (b *SSTableBuilder) Close() error {
	return b.file.Close()
}
