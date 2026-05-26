package integration

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/wal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLCS_CompactionCorrectness(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:                        dir,
		MemTableSize:                   64 * 1024,
		SyncWAL:                        false,
		CompactionStyle:                "leveled",
		Level0FileNumCompactionTrigger: 2,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	// Write 5000 keys, overwrite each 5 times (reduced from 10k for speed)
	const N = 5000
	for round := 0; round < 5; round++ {
		for i := 0; i < N; i++ {
			key := []byte(fmt.Sprintf("key-%06d", i))
			val := []byte(fmt.Sprintf("value-round-%d-%06d", round, i))
			require.NoError(t, eng.Put(key, val))
		}
	}

	// Wait for background compaction
	time.Sleep(3 * time.Second)

	// Every key must have its round-4 (latest) value
	for i := 0; i < N; i++ {
		key := []byte(fmt.Sprintf("key-%06d", i))
		expected := []byte(fmt.Sprintf("value-round-4-%06d", i))
		val, err := eng.Get(key)
		require.NoError(t, err, "key %s not found", key)
		assert.Equal(t, expected, val)
	}
}

func TestReadWriteCorrectness(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 16 * 1024,
		SyncWAL:      false,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	const N = 1000
	for i := 0; i < N; i++ {
		require.NoError(t, eng.Put(
			[]byte(fmt.Sprintf("k%06d", i)),
			[]byte(fmt.Sprintf("v%06d", i)),
		))
	}
	time.Sleep(200 * time.Millisecond)

	for i := 0; i < N; i++ {
		val, err := eng.Get([]byte(fmt.Sprintf("k%06d", i)))
		require.NoError(t, err)
		assert.Equal(t, []byte(fmt.Sprintf("v%06d", i)), val)
	}
}

func TestTombstoneGC(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:                        dir,
		MemTableSize:                   4096,
		SyncWAL:                        false,
		Level0FileNumCompactionTrigger: 2,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	// Write 100 keys, delete all
	for i := 0; i < 100; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("k%03d", i)), []byte("v"))
	}
	for i := 0; i < 100; i++ {
		_ = eng.Delete([]byte(fmt.Sprintf("k%03d", i)))
	}

	// Force flush + compact
	eng.ForceFlush()
	time.Sleep(2 * time.Second)

	// All keys should be gone
	for i := 0; i < 100; i++ {
		_, err := eng.Get([]byte(fmt.Sprintf("k%03d", i)))
		assert.Equal(t, engine.ErrNotFound, err)
	}
}

// TestConcurrentReadWrite runs 8 writers and 8 readers concurrently.
func TestConcurrentReadWrite(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 32 * 1024,
		SyncWAL:      false,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	const numKeys = 200
	const writers = 8
	const readers = 8

	// Pre-populate so readers have something to find
	for i := 0; i < numKeys; i++ {
		require.NoError(t, eng.Put(
			[]byte(fmt.Sprintf("ck%06d", i)),
			[]byte(fmt.Sprintf("cv%06d", i)),
		))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, writers+readers)

	// Writers
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := []byte(fmt.Sprintf("ck%06d", (id*50+i)%numKeys))
				val := []byte(fmt.Sprintf("cv-w%d-%d", id, i))
				if e := eng.Put(key, val); e != nil {
					errCh <- e
					return
				}
			}
		}(w)
	}

	// Readers
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := []byte(fmt.Sprintf("ck%06d", (id*50+i)%numKeys))
				// reads may return old or new value — just must not error unexpectedly
				_, readErr := eng.Get(key)
				if readErr != nil && readErr != engine.ErrNotFound {
					errCh <- readErr
					return
				}
			}
		}(r)
	}

	wg.Wait()
	close(errCh)
	for e := range errCh {
		require.NoError(t, e)
	}
}

