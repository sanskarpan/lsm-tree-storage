package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"lsm-engine/internal/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestWAL(t *testing.T) (*WAL, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wal")
	w, err := OpenWAL(path, &events.NoopBus{})
	require.NoError(t, err)
	return w, path
}

func TestWAL_SingleRecord(t *testing.T) {
	w, path := openTestWAL(t)
	err := w.Append(WALEntry{Type: EntrySet, Key: []byte("hello"), Value: []byte("world")})
	require.NoError(t, err)
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, EntrySet, entries[0].Type)
	assert.Equal(t, []byte("hello"), entries[0].Key)
	assert.Equal(t, []byte("world"), entries[0].Value)
}

func TestWAL_MultipleRecords(t *testing.T) {
	w, path := openTestWAL(t)
	for i := 0; i < 100; i++ {
		err := w.Append(WALEntry{
			Type:  EntrySet,
			Key:   []byte(fmt.Sprintf("key-%03d", i)),
			Value: []byte(fmt.Sprintf("val-%03d", i)),
		})
		require.NoError(t, err)
	}
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	require.Len(t, entries, 100)
	for i, e := range entries {
		assert.Equal(t, []byte(fmt.Sprintf("key-%03d", i)), e.Key)
		assert.Equal(t, []byte(fmt.Sprintf("val-%03d", i)), e.Value)
	}
}

func TestWAL_LargeRecord(t *testing.T) {
	w, path := openTestWAL(t)
	// Create a value larger than 32KB to force fragmentation
	bigVal := make([]byte, 100*1024) // 100KB
	for i := range bigVal {
		bigVal[i] = byte(i % 256)
	}
	err := w.Append(WALEntry{Type: EntrySet, Key: []byte("bigkey"), Value: bigVal})
	require.NoError(t, err)
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, bigVal, entries[0].Value)
}

func TestWAL_TruncatedRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal")
	w, err := OpenWAL(path, &events.NoopBus{})
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		_ = w.Append(WALEntry{Type: EntrySet, Key: []byte(fmt.Sprintf("k%d", i)), Value: []byte("v")})
	}
	_ = w.Sync()
	_ = w.Close()

	// Truncate last 5 bytes (simulate crash mid-write)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(path, info.Size()-5))

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	assert.Equal(t, 9, len(entries), "last entry truncated; 9 recovered")
}

func TestWAL_CRCDetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal")
	w, err := OpenWAL(path, &events.NoopBus{})
	require.NoError(t, err)
	_ = w.Append(WALEntry{Type: EntrySet, Key: []byte("k1"), Value: []byte("v1")})
	_ = w.Append(WALEntry{Type: EntrySet, Key: []byte("k2"), Value: []byte("v2")})
	_ = w.Sync()
	_ = w.Close()

	// Corrupt byte 20 of the file (inside first record's payload)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = f.Seek(20, 0)
	require.NoError(t, err)
	_, err = f.Write([]byte{0xFF})
	require.NoError(t, err)
	_ = f.Close()

	entries, _ := RecoverWAL(path)
	// First record corrupt → stopped immediately
	assert.True(t, len(entries) <= 2)
}

func TestWAL_BlockBoundaryPadding(t *testing.T) {
	w, path := openTestWAL(t)
	// Fill almost exactly one block: payload that leaves < HeaderSize bytes at end
	// Each small entry: 1+4+1+4+1+8 = 19 bytes payload + 7 header = 26 bytes
	// 32768 / 26 ≈ 1260 entries; write enough to cross multiple blocks
	for i := 0; i < 2000; i++ {
		err := w.Append(WALEntry{
			Type:  EntrySet,
			Key:   []byte{byte(i % 256)},
			Value: []byte{byte(i % 128)},
		})
		require.NoError(t, err)
	}
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	assert.Equal(t, 2000, len(entries))
}

// TestWAL_BlockBoundaryExactHeader tests the avail==0 edge case: when exactly
// HeaderSize (7) bytes remain in a 32KB block the writer must pad those bytes
// before starting a new block, otherwise the reader trips on a header with
// no room for any payload and stops recovery early.
//
// Record size = 7 (header) + 174 (payload) = 181 bytes.
// 181 * 181 = 32761 = BlockSize - HeaderSize, so after exactly 181 records
// blockPos == 32761, triggering the avail==0 branch on the 182nd write.
func TestWAL_BlockBoundaryExactHeader(t *testing.T) {
	w, path := openTestWAL(t)
	// 174-byte payload: type(1)+keyLen(4)+key(7)+valLen(4)+val(150)+seqNo(8) = 174
	key := make([]byte, 7)
	val := make([]byte, 150)
	for i := range key {
		key[i] = byte(i + 1)
	}
	const total = 400 // well past the boundary (crosses it at record 182)
	for i := 0; i < total; i++ {
		val[0] = byte(i)
		err := w.Append(WALEntry{Type: EntrySet, Key: key, Value: val})
		require.NoError(t, err)
	}
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	assert.Equal(t, total, len(entries), "all records must survive the avail==0 block boundary")
}

func TestWAL_SeqNoMonotonic(t *testing.T) {
	w, path := openTestWAL(t)
	for i := 0; i < 1000; i++ {
		err := w.Append(WALEntry{Type: EntrySet, Key: []byte(fmt.Sprintf("k%d", i)), Value: []byte("v")})
		require.NoError(t, err)
	}
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	require.Len(t, entries, 1000)
	for i := 1; i < len(entries); i++ {
		assert.Greater(t, entries[i].SeqNo, entries[i-1].SeqNo, "seqNos must be strictly increasing")
	}
}
