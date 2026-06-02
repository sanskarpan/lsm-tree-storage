package simulation

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubEngine is a minimal in-memory Engine for unit testing scenario routing,
// workload helpers, and amplification math without spinning up a real LSM
// engine. It records Put/Delete/Get counts and stores values by key.
type stubEngine struct {
	mu        atomic.Int64
	bus       *events.EventBus
	store     map[string][]byte
	deleted   map[string]struct{}
	putCount  atomic.Uint64
	delCount  atomic.Uint64
	getCount  atomic.Uint64
	missCount atomic.Uint64
}

func newStubEngine() *stubEngine {
	return &stubEngine{
		bus:     events.NewEventBus(),
		store:   make(map[string][]byte),
		deleted: make(map[string]struct{}),
	}
}

func (e *stubEngine) Put(key, value []byte) error {
	e.putCount.Add(1)
	e.store[string(key)] = append([]byte(nil), value...)
	delete(e.deleted, string(key))
	return nil
}

func (e *stubEngine) Delete(key []byte) error {
	e.delCount.Add(1)
	e.deleted[string(key)] = struct{}{}
	delete(e.store, string(key))
	return nil
}

func (e *stubEngine) Get(key []byte) ([]byte, error) {
	e.getCount.Add(1)
	if _, ok := e.deleted[string(key)]; ok {
		e.missCount.Add(1)
		return nil, fmt.Errorf("not found")
	}
	v, ok := e.store[string(key)]
	if !ok {
		e.missCount.Add(1)
		return nil, fmt.Errorf("not found")
	}
	return append([]byte(nil), v...), nil
}

func (e *stubEngine) Scan(_, _ []byte, _ int) [][2]string { return nil }
func (e *stubEngine) ForceFlush()                        {}
func (e *stubEngine) ForceCompaction(_ int)              {}
func (e *stubEngine) Stats() map[string]interface{} {
	return map[string]interface{}{}
}
func (e *stubEngine) EventBus() *events.EventBus { return e.bus }

func TestAllScenarios_AllHaveDescriptions(t *testing.T) {
	for _, name := range AllScenarios {
		_, ok := ScenarioDescription[name]
		assert.Truef(t, ok, "scenario %q has no description entry", name)
	}
}

func TestRunScenario_UnknownNameReturnsError(t *testing.T) {
	eng := newStubEngine()
	err := RunScenario(eng, "no_such_scenario")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown scenario")
}

func TestRunScenario_WriteFlushStubDoesNotPanic(t *testing.T) {
	eng := newStubEngine()
	err := RunScenario(eng, ScenarioWriteFlush)
	require.NoError(t, err)
	// Stub Put is a no-op store; scenario just exercises the routing path.
	assert.Greater(t, eng.putCount.Load(), uint64(0))
}

func TestRunScenario_PublishesStepsOnBus(t *testing.T) {
	eng := newStubEngine()
	var steps atomic.Uint64
	eng.bus.Subscribe(events.EvtScenarioStep, func(ev events.Event) {
		steps.Add(1)
	})

	err := RunScenario(eng, ScenarioWriteFlush)
	require.NoError(t, err)
	assert.Greater(t, steps.Load(), uint64(0), "scenario must publish at least one step")
}

func TestLatencyHistogram_PercentilesMonotonic(t *testing.T) {
	h := &LatencyHistogram{}
	for i := 1; i <= 1000; i++ {
		h.Record(time.Duration(i) * time.Microsecond)
	}
	p50 := h.Percentile(0.50)
	p90 := h.Percentile(0.90)
	p99 := h.Percentile(0.99)
	assert.LessOrEqual(t, p50, p90, "p50 must be <= p90")
	assert.LessOrEqual(t, p90, p99, "p90 must be <= p99")
	assert.Greater(t, p99, time.Duration(0))
}

func TestAmplificationStats_WARAEdges(t *testing.T) {
	s := &AmplificationStats{}
	// Empty stats return safe defaults, not NaN or division-by-zero.
	assert.Equal(t, 1.0, s.WA(), "WA should be 1.0 with no client bytes written")
	assert.Equal(t, 0.0, s.RA(), "RA should be 0.0 with no queries")
}

// TestScenario_RangeScanStub verifies that the RangeScan scenario route runs
// end-to-end against a stub engine. It is a smoke test; correctness of the
// actual scan logic is covered by the engine tests.
func TestScenario_RangeScanStub(t *testing.T) {
	eng := newStubEngine()
	err := RunScenario(eng, ScenarioRangeScan)
	require.NoError(t, err)
}

// TestScenario_CrashRecoveryStub verifies the crash recovery scenario route
// runs and confirms all written keys are readable (the stub doesn't drop
// in-memory state, so the scenario's "verify all 500 present" check should
// always pass on the stub).
func TestScenario_CrashRecoveryStub(t *testing.T) {
	eng := newStubEngine()
	err := RunScenario(eng, ScenarioCrashRecovery)
	require.NoError(t, err)
}

// TestScenario_AmplificationStub runs the amplification scenario against a
// stub engine to make sure the workload + tracker wiring is sound.
func TestScenario_AmplificationStub(t *testing.T) {
	eng := newStubEngine()
	err := RunScenario(eng, ScenarioAmplification)
	require.NoError(t, err)
}

// TestScenario_TombstoneGCStub runs the tombstone GC scenario. The stub
// engine isn't an LSM engine, so we only assert the route runs without error.
func TestScenario_TombstoneGCStub(t *testing.T) {
	eng := newStubEngine()
	err := RunScenario(eng, ScenarioTombstoneGC)
	require.NoError(t, err)
}

// TestEngineInterface_RealLSMEngineConforms is a compile-time + runtime
// assertion that the production engine.LSMEngine satisfies the simulation
// Engine interface. If the interface drifts, this test fails to compile.
func TestEngineInterface_RealLSMEngineConforms(t *testing.T) {
	dir := t.TempDir()
	eng, err := engine.Open(engine.Config{
		DataDir:    filepath.Join(dir, "data"),
		MemTableSize: 64 * 1024,
		BlockSize:    4096,
		SSTMaxSize:   1 << 20,
		BlockCacheSize: 1 << 16,
		MaxImmutableMemTables: 1,
	})
	require.NoError(t, err)
	defer func() { _ = eng.Close() }()

	var iface Engine = eng // compile-time conformance check
	require.NotNil(t, iface)
	assert.NotPanics(t, func() { iface.Stats() })
}
