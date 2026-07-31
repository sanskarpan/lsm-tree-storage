// Package manifest manages the MANIFEST file — an append-only log of VersionEdits that
// tracks which SSTable files belong to which level, enabling crash-safe metadata updates.
package manifest

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"
	"sync"

	"lsm-engine/internal/sstable"
)

// MaxLevels is the maximum number of compaction levels supported by the engine.
const MaxLevels = 7

// manifestMagic is written as the first 4 bytes of a new MANIFEST file.
// Its value (0x464E494D in LE = 1_179_209_549) is implausibly large as a record
// length in the old format (≈1.1 GiB), so the two formats cannot be confused.
var manifestMagic = [4]byte{0x4D, 0x4E, 0x49, 0x46} // "MNIF"

// Version is the in-memory representation of SSTable level state
type Version struct {
	Levels     [MaxLevels][]*sstable.SSTableMeta
	LogNumber  uint64
	NextFileID uint64
	MaxSeqNo   uint64
}

// HasFile returns true if fileID is in any level of this version
func (v *Version) HasFile(fileID uint64) bool {
	for _, level := range v.Levels {
		for _, meta := range level {
			if meta.FileID == fileID {
				return true
			}
		}
	}
	return false
}

// Manifest is an append-only log of VersionEdits
type Manifest struct {
	mu      sync.Mutex
	file    *os.File
	writer  *bufio.Writer
	current *Version
	path    string
	usesCRC bool // true when this manifest uses [CRC:4][length:4][body] record format
}

// OpenManifest opens or creates a MANIFEST file.
// If the file is new (non-existent or empty) it writes the 4-byte magic header
// and sets usesCRC so all subsequent records use the CRC-protected format.
func OpenManifest(path string) (*Manifest, error) {
	info, statErr := os.Stat(path)
	isNew := os.IsNotExist(statErr) || (statErr == nil && info.Size() == 0)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		file:    f,
		writer:  bufio.NewWriter(f),
		path:    path,
		current: &Version{},
		usesCRC: isNew,
	}

	if isNew {
		if _, err := m.writer.Write(manifestMagic[:]); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := m.writer.Flush(); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return nil, err
		}
	}

	return m, nil
}

// SetFormat overrides the record-format flag detected during Recover.
// Must be called (with the result from Recover) before the first Apply so that
// new writes use the same format as existing records in the file.
func (m *Manifest) SetFormat(usesCRC bool) {
	m.mu.Lock()
	m.usesCRC = usesCRC
	m.mu.Unlock()
}

// Apply validates and applies a VersionEdit.
// Fix 6: disk write happens FIRST; in-memory state is only updated on success.
func (m *Manifest) Apply(edit VersionEdit) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate L1+ non-overlap (skip for compaction outputs — inputs are deleted in a
	// subsequent Apply call, so transient overlap with the old files is expected).
	if edit.Type == EditAddSSTable && edit.Level >= 1 && !edit.SkipOverlapCheck {
		for _, existing := range m.current.Levels[edit.Level] {
			if overlaps(existing.FirstKey, existing.LastKey, edit.FirstKey, edit.LastKey) {
				return fmt.Errorf("manifest: SSTable at L%d overlaps existing range [%s, %s] with [%s, %s]",
					edit.Level, existing.FirstKey, existing.LastKey, edit.FirstKey, edit.LastKey)
			}
		}
	}

	// Persist to disk first — only update in-memory state after durability is confirmed.
	if err := m.writeEdit(edit); err != nil {
		return err
	}

	// Update current version (safe: disk write succeeded above).
	switch edit.Type {
	case EditAddSSTable:
		meta := &sstable.SSTableMeta{
			FileID:   edit.FileID,
			Level:    edit.Level,
			FirstKey: edit.FirstKey,
			LastKey:  edit.LastKey,
			FileSize: edit.FileSize,
		}
		m.current.Levels[edit.Level] = append(m.current.Levels[edit.Level], meta)
		sortLevel(m.current.Levels[edit.Level], edit.Level)
		if edit.FileID >= m.current.NextFileID {
			m.current.NextFileID = edit.FileID + 1
		}
	case EditDeleteSSTable:
		old := m.current.Levels[edit.Level]
		// Allocate new slice to avoid mutating any existing references
		newLevel := make([]*sstable.SSTableMeta, 0, len(old))
		for _, meta := range old {
			if meta.FileID != edit.FileID {
				newLevel = append(newLevel, meta)
			}
		}
		m.current.Levels[edit.Level] = newLevel
		sortLevel(m.current.Levels[edit.Level], edit.Level)
	case EditLogNumber:
		if edit.LogNumber > m.current.LogNumber {
			m.current.LogNumber = edit.LogNumber
		}
	case EditNextFileID:
		if edit.NextFileID > m.current.NextFileID {
			m.current.NextFileID = edit.NextFileID
		}
	case EditMaxSeqNo:
		if edit.MaxSeqNo > m.current.MaxSeqNo {
			m.current.MaxSeqNo = edit.MaxSeqNo
		}
	}

	return nil
}

