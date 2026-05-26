// Package compaction implements SSTable compaction strategies for the LSM engine.
// The engine calls PickInputs to select which SSTables to compact;
// the actual merge-and-write is handled by the engine's executeCompaction.
package compaction

import (
	"bytes"

	"lsm-engine/internal/sstable"
)

// MaxLevels is the default maximum number of LSM levels for compaction planning.
const MaxLevels = 7

// LeveledPickInputs returns the set of SSTable metas to compact for a
// leveled compaction starting at inputLevel.
//
// For L0: all files are selected (they may have overlapping ranges).
// For L1+: the first file in the level is selected, then all overlapping
// files from outputLevel are added.
//
// Returns nil if there is nothing to compact.
func LeveledPickInputs(
	levels [][]*sstable.SSTableMeta,
	inputLevel int,
	maxLevels int,
) []*sstable.SSTableMeta {
	outputLevel := inputLevel + 1
	if outputLevel >= maxLevels {
		return nil
	}

	var inputs []*sstable.SSTableMeta

	if inputLevel == 0 {
		// Take all L0 files
		inputs = make([]*sstable.SSTableMeta, len(levels[0]))
		copy(inputs, levels[0])
	} else {
		if len(levels[inputLevel]) == 0 {
			return nil
		}
		inputs = []*sstable.SSTableMeta{levels[inputLevel][0]}
	}

	if len(inputs) == 0 {
		return nil
	}

	// Compute key range of input files
	var minKey, maxKey []byte
	for _, m := range inputs {
		if minKey == nil || bytes.Compare(m.FirstKey, minKey) < 0 {
			minKey = m.FirstKey
		}
		if maxKey == nil || bytes.Compare(m.LastKey, maxKey) > 0 {
			maxKey = m.LastKey
		}
	}

	// Add overlapping files from output level
	for _, m := range levels[outputLevel] {
		if overlaps(m.FirstKey, m.LastKey, minKey, maxKey) {
			inputs = append(inputs, m)
		}
	}

	// Deduplicate
	seen := make(map[uint64]bool)
	unique := inputs[:0]
	for _, m := range inputs {
		if !seen[m.FileID] {
			seen[m.FileID] = true
			unique = append(unique, m)
		}
	}
	return unique
}

func overlaps(a1, a2, b1, b2 []byte) bool {
	return bytes.Compare(a1, b2) <= 0 && bytes.Compare(b1, a2) <= 0
}
