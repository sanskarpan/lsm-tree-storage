# High-Availability Architecture Plan

This document defines the architectural change required to move the current
single-node LSM engine into a production-capable replicated system.

## Current State

The architecture described in this document has now been implemented for the
repository's replicated mode:

- a cluster node facade sits above the engine
- gateway reads and writes flow through that node facade
- clustered mode uses Raft for leader election, quorum commit, and runtime membership changes
- committed logical commands apply into each node's local engine
- followers return leader redirect metadata for writes, and reads support explicit linearizable or eventual consistency modes
- snapshot restore can rebuild engine state even if local engine files are lost

The remaining work is now operational tuning and future scaling extensions,
not foundational consensus or shard-movement plumbing.

## Target Architecture

The correct target is a replicated state machine with:

- Raft-style consensus for leader election and quorum commit
- a replicated command log above the local engine
- deterministic application of committed commands into each node's local LSM
- snapshot/install-snapshot support so followers can catch up efficiently
- leader-routed writes
- controlled read semantics

The local LSM remains the storage engine. It stops being the system of record
for write ordering. The replicated consensus log becomes the source of truth for
ordering and durability.

## Core Design Decision

Do not replicate SSTables directly.

Replicate logical operations:

- `Put(key, value)`
- `Delete(key)`
- `WriteBatch(entries...)`
- configuration/state changes that affect logical behavior

Each node applies the committed operations into its local engine in the same
order. SSTable layout, flush timing, cache state, and compaction timing may vary
per node as long as the logical state converges.

This keeps the replication layer independent from low-level file layout and lets
the existing engine remain useful.

## New Execution Model

### Write Path

1. client sends write to cluster endpoint
2. follower rejects with leader redirect metadata
3. leader appends logical command to replicated log
4. entry replicates to quorum
5. once committed, each node applies the command to its local engine
6. leader replies success only after commit and local apply

### Read Path

The current read contract is explicit:

- `linearizable` reads verify leadership and wait for a Raft barrier before serving locally, or forward to the current leader when they hit a follower
- `eventual` reads may be served from a follower's locally applied state without the extra leader hop

This keeps correctness clear without pretending follower reads are linearizable.

## Required Architectural Changes

### 1. Introduce a Node Layer Above the Engine

Add a cluster/node service that owns:

- node identity
- peer configuration
- consensus state
- replicated log
- apply loop
- leader/follower role transitions
- cluster-facing APIs

The local `LSMEngine` becomes an apply target used by that node service.

### 2. Split Engine APIs Into Local vs Replicated Responsibilities

The current engine directly accepts client writes. That must change.

Long-term split:

- local engine API
  - apply deterministic operations
  - query local state
  - produce/consume snapshots
- cluster API
  - submit client command
  - route to leader
  - expose cluster status

### 3. Deterministic Apply Contract

Follower state must converge from committed log entries.

That requires a stable apply contract:

- no client-side sequence assignment at the edge
- sequence/order derived from committed log position or committed apply sequence
- apply path must be idempotent for replay/recovery

### 4. Snapshot Strategy

Consensus log replay forever is not viable.

Need periodic snapshots containing:

- logical engine state marker
- MANIFEST + SSTables + active metadata
- applied index / term metadata

Snapshot install must:

- stop local writes on target
- replace local state atomically
- reopen engine
- resume replication from the installed index

### 5. Cluster Metadata and Membership

The repository now supports runtime voter membership changes through consensus.

Core metadata still includes:

- `node_id`
- `advertise_addr`
- peer registry / client addresses

### 6. New Observability Domain

Need cluster-level metrics and endpoints:

- node role
- current term
- commit index
- last applied index
- replication lag by peer
- leader changes
- snapshot counts/failures
- quorum health

## Proposed Repository Structure

Add a new cluster layer without breaking the existing engine packages:

