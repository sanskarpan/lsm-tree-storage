package cluster

import (
	"context"

	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"
)

// StandaloneNode adapts the current single-node engine to the cluster-facing
// node interface. This is the migration bridge used before consensus is active.
type StandaloneNode struct {
	nodeID string
	eng    *engine.LSMEngine
}

func NewStandaloneNode(nodeID string, eng *engine.LSMEngine) *StandaloneNode {
	if nodeID == "" {
		nodeID = "standalone"
	}
	return &StandaloneNode{
		nodeID: nodeID,
		eng:    eng,
	}
}

func (n *StandaloneNode) EventBus() *events.EventBus {
	if n == nil || n.eng == nil {
		return nil
	}
	return n.eng.EventBus()
}

func (n *StandaloneNode) Status(context.Context) Status {
	return Status{
		Enabled:    false,
		NodeID:     n.nodeID,
		Role:       RoleStandalone,
		LeaderID:   n.nodeID,
		PeerCount:  1,
		ShardCount: 1,
		Shards: []ShardStatus{{
			ShardID:   "shard-0",
			NodeID:    n.nodeID,
			Role:      RoleStandalone,
			LeaderID:  n.nodeID,
			PeerCount: 1,
		}},
	}
}

func (n *StandaloneNode) Shards(ctx context.Context) []ShardStatus {
	return n.Status(ctx).Shards
}

func (n *StandaloneNode) LeaderAddress(context.Context) string {
	return ""
}

func (n *StandaloneNode) Peers(context.Context) []Peer {
	return []Peer{{
		NodeID: n.nodeID,
	}}
}

func (n *StandaloneNode) AddPeer(context.Context, Peer) error {
	return ErrUnsupported
}

func (n *StandaloneNode) RemovePeer(context.Context, string) error {
	return ErrUnsupported
}

func (n *StandaloneNode) Put(_ context.Context, key, value []byte) error {
	return n.eng.Put(key, value)
}

func (n *StandaloneNode) Delete(_ context.Context, key []byte) error {
	return n.eng.Delete(key)
}

func (n *StandaloneNode) Write(_ context.Context, batch *engine.WriteBatch) error {
	return n.eng.Write(batch)
}

func (n *StandaloneNode) Get(_ context.Context, key []byte) ([]byte, error) {
	return n.eng.Get(key)
}

func (n *StandaloneNode) Scan(_ context.Context, start, end []byte, limit int) ([][2]string, error) {
	return n.eng.Scan(start, end, limit), nil
}

func (n *StandaloneNode) GetWithConsistency(ctx context.Context, key []byte, _ ReadConsistency) ([]byte, error) {
	return n.Get(ctx, key)
}

func (n *StandaloneNode) ScanWithConsistency(ctx context.Context, start, end []byte, limit int, _ ReadConsistency) ([][2]string, error) {
	return n.Scan(ctx, start, end, limit)
}

func (n *StandaloneNode) Version(context.Context) *manifest.Version {
	return n.eng.Manifest().Current()
}

func (n *StandaloneNode) Stats(context.Context) map[string]interface{} {
	return n.eng.Stats()
}

func (n *StandaloneNode) HealthStatus(context.Context) engine.HealthStatus {
	return n.eng.HealthStatus()
}

func (n *StandaloneNode) RuntimeState(context.Context) engine.RuntimeState {
	return n.eng.State()
}

func (n *StandaloneNode) EngineConfig(context.Context) engine.Config {
	return n.eng.Config()
}

func (n *StandaloneNode) MemTableSnapshot(_ context.Context, limit int) engine.MemTablesSnapshot {
	return n.eng.MemTableSnapshot(limit)
}

func (n *StandaloneNode) ForceFlush(_ context.Context) {
	n.eng.ForceFlush()
}

func (n *StandaloneNode) ForceCompaction(_ context.Context, level int) {
	n.eng.ForceCompaction(level)
}

func (n *StandaloneNode) SetCompactionStyle(_ context.Context, style string) {
	n.eng.SetCompactionStyle(style)
}

func (n *StandaloneNode) Close() error {
	return n.eng.Close()
}
