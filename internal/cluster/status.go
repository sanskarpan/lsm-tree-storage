package cluster

// Role identifies the cluster role of a node.
type Role string

const (
	RoleStandalone Role = "standalone"
	RoleFollower   Role = "follower"
	RoleCandidate  Role = "candidate"
	RoleLeader     Role = "leader"
	RoleMixed      Role = "mixed"
)

// ShardStatus describes one shard-local role and replication state.
type ShardStatus struct {
	ShardID      string `json:"shard_id"`
	NodeID       string `json:"node_id,omitempty"`
	Role         Role   `json:"role"`
	LeaderID     string `json:"leader_id,omitempty"`
	Leader       string `json:"leader,omitempty"`
	Term         uint64 `json:"term,omitempty"`
	CommitIndex  uint64 `json:"commit_index,omitempty"`
	LastApplied  uint64 `json:"last_applied,omitempty"`
	PeerCount    int    `json:"peer_count"`
	KeyspaceHint string `json:"keyspace_hint,omitempty"`
}

// Status is the future cluster-facing runtime status shape.
//
// Phase 0 note:
// Initially this will only be populated by the single-node wrapper once the
// gateway is moved behind a node facade.
type Status struct {
	Enabled       bool          `json:"enabled"`
	NodeID        string        `json:"node_id,omitempty"`
	Role          Role          `json:"role"`
	LeaderID      string        `json:"leader_id,omitempty"`
	Term          uint64        `json:"term,omitempty"`
	CommitIndex   uint64        `json:"commit_index,omitempty"`
	LastApplied   uint64        `json:"last_applied,omitempty"`
	PeerCount     int           `json:"peer_count"`
	ReadOnlyCause string        `json:"read_only_cause,omitempty"`
	ShardCount    int           `json:"shard_count,omitempty"`
	Shards        []ShardStatus `json:"shards,omitempty"`
}
