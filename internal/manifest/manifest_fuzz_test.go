package manifest

import (
	"bytes"
	"testing"
)

// FuzzVersionEditRoundtrip verifies encode→decode is a lossless roundtrip.
func FuzzVersionEditRoundtrip(f *testing.F) {
	f.Add(uint8(EditAddSSTable), int32(0), uint64(1), uint64(1024),
		[]byte("aaa"), []byte("zzz"), uint64(5), uint64(6), uint64(100))
	f.Add(uint8(EditDeleteSSTable), int32(2), uint64(99), uint64(0),
		[]byte(nil), []byte(nil), uint64(0), uint64(0), uint64(0))
	f.Add(uint8(EditMaxSeqNo), int32(0), uint64(0), uint64(0),
		[]byte(nil), []byte(nil), uint64(0), uint64(0), uint64(999))
	f.Add(uint8(EditLogNumber), int32(1), uint64(3), uint64(512),
		[]byte("first"), []byte("last"), uint64(42), uint64(7), uint64(0))

	f.Fuzz(func(t *testing.T,
		editType uint8, level int32, fileID, fileSize uint64,
		firstKey, lastKey []byte, logNum, nextFileID, maxSeqNo uint64) {

		edit := VersionEdit{
			Type:       EditType(editType),
			Level:      int(level),
			FileID:     fileID,
			FileSize:   fileSize,
			FirstKey:   firstKey,
			LastKey:    lastKey,
			LogNumber:  logNum,
			NextFileID: nextFileID,
			MaxSeqNo:   maxSeqNo,
		}
		encoded := EncodeVersionEdit(edit)
		decoded, err := DecodeVersionEdit(encoded)
		if err != nil {
			t.Fatalf("decode failed on own encoded output: %v", err)
		}
		if decoded.Type != edit.Type {
			t.Errorf("Type mismatch: %v != %v", decoded.Type, edit.Type)
		}
		if decoded.FileID != edit.FileID {
			t.Errorf("FileID mismatch: %v != %v", decoded.FileID, edit.FileID)
		}
		if decoded.FileSize != edit.FileSize {
			t.Errorf("FileSize mismatch: %v != %v", decoded.FileSize, edit.FileSize)
		}
		if !bytes.Equal(decoded.FirstKey, firstKey) {
			t.Errorf("FirstKey mismatch: %q != %q", decoded.FirstKey, firstKey)
		}
		if !bytes.Equal(decoded.LastKey, lastKey) {
			t.Errorf("LastKey mismatch: %q != %q", decoded.LastKey, lastKey)
		}
		if decoded.LogNumber != edit.LogNumber {
			t.Errorf("LogNumber mismatch: %v != %v", decoded.LogNumber, edit.LogNumber)
		}
		if decoded.NextFileID != edit.NextFileID {
			t.Errorf("NextFileID mismatch: %v != %v", decoded.NextFileID, edit.NextFileID)
		}
		if decoded.MaxSeqNo != edit.MaxSeqNo {
			t.Errorf("MaxSeqNo mismatch: %v != %v", decoded.MaxSeqNo, edit.MaxSeqNo)
		}
	})
}

// FuzzDecodeVersionEditCorrupted verifies DecodeVersionEdit never panics on garbage input.
func FuzzDecodeVersionEditCorrupted(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0xFF})
	f.Add(bytes.Repeat([]byte{0x00}, 50))
	f.Add(bytes.Repeat([]byte{0xFF}, 100))
	f.Add([]byte{0x01, 0x00, 0x00, 0x00, 0x00}) // only type+partial level

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic; errors are acceptable.
		_, _ = DecodeVersionEdit(data)
	})
}
