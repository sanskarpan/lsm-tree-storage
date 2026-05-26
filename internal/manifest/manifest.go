// Package manifest manages the MANIFEST file — an append-only log of VersionEdits that
// tracks which SSTable files belong to which level, enabling crash-safe metadata updates.
package manifest

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"lsm-engine/internal/sstable"
)

// MaxLevels is the maximum number of compaction levels supported by the engine.
const MaxLevels = 7

// Version is the in-memory representation of SSTable level state
type Version struct {
	Levels     [MaxLevels][]*sstable.SSTableMeta
	LogNumber  uint64
	NextFileID uint64
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
}

// OpenManifest opens or creates a MANIFEST file
func OpenManifest(path string) (*Manifest, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		file:    f,
		writer:  bufio.NewWriter(f),
		path:    path,
		current: &Version{},
	}
	return m, nil
}

// Apply validates and applies a VersionEdit, then persists it
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

	// Update current version
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
		m.current.LogNumber = edit.LogNumber
	case EditNextFileID:
		if edit.NextFileID > m.current.NextFileID {
			m.current.NextFileID = edit.NextFileID
		}
	}

	// Persist to disk
	return m.writeEdit(edit)
}

func (m *Manifest) writeEdit(edit VersionEdit) error {
	data := EncodeVersionEdit(edit)
	// Write length-prefixed record
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := m.writer.Write(lenBuf[:]); err != nil {
		return err
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
	}
	for i := range v.Levels {
		if len(v.Levels[i]) > 0 {
			cp.Levels[i] = make([]*sstable.SSTableMeta, len(v.Levels[i]))
			copy(cp.Levels[i], v.Levels[i])
		}
	}
	return cp
}

// Recover reads all VersionEdits from the manifest file and rebuilds the Version
func Recover(path string) (*Version, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Version{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	version := &Version{}
	r := bufio.NewReader(f)

	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			if err == io.EOF {
				break
			}
			if err == io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("manifest truncated while reading edit length: %w", err)
			}
			return nil, err
		}
		length := binary.LittleEndian.Uint32(lenBuf[:])
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			if err == io.ErrUnexpectedEOF {
				return nil, fmt.Errorf("manifest truncated while reading edit body: %w", err)
			}
			return nil, err
		}

		edit, err := DecodeVersionEdit(data)
		if err != nil {
			return nil, err
		}

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
			newLevel := level[:0]
			for _, meta := range level {
				if meta.FileID != edit.FileID {
					newLevel = append(newLevel, meta)
				}
			}
			version.Levels[edit.Level] = newLevel
			sortLevel(version.Levels[edit.Level], edit.Level)
		case EditLogNumber:
			version.LogNumber = edit.LogNumber
		case EditNextFileID:
			if edit.NextFileID > version.NextFileID {
				version.NextFileID = edit.NextFileID
			}
		}

		if edit.LogNumber > version.LogNumber {
			version.LogNumber = edit.LogNumber
		}
		if edit.NextFileID > version.NextFileID {
			version.NextFileID = edit.NextFileID
		}
	}

	return version, nil
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
