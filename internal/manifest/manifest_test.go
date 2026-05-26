package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManifest_AddAndRecover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MANIFEST")

	m, err := OpenManifest(path)
	require.NoError(t, err)

	// Add 10 SSTables across 3 levels
	for i := 0; i < 10; i++ {
		level := i % 3
		edit := VersionEdit{
			Type:     EditAddSSTable,
			Level:    level,
			FileID:   uint64(i + 1),
			FileSize: 1024,
			FirstKey: []byte{byte(i * 10)},
			LastKey:  []byte{byte(i*10 + 9)},
		}
		require.NoError(t, m.Apply(edit))
	}

	require.NoError(t, m.Close())

	// Recover and verify
	version, err := Recover(path)
	require.NoError(t, err)

	total := 0
	for _, level := range version.Levels {
		total += len(level)
	}
	assert.Equal(t, 10, total)
}

func TestManifest_DeleteAndRecover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MANIFEST")

	m, err := OpenManifest(path)
	require.NoError(t, err)

	// Add 5 SSTables at L0
	for i := 0; i < 5; i++ {
		require.NoError(t, m.Apply(VersionEdit{
			Type:     EditAddSSTable,
			Level:    0,
			FileID:   uint64(i + 1),
			FileSize: 1024,
			FirstKey: []byte{byte(i)},
			LastKey:  []byte{byte(i), 0xFF},
		}))
	}

	// Delete 2
	require.NoError(t, m.Apply(VersionEdit{Type: EditDeleteSSTable, Level: 0, FileID: 2}))
	require.NoError(t, m.Apply(VersionEdit{Type: EditDeleteSSTable, Level: 0, FileID: 4}))
	require.NoError(t, m.Close())

	version, err := Recover(path)
	require.NoError(t, err)
	assert.Len(t, version.Levels[0], 3)

	// Verify specific file IDs remain
	ids := make(map[uint64]bool)
	for _, meta := range version.Levels[0] {
		ids[meta.FileID] = true
	}
	assert.True(t, ids[1])
	assert.False(t, ids[2])
	assert.True(t, ids[3])
	assert.False(t, ids[4])
	assert.True(t, ids[5])
}

func TestManifest_L1OverlapRejected(t *testing.T) {
	dir := t.TempDir()
	m, err := OpenManifest(filepath.Join(dir, "MANIFEST"))
	require.NoError(t, err)

	// Add a non-overlapping SSTable at L1
	require.NoError(t, m.Apply(VersionEdit{
		Type:     EditAddSSTable,
		Level:    1,
		FileID:   1,
		FileSize: 1024,
		FirstKey: []byte("a"),
		LastKey:  []byte("m"),
	}))

	// Adding overlapping SSTable at L1 should fail
	err = m.Apply(VersionEdit{
		Type:     EditAddSSTable,
		Level:    1,
		FileID:   2,
		FileSize: 1024,
		FirstKey: []byte("f"),
		LastKey:  []byte("z"),
	})
	assert.Error(t, err, "overlapping SSTable at L1 should be rejected")
}

func TestManifest_RecoverRejectsTruncatedEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MANIFEST")

	m, err := OpenManifest(path)
	require.NoError(t, err)
	require.NoError(t, m.Apply(VersionEdit{
		Type:     EditAddSSTable,
		Level:    0,
		FileID:   1,
		FileSize: 1024,
		FirstKey: []byte("a"),
		LastKey:  []byte("z"),
	}))
	require.NoError(t, m.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Greater(t, len(data), 1)
	require.NoError(t, os.WriteFile(path, data[:len(data)-1], 0644))

	_, err = Recover(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "truncated")
}
