// Package wal — see wal.go for the package doc.
package wal

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
)

// RecoverWAL reads all valid WAL entries from the file at path.
// It stops at the first corrupt or truncated record.
func RecoverWAL(path string) ([]WALEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var entries []WALEntry
	buf := make([]byte, BlockSize)
	var pendingFragments []byte

	for {
		n, err := io.ReadFull(f, buf)
		if n == 0 || errors.Is(err, io.EOF) {
			break
		}
		// io.ErrUnexpectedEOF means partial block (last block)
		block := buf[:n]
		pos := 0

		for pos+HeaderSize <= len(block) {
			crcStored := binary.LittleEndian.Uint32(block[pos:])
			length := int(binary.LittleEndian.Uint16(block[pos+4:]))
			rType := block[pos+6]

			if length == 0 && rType == 0 {
				break // padding
			}

			end := pos + HeaderSize + length
			if end > len(block) {
				// Truncated record — stop
				goto done
			}

			payload := block[pos+HeaderSize : end]

			// Verify CRC
			crcComputed := crc32.ChecksumIEEE(payload)
			if crcComputed != crcStored {
				// Corrupt record — stop here
				goto done
			}

			switch rType {
			case RecordFull:
				decoded, err := DecodeEntries(payload)
				if err == nil {
					entries = append(entries, decoded...)
				}
				pendingFragments = nil
			case RecordFirst:
				pendingFragments = append([]byte{}, payload...)
			case RecordMiddle:
				pendingFragments = append(pendingFragments, payload...)
			case RecordLast:
				pendingFragments = append(pendingFragments, payload...)
				decoded, err := DecodeEntries(pendingFragments)
				if err == nil {
					entries = append(entries, decoded...)
				}
				pendingFragments = nil
			}
			pos = end
		}

		if errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
	}
done:
	return entries, nil
}
