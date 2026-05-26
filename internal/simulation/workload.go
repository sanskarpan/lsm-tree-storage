// Package simulation provides workload generators and benchmark utilities
// for the LSM engine.
package simulation

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// WorkloadType identifies the kind of workload to run.
type WorkloadType string

const (
	// WorkloadSequentialWrite writes keys in sequential ascending order.
	WorkloadSequentialWrite WorkloadType = "sequential_write"
	// WorkloadRandomWrite writes keys in random order.
	WorkloadRandomWrite WorkloadType = "random_write"
	// WorkloadZipfRead pre-populates keys then reads with Zipf-distributed access.
	WorkloadZipfRead WorkloadType = "zipf_read"
	// WorkloadMixed interleaves reads and writes according to ReadWriteRatio.
	WorkloadMixed WorkloadType = "mixed"
	// WorkloadCompactionStress writes the same key range multiple times to trigger compaction.
	WorkloadCompactionStress WorkloadType = "compaction_stress"
	// WorkloadPointDelete writes keys then deletes all of them.
	WorkloadPointDelete WorkloadType = "point_delete"
)

// WorkloadConfig parameterises a workload run.
type WorkloadConfig struct {
	Type           WorkloadType
	NumKeys        int
	ValueSize      int     // bytes per value
	KeySize        int     // bytes per key (used for random keys)
	ReadWriteRatio float64 // fraction of ops that are reads (for mixed)
	ZipfSkew       float64 // Zipf exponent (1.0 = heavy skew)
	BatchSize      int     // keys per WriteBatch (0 = individual puts)
}

// LatencyHistogram tracks nanosecond-precision latency percentiles.
type LatencyHistogram struct {
	samples []int64
}

// Record appends a single latency sample to the histogram.
func (h *LatencyHistogram) Record(d time.Duration) {
	h.samples = append(h.samples, int64(d))
}

// Percentile returns the latency at the given percentile (0-100) over all recorded samples.
func (h *LatencyHistogram) Percentile(pct float64) time.Duration {
	if len(h.samples) == 0 {
		return 0
	}
	sorted := make([]int64, len(h.samples))
	copy(sorted, h.samples)
	// partial insertion sort (good enough for benchmarks)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	idx := int(math.Ceil(pct/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return time.Duration(sorted[idx])
}

// BenchmarkResult holds the outcome of a workload run.
type BenchmarkResult struct {
	TotalOps     uint64
	Duration     time.Duration
	OpsPerSec    float64
	WriteLatency LatencyHistogram
	ReadLatency  LatencyHistogram
}

// RunWorkload executes the given workload against eng and returns results.
func RunWorkload(eng Engine, cfg WorkloadConfig) BenchmarkResult {
	if cfg.NumKeys <= 0 {
		cfg.NumKeys = 10000
	}
	if cfg.ValueSize <= 0 {
		cfg.ValueSize = 100
	}
	if cfg.KeySize <= 0 {
		cfg.KeySize = 16
	}

	start := time.Now()
	res := BenchmarkResult{}

	value := make([]byte, cfg.ValueSize)
	for i := range value {
		value[i] = 'v'
	}

	switch cfg.Type {
	case WorkloadSequentialWrite:
		for i := 0; i < cfg.NumKeys; i++ {
			key := []byte(fmt.Sprintf("key-%0*d", cfg.KeySize, i))
			t0 := time.Now()
			_ = eng.Put(key, value)
			res.WriteLatency.Record(time.Since(t0))
			res.TotalOps++
		}

	case WorkloadRandomWrite:
		rng := rand.New(rand.NewSource(42))
		for i := 0; i < cfg.NumKeys; i++ {
			key := []byte(fmt.Sprintf("key-%0*d", cfg.KeySize, rng.Intn(cfg.NumKeys*10)))
			t0 := time.Now()
			_ = eng.Put(key, value)
			res.WriteLatency.Record(time.Since(t0))
			res.TotalOps++
		}

	case WorkloadZipfRead:
		skew := cfg.ZipfSkew
		if skew <= 0 {
			skew = 1.0
		}
		// Pre-populate keys
		for i := 0; i < cfg.NumKeys; i++ {
			_ = eng.Put([]byte(fmt.Sprintf("key-%0*d", cfg.KeySize, i)), value)
		}
		rng := rand.New(rand.NewSource(42))
		zipf := rand.NewZipf(rng, skew+1, 1, uint64(cfg.NumKeys-1))
		for i := 0; i < cfg.NumKeys; i++ {
			key := []byte(fmt.Sprintf("key-%0*d", cfg.KeySize, zipf.Uint64()))
			t0 := time.Now()
			_, _ = eng.Get(key)
			res.ReadLatency.Record(time.Since(t0))
			res.TotalOps++
		}

	case WorkloadMixed:
		rr := cfg.ReadWriteRatio
		if rr <= 0 {
			rr = 0.5
		}
		// Pre-populate half the keys
		for i := 0; i < cfg.NumKeys/2; i++ {
			_ = eng.Put([]byte(fmt.Sprintf("key-%0*d", cfg.KeySize, i)), value)
		}
		rng := rand.New(rand.NewSource(42))
		for i := 0; i < cfg.NumKeys; i++ {
			key := []byte(fmt.Sprintf("key-%0*d", cfg.KeySize, rng.Intn(cfg.NumKeys)))
			if rng.Float64() < rr {
				t0 := time.Now()
				_, _ = eng.Get(key)
				res.ReadLatency.Record(time.Since(t0))
			} else {
				t0 := time.Now()
				_ = eng.Put(key, value)
				res.WriteLatency.Record(time.Since(t0))
			}
			res.TotalOps++
		}

	case WorkloadCompactionStress:
		// Write keys in multiple rounds to force compaction
		for round := 0; round < 5; round++ {
			for i := 0; i < cfg.NumKeys; i++ {
				key := []byte(fmt.Sprintf("key-%0*d", cfg.KeySize, i))
				val := []byte(fmt.Sprintf("round-%d-val", round))
				t0 := time.Now()
				_ = eng.Put(key, val)
				res.WriteLatency.Record(time.Since(t0))
				res.TotalOps++
			}
		}

	case WorkloadPointDelete:
		// Write then delete all keys
		for i := 0; i < cfg.NumKeys; i++ {
			_ = eng.Put([]byte(fmt.Sprintf("key-%0*d", cfg.KeySize, i)), value)
		}
		for i := 0; i < cfg.NumKeys; i++ {
			key := []byte(fmt.Sprintf("key-%0*d", cfg.KeySize, i))
			t0 := time.Now()
			_ = eng.Delete(key)
			res.WriteLatency.Record(time.Since(t0))
			res.TotalOps++
		}
	}

	res.Duration = time.Since(start)
	if res.Duration > 0 {
		res.OpsPerSec = float64(res.TotalOps) / res.Duration.Seconds()
	}
	return res
}
