package simulation

import "lsm-engine/internal/events"

// Engine defines the subset of storage behavior needed by benchmarks and demo
// scenarios. It deliberately avoids depending on the concrete engine type so
// these flows can also execute through the cluster node facade.
type Engine interface {
	Put(key, value []byte) error
	Delete(key []byte) error
	Get(key []byte) ([]byte, error)
	Scan(start, end []byte, limit int) [][2]string
	ForceFlush()
	ForceCompaction(level int)
	Stats() map[string]interface{}
	EventBus() *events.EventBus
}
