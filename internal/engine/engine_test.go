package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestEngine(t *testing.T, cfg Config) *LSMEngine {
	t.Helper()
	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}
	e, err := Open(cfg)
	require.NoError(t, err)
	return e
}

func TestEngine_BasicPutGet(t *testing.T) {
	e := openTestEngine(t, Config{})
	defer func() { _ = e.Close() }()

	require.NoError(t, e.Put([]byte("hello"), []byte("world")))
	val, err := e.Get([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, []byte("world"), val)

	_, err = e.Get([]byte("nonexistent"))
	assert.Equal(t, ErrNotFound, err)
}

func TestEngine_Delete(t *testing.T) {
	e := openTestEngine(t, Config{})
	defer func() { _ = e.Close() }()

	require.NoError(t, e.Put([]byte("k1"), []byte("v1")))
	require.NoError(t, e.Delete([]byte("k1")))

	_, err := e.Get([]byte("k1"))
	assert.Equal(t, ErrNotFound, err)
}

func TestGet_MemTable(t *testing.T) {
	e := openTestEngine(t, Config{})
	defer func() { _ = e.Close() }()

	require.NoError(t, e.Put([]byte("key"), []byte("val")))
	val, err := e.Get([]byte("key"))
	require.NoError(t, err)
	assert.Equal(t, []byte("val"), val)
}

func TestFlush_MemTableToL0(t *testing.T) {
	dir := t.TempDir()
	// Small MemTable to force flush
	e := openTestEngine(t, Config{DataDir: dir, MemTableSize: 4096, SyncWAL: false})
	defer func() { _ = e.Close() }()

	// Write enough data to trigger flush
	for i := 0; i < 500; i++ {
		require.NoError(t, e.Put(
			[]byte(fmt.Sprintf("key-%04d", i)),
			[]byte(fmt.Sprintf("val-%04d", i)),
		))
	}
	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	version := e.manifest.Current()
	assert.GreaterOrEqual(t, len(version.Levels[0]), 1, "should have at least 1 SSTable in L0")

	// Data must still be readable
	for i := 0; i < 500; i++ {
		val, err := e.Get([]byte(fmt.Sprintf("key-%04d", i)))
		require.NoError(t, err)
		assert.Equal(t, []byte(fmt.Sprintf("val-%04d", i)), val)
	}
}

func TestCrashRecovery_MidWrite(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: write 500 keys
	{
		engine, err := Open(Config{DataDir: dir, SyncWAL: true})
		require.NoError(t, err)
		for i := 0; i < 500; i++ {
			_ = engine.Put([]byte(fmt.Sprintf("k%04d", i)), []byte(fmt.Sprintf("v%04d", i)))
		}
		// Simulate crash: close file handles without graceful shutdown
		engine.crashClose()
	}

	// Phase 2: reopen and verify
	{
		engine, err := Open(Config{DataDir: dir, SyncWAL: true})
		require.NoError(t, err)
		defer func() { _ = engine.Close() }()
		for i := 0; i < 500; i++ {
			val, err := engine.Get([]byte(fmt.Sprintf("k%04d", i)))
			require.NoError(t, err, "key k%04d not found after recovery", i)
			assert.Equal(t, []byte(fmt.Sprintf("v%04d", i)), val)
		}
	}
}

func TestCrashRecovery_WriteBatchIsAtomic(t *testing.T) {
	dir := t.TempDir()

	{
		engine, err := Open(Config{DataDir: dir, SyncWAL: true})
		require.NoError(t, err)

		batch := &WriteBatch{}
		batch.Put([]byte("a"), []byte("1"))
		batch.Put([]byte("b"), []byte("2"))
		batch.Delete([]byte("ghost"))
		require.NoError(t, engine.Write(batch))
		engine.crashClose()
	}

	{
		engine, err := Open(Config{DataDir: dir, SyncWAL: true})
		require.NoError(t, err)
		defer func() { _ = engine.Close() }()

		val, err := engine.Get([]byte("a"))
		require.NoError(t, err)
		assert.Equal(t, []byte("1"), val)

		val, err = engine.Get([]byte("b"))
		require.NoError(t, err)
		assert.Equal(t, []byte("2"), val)

		_, err = engine.Get([]byte("ghost"))
		assert.Equal(t, ErrNotFound, err)
	}
}

func TestTombstoneNotResurrected(t *testing.T) {
	dir := t.TempDir()
	engine, err := Open(Config{DataDir: dir, MemTableSize: 4096, SyncWAL: false})
	require.NoError(t, err)
	defer func() { _ = engine.Close() }()

	// Write key, then delete it
	_ = engine.Put([]byte("hello"), []byte("world"))
	_ = engine.Delete([]byte("hello"))

	// Force flush + compaction to bottom level
	engine.ForceFlush()
	time.Sleep(500 * time.Millisecond)
	engine.ForceCompaction(0)
	time.Sleep(500 * time.Millisecond)

	// Key must be gone (not resurrected from old SSTable)
	_, err = engine.Get([]byte("hello"))
	assert.Equal(t, ErrNotFound, err)
}

func TestEngine_OverwriteKey(t *testing.T) {
	e := openTestEngine(t, Config{})
	defer func() { _ = e.Close() }()

	require.NoError(t, e.Put([]byte("k"), []byte("v1")))
	require.NoError(t, e.Put([]byte("k"), []byte("v2")))
	require.NoError(t, e.Put([]byte("k"), []byte("v3")))

	val, err := e.Get([]byte("k"))
	require.NoError(t, err)
	assert.Equal(t, []byte("v3"), val)
}

func TestEngine_GetAfterFlush(t *testing.T) {
	dir := t.TempDir()
	e := openTestEngine(t, Config{DataDir: dir, MemTableSize: 4096, SyncWAL: false})
	defer func() { _ = e.Close() }()

	for i := 0; i < 200; i++ {
		_ = e.Put([]byte(fmt.Sprintf("k%04d", i)), []byte(fmt.Sprintf("v%04d", i)))
	}
	time.Sleep(200 * time.Millisecond)

	for i := 0; i < 200; i++ {
		val, err := e.Get([]byte(fmt.Sprintf("k%04d", i)))
		require.NoError(t, err)
		assert.Equal(t, []byte(fmt.Sprintf("v%04d", i)), val)
	}
}

func BenchmarkSequentialWrite(b *testing.B) {
	dir := b.TempDir()
	e, err := Open(Config{DataDir: dir, MemTableSize: 64 * 1024 * 1024, SyncWAL: false})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = e.Close() }()

	val := make([]byte, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("bench-key-%010d", i))
		if err := e.Put(key, val); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(val)))
}