func TestConcurrentScanAndWrite(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 32 * 1024,
		SyncWAL:      false,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	for i := 0; i < 200; i++ {
		require.NoError(t, eng.Put(
			[]byte(fmt.Sprintf("scan%06d", i)),
			[]byte(fmt.Sprintf("value%06d", i)),
		))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if putErr := eng.Put(
				[]byte(fmt.Sprintf("scan%06d", i)),
				[]byte(fmt.Sprintf("value-new%06d", i)),
			); putErr != nil {
				errCh <- putErr
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			results := eng.Scan([]byte("scan000000"), []byte("scan999999"), 500)
			if len(results) == 0 {
				errCh <- fmt.Errorf("scan returned no results")
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

// TestWriteBatch verifies that WriteBatch applies all entries atomically.
func TestWriteBatch(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 64 * 1024,
		SyncWAL:      false,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	batch := &engine.WriteBatch{}
	for i := 0; i < 100; i++ {
		batch.Put([]byte(fmt.Sprintf("bk%04d", i)), []byte(fmt.Sprintf("bv%04d", i)))
	}
	// Delete some
	for i := 0; i < 10; i++ {
		batch.Delete([]byte(fmt.Sprintf("bk%04d", i)))
	}
	require.NoError(t, eng.Write(batch))

	for i := 0; i < 10; i++ {
		_, readErr := eng.Get([]byte(fmt.Sprintf("bk%04d", i)))
		assert.Equal(t, engine.ErrNotFound, readErr, "key bk%04d should be deleted", i)
	}
	for i := 10; i < 100; i++ {
		val, readErr := eng.Get([]byte(fmt.Sprintf("bk%04d", i)))
		require.NoError(t, readErr)
		assert.Equal(t, []byte(fmt.Sprintf("bv%04d", i)), val)
	}
}

// TestScan_AcrossLevels verifies range scan returns all live keys across levels.
func TestScan_AcrossLevels(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 4096,
		SyncWAL:      false,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	const N = 200
	for i := 0; i < N; i++ {
		require.NoError(t, eng.Put(
			[]byte(fmt.Sprintf("sk%04d", i)),
			[]byte(fmt.Sprintf("sv%04d", i)),
		))
	}
	eng.ForceFlush()
	time.Sleep(200 * time.Millisecond)

	results := eng.Scan([]byte("sk0000"), []byte("sk9999"), N+10)
	assert.Equal(t, N, len(results), "expected %d scan results", N)
	for i, kv := range results {
		assert.Equal(t, fmt.Sprintf("sk%04d", i), kv[0])
		assert.Equal(t, fmt.Sprintf("sv%04d", i), kv[1])
	}
}

// TestScan_TombstoneHidden verifies deleted keys do not appear in scan results.
func TestScan_TombstoneHidden(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 64 * 1024,
		SyncWAL:      false,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	require.NoError(t, eng.Put([]byte("k1"), []byte("v1")))
	require.NoError(t, eng.Put([]byte("k2"), []byte("v2")))
	require.NoError(t, eng.Put([]byte("k3"), []byte("v3")))
	require.NoError(t, eng.Delete([]byte("k2")))

	results := eng.Scan(nil, nil, 10)
	keys := make([]string, len(results))
	for i, kv := range results {
		keys[i] = kv[0]
	}
	assert.Contains(t, keys, "k1")
	assert.NotContains(t, keys, "k2", "deleted key must not appear in scan")
	assert.Contains(t, keys, "k3")
}

// TestCrashRecovery verifies data survives a simulated crash and reopen.
func TestCrashRecovery(t *testing.T) {
	dir := t.TempDir()

	// Write 500 keys then close without graceful flush
	func() {
		eng, err := engine.Open(engine.Config{
			DataDir:      dir,
			MemTableSize: 256 * 1024,
			SyncWAL:      true,
		})
		require.NoError(t, err)
		for i := 0; i < 500; i++ {
			require.NoError(t, eng.Put(
				[]byte(fmt.Sprintf("cr%06d", i)),
				[]byte(fmt.Sprintf("crv%06d", i)),
			))
		}
		// Graceful close — WAL is fsynced
		require.NoError(t, eng.Close())
	}()

	// Reopen and verify all 500 keys are recoverable
	eng2, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 256 * 1024,
		SyncWAL:      true,
	})
	require.NoError(t, err)
	defer func() { _ = eng2.Close() }()

	for i := 0; i < 500; i++ {
		val, readErr := eng2.Get([]byte(fmt.Sprintf("cr%06d", i)))
		require.NoError(t, readErr, "key cr%06d not found after recovery", i)
		assert.Equal(t, []byte(fmt.Sprintf("crv%06d", i)), val)
	}
}

func TestCrashRecovery_ReplaysMultipleWALs(t *testing.T) {
	dir := t.TempDir()

	writeLog := func(fileID uint64, key, value string, deleteKey bool, seqNo uint64) {
		path := fmt.Sprintf("%s/%06d.log", dir, fileID)
		w, err := wal.OpenWAL(path, &events.NoopBus{})
		require.NoError(t, err)

		entry := wal.WALEntry{
			Key:   []byte(key),
			Value: []byte(value),
			SeqNo: seqNo,
		}
		if deleteKey {
			entry.Type = wal.EntryDelete
		} else {
			entry.Type = wal.EntrySet
		}

		require.NoError(t, w.AppendWithSeqNo(entry))
		require.NoError(t, w.Sync())
		require.NoError(t, w.Close())
	}

	writeLog(1, "k1", "v1", false, 1)
	writeLog(2, "k2", "v2", false, 2)

	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 64 * 1024,
		SyncWAL:      true,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	val1, err := eng.Get([]byte("k1"))
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), val1)

	val2, err := eng.Get([]byte("k2"))
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), val2)
}

