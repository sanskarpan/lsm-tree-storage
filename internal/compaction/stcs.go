// Package compaction — see leveled.go for the package doc.
package compaction

import (
	"sort"

	"lsm-engine/internal/sstable"
)

// STCSConfig holds parameters for Size-Tiered Compaction Strategy.
type STCSConfig struct {
	MinThreshold int     // min files per bucket to trigger (default 4)
	MaxThreshold int     // max files to merge at once (default 32)
	BucketLow    float64 // lower size ratio for a bucket (default 0.5)
	BucketHigh   float64 // upper size ratio for a bucket (default 1.5)
}

// DefaultSTCSConfig returns a STCSConfig with standard defaults.
func DefaultSTCSConfig() STCSConfig {
	return STCSConfig{
		MinThreshold: 4,
		MaxThreshold: 32,
		BucketLow:    0.5,
		BucketHigh:   1.5,
	}
}

// STCSPickInputs groups all provided sstables into size buckets and returns
// the files from the first bucket that has >= MinThreshold members.
// Returns nil if no compaction is needed.
func STCSPickInputs(sstables []*sstable.SSTableMeta, cfg STCSConfig) []*sstable.SSTableMeta {
	if len(sstables) < cfg.MinThreshold {
		return nil
	}
	buckets := stcsGroupBuckets(sstables, cfg)
	for _, bucket := range buckets {
		if len(bucket) >= cfg.MinThreshold {
			// Cap at MaxThreshold
			if cfg.MaxThreshold > 0 && len(bucket) > cfg.MaxThreshold {
				bucket = bucket[:cfg.MaxThreshold]
			}
			return bucket
		}
	}
	return nil
}

// stcsGroupBuckets groups sstables into buckets where each bucket contains
// files whose size is within [avgSize*BucketLow, avgSize*BucketHigh].
func stcsGroupBuckets(sstables []*sstable.SSTableMeta, cfg STCSConfig) [][]*sstable.SSTableMeta {
	if len(sstables) == 0 {
		return nil
	}

	// Sort by file size
	sorted := make([]*sstable.SSTableMeta, len(sstables))
	copy(sorted, sstables)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].FileSize < sorted[j].FileSize
	})

	var buckets [][]*sstable.SSTableMeta
	var current []*sstable.SSTableMeta

	for _, m := range sorted {
		if len(current) == 0 {
			current = append(current, m)
			continue
		}
		// Compute average size of current bucket
		var total uint64
		for _, c := range current {
			total += c.FileSize
		}
		avg := float64(total) / float64(len(current))

		lo := avg * cfg.BucketLow
		hi := avg * cfg.BucketHigh
		sz := float64(m.FileSize)

		if sz >= lo && sz <= hi {
			current = append(current, m)
		} else {
			buckets = append(buckets, current)
			current = []*sstable.SSTableMeta{m}
		}
	}
	if len(current) > 0 {
		buckets = append(buckets, current)
	}
	return buckets
}
