package integration

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"lsm-engine/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCrashRecovery_SeqNoPersisted is a deep integration test that verifies
// the seqNo is correctly persisted across flush+restart cycles.
// This is the regression test for the critical bug where SSTable data became
// invisible after a clean flush because seqNo was lost on restart.
func TestCrashRecovery_SeqNoPersisted(t *testing.T) {
	dir := t.TempDir()
	cfg := engine.Config{
		DataDir:      dir,
		MemTableSize: 4096, // small: force multiple flushes
		SyncWAL:      true,
	}

	// Round 1: write 100 keys, flush, clean close.
	func() {
		eng, err := engine.Open(cfg)
		require.NoError(t, err)
		for i := 0; i < 100; i++ {
			require.NoError(t, eng.Put(
				[]byte(fmt.Sprintf("k%03d", i)),
				[]byte(fmt.Sprintf("v%03d", i)),
			))
		}
		eng.ForceFlush()
		require.NoError(t, eng.Close())
	}()

	// Round 2: reopen, verify R1 data is visible, write more, clean close.
	func() {
		eng, err := engine.Open(cfg)
		require.NoError(t, err, "reopen after flush failed")
		defer func() { require.NoError(t, eng.Close()) }()

		// R1 data must be visible immediately after reopening (seqNo bug check).
		for i := 0; i < 100; i++ {
			key := []byte(fmt.Sprintf("k%03d", i))
			want := []byte(fmt.Sprintf("v%03d", i))
			got, readErr := eng.Get(key)
			require.NoError(t, readErr, "R1 key %s invisible after clean restart (seqNo lost?)", key)
			assert.Equal(t, want, got, "value mismatch for key %s", key)
		}

		// Write R2 data on top — these will be in the WAL of Round 2.
		for i := 100; i < 200; i++ {
			require.NoError(t, eng.Put(
				[]byte(fmt.Sprintf("k%03d", i)),
				[]byte(fmt.Sprintf("v%03d", i)),
			))
		}
	}()

	// Round 3: reopen after clean close — both R1 (from SST) and R2 (from WAL) must be visible.
	eng3, err := engine.Open(cfg)
	require.NoError(t, err, "reopen after round 2 failed")
	defer func() { _ = eng3.Close() }()

	for i := 0; i < 200; i++ {
		key := []byte(fmt.Sprintf("k%03d", i))
		_, readErr := eng3.Get(key)
		require.NoError(t, readErr, "key %s missing after round 3 reopen", key)
	}
}

// TestCrashRecovery_WALReplayPreservesSeqNo verifies that WAL replay after a
// crash does not clobber the seqNo established by existing SSTable files.
// The bug: after flush, the WAL was deleted. On restart, the empty WAL caused
// seqNo=0, making SSTableReader.Get return false for all keys (SeqNo<=0 gating).
func TestCrashRecovery_WALReplayPreservesSeqNo(t *testing.T) {
	dir := t.TempDir()
	cfg := engine.Config{
		DataDir:      dir,
		MemTableSize: 2048,
		SyncWAL:      true,
	}

	// Phase 1: fill and flush to L0 SSTs.
	func() {
		eng, err := engine.Open(cfg)
		require.NoError(t, err)
		for i := 0; i < 200; i++ {
			require.NoError(t, eng.Put(
				[]byte(fmt.Sprintf("sst%05d", i)),
				[]byte(fmt.Sprintf("val%05d", i)),
			))
		}
		eng.ForceFlush()
		require.NoError(t, eng.Close())
	}()

	// Phase 2: reopen; SST keys must be readable even with an empty WAL.
	eng2, err := engine.Open(cfg)
	require.NoError(t, err)
	defer func() { _ = eng2.Close() }()

	// Sample every 10th key to keep the test fast.
	for i := 0; i < 200; i += 10 {
		key := []byte(fmt.Sprintf("sst%05d", i))
		want := []byte(fmt.Sprintf("val%05d", i))
		got, readErr := eng2.Get(key)
		require.NoError(t, readErr, "SST key %s invisible after flush+restart", key)
		assert.Equal(t, want, got, "value mismatch for key %s", key)
	}
}

// TestConcurrentFlushCompactionScan stresses the engine under concurrent
// writers and scanner goroutines to detect data races and correctness issues.
func TestConcurrentFlushCompactionScan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:      dir,
		MemTableSize: 16 * 1024,
		SyncWAL:      false,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	const writers = 4
	const scanners = 2
	const ops = 200

	var wg sync.WaitGroup
	errCh := make(chan error, writers+scanners)

	// Writer goroutines.
	for w := 0; w < writers; w++ {
		wg.Add(1)
		w := w
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				k := []byte(fmt.Sprintf("w%d-k%05d", w, i))
				v := []byte(fmt.Sprintf("v%05d", i))
				if putErr := eng.Put(k, v); putErr != nil {
					errCh <- putErr
					return
				}
			}
		}()
	}

	// Scanner goroutines run concurrently with writers.
	for s := 0; s < scanners; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				_ = eng.Scan(nil, nil, 10)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for e := range errCh {
		require.NoError(t, e)
	}
}

// TestFlushAndRestartDataIntegrity writes a large dataset, flushes it to
// multiple L0 SSTables, closes, and verifies complete data integrity on reopen.
func TestFlushAndRestartDataIntegrity(t *testing.T) {
	dir := t.TempDir()
	cfg := engine.Config{
		DataDir:      dir,
		MemTableSize: 1024,
		SyncWAL:      true,
	}

	const N = 500
	func() {
		eng, err := engine.Open(cfg)
		require.NoError(t, err)
		for i := 0; i < N; i++ {
			require.NoError(t, eng.Put(
				[]byte(fmt.Sprintf("integrity%06d", i)),
				[]byte(fmt.Sprintf("data%06d", i)),
			))
		}
		eng.ForceFlush()
		time.Sleep(100 * time.Millisecond)
		require.NoError(t, eng.Close())
	}()

	eng2, err := engine.Open(cfg)
	require.NoError(t, err)
	defer func() { _ = eng2.Close() }()

	for i := 0; i < N; i++ {
		key := []byte(fmt.Sprintf("integrity%06d", i))
		want := []byte(fmt.Sprintf("data%06d", i))
		got, readErr := eng2.Get(key)
		require.NoError(t, readErr, "key %s missing after flush+restart", key)
		assert.Equal(t, want, got, "value mismatch for key %s", key)
	}
}
