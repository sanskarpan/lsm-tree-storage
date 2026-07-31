package wal

import (
	"bytes"
	"os"
	"testing"
)

// FuzzWALEntryRoundtrip verifies that any valid WAL entry survives a
// write→recover cycle without data loss or corruption.
func FuzzWALEntryRoundtrip(f *testing.F) {
	// Seed corpus with varied entry types and sizes.
	f.Add([]byte("key1"), []byte("value1"), uint8(0))                                          // maps to EntrySet
	f.Add([]byte(""), []byte(""), uint8(1))                                                    // maps to EntryDelete
	f.Add(bytes.Repeat([]byte("k"), 1024), bytes.Repeat([]byte("v"), 4096), uint8(0))         // large entry
	f.Add([]byte("delete-key"), []byte("ignored-value"), uint8(1))

	f.Fuzz(func(t *testing.T, key, value []byte, entryTypeByte uint8) {
		dir := t.TempDir()
		path := dir + "/fuzz.log"

		// Map to valid entry types: 1=EntrySet, 2=EntryDelete
		entryType := EntryType((entryTypeByte%2) + 1)

		w, err := OpenWAL(path, nil)
		if err != nil {
			t.Skip("cannot open WAL:", err)
		}

		entry := WALEntry{Type: entryType, Key: key, Value: value, SeqNo: 42}
		if err := w.AppendWithSeqNo(entry); err != nil {
			_ = w.Close()
			return // legitimate: e.g. very large entry that the OS rejects
		}
		if err := w.Close(); err != nil {
			return
		}

		recovered, err := RecoverWAL(path)
		if err != nil {
			t.Fatalf("recovery failed after clean write: %v", err)
		}
		if len(recovered) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(recovered))
		}
		got := recovered[0]
		if !bytes.Equal(got.Key, key) {
			t.Errorf("key mismatch: wrote %q got %q", key, got.Key)
		}
		if entryType == EntrySet && !bytes.Equal(got.Value, value) {
			t.Errorf("value mismatch: wrote %q got %q", value, got.Value)
		}
		if got.Type != entryType {
			t.Errorf("type mismatch: wrote %v got %v", entryType, got.Type)
		}
	})
}

// FuzzWALCorruptedInput verifies that RecoverWAL never panics on corrupted input.
func FuzzWALCorruptedInput(f *testing.F) {
	// Seed with known good record + corrupted variants.
	f.Add([]byte{})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, RecordFull})
	f.Add(bytes.Repeat([]byte{0x00}, 100))
	f.Add(bytes.Repeat([]byte{0xFF}, 100))
	f.Add([]byte{RecordFull, 0x01, 0x00, 0x00, 0x00}) // truncated record

	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		path := dir + "/corrupt.log"
		if err := os.WriteFile(path, data, 0644); err != nil {
			return
		}
		// Must not panic; errors are acceptable.
		_, _ = RecoverWAL(path)
	})
}