// writeEdit serialises edit and appends it to the manifest file.
// Format depends on m.usesCRC:
//   - CRC format (new):  [CRC32_of_body:4][length:4][body]
//   - legacy format (old): [length:4][body]
func (m *Manifest) writeEdit(edit VersionEdit) error {
	data := EncodeVersionEdit(edit)

	if m.usesCRC {
		crc := crc32.ChecksumIEEE(data)
		var buf [8]byte
		binary.LittleEndian.PutUint32(buf[0:4], crc)
		binary.LittleEndian.PutUint32(buf[4:8], uint32(len(data)))
		if _, err := m.writer.Write(buf[:]); err != nil {
			return err
		}
	} else {
		var lenBuf [4]byte
		binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
		if _, err := m.writer.Write(lenBuf[:]); err != nil {
			return err
		}
	}

	if _, err := m.writer.Write(data); err != nil {
		return err
	}
	if err := m.writer.Flush(); err != nil {
		return err
	}
	return m.file.Sync()
}

// Current returns a snapshot copy of the current version (safe to read without lock)
func (m *Manifest) Current() *Version {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.copyVersion(m.current)
}

// copyVersion deep-copies a Version
func (m *Manifest) copyVersion(v *Version) *Version {
	if v == nil {
		return &Version{}
	}
	cp := &Version{
		LogNumber:  v.LogNumber,
		NextFileID: v.NextFileID,
		MaxSeqNo:   v.MaxSeqNo,
	}
	for i := range v.Levels {
		if len(v.Levels[i]) > 0 {
			cp.Levels[i] = make([]*sstable.SSTableMeta, len(v.Levels[i]))
			copy(cp.Levels[i], v.Levels[i])
		}
	}
	return cp
}