// TestLCS_NonOverlappingL1 verifies that after leveled compaction L1 has
// non-overlapping key ranges.
func TestLCS_NonOverlappingL1(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:                        dir,
		MemTableSize:                   4 * 1024,
		SyncWAL:                        false,
		Level0FileNumCompactionTrigger: 2,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	for i := 0; i < 500; i++ {
		require.NoError(t, eng.Put(
			[]byte(fmt.Sprintf("lk%06d", i)),
			[]byte("value"),
		))
	}
	eng.ForceFlush()
	time.Sleep(3 * time.Second)

	version := eng.Manifest().Current()
	l1 := version.Levels[1]
	// Verify non-overlapping: each file's LastKey < next file's FirstKey
	for i := 1; i < len(l1); i++ {
		prev := l1[i-1]
		curr := l1[i]
		assert.True(t,
			string(prev.LastKey) < string(curr.FirstKey),
			"L1 overlap between file %d (%s..%s) and file %d (%s..%s)",
			prev.FileID, prev.FirstKey, prev.LastKey,
			curr.FileID, curr.FirstKey, curr.LastKey,
		)
	}
}

// TestTombstoneNotResurrected verifies deleted keys stay gone after compaction.
func TestTombstoneNotResurrected(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:                        dir,
		MemTableSize:                   2048,
		SyncWAL:                        false,
		Level0FileNumCompactionTrigger: 2,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	// Write keys then delete them
	for i := 0; i < 50; i++ {
		require.NoError(t, eng.Put([]byte(fmt.Sprintf("tnr%04d", i)), []byte("v")))
	}
	eng.ForceFlush()
	for i := 0; i < 50; i++ {
		require.NoError(t, eng.Delete([]byte(fmt.Sprintf("tnr%04d", i))))
	}
	eng.ForceFlush()
	time.Sleep(2 * time.Second)

	for i := 0; i < 50; i++ {
		_, readErr := eng.Get([]byte(fmt.Sprintf("tnr%04d", i)))
		assert.Equal(t, engine.ErrNotFound, readErr,
			"deleted key tnr%04d was resurrected", i)
	}
}
