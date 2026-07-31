package engine

import (
	"fmt"
	"testing"
)

// TestSeqNoPersistence is the regression test for the critical bug where
// all SSTable data became invisible after a clean flush+restart cycle.
// Previously: flush deleted the WAL, restart found empty WAL, seqNo=0,
// SSTableReader.Get gated on SeqNo<=readSeqNo — all entries invisible.
func TestSeqNoPersistence(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, MemTableSize: 1024, SyncWAL: false}

	// Phase 1: write keys and force flush to SSTable.
	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const numKeys = 50
	for i := 0; i < numKeys; i++ {
		if err := eng.Put([]byte(fmt.Sprintf("key%03d", i)), []byte(fmt.Sprintf("val%03d", i))); err != nil {
			t.Fatal(err)
		}
	}
	eng.ForceFlush()
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}

	// Phase 2: reopen and verify ALL keys are visible.
	eng2, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer func() { _ = eng2.Close() }()

	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		want := fmt.Sprintf("val%03d", i)
		got, err := eng2.Get(key)
		if err != nil {
			t.Errorf("key %s missing after flush+restart: %v", key, err)
			continue
		}
		if string(got) != want {
			t.Errorf("key %s: want %q got %q", key, want, got)
		}
	}
}

// TestSeqNoPersistenceMultipleFlushes verifies seqNo is correctly tracked
// across multiple flush cycles (each flush should update the persisted maxSeqNo).
func TestSeqNoPersistenceMultipleFlushes(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, MemTableSize: 512, SyncWAL: false}

	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Write enough keys to trigger multiple flushes.
	for i := 0; i < 200; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("key%04d", i)), []byte(fmt.Sprintf("v%04d", i)))
	}
	eng.ForceFlush()
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}

	eng2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng2.Close() }()

	// Spot-check a spread of keys from the written set.
	for _, i := range []int{0, 50, 100, 150, 199} {
		key := []byte(fmt.Sprintf("key%04d", i))
		if _, err := eng2.Get(key); err != nil {
			t.Errorf("key %s missing after multi-flush restart: %v", key, err)
		}
	}
}

// TestSeqNoPersistenceOverwriteThenFlush verifies that after overwriting a key
// and flushing, only the latest value is visible after restart.
func TestSeqNoPersistenceOverwriteThenFlush(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, MemTableSize: 1024, SyncWAL: false}

	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}

	_ = eng.Put([]byte("key"), []byte("old-value"))
	_ = eng.Put([]byte("key"), []byte("new-value"))
	eng.ForceFlush()
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}

	eng2, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng2.Close() }()

	got, err := eng2.Get([]byte("key"))
	if err != nil {
		t.Fatalf("key missing after flush+restart: %v", err)
	}
	if string(got) != "new-value" {
		t.Errorf("expected %q, got %q", "new-value", got)
	}
}
