// Package wal — see wal.go for the package doc.
package wal

import "encoding/binary"

// Record type constants — LevelDB 32KB block format
const (
	RecordFull   = uint8(1) // record fits entirely in one block
	RecordFirst  = uint8(2) // first fragment of a multi-block record
	RecordMiddle = uint8(3) // middle fragment
	RecordLast   = uint8(4) // final fragment
)

// EntryType distinguishes WAL record kinds.
type EntryType uint8

const (
	EntrySet    EntryType = 1 // key + value write
	EntryDelete EntryType = 2 // key deletion (tombstone marker)
	EntryFlush  EntryType = 3 // marks memtable flush complete
	EntryBatch  EntryType = 4 // batch of sequential set/delete entries
)

// WALEntry is a single logical entry written to the WAL.
// Wire format: [type:1][keyLen:4LE][key][valLen:4LE][val][seqNo:8LE]
type WALEntry struct {
	Type  EntryType
	Key   []byte
	Value []byte // empty for EntryDelete
	SeqNo uint64
}

// EncodeEntry serialises a WALEntry to its wire format.
func EncodeEntry(e WALEntry) []byte {
	keyLen := len(e.Key)
	valLen := len(e.Value)
	buf := make([]byte, 1+4+keyLen+4+valLen+8)
	buf[0] = byte(e.Type)
	binary.LittleEndian.PutUint32(buf[1:], uint32(keyLen))
	copy(buf[5:], e.Key)
	binary.LittleEndian.PutUint32(buf[5+keyLen:], uint32(valLen))
	copy(buf[9+keyLen:], e.Value)
	binary.LittleEndian.PutUint64(buf[9+keyLen+valLen:], e.SeqNo)
	return buf
}

// EncodeBatch serialises a batch of WAL entries into one atomic payload.
// Wire format: [EntryBatch:1][numEntries:4LE][entry0Len:4LE][entry0]...
func EncodeBatch(entries []WALEntry) []byte {
	total := 1 + 4
	encoded := make([][]byte, len(entries))
	for i, entry := range entries {
		encoded[i] = EncodeEntry(entry)
		total += 4 + len(encoded[i])
	}

	buf := make([]byte, total)
	buf[0] = byte(EntryBatch)
	binary.LittleEndian.PutUint32(buf[1:], uint32(len(entries)))

	pos := 5
	for _, entry := range encoded {
		binary.LittleEndian.PutUint32(buf[pos:], uint32(len(entry)))
		pos += 4
		copy(buf[pos:], entry)
		pos += len(entry)
	}

	return buf
}

// DecodeEntry deserialises a WALEntry from its wire format.
func DecodeEntry(data []byte) (WALEntry, error) {
	if len(data) < 9 {
		return WALEntry{}, ErrCorrupt
	}
	entryType := EntryType(data[0])
	keyLen := int(binary.LittleEndian.Uint32(data[1:]))
	if len(data) < 5+keyLen+4 {
		return WALEntry{}, ErrCorrupt
	}
	key := make([]byte, keyLen)
	copy(key, data[5:])
	valLen := int(binary.LittleEndian.Uint32(data[5+keyLen:]))
	if len(data) < 9+keyLen+valLen+8 {
		return WALEntry{}, ErrCorrupt
	}
	val := make([]byte, valLen)
	copy(val, data[9+keyLen:])
	seqNo := binary.LittleEndian.Uint64(data[9+keyLen+valLen:])
	return WALEntry{Type: entryType, Key: key, Value: val, SeqNo: seqNo}, nil
}

// DecodeEntries deserialises either a single legacy WAL entry or a batched payload.
func DecodeEntries(data []byte) ([]WALEntry, error) {
	if len(data) == 0 {
		return nil, ErrCorrupt
	}
	if EntryType(data[0]) != EntryBatch {
		entry, err := DecodeEntry(data)
		if err != nil {
			return nil, err
		}
		return []WALEntry{entry}, nil
	}
	if len(data) < 5 {
		return nil, ErrCorrupt
	}

	count := int(binary.LittleEndian.Uint32(data[1:5]))
	pos := 5
	entries := make([]WALEntry, 0, count)
	for i := 0; i < count; i++ {
		if len(data[pos:]) < 4 {
			return nil, ErrCorrupt
		}
		entryLen := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if entryLen < 0 || len(data[pos:]) < entryLen {
			return nil, ErrCorrupt
		}
		entry, err := DecodeEntry(data[pos : pos+entryLen])
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
		pos += entryLen
	}
	if pos != len(data) {
		return nil, ErrCorrupt
	}
	return entries, nil
}
