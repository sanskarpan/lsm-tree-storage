package cluster

import (
	"context"
	"errors"
	"fmt"

	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"
)

// ErrNotLeader indicates the request reached a follower or a node without a
// current leader view. Callers can inspect it for redirect metadata.
var ErrNotLeader = errors.New("cluster: not leader")
var ErrUnsupported = errors.New("cluster: unsupported operation")

// NotLeaderError carries leader hint metadata for redirected requests.
type NotLeaderError struct {
	LeaderID      string
	LeaderAddress string
}

func (e *NotLeaderError) Error() string {
	if e == nil {
		return ErrNotLeader.Error()
	}
	if e.LeaderID == "" && e.LeaderAddress == "" {
		return ErrNotLeader.Error()
	}
	return fmt.Sprintf("%s (leader_id=%q leader_address=%q)", ErrNotLeader, e.LeaderID, e.LeaderAddress)
}

func (e *NotLeaderError) Unwrap() error {
	return ErrNotLeader
}

// Node is the cluster-facing service boundary above the local LSM engine.
//
// All client-visible mutations must eventually flow through this abstraction so
// consensus, redirects, snapshots, and engine swaps can be introduced without
// bypasses in higher layers.
type Node interface {
	EventBus() *events.EventBus

	Status(ctx context.Context) Status
	Shards(ctx context.Context) []ShardStatus
	LeaderAddress(ctx context.Context) string
	Peers(ctx context.Context) []Peer
	AddPeer(ctx context.Context, peer Peer) error
	RemovePeer(ctx context.Context, nodeID string) error

	Put(ctx context.Context, key, value []byte) error
	Delete(ctx context.Context, key []byte) error
	Write(ctx context.Context, batch *engine.WriteBatch) error

	Get(ctx context.Context, key []byte) ([]byte, error)
	Scan(ctx context.Context, start, end []byte, limit int) ([][2]string, error)
	GetWithConsistency(ctx context.Context, key []byte, consistency ReadConsistency) ([]byte, error)
	ScanWithConsistency(ctx context.Context, start, end []byte, limit int, consistency ReadConsistency) ([][2]string, error)

	Version(ctx context.Context) *manifest.Version
	Stats(ctx context.Context) map[string]interface{}
	HealthStatus(ctx context.Context) engine.HealthStatus
	RuntimeState(ctx context.Context) engine.RuntimeState
	EngineConfig(ctx context.Context) engine.Config
	MemTableSnapshot(ctx context.Context, limit int) engine.MemTablesSnapshot

	ForceFlush(ctx context.Context)
	ForceCompaction(ctx context.Context, level int)
	SetCompactionStyle(ctx context.Context, style string)

	Close() error
}