// Recover reads all VersionEdits from the manifest file and rebuilds the Version.
// It returns (version, usedCRC, err) where usedCRC indicates whether the file
// used the CRC-protected record format (new magic present) or the legacy format.
// The caller should pass usedCRC to SetFormat so new writes use the same format.
func Recover(path string) (*Version, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Version{}, false, nil
		}
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	version := &Version{}
	r := bufio.NewReader(f)

	// Read first 4 bytes to determine record format.
	// New manifests start with manifestMagic; old manifests start with the
	// 4-byte LE length of the first record.
	var firstFour [4]byte
	if _, err := io.ReadFull(r, firstFour[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			// Empty or nearly-empty manifest — nothing to recover.
			return version, false, nil
		}
		return nil, false, fmt.Errorf("manifest: read format header: %w", err)
	}

	useCRC := firstFour == manifestMagic

	// applyEdit folds one decoded VersionEdit into the version under construction.
	applyEdit := func(edit VersionEdit) {
		switch edit.Type {
		case EditAddSSTable:
			meta := &sstable.SSTableMeta{
				FileID:   edit.FileID,
				Level:    edit.Level,
				FirstKey: edit.FirstKey,
				LastKey:  edit.LastKey,
				FileSize: edit.FileSize,
			}
			version.Levels[edit.Level] = append(version.Levels[edit.Level], meta)
			sortLevel(version.Levels[edit.Level], edit.Level)
			if edit.FileID >= version.NextFileID {
				version.NextFileID = edit.FileID + 1
			}
		case EditDeleteSSTable:
			level := version.Levels[edit.Level]
			newLevel := make([]*sstable.SSTableMeta, 0, len(level))
			for _, meta := range level {
				if meta.FileID != edit.FileID {
					newLevel = append(newLevel, meta)
				}
			}
			version.Levels[edit.Level] = newLevel
			sortLevel(version.Levels[edit.Level], edit.Level)
		case EditLogNumber:
			if edit.LogNumber > version.LogNumber {
				version.LogNumber = edit.LogNumber
			}
		case EditNextFileID:
			if edit.NextFileID > version.NextFileID {
				version.NextFileID = edit.NextFileID
			}
		case EditMaxSeqNo:
			if edit.MaxSeqNo > version.MaxSeqNo {
				version.MaxSeqNo = edit.MaxSeqNo
			}
		}
		// Unconditional high-water-mark updates (handles edits that carry auxiliary
		// fields in addition to their primary Type payload).
		if edit.LogNumber > version.LogNumber {
			version.LogNumber = edit.LogNumber
		}
		if edit.NextFileID > version.NextFileID {
			version.NextFileID = edit.NextFileID
		}
		if edit.MaxSeqNo > version.MaxSeqNo {
			version.MaxSeqNo = edit.MaxSeqNo
		}
	}

	// Legacy format: firstFour is the length of the first record — read and apply it.
	if !useCRC {
		length := binary.LittleEndian.Uint32(firstFour[:])
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			if err == io.ErrUnexpectedEOF {
				return nil, false, fmt.Errorf("manifest truncated while reading edit body: %w", err)
			}
			return nil, false, err
		}
		edit, err := DecodeVersionEdit(data)
		if err != nil {
			return nil, false, err
		}
		applyEdit(edit)
	}

	// Main record loop — reads remaining records in the detected format.
	for {
		var data []byte

		if useCRC {
			// CRC format: [crc:4][length:4][body]
			var header [8]byte
			if _, err := io.ReadFull(r, header[:]); err != nil {
				if err == io.EOF {
					break
				}
				if err == io.ErrUnexpectedEOF {
					return nil, false, fmt.Errorf("manifest truncated while reading record header: %w", err)
				}
				return nil, false, err
			}
			storedCRC := binary.LittleEndian.Uint32(header[0:4])
			length := binary.LittleEndian.Uint32(header[4:8])
			body := make([]byte, length)
			if _, err := io.ReadFull(r, body); err != nil {
				if err == io.ErrUnexpectedEOF {
					return nil, false, fmt.Errorf("manifest truncated while reading record body: %w", err)
				}
				return nil, false, err
			}
			if crc32.ChecksumIEEE(body) != storedCRC {
				return nil, false, fmt.Errorf("manifest: CRC mismatch: %w", ErrCorruptManifest)
			}
			data = body
		} else {
			// Legacy format: [length:4][body]
			var lenBuf [4]byte
			if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
				if err == io.EOF {
					break
				}
				if err == io.ErrUnexpectedEOF {
					return nil, false, fmt.Errorf("manifest truncated while reading edit length: %w", err)
				}
				return nil, false, err
			}
			length := binary.LittleEndian.Uint32(lenBuf[:])
			body := make([]byte, length)
			if _, err := io.ReadFull(r, body); err != nil {
				if err == io.ErrUnexpectedEOF {
					return nil, false, fmt.Errorf("manifest truncated while reading edit body: %w", err)
				}
				return nil, false, err
			}
			data = body
		}

		edit, err := DecodeVersionEdit(data)
		if err != nil {
			return nil, false, err
		}
		applyEdit(edit)
	}

	return version, useCRC, nil
}

// overlaps returns true if [a1,a2] and [b1,b2] key ranges overlap
func overlaps(a1, a2, b1, b2 []byte) bool {
	return bytes.Compare(a1, b2) <= 0 && bytes.Compare(b1, a2) <= 0
}

func sortLevel(level []*sstable.SSTableMeta, levelNum int) {
	if levelNum == 0 || len(level) < 2 {
		return
	}
	sort.Slice(level, func(i, j int) bool {
		if cmp := bytes.Compare(level[i].FirstKey, level[j].FirstKey); cmp != 0 {
			return cmp < 0
		}
		return level[i].FileID < level[j].FileID
	})
}

// SetCurrent sets the in-memory version without writing to disk (used after recovery)
func (m *Manifest) SetCurrent(v *Version) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = v
}

// Close flushes and closes the manifest file
func (m *Manifest) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.writer.Flush(); err != nil {
		return err
	}
	return m.file.Close()
}
