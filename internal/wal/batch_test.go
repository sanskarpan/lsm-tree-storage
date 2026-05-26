package wal

import (
	"os"
	"path/filepath"
	"testing"

	"lsm-engine/internal/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWAL_BatchRecoveryPreservesAtomicity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal")

	w, err := OpenWAL(path, &events.NoopBus{})
	require.NoError(t, err)

	require.NoError(t, w.AppendWithSeqNo(WALEntry{
		Type:  EntrySet,
		Key:   []byte("stable"),
		Value: []byte("v1"),
		SeqNo: 1,
	}))
	require.NoError(t, w.AppendBatch([]WALEntry{
		{Type: EntrySet, Key: []byte("batch-a"), Value: []byte("va"), SeqNo: 2},
		{Type: EntryDelete, Key: []byte("batch-b"), SeqNo: 3},
	}))
	require.NoError(t, w.Sync())
	require.NoError(t, w.Close())

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Truncate(path, info.Size()-9))

	entries, err := RecoverWAL(path)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, []byte("stable"), entries[0].Key)
	assert.Equal(t, uint64(1), entries[0].SeqNo)
}
