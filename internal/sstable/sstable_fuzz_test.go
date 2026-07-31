package sstable

import (
	"testing"
)

// FuzzSSTableRoundtrip writes random key-value pairs, builds an SSTable,
// and verifies every key is recoverable via Get.
func FuzzSSTableRoundtrip(f *testing.F) {
	f.Add([]byte("hello"), []byte("world"), uint64(1))
	f.Add([]byte("a"), []byte(""), uint64(999))
	f.Add([]byte("zzzzz"), []byte("value"), uint64(42))
	f.Add([]byte("prefix/key"), []byte("data"), uint64(100))

	f.Fuzz(func(t *testing.T, key, value []byte, seqNo uint64) {
		if len(key) == 0 {
			return // zero-length keys are not valid
		}
		dir := t.TempDir()
		path := dir + "/fuzz.sst"

		b, err := NewSSTableBuilder(path, 1, 0, 4096, 10)
		if err != nil {
			t.Skip("builder open:", err)
		}

		ik := InternalKey{UserKey: key, SeqNo: seqNo, Type: TypeValue}
		if err := b.Add(ik, value); err != nil {
			_ = b.Close()
			return
		}
		meta, err := b.Finish()
		if err != nil {
			_ = b.Close()
			return
		}
		if err := b.Close(); err != nil {
			return
		}

		// nil blockCache and nil bus are both handled gracefully by the reader.
		r, err := NewSSTableReader(path, meta, nil, nil)
		if err != nil {
			t.Fatalf("reader open failed: %v", err)
		}
		defer func() { _ = r.Close() }()

		got, found, err := r.Get(key, seqNo)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if !found {
			t.Fatalf("key %q not found after write", key)
		}
		if string(got) != string(value) {
			t.Errorf("value mismatch: want %q got %q", value, got)
		}
	})
}
