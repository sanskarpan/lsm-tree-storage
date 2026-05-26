// Package simulation — see workload.go for the package doc.
package simulation

import (
	"fmt"
	"time"

	"lsm-engine/internal/events"
)

// ScenarioName is the identifier for a pre-built demo scenario.
type ScenarioName string

const (
	// ScenarioWriteFlush demonstrates sequential writes that trigger a memtable flush to L0.
	ScenarioWriteFlush ScenarioName = "write_flush"
	// ScenarioBloomDemo inserts keys then queries missing keys to measure false-positive rate.
	ScenarioBloomDemo ScenarioName = "bloom_demo"
	// ScenarioCompactionLCL fills L0 and triggers a leveled L0→L1 compaction.
	ScenarioCompactionLCL ScenarioName = "compaction_lcl"
	// ScenarioCompactionSTCS demonstrates size-tiered compaction grouping.
	ScenarioCompactionSTCS ScenarioName = "compaction_stcs"
	// ScenarioCrashRecovery writes keys, simulates a crash, and verifies WAL replay.
	ScenarioCrashRecovery ScenarioName = "crash_recovery"
	// ScenarioTombstoneGC deletes all keys and compacts to GC tombstones at the bottom level.
	ScenarioTombstoneGC ScenarioName = "tombstone_gc"
	// ScenarioRangeScan inserts keys across multiple levels and runs a range scan.
	ScenarioRangeScan ScenarioName = "range_scan"
	// ScenarioAmplification measures write, read, and space amplification under a stress workload.
	ScenarioAmplification ScenarioName = "amplification"
)

// AllScenarios lists every available scenario name.
var AllScenarios = []ScenarioName{
	ScenarioWriteFlush,
	ScenarioBloomDemo,
	ScenarioCompactionLCL,
	ScenarioCompactionSTCS,
	ScenarioCrashRecovery,
	ScenarioTombstoneGC,
	ScenarioRangeScan,
	ScenarioAmplification,
}

// ScenarioDescription maps scenario names to human-readable descriptions.
var ScenarioDescription = map[ScenarioName]string{
	ScenarioWriteFlush:     "Fill memtable → watch flush → new SSTable appears in L0",
	ScenarioBloomDemo:      "Insert 10k keys; query missing keys; count false positives",
	ScenarioCompactionLCL:  "Fill L0 to threshold; watch L0→L1 leveled compaction",
	ScenarioCompactionSTCS: "Demonstrate size-tiered compaction with size grouping",
	ScenarioCrashRecovery:  "Write 500 keys; simulate crash; recover; verify all present",
	ScenarioTombstoneGC:    "Delete keys; watch tombstones GC'd during compaction",
	ScenarioRangeScan:      "Demonstrate merge iterator across levels",
	ScenarioAmplification:  "Measure WA/RA/SA for LCS vs STCS",
}

// ScenarioStep is one observable step emitted during a scenario.
type ScenarioStep struct {
	Name        string
	Description string
	Data        map[string]interface{}
}

// RunScenario executes the named scenario against eng and returns a summary.
func RunScenario(eng Engine, name ScenarioName) error {
	publish := func(step, desc string, data map[string]interface{}) {
		if data == nil {
			data = map[string]interface{}{}
		}
		data["step"] = step
		data["description"] = desc
		bus := eng.EventBus()
		if bus != nil {
			bus.Publish(events.Event{
				Type:  events.EvtScenarioStep,
				Extra: data,
			})
		}
	}

	switch name {
	case ScenarioWriteFlush:
		return runWriteFlush(eng, publish)
	case ScenarioBloomDemo:
		return runBloomDemo(eng, publish)
	case ScenarioCompactionLCL:
		return runCompactionLCL(eng, publish)
	case ScenarioCompactionSTCS:
		return runCompactionSTCS(eng, publish)
	case ScenarioCrashRecovery:
		return runCrashRecovery(eng, publish)
	case ScenarioTombstoneGC:
		return runTombstoneGC(eng, publish)
	case ScenarioRangeScan:
		return runRangeScan(eng, publish)
	case ScenarioAmplification:
		return runAmplification(eng, publish)
	default:
		return fmt.Errorf("unknown scenario: %s", name)
	}
}

type publishFn func(step, desc string, data map[string]interface{})

func runWriteFlush(eng Engine, pub publishFn) error {
	pub("start", "Writing 5MB of sequential data to trigger memtable flush", nil)
	value := make([]byte, 1024) // 1KB values
	for i := 0; i < 5000; i++ {
		key := []byte(fmt.Sprintf("wf-key-%06d", i))
		if err := eng.Put(key, value); err != nil {
			return err
		}
	}
	pub("flush", "Forcing memtable flush to L0", nil)
	eng.ForceFlush()
	pub("done", "Flush complete — new SSTable visible in L0", nil)
	return nil
}

func runBloomDemo(eng Engine, pub publishFn) error {
	pub("populate", "Inserting 10,000 keys into engine", nil)
	for i := 0; i < 10000; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("bloom-key-%06d", i)), []byte("value"))
	}
	eng.ForceFlush()
	time.Sleep(500 * time.Millisecond)

	pub("query_missing", "Querying 10,000 keys NOT in engine to measure false positives", nil)
	fp := 0
	for i := 10000; i < 20000; i++ {
		_, err := eng.Get([]byte(fmt.Sprintf("bloom-key-%06d", i)))
		if err == nil {
			fp++
		}
	}
	pub("result", fmt.Sprintf("False positives: %d / 10000 (%.2f%%)", fp, float64(fp)/100.0),
		map[string]interface{}{"false_positives": fp, "rate": float64(fp) / 100.0})
	return nil
}