func BenchmarkPointRead_WarmCache(b *testing.B) {
	dir := b.TempDir()
	e, err := Open(Config{DataDir: dir, MemTableSize: 4 * 1024 * 1024, SyncWAL: false})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = e.Close() }()

	const N = 1000
	for i := 0; i < N; i++ {
		_ = e.Put([]byte(fmt.Sprintf("bk%06d", i)), []byte(fmt.Sprintf("bv%06d", i)))
	}
	e.ForceFlush()
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("bk%06d", i%N))
		if _, err := e.Get(key); err != nil && err != ErrNotFound {
			b.Fatal(err)
		}
	}
}

func BenchmarkPointRead_NoBloom_vs_Bloom(b *testing.B) {
	dir := b.TempDir()
	e, err := Open(Config{
		DataDir: dir, MemTableSize: 4 * 1024 * 1024,
		SyncWAL: false, BloomBitsPerKey: 10,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = e.Close() }()

	const N = 2000
	for i := 0; i < N; i++ {
		_ = e.Put([]byte(fmt.Sprintf("bloom-key-%06d", i)), []byte("value"))
	}
	e.ForceFlush()
	time.Sleep(200 * time.Millisecond)

	missingKey := []byte("definitely-not-present-key-xyz")
	b.ResetTimer()
	// With bloom filters: missing keys are rejected quickly
	for i := 0; i < b.N; i++ {
		_, _ = e.Get(missingKey)
	}
}
