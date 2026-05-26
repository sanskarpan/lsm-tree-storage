// Package wal implements the write-ahead log using the LevelDB 32KB block format.
// Each write is broken into fragments that fit within 32KB blocks and prefixed
// with a CRC-protected header.
package wal

import (
	"bufio"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"sync"
	"sync/atomic"

	"lsm-engine/internal/events"
)

const (
	// BlockSize is the fixed block size used for WAL records (32KB, matching the LevelDB format).
	BlockSize = 32 * 1024 // 32KB
	// HeaderSize is the number of bytes in each record header: CRC(4) + Length(2) + Type(1).
	HeaderSize = 7 // CRC(4) + Length(2) + Type(1)
)

// ErrCorrupt is returned when a WAL record is corrupt
var ErrCorrupt = errors.New("wal: corrupt record")

// WAL is a write-ahead log using the LevelDB 32KB block format
type WAL struct {
	mu       sync.Mutex
	file     *os.File
	buf      *bufio.Writer
	blockPos int // current position within current 32KB block
	seqNo    atomic.Uint64
	bus      events.EventPublisher
}

// OpenWAL opens (or creates) a WAL file at path
func OpenWAL(path string, bus events.EventPublisher) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	// Determine current offset to compute blockPos
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	size := info.Size()
	blockPos := int(size % BlockSize)
	return &WAL{
		file:     f,
		buf:      bufio.NewWriterSize(f, BlockSize),
		blockPos: blockPos,
		bus:      bus,
	}, nil
}

// Append writes a WAL entry (assigns seqNo)
func (w *WAL) Append(entry WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	entry.SeqNo = w.seqNo.Add(1)
	payload := EncodeEntry(entry)
	if err := w.writeRecord(payload); err != nil {
		return err
	}
	w.publishAppend(entry)
	return nil
}

// AppendWithSeqNo writes a WAL entry with an explicit seqNo (for recovery replay)
func (w *WAL) AppendWithSeqNo(entry WALEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	payload := EncodeEntry(entry)
	if err := w.writeRecord(payload); err != nil {
		return err
	}
	// Keep seqNo monotonic
	for {
		cur := w.seqNo.Load()
		if entry.SeqNo <= cur {
			break
		}
		if w.seqNo.CompareAndSwap(cur, entry.SeqNo) {
			break
		}
	}
	w.publishAppend(entry)
	return nil
}

// AppendBatch atomically appends all entries as one WAL record.
func (w *WAL) AppendBatch(entries []WALEntry) error {
	if len(entries) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	payload := EncodeBatch(entries)
	if err := w.writeRecord(payload); err != nil {
		return err
	}

	for _, entry := range entries {
		for {
			cur := w.seqNo.Load()
			if entry.SeqNo <= cur {
				break
			}
			if w.seqNo.CompareAndSwap(cur, entry.SeqNo) {
				break
			}
		}
		w.publishAppend(entry)
	}

	return nil
}

func (w *WAL) publishAppend(entry WALEntry) {
	if w.bus == nil {
		return
	}
	w.bus.Publish(events.Event{Type: events.EvtWALAppend, Extra: map[string]interface{}{
		"type":    entry.Type,
		"key":     string(entry.Key),
		"key_len": len(entry.Key),
		"value":   string(entry.Value),
		"val_len": len(entry.Value),
		"seq":     entry.SeqNo,
		"seq_no":  entry.SeqNo,
	}})
}

func (w *WAL) writeRecord(data []byte) error {
	remaining := len(data)
	isFirst := true
	for remaining > 0 {
		avail := BlockSize - w.blockPos - HeaderSize
		if avail < 0 {
			// Pad remaining bytes in block with zeros
			zeros := make([]byte, BlockSize-w.blockPos)
			_, err := w.buf.Write(zeros)
			if err != nil {
				return err
			}
			w.blockPos = 0
			avail = BlockSize - HeaderSize
		}
		if avail == 0 {
			// Exactly HeaderSize bytes remain — not enough for any payload.
			// Pad them to zero and start fresh in the next block.
			zeros := make([]byte, HeaderSize)
			if _, err := w.buf.Write(zeros); err != nil {
				return err
			}
			w.blockPos = 0
			avail = BlockSize - HeaderSize
		}
		chunkLen := remaining
		if chunkLen > avail {
			chunkLen = avail
		}
		offset := len(data) - remaining
		chunk := data[offset : offset+chunkLen]

		var rType uint8
		isFinal := chunkLen == remaining
		switch {
		case isFirst && isFinal:
			rType = RecordFull
		case isFirst:
			rType = RecordFirst
		case isFinal:
			rType = RecordLast
		default:
			rType = RecordMiddle
		}

		crc := crc32.ChecksumIEEE(chunk)
		header := [HeaderSize]byte{}
		binary.LittleEndian.PutUint32(header[0:], crc)
		binary.LittleEndian.PutUint16(header[4:], uint16(chunkLen))
		header[6] = rType
		if _, err := w.buf.Write(header[:]); err != nil {
			return err
		}
		if _, err := w.buf.Write(chunk); err != nil {
			return err
		}
		w.blockPos += HeaderSize + chunkLen
		if w.blockPos >= BlockSize {
			w.blockPos = 0
		}
		remaining -= chunkLen
		isFirst = false
	}
	return nil
}

// Sync flushes the buffer to OS and calls fsync
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.buf.Flush(); err != nil {
		return err
	}
	if err := w.file.Sync(); err != nil {
		return err
	}
	w.bus.Publish(events.Event{Type: events.EvtWALSync})
	return nil
}

// Close flushes and closes the WAL file
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.buf.Flush(); err != nil {
		return err
	}
	return w.file.Close()
}

// SeqNo returns the current sequence number
func (w *WAL) SeqNo() uint64 {
	return w.seqNo.Load()
}
