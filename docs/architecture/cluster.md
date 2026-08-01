# Cluster and Raft Replication

The cluster layer (`internal/cluster/`) wraps the local LSM engine in a replicated state machine. The HTTP gateway always talks to a `cluster.Node` interface, never directly to `LSMEngine`.

---

## Architecture

```text
client write
     │
     ▼
cluster.Node.Put()
     │
     ├─ StandaloneNode (internal/cluster/standalone.go)
     │    └── direct call to LSMEngine.Put()
     │
     └─ RaftNode (internal/cluster/raft_node.go)
          └── append logical command to Raft log
               │
               ▼ quorum commit
          FSM.Apply(command)
               │
               ▼
          LSMEngine.Put() (local engine on each node)
```

Raft does **not** replicate SSTable files. Each node runs its own independent LSM engine. The consensus log replicates logical operations (`Put`, `Delete`, `WriteBatch`). SSTable layout, flush timing, block cache state, and compaction scheduling may differ between nodes as long as the logical key-value state converges.

---

## Write path (cluster mode)

| Step | Description |
|------|-------------|
| 1 | Client sends write to any node |
| 2 | Follower rejects with `409 Conflict` + leader address for redirect |
| 3 | Leader appends logical command to replicated log |
| 4 | Log entry replicates to quorum of peers |
| 5 | Once committed, FSM applies command to local LSM on each node |
| 6 | Leader replies success after local apply completes (within `CLUSTER_APPLY_TIMEOUT`) |

---

## Read modes

Reads support two explicit consistency modes via the `?consistency=` query parameter:

| Mode | Behaviour | Cost |
|------|-----------|------|
| `linearizable` (default) | Calls `raft.VerifyLeader()` before serving the local read | One extra RPC to confirm leadership |
| `eventual` | Reads from local follower state at the last applied log index | No extra RPC; may return stale data |

!!! warning "No read lease"
    The `linearizableRead()` path always calls `VerifyLeader` — there is no read lease optimisation. This is intentional: read leases require careful clock-skew handling and are a common source of stale-read bugs. The extra RPC latency is acceptable for correctness.

---

## Leader forwarding and SSRF protection

When a follower receives a write request, it reads the `leaderAddress` from the Raft state and may forward the request to the leader. Before forwarding:

1. The leader's HTTP address is validated against the in-memory `peerRegistry`.
2. If the address is not in the registry, the request is rejected — not forwarded.

This prevents SSRF attacks where a compromised leader hint could cause a node to forward requests to an arbitrary host.

---

## Snapshot and install-snapshot

Snapshots are used to bound Raft log replay time and allow new followers to catch up without replaying the full log:

1. The FSM periodically persists engine state: `MANIFEST` + SSTable files + `_cluster/applied-state.json`.
2. `CLUSTER_SNAPSHOT_INTERVAL` (default 5 minutes) controls the check interval.
3. A snapshot is created when unapplied log entries exceed `CLUSTER_SNAPSHOT_MIN_ENTRIES` (default 10000).
4. Up to `CLUSTER_SNAPSHOT_RETAIN` (default 2) snapshots are kept on disk.

On snapshot install (follower catching up from scratch):

```text
1. Stop local writes.
2. Replace local engine files with snapshot contents.
3. Re-open LSMEngine from the restored files.
4. Resume Raft replication from the snapshot's applied index.
```

The FSM engine-swap is protected by a dedicated mutex in `internal/cluster/raft_node.go` that guards concurrent reads and writes during the brief window when the engine pointer is being replaced.

---

## Sharding

When `CLUSTER_SHARD_COUNT > 1`, the node hosts multiple local Raft groups. Each group owns a slice of the 256-slot key space (`CLUSTER_ROUTING_SLOTS=256`). Requests are routed by `hash(key) % 256` to the appropriate shard.

| Config | Default | Description |
|--------|---------|-------------|
| `CLUSTER_SHARD_COUNT` | 1 | Number of local Raft groups |
| `CLUSTER_SHARD_PORT_STRIDE` | 100 | Port offset between per-shard Raft listeners |
| `CLUSTER_ROUTING_SLOTS` | 256 | Fixed slot count for the shared slot map |
| `CLUSTER_REBALANCE_INTERVAL` | 30s | How often hot-slot rebalancing runs |
| `CLUSTER_REBALANCE_THRESHOLD_BYTES` | 64 MB | Minimum byte skew before moving a slot |
| `CLUSTER_REBALANCE_MAX_SLOTS` | 1 | Maximum slots moved per rebalance pass |

---

## Raft timing parameters

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER_ELECTION_TIMEOUT` | 3s | Leader election timeout |
| `CLUSTER_HEARTBEAT_INTERVAL` | 500ms | Leader heartbeat / lease interval |
| `CLUSTER_COMMIT_TIMEOUT` | 250ms | AppendEntries commit tick |
| `CLUSTER_APPLY_TIMEOUT` | 10s | Maximum wait for client apply acknowledgement |
| `CLUSTER_TRAILING_LOGS` | 256 | Raft log entries retained after a snapshot |

---

## Cluster TLS (inter-node transport)

Set `CLUSTER_TLS_ENABLED=1` to encrypt inter-node Raft transport. For mutual TLS (mTLS), also set `CLUSTER_TLS_CA_FILE`.

| Variable | Description |
|----------|-------------|
| `CLUSTER_TLS_CERT_FILE` | PEM certificate for the Raft transport listener |
| `CLUSTER_TLS_KEY_FILE` | PEM private key |
| `CLUSTER_TLS_CA_FILE` | PEM CA bundle for peer certificate verification |
| `CLUSTER_TLS_SERVER_NAME` | Explicit TLS server name override (optional) |
| `CLUSTER_TLS_INSECURE_SKIP_VERIFY` | Disable peer verification; debugging only |

!!! warning
    `CLUSTER_TLS_INSECURE_SKIP_VERIFY` disables all peer certificate verification. Never use in production.

---

## Peer list format

The `CLUSTER_PEERS` environment variable uses the format:

```text
nodeID@rpcAddr[@clientAddr], ...

Example:
  node-2@127.0.0.1:7002@http://127.0.0.1:8081,node-3@127.0.0.1:7003@http://127.0.0.1:8082
```

- `rpcAddr` is the Raft TCP bind address.
- `clientAddr` is the HTTP address used for leader-redirect responses to clients. If omitted, `CLUSTER_ADVERTISE_ADDR` is used.
