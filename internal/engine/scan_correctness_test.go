package engine

import (
	"fmt"
	"testing"
	"time"
)

// TestScanTombstoneHidesValue verifies that a Delete tombstone in the MemTable
// correctly hides a previously written value during a Scan.
func TestScanTombstoneHidesValue(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, MemTableSize: 64 * 1024 * 1024, SyncWAL: false}

	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	_ = eng.Put([]byte("k1"), []byte("v1"))
	_ = eng.Put([]byte("will-delete"), []byte("value"))
	_ = eng.Put([]byte("k3"), []byte("v3"))
	_ = eng.Delete([]byte("will-delete"))

	results := eng.Scan(nil, nil, 1000)
	for _, kv := range results {
		if kv[0] == "will-delete" {
			t.Error("deleted key appeared in scan results (tombstone hidden)")
		}
	}
	// k1 and k3 must still be present
	found := map[string]bool{}
	for _, kv := range results {
		found[kv[0]] = true
	}
	if !found["k1"] {
		t.Error("k1 missing from scan")
	}
	if !found["k3"] {
		t.Error("k3 missing from scan")
	}
}

// TestScanStreamingLimit verifies that the Scan limit is honoured and results
// are returned in sorted order.
func TestScanStreamingLimit(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, MemTableSize: 64 * 1024 * 1024, SyncWAL: false}
	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	const total = 1000
	for i := 0; i < total; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("key%04d", i)), []byte("value"))
	}

	const limit = 10
	results := eng.Scan(nil, nil, limit)
	if len(results) != limit {
		t.Errorf("expected %d results with limit, got %d", limit, len(results))
	}
	// Results must be in ascending sorted order.
	for i := 1; i < len(results); i++ {
		if results[i][0] < results[i-1][0] {
			t.Errorf("results not sorted at index %d: %q before %q", i, results[i-1][0], results[i][0])
		}
	}
}

// TestScanRangeFilter verifies that start and end bounds are respected.
func TestScanRangeFilter(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, MemTableSize: 64 * 1024 * 1024, SyncWAL: false}
	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	for i := 0; i < 100; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("key%04d", i)), []byte(fmt.Sprintf("val%04d", i)))
	}

	// Scan only keys in [key0010, key0019]
	results := eng.Scan([]byte("key0010"), []byte("key0019"), 100)
	for _, kv := range results {
		if kv[0] < "key0010" || kv[0] > "key0019" {
			t.Errorf("result %q outside scan range [key0010, key0019]", kv[0])
		}
	}
}

// TestScanL0AfterFlush verifies that Scan returns all keys written to L0 after
// a ForceFlush, including the correct (latest) value for overwritten keys.
func TestScanL0AfterFlush(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		DataDir:      dir,
		MemTableSize: 512,
		SyncWAL:      false,
	}

	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	// Write initial value, flush to L0.
	_ = eng.Put([]byte("overlap"), []byte("old-value"))
	eng.ForceFlush()
	time.Sleep(100 * time.Millisecond)

	// Write updated value — goes to new MemTable which will flush to a new L0 file.
	_ = eng.Put([]byte("overlap"), []byte("new-value"))
	eng.ForceFlush()
	time.Sleep(100 * time.Millisecond)

	// Scan must return the newest value for "overlap".
	results := eng.Scan(nil, nil, 100)
	found := false
	for _, kv := range results {
		if kv[0] == "overlap" {
			found = true
			if kv[1] != "new-value" {
				t.Errorf("scan returned stale value %q, want %q", kv[1], "new-value")
			}
		}
	}
	if !found {
		t.Error("key 'overlap' not found in scan results")
	}
}

// TestScanTombstoneHidesL0Value verifies that a tombstone written after a
// value hides the value during a Scan, even after both are flushed to L0.
func TestScanTombstoneHidesL0Value(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		DataDir:      dir,
		MemTableSize: 512,
		SyncWAL:      false,
	}

	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	// Write a value, flush it to L0.
	_ = eng.Put([]byte("will-delete"), []byte("value"))
	eng.ForceFlush()
	time.Sleep(100 * time.Millisecond)

	// Delete the key — tombstone goes to a new MemTable, which flushes to a new L0 file.
	_ = eng.Delete([]byte("will-delete"))
	eng.ForceFlush()
	time.Sleep(100 * time.Millisecond)

	results := eng.Scan(nil, nil, 1000)
	for _, kv := range results {
		if kv[0] == "will-delete" {
			t.Error("deleted key appeared in scan results after flush")
		}
	}
}

// TestScanCompactionResults verifies that after a manual compaction, Scan still
// returns correct and complete data.
func TestScanCompactionResults(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		DataDir:                        dir,
		MemTableSize:                   512,
		SyncWAL:                        false,
		Level0FileNumCompactionTrigger: 2,
	}

	eng, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = eng.Close() }()

	const N = 50
	for i := 0; i < N; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("cmp%04d", i)), []byte(fmt.Sprintf("v%04d", i)))
	}
	eng.ForceFlush()
	eng.ForceCompaction(0)
	time.Sleep(500 * time.Millisecond)

	results := eng.Scan(nil, nil, N+10)
	if len(results) < N {
		t.Errorf("expected at least %d results after compaction, got %d", N, len(results))
	}
}
