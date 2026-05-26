// Package manifest — see manifest.go for the package doc.
package manifest

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// EditType is the discriminator for a VersionEdit record.
type EditType uint8

const (
	// EditAddSSTable records the addition of a new SSTable to a level.
	EditAddSSTable EditType = 1
	// EditDeleteSSTable records the removal of an SSTable from a level.
	EditDeleteSSTable EditType = 2
	// EditLogNumber records the current WAL log file number.
	EditLogNumber EditType = 3
	// EditNextFileID records the next file ID to be assigned.
	EditNextFileID EditType = 4
)

// VersionEdit represents a single change to the Version (SSTable level state)
type VersionEdit struct {
	Type      EditType
	Level     int
	FileID    uint64
	FileSize  uint64
	FirstKey  []byte
	LastKey   []byte
	Deleted   bool
	LogNumber uint64
	NextFileID uint64
	// SkipOverlapCheck bypasses L1+ overlap validation for compaction outputs
	// (the old input files are still in the manifest while new outputs are added,
	// so the transient overlap is expected).  Not persisted to disk.
	SkipOverlapCheck bool
}

// ErrCorruptManifest is returned when a MANIFEST record cannot be decoded.
var ErrCorruptManifest = errors.New("manifest: corrupt record")

// EncodeVersionEdit encodes a VersionEdit to bytes.
// Format: [type:1][level:4LE][fileID:8LE][fileSize:8LE][firstKeyLen:4LE][firstKey][lastKeyLen:4LE][lastKey][logNumber:8LE][nextFileID:8LE]
func EncodeVersionEdit(edit VersionEdit) []byte {
	buf := &bytes.Buffer{}
	buf.WriteByte(byte(edit.Type))
	_ = binary.Write(buf, binary.LittleEndian, int32(edit.Level))
	_ = binary.Write(buf, binary.LittleEndian, edit.FileID)
	_ = binary.Write(buf, binary.LittleEndian, edit.FileSize)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(edit.FirstKey)))
	buf.Write(edit.FirstKey)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(edit.LastKey)))
	buf.Write(edit.LastKey)
	_ = binary.Write(buf, binary.LittleEndian, edit.LogNumber)
	_ = binary.Write(buf, binary.LittleEndian, edit.NextFileID)
	return buf.Bytes()
}

// DecodeVersionEdit decodes a VersionEdit from bytes
func DecodeVersionEdit(data []byte) (VersionEdit, error) {
	if len(data) < 1+4+8+8+4+4+8+8 {
		return VersionEdit{}, ErrCorruptManifest
	}
	r := bytes.NewReader(data)

	var editType uint8
	_ = binary.Read(r, binary.LittleEndian, &editType)

	var level int32
	_ = binary.Read(r, binary.LittleEndian, &level)

	var fileID, fileSize uint64
	_ = binary.Read(r, binary.LittleEndian, &fileID)
	_ = binary.Read(r, binary.LittleEndian, &fileSize)

	var firstKeyLen uint32
	_ = binary.Read(r, binary.LittleEndian, &firstKeyLen)
	firstKey := make([]byte, firstKeyLen)
	if _, err := r.Read(firstKey); err != nil && firstKeyLen > 0 {
		return VersionEdit{}, ErrCorruptManifest
	}

	var lastKeyLen uint32
	_ = binary.Read(r, binary.LittleEndian, &lastKeyLen)
	lastKey := make([]byte, lastKeyLen)
	if _, err := r.Read(lastKey); err != nil && lastKeyLen > 0 {
		return VersionEdit{}, ErrCorruptManifest
	}

	var logNumber, nextFileID uint64
	_ = binary.Read(r, binary.LittleEndian, &logNumber)
	_ = binary.Read(r, binary.LittleEndian, &nextFileID)

	return VersionEdit{
		Type:       EditType(editType),
		Level:      int(level),
		FileID:     fileID,
		FileSize:   fileSize,
		FirstKey:   firstKey,
		LastKey:    lastKey,
		Deleted:    EditType(editType) == EditDeleteSSTable,
		LogNumber:  logNumber,
		NextFileID: nextFileID,
	}, nil
}
