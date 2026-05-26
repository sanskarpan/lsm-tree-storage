// Package compaction — see leveled.go for the package doc.
package compaction

import (
	"sort"
	"time"

	"lsm-engine/internal/sstable"
)

// TWCSConfig holds parameters for Time-Window Compaction Strategy.
type TWCSConfig struct {
	WindowSize   time.Duration // e.g., 24h; SSTables created in the same window are compacted together
	MinThreshold int           // min files per window to trigger STCS within window (default 4)
	MaxThreshold int           // max files to merge at once (default 32)
}

// DefaultTWCSConfig returns a TWCSConfig with standard defaults.
func DefaultTWCSConfig() TWCSConfig {
	return TWCSConfig{
		WindowSize:   24 * time.Hour,
		MinThreshold: 4,
		MaxThreshold: 32,
	}
}

// TWCSPickInputs groups sstables by their creation time window and returns
// inputs for the oldest closed window that has >= MinThreshold files.
// The "current" window (the one containing now) is not compacted yet.
// Returns nil if no compaction is needed.
func TWCSPickInputs(sstables []*sstable.SSTableMeta, cfg TWCSConfig, now time.Time) []*sstable.SSTableMeta {
	if len(sstables) < cfg.MinThreshold {
		return nil
	}

	windows := twcsGroupWindows(sstables, cfg.WindowSize)

	// currentWindowStart is the start of the window containing 'now'
	currentWindowStart := twcsWindowStart(now, cfg.WindowSize)

	// Try to find a closed window (not the current one) with enough files
	// Sort window starts so we try oldest first
	windowStarts := make([]int64, 0, len(windows))
	for ts := range windows {
		windowStarts = append(windowStarts, ts)
	}
	sort.Slice(windowStarts, func(i, j int) bool { return windowStarts[i] < windowStarts[j] })

	for _, ts := range windowStarts {
		if ts == currentWindowStart.UnixNano() {
			continue // skip the active window
		}
		files := windows[ts]
		if len(files) < cfg.MinThreshold {
			continue
		}
		// Apply STCS within this window
		stcsCfg := STCSConfig{
			MinThreshold: cfg.MinThreshold,
			MaxThreshold: cfg.MaxThreshold,
			BucketLow:    0.5,
			BucketHigh:   1.5,
		}
		inputs := STCSPickInputs(files, stcsCfg)
		if inputs != nil {
			return inputs
		}
		// If STCS doesn't pick (all in one big bucket scenario), merge all
		if len(files) >= cfg.MinThreshold {
			out := make([]*sstable.SSTableMeta, len(files))
			copy(out, files)
			if cfg.MaxThreshold > 0 && len(out) > cfg.MaxThreshold {
				out = out[:cfg.MaxThreshold]
			}
			return out
		}
	}

	return nil
}

// twcsGroupWindows groups sstables by the time window their CreatedAt falls into.
// Returns map of windowStart (Unix nanoseconds) → files.
func twcsGroupWindows(sstables []*sstable.SSTableMeta, windowSize time.Duration) map[int64][]*sstable.SSTableMeta {
	m := make(map[int64][]*sstable.SSTableMeta)
	for _, s := range sstables {
		created := time.Unix(0, s.CreatedAt)
		ws := twcsWindowStart(created, windowSize)
		key := ws.UnixNano()
		m[key] = append(m[key], s)
	}
	return m
}

// twcsWindowStart returns the start of the time window containing t.
func twcsWindowStart(t time.Time, windowSize time.Duration) time.Time {
	nanos := t.UnixNano()
	windowNanos := int64(windowSize)
	if windowNanos <= 0 {
		windowNanos = int64(24 * time.Hour)
	}
	return time.Unix(0, (nanos/windowNanos)*windowNanos)
}
