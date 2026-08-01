# ADR-0002: Replicate Logical Operations, Not SSTables

**Status:** Accepted
**Date:** 2025-01

---

## Context

High availability required that the storage engine survive the loss of a single node without data loss or a manual recovery procedure. Two approaches were evaluated:

**Option A — Volume replication**: replicate SSTable files at the storage layer (e.g., DRBD, cloud block-storage replication). Each replica has an identical copy of all SSTable files.

**Option B — Logical replication**: use Raft consensus to replicate logical operations (`Put`, `Delete`, `WriteBatch`). Each node runs its own independent LSM engine and applies committed operations locally.

---

## Decision

Implement logical replication via Raft. The cluster layer (`internal/cluster/`) sits above the LSM engine:

- `cluster.Node` is the interface the gateway uses for all reads and writes.
- `StandaloneNode` (`internal/cluster/standalone.go`) wraps the single-node case.
- `RaftNode` (`internal/cluster/raft_node.go`) uses `hashicorp/raft` backed by Bolt for log storage and file-based snapshot storage.
- `command.go` defines the wire format for replicated logical commands.
- The FSM (`raft_node.go FSM.Apply`) decodes each committed command and calls `LSMEngine.Put` / `LSMEngine.Delete` / `LSMEngine.Write`.

The MANIFEST, WAL, and SSTables are **local** to each node. SSTable layout may differ between nodes (different compaction timing, different L0/L1 distributions). Only the logical key-value state converges.

---

## Consequences

**Positive:**

- The replication layer is completely independent of the LSM file format. Engine upgrades (new SSTable format, new compaction strategy) do not require replication changes.
- Each follower can optimise its local LSM independently (different compaction timing, different cache warm-up).
- Snapshot/install-snapshot for new followers is straightforward: ship the engine's MANIFEST + SSTables and resume from the snapshot's applied index.
- No file-system-level coupling: nodes can run on different hardware, OS, or file systems.

**Negative:**

- SSTable layouts diverge between nodes. If a follower falls far behind, it must receive a full engine snapshot (not just the Raft log delta). This is handled by `CLUSTER_SNAPSHOT_MIN_ENTRIES` and `CLUSTER_SNAPSHOT_RETAIN`.
- Cross-shard write batches require a local transaction coordinator (`internal/cluster/`) when `CLUSTER_SHARD_COUNT > 1`.
- Reads at followers are eventually consistent by default; linearizable reads require a `VerifyLeader` round-trip per read.
- The FSM engine-swap during snapshot restore must be guarded by a mutex separate from the engine's own `RWMutex` (implemented in `raft_node.go`).
