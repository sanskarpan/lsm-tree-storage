package cluster

import "time"

// Peer describes one cluster member in the static membership model.
type Peer struct {
	NodeID        string `json:"node_id"`
	RPCAddress    string `json:"rpc_address"`
	ClientAddress string `json:"client_address,omitempty"`
	Suffrage      string `json:"suffrage,omitempty"`
}

// TLSConfig configures optional TLS or mTLS for inter-node Raft transport.
type TLSConfig struct {
	Enabled            bool
	CertFile           string
	KeyFile            string
	CAFile             string
	ServerName         string
	InsecureSkipVerify bool
}

// Config holds the top-level cluster node configuration.
//
// Phase 0 note:
// This does not activate clustered behavior yet. It defines the shape of the
// future node service so the rest of the repository can evolve against stable
// types before consensus is implemented.
type Config struct {
	Enabled bool

	NodeID string

	DataDir   string
	AuthToken string

	BindAddress      string
	AdvertiseAddress string
	ClientAddress    string

	Peers []Peer

	Bootstrap bool

	ElectionTimeout    time.Duration
	HeartbeatInterval  time.Duration
	CommitTimeout      time.Duration
	ApplyTimeout       time.Duration
	SnapshotInterval   time.Duration
	SnapshotMinEntries uint64
	SnapshotRetain     int
	TrailingLogs       uint64

	ShardCount              int
	ShardPortStride         int
	RoutingSlots            int
	RebalanceInterval       time.Duration
	RebalanceThresholdBytes int64
	RebalanceMaxSlots       int

	TLS TLSConfig
}

// DefaultConfig returns conservative defaults for the future cluster node.
func DefaultConfig() Config {
	return Config{
		Enabled:                 false,
		ElectionTimeout:         3 * time.Second,
		HeartbeatInterval:       500 * time.Millisecond,
		CommitTimeout:           250 * time.Millisecond,
		ApplyTimeout:            10 * time.Second,
		SnapshotInterval:        5 * time.Minute,
		SnapshotMinEntries:      10_000,
		SnapshotRetain:          2,
		TrailingLogs:            256,
		ShardCount:              1,
		ShardPortStride:         100,
		RoutingSlots:            256,
		RebalanceInterval:       30 * time.Second,
		RebalanceThresholdBytes: 64 << 20,
		RebalanceMaxSlots:       1,
	}
}

// SingleNode reports whether this config describes a non-clustered topology.
func (c Config) SingleNode() bool {
	return !c.Enabled || (len(c.Peers) == 0 && c.ShardCount <= 1)
}