```text
internal/cluster/
  config.go            # node/peer/cluster configuration
  command.go           # replicated logical command format
  node.go              # node service interface
  transport.go         # peer transport abstraction
  apply.go             # apply loop into local engine
  snapshot.go          # snapshot metadata interface
  status.go            # cluster status types

internal/raft/
  log.go               # replicated log storage
  node.go              # consensus node state machine
  rpc.go               # append entries, request vote, install snapshot messages
  storage.go           # term/vote/index persistence
```

The gateway then grows cluster routes instead of direct single-engine control:

- `/cluster/status`
- `/cluster/leader`
- `/cluster/peers`
- `/cluster/readiness`

## Phase Plan

Status summary:

- Phase 0: complete
- Phase 1: complete
- Phase 2: complete
- Phase 3: complete
- Phase 4: complete
- Phase 5: complete for the targeted repo scope, including metrics, readiness, runtime membership changes, TLS-capable transport, shard routing, and broader validation

### Phase 0. Foundations

Goal:

- introduce cluster config and command types
- define separation between cluster node and local engine
- no behavior change yet

Deliverables:

- `internal/cluster/config.go`
- `internal/cluster/command.go`
- `internal/cluster/status.go`
- architecture doc

### Phase 1. Single-Node Cluster Wrapper

Goal:

- run the existing engine behind a cluster node facade
- still one node, but through the future cluster interfaces

Deliverables:

- `ClusterNode` interface
- single-node implementation
- gateway writes routed through node facade, not directly to engine
- cluster status endpoint

This is the migration bridge.

### Phase 2. Consensus Log and Peer Transport

Goal:

- add leader election and append-entries replication
- establish the base transport and persistent peer identity model

Deliverables:

- Raft persistence for term/vote/log
- peer RPC transport
- leader redirects
- quorum commit

### Phase 3. Apply Loop and Recovery

Goal:

- committed log entries apply into local engines
- crash recovery restores consensus state and engine state coherently

Deliverables:

- durable apply index
- replay path
- idempotent command application

### Phase 4. Snapshots

Goal:

- bounded recovery time and bounded replicated log growth

Deliverables:

- snapshot creation
- snapshot install
- log compaction after snapshot

## Implemented Notes

- The node layer lives in `internal/cluster`.
- Single-node mode uses `StandaloneNode`.
- Clustered mode uses `RaftNode` with Bolt-backed Raft log/state and file snapshot storage.
- Multi-shard mode uses `ShardedNode`, which hosts multiple local Raft groups and routes by key hash.
- The FSM stores the last applied log index in `_cluster/applied-state.json`.
- Runtime peer metadata is stored in `_cluster/peers.json`.
- Snapshot restore replaces engine files while preserving the `_cluster` state directory.
- Reads support explicit `linearizable` and `eventual` consistency modes per shard.

### Phase 5. Operational Hardening

Goal:

- make the cluster operable under failures

Deliverables:

- leader metrics
- peer lag metrics
- chaos/failure tests
- rolling-restart docs

## Major Risks

### Risk 1. Existing Engine Sequence Semantics

The engine currently owns local sequence progression. In clustered mode, write
ordering must be driven by committed replication order, not by whichever node
accepted the request first.

Mitigation:

- move externally visible ordering to replicated command index or cluster apply
  sequence
- confine local sequence numbers to internal version ordering if needed

### Risk 2. Snapshot/Compaction Interaction

Copying local engine files during concurrent flush/compaction can create invalid
snapshots.

Mitigation:

- explicit snapshot barrier
- freeze/coordinate background workers while snapshot materialization happens
- record manifest generation and applied index atomically

### Risk 3. Read Semantics Confusion

Follower reads are easy to expose and easy to get wrong.

Mitigation:

- default to `linearizable` reads
- make `eventual` follower reads an explicit client choice

## Acceptance Criteria For "HA"

This repo should only be described as highly available when all of these are
true:

- three-node cluster support exists
- leader election works under node failure
- committed writes survive leader loss
- followers catch up after downtime
- snapshot install works
- rolling restarts are documented and tested
- cluster metrics and alerts exist

Until then, this remains a production-grade single-node repo with an HA roadmap.
