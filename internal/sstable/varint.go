// Package sstable — see format.go for the package doc.
package sstable

import "encoding/binary"

// appendVarint appends a variable-length integer to buf
func appendVarint(buf []byte, v uint64) []byte {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	return append(buf, tmp[:n]...)
}

// decodeVarint decodes a variable-length integer from data
// Returns (value, bytesConsumed)
func decodeVarint(data []byte) (uint64, int) {
	v, n := binary.Uvarint(data)
	return v, n
}

// encodeKey encodes an InternalKey as [UserKey | SeqNo:8BE | TypeByte].
func encodeKey(key InternalKey) []byte {
	result := make([]byte, len(key.UserKey)+8+1)
	copy(result, key.UserKey)
	binary.BigEndian.PutUint64(result[len(key.UserKey):len(key.UserKey)+8], key.SeqNo)
	result[len(result)-1] = byte(key.Type)
	return result
}

// decodeKey decodes an InternalKey from [UserKey | SeqNo:8BE | TypeByte].
func decodeKey(data []byte) InternalKey {
	if len(data) < 9 {
		return InternalKey{}
	}
	return InternalKey{
		UserKey: data[:len(data)-9],
		SeqNo:   binary.BigEndian.Uint64(data[len(data)-9 : len(data)-1]),
		Type:    EntryType(data[len(data)-1]),
	}
}

// encodeBlockHandle encodes a BlockHandle into buf, returns bytes written
func encodeBlockHandle(buf []byte, h BlockHandle) int {
	var tmp [binary.MaxVarintLen64 * 2]byte
	n := binary.PutUvarint(tmp[:], h.Offset)
	n += binary.PutUvarint(tmp[n:], h.Size)
	copy(buf, tmp[:n])
	return n
}

// decodeBlockHandle decodes a BlockHandle from data, returns bytes consumed
func decodeBlockHandle(data []byte) (BlockHandle, int) {
	offset, n1 := binary.Uvarint(data)
	size, n2 := binary.Uvarint(data[n1:])
	return BlockHandle{Offset: offset, Size: size}, n1 + n2
}
