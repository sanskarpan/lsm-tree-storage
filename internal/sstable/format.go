// Package sstable implements the Sorted String Table (SSTable) file format,
// including the block builder, block-based reader, and supporting types used
// throughout the LSM engine.
package sstable

import "bytes"

// EntryType identifies whether a key-value entry is a live value or a deletion tombstone.
type EntryType uint8

const (
	// TypeValue indicates a regular key-value entry.
	TypeValue EntryType = 1
	// TypeDeletion indicates a tombstone (deleted key).
	TypeDeletion EntryType = 2 // tombstone
)

// InternalKey is the composite key used throughout the engine.
// Sort order: UserKey ASC, then SeqNo DESC (newer = lower sort position = found first).
type InternalKey struct {
	UserKey []byte
	SeqNo   uint64
	Type    EntryType
}

// Less reports whether a sorts before b in the internal key ordering.
func (a InternalKey) Less(b InternalKey) bool {
	cmp := bytes.Compare(a.UserKey, b.UserKey)
	if cmp != 0 {
		return cmp < 0
	}
	return a.SeqNo > b.SeqNo // CRITICAL: higher SeqNo sorts FIRST
}

// Equal reports whether a and b have the same UserKey and SeqNo.
func (a InternalKey) Equal(b InternalKey) bool {
	return bytes.Equal(a.UserKey, b.UserKey) && a.SeqNo == b.SeqNo
}

// BlockHandle holds the byte offset and size of a data block within an SSTable file.
type BlockHandle struct {
	Offset uint64
	Size   uint64
}

// SSTableMeta contains identifying metadata for a single SSTable file.
type SSTableMeta struct {
	FileID    uint64
	Level     int
	FirstKey  []byte
	LastKey   []byte
	FileSize  uint64
	NumKeys   uint64
	FilePath  string
	CreatedAt int64 // unix nano, for TWCS
}
