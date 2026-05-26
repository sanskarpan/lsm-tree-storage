// Package simulation — see workload.go for the package doc.
package simulation

import (
	"sync/atomic"
	"time"

	"lsm-engine/internal/events"
)

// AmplificationStats tracks write, read, and space amplification live.
type AmplificationStats struct {
	// Bytes written by the client (Put payloads)
	ClientBytesWritten uint64
	// Bytes written to disk (WAL + SSTable flushes + compaction outputs)
	DiskBytesWritten uint64
	// Bytes written during compaction specifically
	CompactionBytesWritten uint64
	// Number of queries issued
	TotalQueries uint64
	// Total disk reads (block loads) across all queries
	TotalDiskReads uint64

	// Derived metrics (computed lazily)
	WriteAmplification float64
	ReadAmplification  float64
	SpaceAmplification float64
}

// WriteAmplification returns bytes-on-disk / bytes-from-client.
func (s *AmplificationStats) WA() float64 {
	c := atomic.LoadUint64(&s.ClientBytesWritten)
	if c == 0 {
		return 1.0
	}
	return float64(atomic.LoadUint64(&s.DiskBytesWritten)) / float64(c)
}

// ReadAmplification returns average disk reads per query.
func (s *AmplificationStats) RA() float64 {
	q := atomic.LoadUint64(&s.TotalQueries)
	if q == 0 {
		return 0
	}
	return float64(atomic.LoadUint64(&s.TotalDiskReads)) / float64(q)
}

// SpaceAmplification returns total_disk_size / actual_live_data_size.
// Approximated as total SST bytes / client bytes written (last round).
func (s *AmplificationStats) SA(eng Engine) float64 {
	stats := eng.Stats()
	totalBytes, _ := stats["total_sst_bytes"].(int)
	c := atomic.LoadUint64(&s.ClientBytesWritten)
	if c == 0 {
		return 1.0
	}
	return float64(totalBytes) / float64(c)
}

// AmplificationTracker wires into the engine event bus and tracks metrics.
type AmplificationTracker struct {
	Stats  AmplificationStats
	eng    Engine
	stopCh chan struct{}
}

// NewAmplificationTracker creates and starts tracking.
func NewAmplificationTracker(eng Engine) *AmplificationTracker {
	t := &AmplificationTracker{
		eng:    eng,
		stopCh: make(chan struct{}),
	}
	bus := eng.EventBus()
	if bus != nil {
		bus.Subscribe(events.EvtWALAppend, func(evt events.Event) {
			if extra, ok := evt.Extra["key_len"].(int); ok {
				atomic.AddUint64(&t.Stats.ClientBytesWritten, uint64(extra))
			}
			if extra, ok := evt.Extra["val_len"].(int); ok {
				atomic.AddUint64(&t.Stats.ClientBytesWritten, uint64(extra))
			}
		})
		bus.Subscribe(events.EvtFlushComplete, func(evt events.Event) {
			if sz, ok := evt.Extra["size"].(uint64); ok {
				atomic.AddUint64(&t.Stats.DiskBytesWritten, sz)
			}
		})
		bus.Subscribe(events.EvtCompactionComplete, func(evt events.Event) {
			// We don't have exact output bytes here, but we use stats instead
		})
		bus.Subscribe(events.EvtReadStart, func(_ events.Event) {
			atomic.AddUint64(&t.Stats.TotalQueries, 1)
		})
		bus.Subscribe(events.EvtBlockRead, func(_ events.Event) {
			atomic.AddUint64(&t.Stats.TotalDiskReads, 1)
		})
	}

	// Periodic emit of amplification event
	go t.emitLoop()
	return t
}

func (t *AmplificationTracker) emitLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			bus := t.eng.EventBus()
			if bus == nil {
				continue
			}
			bus.Publish(events.Event{
				Type: events.EvtAmplification,
				Extra: map[string]interface{}{
					"wa": t.Stats.WA(),
					"ra": t.Stats.RA(),
					"sa": t.Stats.SA(t.eng),
				},
			})
		}
	}
}

// Stop halts the periodic emit loop.
func (t *AmplificationTracker) Stop() {
	close(t.stopCh)
}