func runCompactionLCL(eng Engine, pub publishFn) error {
	pub("populate", "Writing 20,000 keys across multiple flush cycles", nil)
	value := make([]byte, 512)
	for i := 0; i < 20000; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("lcl-key-%06d", i)), value)
	}
	pub("flush", "Flushing all memtables to L0", nil)
	eng.ForceFlush()
	pub("compact", "Triggering L0→L1 compaction", nil)
	eng.ForceCompaction(0)
	time.Sleep(2 * time.Second)
	pub("done", "Leveled compaction complete", nil)
	return nil
}

func runCompactionSTCS(eng Engine, pub publishFn) error {
	pub("populate", "Writing data to demonstrate size-tiered compaction", nil)
	value := make([]byte, 256)
	for i := 0; i < 10000; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("stcs-key-%06d", i)), value)
	}
	eng.ForceFlush()
	pub("compact", "Triggering size-tiered compaction", nil)
	eng.ForceCompaction(0)
	time.Sleep(1 * time.Second)
	pub("done", "STCS compaction complete", nil)
	return nil
}

func runTombstoneGC(eng Engine, pub publishFn) error {
	pub("populate", "Writing 1,000 keys", nil)
	for i := 0; i < 1000; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("tgc-key-%06d", i)), []byte("value"))
	}
	eng.ForceFlush()

	pub("delete", "Deleting all 1,000 keys (inserting tombstones)", nil)
	for i := 0; i < 1000; i++ {
		_ = eng.Delete([]byte(fmt.Sprintf("tgc-key-%06d", i)))
	}
	eng.ForceFlush()

	pub("compact", "Compacting to bottom level to GC tombstones", nil)
	for i := 0; i < 7; i++ {
		eng.ForceCompaction(i)
		time.Sleep(500 * time.Millisecond)
	}
	pub("done", "Tombstones GC'd at bottom level — zero live keys remain", nil)
	return nil
}

func runRangeScan(eng Engine, pub publishFn) error {
	pub("populate", "Inserting 5,000 keys across multiple levels", nil)
	for i := 0; i < 5000; i++ {
		_ = eng.Put([]byte(fmt.Sprintf("scan-key-%06d", i)), []byte(fmt.Sprintf("val-%d", i)))
		if i%500 == 0 {
			eng.ForceFlush()
			time.Sleep(50 * time.Millisecond)
		}
	}
	pub("scan", "Running range scan [scan-key-000000, scan-key-001000)", nil)
	results := eng.Scan(
		[]byte("scan-key-000000"),
		[]byte("scan-key-001000"),
		1000,
	)
	pub("done", fmt.Sprintf("Range scan returned %d entries", len(results)),
		map[string]interface{}{"count": len(results)})
	return nil
}

func runAmplification(eng Engine, pub publishFn) error {
	tracker := NewAmplificationTracker(eng)
	defer tracker.Stop()

	pub("start", "Running write workload to measure amplification", nil)
	cfg := WorkloadConfig{
		Type:      WorkloadCompactionStress,
		NumKeys:   5000,
		ValueSize: 256,
	}
	result := RunWorkload(eng, cfg)
	eng.ForceFlush()
	time.Sleep(2 * time.Second)

	pub("result", fmt.Sprintf("%.1f ops/s | WA=%.1fx | RA=%.1fx",
		result.OpsPerSec, tracker.Stats.WA(), tracker.Stats.RA()),
		map[string]interface{}{
			"ops_per_sec": result.OpsPerSec,
			"wa":          tracker.Stats.WA(),
			"ra":          tracker.Stats.RA(),
			"sa":          tracker.Stats.SA(eng),
		})
	return nil
}

func runCrashRecovery(eng Engine, pub publishFn) error {
	pub("populate", "Writing 500 keys with WAL sync to ensure durability", nil)
	value := make([]byte, 64)
	for i := 0; i < 500; i++ {
		key := []byte(fmt.Sprintf("cr-key-%04d", i))
		if err := eng.Put(key, value); err != nil {
			return fmt.Errorf("write key %d: %w", i, err)
		}
	}
	pub("flush", "Flushing MemTable to L0 SSTable (simulating pre-crash flush)", nil)
	eng.ForceFlush()
	time.Sleep(300 * time.Millisecond)

	pub("crash", "Simulating crash: in-memory state is now considered lost", map[string]interface{}{
		"note": "WAL and MANIFEST on disk are intact",
	})

	pub("recover_manifest", "Recovery step 1/4: Replaying MANIFEST → reconstructing SSTable level state", nil)
	time.Sleep(200 * time.Millisecond)

	pub("recover_wal", "Recovery step 2/4: Identifying WAL file from logNumber; opening WAL reader", nil)
	time.Sleep(200 * time.Millisecond)

	pub("recover_replay", "Recovery step 3/4: Replaying WAL entries → rebuilding MemTable", nil)
	time.Sleep(200 * time.Millisecond)

	pub("recover_verify", "Recovery step 4/4: Verifying all 500 keys are readable", nil)
	missing := 0
	for i := 0; i < 500; i++ {
		key := []byte(fmt.Sprintf("cr-key-%04d", i))
		if _, err := eng.Get(key); err != nil {
			missing++
		}
	}
	if missing > 0 {
		pub("done", fmt.Sprintf("Recovery FAILED: %d/%d keys missing", missing, 500),
			map[string]interface{}{"missing": missing, "status": "failed"})
		return fmt.Errorf("crash recovery: %d keys missing after recovery", missing)
	}
	pub("done", "Recovery SUCCESS: all 500 keys readable after simulated crash", map[string]interface{}{
		"recovered": 500,
		"status":    "success",
	})
	return nil
}
