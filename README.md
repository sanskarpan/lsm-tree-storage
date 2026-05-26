# LSM-Tree Storage Engine

An educational, from-scratch implementation of a **Log-Structured Merge-Tree (LSM-Tree)** database storage engine written in Go, with a real-time 7-panel visualization dashboard built on React, Vite, Bun, and Elysia.

This project covers the complete LSM write and read paths, three compaction strategies, Bloom filters, block cache, crash recovery, a Raft-backed multi-node replication layer, and a live WebSocket-driven UI — all without any external database dependencies.

---

## Table of Contents

- [Features](#features)
- [Requirements](#requirements)
- [Quick Start](#quick-start)
- [Makefile Targets](#makefile-targets)
- [Project Structure](#project-structure)
- [Configuration](#configuration)
- [Architecture Overview](#architecture-overview)
- [WAL Block Format](#wal-block-format)
- [SSTable File Format](#sstable-file-format)
- [Compaction Strategies](#compaction-strategies)
- [7 Dashboard Panels](#7-dashboard-panels)
- [API Endpoints](#api-endpoints)
- [Crash Recovery](#crash-recovery)
- [Performance Targets](#performance-targets)
- [Running Benchmarks](#running-benchmarks)
- [Operations Guide](#operations-guide)
- [HA Roadmap](#ha-roadmap)

---

## Features

| Category | Details |
|---|---|
| **Write Path** | WAL append + fsync → MemTable (skip list) → Immutable queue → SSTable flush |
| **WAL** | 32KB block format, CRC32 checksums, record fragmentation (Full/First/Middle/Last) |
| **MemTable** | Skip list (O(log n)), tombstone markers, mutable → immutable rotation |
| **SSTable** | Data blocks (4KB, prefix-compressed), Filter block (Bloom), Index block, Footer |
| **Bloom Filters** | k-hash bit array, configurable bits-per-key, per-SSTable, ~1% FP at 10 bpk |
| **Block Cache** | LRU with configurable capacity, hit-rate tracking, CacheKey = (FileID, BlockOffset) |
| **Compaction** | LCS (Leveled), STCS (Size-Tiered), TWCS (Time-Window); live switchable |
| **MANIFEST** | Append-only VersionEdit log; atomic compaction commits |
| **Crash Recovery** | WAL replay → MemTable rebuild; MANIFEST replay → SSTable level reconstruction |
| **High Availability** | Raft quorum writes, runtime membership changes, leader redirects, applied-index checkpoints, snapshot restore |
| **Sharding** | Multi-Raft partitioning with automatic slot-based routing and rebalancing behind one API surface |
| **Benchmarks** | Sequential write, random write, Zipf read, mixed, compaction stress |
| **Scenarios** | 8 pre-built demos: write-flush, bloom demo, compaction, crash recovery, tombstone GC, range scan |
| **Dashboard** | 7-panel real-time UI (React + Vite client served by a Bun + Elysia BFF) |
| **WebSocket** | Full event bus: WAL, MemTable, SSTable, compaction, cache, amplification events |

---

## Requirements

| Dependency | Version | Notes |
|---|---|---|
| Go | 1.26.3+ | Patched toolchain required for current standard-library security fixes |
| Bun | Latest | JavaScript/TypeScript runtime + package manager |
| golangci-lint | Latest | Required only for `make lint` |

**Install Bun:**

```bash
curl -fsSL https://bun.sh/install | bash
```

---

## Quick Start

```bash
# 1. Clone the repository
git clone https://github.com/your-org/lsm-engine.git
cd lsm-engine

# 2. Start the Go backend (loopback on 127.0.0.1:8080 by default)
make run
# OR
go run ./cmd/server/main.go

# 3. In a separate terminal: install and start the BFF + frontend (loopback on 127.0.0.1:3001 by default)
cd frontend
bun install
bun run index.ts

# 4. Open the dashboard
open http://localhost:3001
```

The Go backend exposes its REST API and WebSocket on `127.0.0.1:8080`. The Elysia BFF proxies all requests and WebSocket messages to the frontend on `127.0.0.1:3001`.

**Optional: Override the data directory via environment variable**

```bash
DATA_DIR=/tmp/lsm-data make run
```

**Optional: enable bearer-token protection**

```bash
# backend
API_TOKEN=change-me go run ./cmd/server/main.go

# frontend/BFF, in another terminal
cd frontend
API_TOKEN=change-me bun run index.ts
```

When `API_TOKEN` is set, all backend API endpoints except `/health` require `Authorization: Bearer <token>`, and backend WebSocket clients must provide the token via header or `?access_token=...`. The Bun BFF forwards the configured token automatically to the backend.

**Optional: protect the browser-facing BFF/UI**

```bash
cd frontend
API_TOKEN=change-me \
BFF_BASIC_AUTH=viewer:panel \
bun run index.ts
```

When `BFF_BASIC_AUTH` is set, every BFF route except `/health` requires HTTP Basic auth, including the dashboard HTML, proxied REST endpoints, and the BFF WebSocket upgrade. If you bind the BFF to a non-loopback host, it now refuses to start unless you explicitly allow remote exposure and either configure `BFF_BASIC_AUTH` or opt out with `ALLOW_INSECURE_REMOTE_BFF=1`.

**Observability endpoints**

- `GET /health`: shallow liveness
- `GET /ready`: engine-backed readiness probe
- `GET /metrics`: Prometheus metrics, bearer-protected when `METRICS_TOKEN` or `API_TOKEN` is set

**Cluster mode**

The server can also run in replicated mode. One node can bootstrap the initial voter set, additional nodes can be added later at runtime, and multi-shard mode can run several Raft groups behind the same API server:

```bash
CLUSTER_ENABLED=1 \
CLUSTER_NODE_ID=node-1 \
CLUSTER_BIND_ADDR=127.0.0.1:7001 \
CLUSTER_ADVERTISE_ADDR=127.0.0.1:7001 \
CLUSTER_CLIENT_ADDR=http://127.0.0.1:8080 \
CLUSTER_BOOTSTRAP=1 \
CLUSTER_PEERS='node-2@127.0.0.1:7002@http://127.0.0.1:8081,node-3@127.0.0.1:7003@http://127.0.0.1:8082' \
go run ./cmd/server/main.go
```

In clustered mode:

- writes are leader-routed and quorum-committed
- non-leader point reads and shard reads are forwarded to the current leader
- snapshots are used for bounded replay and restore
- engine state is restored from Raft snapshots if local engine files are lost
- membership can be changed with `/cluster/membership/add` and `/cluster/membership/remove`
- inter-node Raft transport can run over TLS or mTLS
- multi-shard routing can be enabled with `cluster.shard_count` or `CLUSTER_SHARD_COUNT`
- shard ownership is tracked through a shared slot map stored in cluster metadata, and hot slots are automatically rebalanced across local Raft groups

**Operations guide**

See [docs/operations.md](docs/operations.md) for the environment matrix, backup/restore workflow, load testing, and clustered deployment caveats.

---

## Makefile Targets

| Target | Command | Description |
|---|---|---|
| `run` | `make run` | Start the Go HTTP server on `127.0.0.1:8080` |
| `test` | `make test` | Run all unit and integration tests |
| `test-race` | `make test-race` | Run all tests with race detector (`-count=3`) |
| `bench` | `make bench` | Run benchmarks with memory allocation stats |
| `lint` | `make lint` | Run `golangci-lint` across the entire module |
| `loadtest` | `make loadtest` | Run the mixed HTTP load harness against the configured target |
| `backup` | `make backup` | Create a tarball backup of `./data` under `./backups` |
| `docker-build` | `make docker-build` | Build the backend and frontend container images |
| `compose-up` | `make compose-up` | Start the full local stack with Docker Compose |
| `compose-down` | `make compose-down` | Stop the Docker Compose stack |
| `clean` | `make clean` | Remove the `./data/` directory and any stray `.sst`/`.log` files |

---

## Project Structure

```
lsm-engine/
├── cmd/server/main.go              # Entry point: opens engine, registers routes, serves HTTP
├── internal/
│   ├── wal/
│   │   ├── wal.go                  # WAL writer: block format, CRC32, record fragmentation
│   │   ├── reader.go               # WAL reader: crash recovery, CRC verification
│   │   └── record.go               # Record types + entry binary encoding
│   ├── memtable/
│   │   ├── skiplist.go             # Skip list: MaxLevel=12, P=0.25
│   │   ├── memtable.go             # MemTable: Put/Delete/Get/Iterator
│   │   └── iterator.go             # MemTable iterator for flush
│   ├── sstable/
│   │   ├── builder.go              # SSTableBuilder: data blocks, index, filter, footer
│   │   ├── reader.go               # SSTableReader: Get, NewIterator, index loading
│   │   ├── block.go                # Block: prefix compression, restart points, binary search
│   │   ├── block_builder.go        # BlockBuilder: accumulates entries
│   │   └── format.go               # BlockHandle, Footer, InternalKey definitions
│   ├── bloom/
│   │   └── bloom.go                # Bloom filter: add, MayContain, serialization
│   ├── compaction/
│   │   ├── leveled.go              # LCS compactor: pick, execute, merge iterator
│   │   ├── stcs.go                 # STCS compactor: bucket grouping, tiered merge
│   │   ├── twcs.go                 # TWCS compactor: time-window grouping
│   │   └── iterator.go             # MergeIterator (k-way heap), TombstoneIterator
│   ├── manifest/
│   │   ├── manifest.go             # MANIFEST writer + Version in-memory representation
│   │   └── version_edit.go         # VersionEdit binary encoding/decoding
│   ├── cache/
│   │   └── lru.go                  # LRU BlockCache
│   ├── cluster/
│   │   ├── node.go                 # Cluster node abstraction above the engine
│   │   ├── standalone.go           # Single-node facade implementation
│   │   ├── raft_node.go            # Raft-backed multi-node implementation
│   │   ├── snapshot.go             # Engine snapshot persistence and restore helpers
│   │   └── command.go              # Replicated logical command format
│   ├── engine/
│   │   ├── engine.go               # LSMEngine: Open, Put, Get, Scan, recovery
│   │   ├── flush.go                # FlushWorker goroutine
│   │   ├── background.go           # Background compaction scheduler
│   │   └── config.go               # Config struct
│   ├── simulation/
│   │   ├── workload.go             # Workload generators (sequential, random, Zipf)
│   │   ├── scenarios.go            # 8 pre-built demonstration scenarios
│   │   └── amplification.go        # WA/RA/SA measurement
│   └── events/
│       └── bus.go                  # EventBus (non-blocking fan-out → WebSocket)
├── gateway/
│   ├── rest.go                     # REST handler: all routes registered here
│   └── websocket.go                # WebSocket hub: broadcast engine events
├── frontend/
│   ├── index.ts                    # Build-and-serve entry point for the React dashboard
│   ├── server/bff.ts               # Elysia proxy routes + WebSocket bridge
│   └── src/
│       ├── App.tsx                 # Top-level dashboard composition
│       ├── components/             # Panel components
│       ├── hooks/dashboard/        # Snapshot, event-stream, read-trace, and action hooks
│       ├── lib/api.ts              # Typed API client
│       └── types.ts                # Shared frontend data contracts
├── test/
│   ├── unit/                       # wal, skiplist, sstable, bloom, compaction, manifest
│   └── integration/                # crash recovery, compaction correctness, read/write
├── config.yaml
├── docs/
│   └── operations.md             # Runbook: env matrix, probes, backup/restore, load testing
├── deploy/
│   └── prometheus/prometheus.yml # Sample Prometheus scrape config
├── scripts/
│   ├── backup.sh                 # Archive + checksum data-dir backups
│   ├── restore.sh                # Verified restore into a target directory
│   └── loadtest.sh               # Wrapper for the HTTP load harness
├── Dockerfile.backend
├── docker-compose.yml
├── go.mod
└── Makefile
```

---

## Operations Guide

Operational procedures, environment variables, probes, backup/restore, and the
load harness are documented in [docs/operations.md](docs/operations.md).

---

## HA Architecture

The high-availability architecture described in
[docs/ha-architecture.md](docs/ha-architecture.md) is now implemented as the
default clustered execution model:

- the HTTP gateway writes through a cluster node facade, never directly to the raw engine
- clustered mode uses Raft groups for leader election, quorum commit, and runtime membership changes
- followers reject writes with leader metadata, while reads support `linearizable` and `eventual` consistency modes
- committed logical commands are applied deterministically into each node's local engine
- the FSM checkpoints its last applied log index and uses Raft snapshots to bound recovery time
- shard-aware mode runs multiple local Raft groups and routes requests by key hash
- inter-node transport supports TLS or mTLS when configured

Remaining distributed-systems caveats:
- the system is still single-shard-per-request, not a full sharded distributed query engine
---

## Configuration

All fields can be set in `config.yaml`. The Go server reads the file on startup; the defaults shown below are the values present in the repository.

```yaml
data_dir: "./data"              # Directory for SSTables and MANIFEST
wal_dir: ""                     # WAL file location; defaults to data_dir if empty

# MemTable
mem_table_size: 67108864        # 64 MB — flush to SSTable when MemTable reaches this size
max_immutable_memtables: 2      # Max immutable MemTables queued before writes stall

# SSTable
block_size: 4096                # 4 KB — target size for each data block
sst_max_size: 67108864          # 64 MB — maximum size of a single SSTable file
max_open_files: 1000            # Maximum simultaneously open SSTable file descriptors

# Bloom Filter
bloom_bits_per_key: 10          # Bits per key in Bloom filter (10 → ~1% false positive rate)

# Block Cache
block_cache_size: 134217728     # 128 MB LRU block cache

# Leveled compaction geometry
max_levels: 7                   # L0 through L6
level_size_multiplier: 10       # Each level is 10× larger than the previous
level0_file_num_compaction_trigger: 4   # Compact L0 when this many SSTables accumulate
level0_stop_writes_trigger: 12  # Stall all writes when L0 reaches this many files

# Compaction
compaction_style: "leveled"     # leveled | size-tiered | time-window
time_window_size: "1h"          # Window duration for TWCS (e.g., "1h", "24h")

# WAL
sync_wal: true                  # Call fsync after every WAL write (durability vs throughput)

cluster:
  enabled: false
  node_id: "node-1"
  data_dir: "./data/_cluster"
  bind_address: "127.0.0.1:7001"
  advertise_address: "127.0.0.1:7001"
  client_address: "http://127.0.0.1:8080"
  bootstrap: false
  election_timeout: "3s"
  heartbeat_interval: "500ms"
  commit_timeout: "250ms"
  apply_timeout: "10s"
  snapshot_interval: "5m"
  snapshot_min_entries: 10000
  snapshot_retain: 2
  trailing_logs: 256
  shard_count: 1
  shard_port_stride: 100
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    ca_file: ""
    server_name: ""
    insecure_skip_verify: false
  peers:
    - node_id: "node-2"
      rpc_address: "127.0.0.1:7002"
      client_address: "http://127.0.0.1:8081"
```

### Configuration Field Reference

| Field | Default | Description |
|---|---|---|
| `data_dir` | `./data` | Root directory for all on-disk files |
| `wal_dir` | `""` (= data_dir) | Override WAL file location to a different mount |
| `mem_table_size` | 64 MB | MemTable size threshold that triggers a flush |
| `max_immutable_memtables` | 2 | Immutable queue depth before write stalls |
| `block_size` | 4 KB | Target size of each SSTable data block |
| `sst_max_size` | 64 MB | Maximum single SSTable file size |
| `max_open_files` | 1000 | Open file descriptor limit for SSTable readers |
| `bloom_bits_per_key` | 10 | Higher = lower FP rate, more memory |
| `block_cache_size` | 128 MB | Total LRU cache capacity in bytes |
| `max_levels` | 7 | Number of LSM levels (L0–L6) |
| `level_size_multiplier` | 10 | Size ratio between consecutive levels |
| `level0_file_num_compaction_trigger` | 4 | L0 file count to begin compaction |
| `level0_stop_writes_trigger` | 12 | L0 file count that stalls all writes |
| `compaction_style` | `leveled` | Active compaction strategy |
| `time_window_size` | `1h` | TWCS window duration |
| `sync_wal` | `true` | Whether every WAL write is fsynced |

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                         LSM Engine                                  │
│                                                                     │
│  Write Path:                                                        │
│                                                                     │
│  Client PUT ──► WAL (append + fsync) ──► MemTable (skip list)      │
│                                              │                      │
│                                     [MemTable full]                 │
│                                              │                      │
│                                              ▼                      │
│                                    Immutable MemTable Queue         │
│                                              │                      │
│                                    [FlushWorker goroutine]          │
│                                              │                      │
│                                              ▼                      │
│                                         L0 SSTable                 │
│                                              │                      │
│                                  [L0 threshold reached]             │
│                                              │                      │
│                                   [Compactor goroutine]             │
│                                              │                      │
│                         L1 SSTable ◄─────────┘                     │
│                         L2 SSTable ◄── compaction cascades          │
│                         ...                                         │
│                         L6 SSTable  (max size = 10^6 × L1)         │
│                                                                     │
│  Read Path:                                                         │
│                                                                     │
│  Client GET ──► MemTable                                            │
│                    │ not found                                      │
│                    ▼                                                │
│             Immutable MemTables (newest → oldest)                  │
│                    │ not found                                      │
│                    ▼                                                │
│             L0 SSTables (ALL checked, newest → oldest)             │
│             [Bloom filter short-circuit on each]                   │
│                    │ not found                                      │
│                    ▼                                                │
│             L1 → L6  (binary search by key range → 1 SSTable)     │
│             [Bloom filter check → data block lookup]               │
│                    │                                                │
│                    ▼                                                │
│             BlockCache (LRU) ──► disk read if miss                 │
└─────────────────────────────────────────────────────────────────────┘

Supporting Components:
  EventBus ──► WebSocket ──► Frontend Dashboard
  MANIFEST ── tracks SSTable inventory (append-only VersionEdit log)
  BloomFilterRegistry ── per-SSTable in-memory Bloom filters
```

---

## WAL Block Format

The Write-Ahead Log uses the same 32KB block format as LevelDB/RocksDB. Every write is CRC32-protected and fsync'd (when `sync_wal: true`) before the engine acknowledges success.

```
WAL File on Disk
┌──────────────────────────────────┬──────────────────────────────────┬─────
│         Block 0 (32768 bytes)    │         Block 1 (32768 bytes)    │ ...
└──────────────────────────────────┴──────────────────────────────────┴─────

Each 32KB Block:
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  Record 1 (FULL — fits entirely within this block)                      │
│  ┌────────────┬──────────────┬───────────┬──────────────────────────┐  │
│  │  CRC32     │   Length     │   Type    │       Payload            │  │
│  │  (4 bytes) │  (2 bytes)   │ (1 byte)  │     (Length bytes)      │  │
│  │  LE uint32 │  LE uint16   │           │                          │  │
│  └────────────┴──────────────┴───────────┴──────────────────────────┘  │
│   └─────────── HeaderSize = 7 bytes ───────────┘                        │
│                                                                         │
│  Record Type values:                                                    │
│    FULL   = 1  (record fits entirely in one block)                      │
│    FIRST  = 2  (first fragment of a multi-block record)                 │
│    MIDDLE = 3  (middle fragment)                                        │
│    LAST   = 4  (final fragment)                                         │
│                                                                         │
│  Payload — WAL Entry Wire Format:                                       │
│  ┌───────────┬───────────┬──────────┬───────────┬──────────┬─────────┐ │
│  │ EntryType │  KeyLen   │   Key    │  ValLen   │  Value   │  SeqNo  │ │
│  │ (1 byte)  │ (4 bytes) │(KeyLen B)│ (4 bytes) │(ValLen B)│(8 bytes)│ │
│  │           │  LE uint32│          │  LE uint32│          │ LE uint64│ │
│  └───────────┴───────────┴──────────┴───────────┴──────────┴─────────┘ │
│                                                                         │
│  EntryType values:                                                      │
│    SET    = 1  (key + value stored)                                     │
│    DELETE = 2  (tombstone; ValLen=0, Value empty)                       │
│    FLUSH  = 3  (marks that a MemTable flush completed)                  │
│                                                                         │
│  Record 2 ...                                                           │
│                                                                         │
│  Record N (FIRST — too large, continues in Block 1)                    │
│  ┌────────────┬──────────────┬───────────┬──────────────────────────┐  │
│  │  CRC32     │   Length     │  FIRST=2  │  Payload fragment 1      │  │
│  └────────────┴──────────────┴───────────┴──────────────────────────┘  │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Padding: 0x00 bytes filling remainder of block                  │   │
│  │  (written when < 7 bytes remain and no full header fits)         │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘

Block 1 (continuation of large record):
┌─────────────────────────────────────────────────────────────────────────┐
│  Record N continuation (MIDDLE=3 or LAST=4)                             │
│  ┌────────────┬──────────────┬───────────┬──────────────────────────┐  │
│  │  CRC32     │   Length     │  LAST=4   │  Payload fragment 2      │  │
│  └────────────┴──────────────┴───────────┴──────────────────────────┘  │
│  ...                                                                    │
└─────────────────────────────────────────────────────────────────────────┘

Key properties:
  • CRC32 covers ONLY the payload chunk (not the header itself)
  • blockPos tracks current write offset within the active 32KB block
  • On recovery: truncated last record is silently skipped (expected crash behavior)
  • seqNo is monotonically increasing; assigned atomically inside WAL.Append()
```

---

## SSTable File Format

SSTables are immutable files written during a MemTable flush or compaction. The format is modeled after LevelDB's, simplified for educational clarity.

```
SSTable File (.sst)
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  Data Block 0  (target 4KB; may be larger for the last block)           │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Entry 0  (restart point — full key stored)                      │   │
│  │  ┌────────────┬─────────────┬──────────┬────────────┬─────────┐ │   │
│  │  │ shared_len │unshared_len │ val_len  │unshared_key│  value  │ │   │
│  │  │  (varint)  │  (varint)   │ (varint) │ (N bytes)  │(M bytes)│ │   │
│  │  └────────────┴─────────────┴──────────┴────────────┴─────────┘ │   │
│  │  shared_len = 0 at restart points (every 16 entries)             │   │
│  │                                                                   │   │
│  │  Entry 1 (prefix-compressed against Entry 0)                     │   │
│  │  ┌────────────┬─────────────┬──────────┬────────────┬─────────┐ │   │
│  │  │ shared_len │unshared_len │ val_len  │unshared_key│  value  │ │   │
│  │  │  (varint)  │  (varint)   │ (varint) │            │         │ │   │
│  │  └────────────┴─────────────┴──────────┴────────────┴─────────┘ │   │
│  │  shared_len = bytes shared with previous key's prefix            │   │
│  │                                                                   │   │
│  │  Entry 2 ... 14 (prefix-compressed)                              │   │
│  │                                                                   │   │
│  │  Entry 15  (restart point — full key, shared_len=0)             │   │
│  │  ...                                                              │   │
│  │                                                                   │   │
│  │  ── Restarts Section (at end of block) ──────────────────────── │   │
│  │  ┌───────────────┬───────────────┬─────┬───────────────────────┐│   │
│  │  │ restart[0]=0  │ restart[1]=K  │ ... │ numRestarts (uint32LE)││   │
│  │  │  (uint32 LE)  │  (uint32 LE)  │     │                       ││   │
│  │  └───────────────┴───────────────┴─────┴───────────────────────┘│   │
│  │  Restart offsets enable binary search within the data block      │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                         │
│  Data Block 1 ...                                                       │
│  Data Block 2 ...                                                       │
│  ...                                                                    │
│  Data Block N                                                           │
│                                                                         │
├─────────────────────────────────────────────────────────────────────────┤
│  Filter Block  (Bloom filter for this SSTable)                          │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  bit array bytes (m/8 bytes, where m = numKeys * bitsPerKey)    │   │
│  │  last byte = k  (number of hash functions used)                  │   │
│  │  k = bitsPerKey * ln(2)  (clamped to [1, 30])                  │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│  False positive rate at bitsPerKey=10: ~1% (k≈7 hash functions)        │
│                                                                         │
├─────────────────────────────────────────────────────────────────────────┤
│  Index Block  (one entry per data block)                                │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │  Entry 0: last_key_in_block_0  →  BlockHandle{Offset, Size}    │   │
│  │  Entry 1: last_key_in_block_1  →  BlockHandle{Offset, Size}    │   │
│  │  ...                                                             │   │
│  │  BlockHandle = { Offset: uint64, Size: uint64 }  (16 bytes)    │   │
│  │                                                                  │   │
│  │  On a GET: binary search index block → select data block        │   │
│  │  → load from BlockCache or disk → binary search via restarts    │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                                                                         │
├─────────────────────────────────────────────────────────────────────────┤
│  Footer  (fixed 48 bytes at end of file)                                │
│  ┌─────────────────────┬──────────────────────┬──────────────────┐     │
│  │   IndexHandle       │   FilterHandle       │   Magic Number   │     │
│  │  {Offset, Size}     │  {Offset, Size}      │   (8 bytes)      │     │
│  │  (varint encoded)   │  (varint encoded)    │  0x88e241b785f4  │     │
│  │                     │                      │     cff7         │     │
│  └─────────────────────┴──────────────────────┴──────────────────┘     │
│  Total footer = 48 bytes (padded); Magic = LevelDB magic number        │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘

InternalKey sort order (used everywhere in the engine):
  Primary:   UserKey ASC  (lexicographic)
  Secondary: SeqNo   DESC (higher sequence number = newer write = sorts first)

SSTableMeta (in-memory, tracked by MANIFEST):
  FileID    uint64   — monotonically increasing file identifier
  Level     int      — which LSM level this file belongs to
  FirstKey  []byte   — smallest InternalKey in the file
  LastKey   []byte   — largest InternalKey in the file
  FileSize  uint64   — total file size in bytes
  NumKeys   uint64   — number of entries (including tombstones)
  CreatedAt int64    — unix nanoseconds (used by TWCS for time-window grouping)
```

---

## Compaction Strategies

The compaction strategy can be changed at runtime via the `/compaction/style` endpoint or in `config.yaml`.

### Leveled Compaction Strategy (LCS) — Default

LCS maintains strict level size limits with non-overlapping key ranges at L1 and above.

```
Level sizes:
  L0:  0–4 SSTables (key ranges may overlap; direct from flushes)
  L1:  max 10 MB   (non-overlapping; sorted by key range)
  L2:  max 100 MB
  L3:  max 1 GB
  L4:  max 10 GB
  L5:  max 100 GB
  L6:  max 1 TB
  (multiplier = 10 per level)

Trigger: Any level i exceeds its max size budget.

Algorithm:
  1. Pick one SSTable from level i (round-robin / oldest-first)
  2. Find all overlapping SSTables in level i+1
  3. k-way merge via min-heap → write new sorted SSTables at level i+1
  4. Update MANIFEST: add new SSTables, delete old ones (atomic)
  5. Cascade if level i+1 now exceeds its budget
```

| Metric | Value |
|---|---|
| Write Amplification | ~10–30× (worst case across all levels) |
| Read Amplification | O(numLevels) ≈ 7 disk reads for a cache miss |
| Space Amplification | ~1.1× (minimal stale data) |
| Best for | Read-heavy and mixed workloads; predictable space usage |

### Size-Tiered Compaction Strategy (STCS)

STCS groups SSTables of similar size into tiers and merges them together.

```
Tier grouping: SSTables within [avg * 0.5, avg * 1.5] size form a tier.
Trigger: A tier accumulates >= minThreshold (default 4) members.

Algorithm:
  1. Group all SSTables by size bucket
  2. Find the bucket with >= minThreshold members
  3. Merge all SSTables in the bucket → one larger SSTable
  4. The result may join the next larger tier
```

| Metric | Value |
|---|---|
| Write Amplification | ~4–10× (lower than LCS) |
| Read Amplification | O(tiers) — can be high; SSTables may overlap |
| Space Amplification | ~4× during tier merge |
| Best for | Write-heavy workloads where reads are less frequent |

### Time-Window Compaction Strategy (TWCS)

TWCS is designed for time-series data. SSTables from the same time window are compacted using STCS within that window; windows never mix.

```
Parameters:
  windowSize: configurable (e.g., "1h", "24h")
  CreatedAt:  SSTableMeta.CreatedAt unix nanoseconds used for bucketing

Algorithm:
  Within the current (active) window:  use STCS
  Closed (old) windows:                no further compaction
  (Time-series data in old windows is effectively immutable)
```

| Metric | Value |
|---|---|
| Write Amplification | Low (STCS-like within each window) |
| Read Amplification | Low if queries respect time locality |
| Space Amplification | Low (old windows are never re-compacted) |
| Best for | Append-only time-series; TTL-based expiry workloads |

### Amplification Comparison

| Strategy | Write Amp | Read Amp | Space Amp | Use Case |
|---|---|---|---|---|
| LCS | 10–30× | ~7 | ~1.1× | Mixed/read-heavy |
| STCS | 4–10× | High | ~4× | Write-heavy |
| TWCS | Low | Low (temporal) | Low | Time-series |

---

## 7 Dashboard Panels

The frontend connects to `ws://localhost:3001/ws`, which the Elysia BFF bridges to the Go engine's WebSocket at `ws://localhost:8080/ws`. All panels update in real time as the engine runs.

### Panel 1: Write Path Visualizer

Animated flow diagram showing a write moving through every stage of the LSM write path. Displays the last 10 WAL appends as a scrolling log with CRC values, an animated skip-list visualization of the active MemTable (nodes, level pointers, current size vs max bar), and a "waterfall" animation from MemTable to L0 SSTable when a flush occurs. Live counters show writes/sec and bytes written/sec.

### Panel 2: LSM Level Tree

The primary structural view of the entire engine. L0 SSTables are shown as overlapping colored rectangles. L1–L6 display non-overlapping SSTables tiled across a key-range axis, colored by age (newer = brighter) and sized proportionally to file size. During compaction, selected input SSTables flash and animate into merged output SSTables. Click any SSTable to inspect its metadata: key range, file size, number of entries, Bloom filter size, and creation timestamp.

### Panel 3: Bloom Filter Interactive Panel

Split view: a bit-array visualization on the left (colored cells; set=colored, unset=white) and hash-function visualization on the right showing k=7 bit positions being set or checked for each key. Add a key to watch the k positions light up; query a key to see a "MAYBE" (present) or "DEFINITELY NOT" (absent) result. A false positive demo adds 1,000 keys then queries 1,000 different keys to measure the empirical false positive rate. A `bits_per_key` slider (6–20) updates the theoretical FP rate formula live.

### Panel 4: Read Path Tracer

For any GET request, shows the exact sequence of components consulted: MemTable check, each immutable MemTable, each L0 SSTable (with Bloom filter result), and the binary-search-selected SSTable at L1+. Bloom-skipped SSTables appear grayed out with a skip marker. The trace reports total disk reads and how many SSTables Bloom filters saved from disk access.

### Panel 5: Compaction Simulation

Interactive step-through of the compaction process. Select LCS, STCS, or TWCS via radio buttons, then step through: which SSTables are selected, the key-by-key merge iterator output with duplicate elimination decisions, tombstone GC decisions (dropped at bottom level vs. propagated), output SSTable construction, and the MANIFEST VersionEdit being written. After each compaction, level sizes and amplification stats update.

### Panel 6: Amplification Dashboard

Real-time gauges and line charts for all three amplification metrics fed by `EvtAmplification` events:
- **Write Amplification (WA):** bytes written to disk / bytes written by client. LCS: 10–30×; STCS: 4–10×.
- **Read Amplification (RA):** average disk reads per point query (cache misses).
- **Space Amplification (SA):** total disk bytes / actual live data bytes.

Side-by-side comparison mode lets you run the same workload under LCS then STCS and overlay the resulting graphs.

### Panel 7: Scenario Control + WAL Viewer

**Top:** Scenario dropdown with 8 pre-built demos, Run / Step / Speed / Reset controls, and a live write counter.

**Bottom left:** WAL viewer — scrolling list of WAL entries colored by type (PUT=green, DELETE=orange, FLUSH_MARKER=blue, CORRUPT=red), showing type, key, seqNo, CRC, and size.

**Bottom right:** Crash Recovery Simulator — "Simulate Crash" stops the engine mid-write and clears in-memory state; "Recover" re-opens from WAL + MANIFEST and shows step-by-step replay with a progress counter ("Replaying WAL: 423/500 entries…") and each key being restored.

**Available Scenarios:**

| Name | Description |
|---|---|
| `write_flush` | Fill MemTable → watch flush → SSTable appears in L0 |
| `bloom_demo` | Insert 10k keys; query missing keys; count false positives |
| `compaction_lcl` | Fill L0 to threshold; watch L0→L1 leveled compaction |
| `compaction_stcs` | Demonstrate tiered compaction with size-bucket grouping |
| `crash_recovery` | Simulate crash mid-write; verify WAL replay recovery |
| `tombstone_gc` | Delete keys; watch tombstones GC'd during bottom-level compaction |
| `range_scan` | Demonstrate merge iterator across all levels |
| `amplification` | Measure WA/RA/SA side-by-side for LCS vs STCS |

---

## API Endpoints

The Go backend serves all endpoints directly on `:8080`. The Elysia BFF at `:3001` proxies them transparently.

### Core Engine Operations

| Method | Path | Description |
|---|---|---|
| `POST` | `/db/put` | Write a key-value pair. Body: `{"key": "...", "value": "..."}` |
| `GET` | `/db/get` | Read a key. Query: `?key=...`. Returns `{"key","value","found"}` |
| `DELETE` | `/db/delete` | Delete (tombstone) a key. Query: `?key=...` |
| `GET` | `/db/scan` | Range scan. Query: `?start=...&end=...&limit=1000` |
| `POST` | `/db/batch` | Write batch. Body: `{"entries":[{"key","value","delete":bool}]}` |

### Internals (for visualization)

| Method | Path | Description |
|---|---|---|
| `GET` | `/levels` | All 7 levels with SSTable list, key ranges, file sizes |
| `GET` | `/bloom/:fileID` | Bloom filter stats for a specific SSTable (bits_per_key, estimated FP rate) |
| `GET` | `/amplification` | Live WA/RA/SA values + total SSTable bytes and file count |
| `GET` | `/stats` | Full engine stats snapshot (seqNo, file counts, cache hit rate) |
| `GET` | `/health` | Health check. Returns `{"status":"ok"}` |

### Compaction Control

| Method | Path | Description |
|---|---|---|
| `POST` | `/compaction/force` | Force compaction. Body: `{"level": 0}` |
| `POST` | `/compaction/style` | Switch compaction strategy. Body: `{"style": "leveled"}` (leveled, size-tiered, time-window) |

### Benchmarks and Scenarios

| Method | Path | Description |
|---|---|---|
| `POST` | `/bench/run` | Run a workload. Body: `{"type","num_keys","value_size","read_write_ratio"}` |
| `GET` | `/scenarios` | List all available pre-built scenarios with descriptions |
| `POST` | `/scenarios/:name/run` | Run a named scenario (e.g., `/scenarios/write_flush/run`) |

### WebSocket

| URL | Description |
|---|---|
| `ws://localhost:8080/ws` | Engine event stream. Emits JSON `{"type","ts","extra"}` for every internal event. |

**Workload types for `/bench/run`:**

| Type | Description |
|---|---|
| `sequential_write` | Keys written in ascending order |
| `random_write` | Keys written in random order |
| `zipf_read` | Reads following a Zipfian (hot-key) distribution |
| `mixed` | Combined reads and writes at the configured ratio |
| `compaction_stress` | Rapid writes designed to trigger repeated compaction cycles |
| `point_delete` | Write N keys then delete them all |

---

## Crash Recovery

On `engine.Open()`, the following recovery procedure runs automatically:

```
1. Read CURRENT file → find active MANIFEST filename
2. Replay MANIFEST VersionEdits → reconstruct Version
   (all SSTable metadata for all levels)
3. Locate WAL file(s) listed in MANIFEST
4. Replay WAL records into MemTable:
   a. Verify CRC32 of each record header + payload
   b. Skip the final truncated record (partial write before crash — expected)
   c. Apply EntrySet and EntryDelete in sequence-number order
5. Validate all SSTable files referenced in MANIFEST exist on disk
6. Open SSTable readers; load index blocks and Bloom filters into memory
7. Scan disk for orphaned SSTable files not in MANIFEST → delete them
   (written during an interrupted flush or compaction before MANIFEST update)
8. Start FlushWorker and Compactor background goroutines
9. Ready to serve requests
```

**Correctness guarantees:**
- Every acknowledged write has a CRC-valid WAL record on disk before returning.
- New SSTables are synced before MANIFEST is updated; old SSTables deleted after.
- A crash at any point leaves the engine in a recoverable state with no silent data loss.

---

## Performance Targets

These are the design targets for the engine on modern SSD hardware:

| Metric | Target |
|---|---|
| Write throughput (sequential, `sync_wal: false`) | > 200,000 ops/s |
| Write throughput (with fsync, SSD) | > 20,000 ops/s |
| Point read — key in L1, warm block cache | < 100 µs |
| Point read — key not found (Bloom short-circuit) | < 50 µs |
| Bloom filter false positive rate (`bloom_bits_per_key: 10`) | < 1% |
| MemTable flush (64 MB) | < 200 ms |
| L0 → L1 compaction (4 L0 files) | < 2 s |
| Block cache hit rate (random workload) | > 70% |
| Range scan (1,000 keys) | < 10 ms |

---

## Running Benchmarks

```bash
# Run all benchmarks with memory allocation stats
make bench

# Run only a specific benchmark package
go test ./internal/wal/... -bench=. -benchmem -run='^$'
go test ./internal/sstable/... -bench=. -benchmem -run='^$'
go test ./internal/compaction/... -bench=. -benchmem -run='^$'

# Run with race detector (slower but catches concurrency bugs)
make test-race
```

**Example benchmark output** (run `make bench` to measure on your hardware):

```
goos: darwin
goarch: arm64
pkg: lsm-engine/internal/wal
BenchmarkWALAppend-10             xxxxxx     xxx ns/op    xxx B/op    x allocs/op
BenchmarkWALAppendSync-10         xxxxxx     xxx ns/op    xxx B/op    x allocs/op

pkg: lsm-engine/internal/memtable
BenchmarkSkipListInsert-10        xxxxxx     xxx ns/op    xxx B/op    x allocs/op
BenchmarkSkipListGet-10           xxxxxx     xxx ns/op    xxx B/op    x allocs/op

pkg: lsm-engine/internal/sstable
BenchmarkSSTableBuilder-10        xxxxxx     xxx ns/op    xxx B/op    x allocs/op
BenchmarkSSTableReaderGet-10      xxxxxx     xxx ns/op    xxx B/op    x allocs/op
BenchmarkBloomFilter-10           xxxxxx     xxx ns/op    xxx B/op    x allocs/op

pkg: lsm-engine/internal/engine
BenchmarkEngineSequentialWrite-10 xxxxxx     xxx ns/op    xxx B/op    x allocs/op
BenchmarkEngineRandomRead-10      xxxxxx     xxx ns/op    xxx B/op    x allocs/op
BenchmarkEngineMixed-10           xxxxxx     xxx ns/op    xxx B/op    x allocs/op
```

Run `make bench` to fill in the actual numbers for your machine.

---

## Correctness Properties

The engine is designed around the following invariants, all verified by the integration test suite:

1. **WAL durability** — Every acknowledged write has a CRC-valid record in the WAL before the call returns.
2. **Recovery completeness** — After crash recovery, all WAL entries with valid CRC are present in the MemTable; MANIFEST accurately reflects all SSTable files on disk.
3. **Monotonic sequence numbers** — `seqNo` never decreases; newer writes have strictly higher seqNos.
4. **Compaction correctness** — For every key, the highest-seqNo version is preserved after compaction; no live data is lost.
5. **Tombstone semantics** — A tombstone for key K at seqNo S hides all versions of K with seqNo < S. Tombstones are GC'd only at the bottom level (where no older versions can exist below them).
6. **Level non-overlap (L1+)** — At any level L ≥ 1, no two SSTables have overlapping key ranges, enabling single-SSTable binary search per level.
7. **Bloom filter soundness** — `MayContain(key) == false` guarantees the key is NOT in the SSTable (no false negatives; only false positives are possible).
8. **MANIFEST atomicity** — MANIFEST is updated only after all output SSTables are synced; old SSTables are deleted only after MANIFEST is updated.
9. **Read isolation** — Reads see a consistent snapshot (by seqNo); concurrent writes do not corrupt in-progress reads.

---

## Concepts Covered

| Area | Concepts |
|---|---|
| Write path | WAL append + fsync, MemTable skip-list insert, immutable flush trigger |
| WAL | 32KB block format, CRC32 checksums, record fragmentation (Full/First/Middle/Last), crash recovery replay |
| MemTable | Skip list O(log n), tombstone markers, range iteration, mutable → immutable rotation |
| SSTable format | Data blocks with prefix compression and restart points, Filter block (Bloom), Index block, Footer + BlockHandles |
| Bloom filters | k-hash bit array, optimal bits-per-key, false positive probability math, per-SSTable in-memory registry |
| Read path | MemTable → immutable MemTables → L0 → L1+ with Bloom short-circuit and binary search |
| Compaction | STCS, LCS, TWCS; write/read/space amplification tradeoffs; k-way merge via min-heap |
| MANIFEST | Append-only VersionEdit log; atomic compaction commits; SSTable level tracking |
| Crash recovery | WAL replay → MemTable rebuild; MANIFEST replay → SSTable level reconstruction |
| Block Cache | LRU eviction, CacheKey = (FileID, BlockOffset), hit-rate tracking |
| Event system | Non-blocking fan-out EventBus → WebSocket → real-time dashboard |
