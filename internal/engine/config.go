// Package engine implements the core LSM-Tree storage engine, including
// the write path (WAL → MemTable), read path (MemTable → SSTable levels),
// background flush, and pluggable compaction strategies.
package engine

import "time"

// Config holds LSM engine configuration.
type Config struct {
	DataDir                        string
	MemTableSize                   int64
	BlockSize                      int
	BloomBitsPerKey                int
	SSTMaxSize                     uint64
	SyncWAL                        bool
	MaxOpenFiles                   int
	BlockCacheSize                 int64
	MaxLevels                      int
	LevelSizeMultiplier            int
	Level0FileNumCompactionTrigger int
	Level0StopWritesTrigger        int
	MaxImmutableMemTables          int
	CompactionStyle                string // "leveled" | "size-tiered" | "time-window"
	TimeWindowSize                 time.Duration
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig(dataDir string) Config {
	return Config{
		DataDir:                        dataDir,
		MemTableSize:                   64 * 1024 * 1024, // 64MB
		BlockSize:                      4096,             // 4KB
		BloomBitsPerKey:                10,
		SSTMaxSize:                     64 * 1024 * 1024, // 64MB
		SyncWAL:                        true,
		MaxOpenFiles:                   1000,
		BlockCacheSize:                 128 * 1024 * 1024, // 128MB
		MaxLevels:                      7,
		LevelSizeMultiplier:            10,
		Level0FileNumCompactionTrigger: 4,
		Level0StopWritesTrigger:        12,
		MaxImmutableMemTables:          2,
		CompactionStyle:                "leveled",
		TimeWindowSize:                 time.Hour,
	}
}
