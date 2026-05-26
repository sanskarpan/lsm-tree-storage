# Operations Guide

This repository supports both single-node and replicated deployments.
Operational safety still depends on backup, monitoring, and disciplined
rollout procedures.

## Deployment Profile

- Backend: Go storage/API server
- Frontend: Bun/Elysia BFF + React dashboard
- Storage model: local LSM engine per shard, optionally replicated by one Raft group per shard
- Availability model: single-node restart/restore or multi-node leader failover per shard

## Environment Matrix

### Backend

| Variable | Default | Purpose |
|---|---|---|
| `ADDR` | `127.0.0.1:8080` | Bind address for the Go server |
| `PORT` | unset | Alternative port-only bind input |
| `DATA_DIR` | `./data` | Storage root |
| `CONFIG_PATH` | `config.yaml` | YAML config file |
| `API_TOKEN` | unset | Bearer auth for all backend routes except `/health` |
| `METRICS_TOKEN` | falls back to `API_TOKEN` | Bearer auth for `/metrics` |
| `ALLOWED_ORIGINS` | same-origin only | Allowed browser origins |
| `LOG_FORMAT` | `json` | `json` or `text` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `ALLOW_INSECURE_REMOTE` | unset | Overrides remote-bind protection when no API token is set |
| `CLUSTER_ENABLED` | unset | Enables Raft-backed clustered mode |
| `CLUSTER_NODE_ID` | `standalone` | Local node identifier |
| `CLUSTER_DATA_DIR` | `<data_dir>/_cluster` | Raft metadata and snapshot store |
| `CLUSTER_BIND_ADDR` | unset | Local Raft TCP bind address |
| `CLUSTER_ADVERTISE_ADDR` | `CLUSTER_BIND_ADDR` | Peer-visible Raft address |
| `CLUSTER_CLIENT_ADDR` | backend bind addr | Client-visible HTTP address for redirects |
| `CLUSTER_BOOTSTRAP` | unset | Bootstraps the initial static voter set |
| `CLUSTER_PEERS` | unset | `nodeID@rpcAddr[@clientAddr],...` peer list |
| `CLUSTER_ELECTION_TIMEOUT` | `3s` | Raft election timeout |
| `CLUSTER_HEARTBEAT_INTERVAL` | `500ms` | Raft heartbeat timeout / leader lease |
| `CLUSTER_COMMIT_TIMEOUT` | `250ms` | AppendEntries commit tick |
| `CLUSTER_APPLY_TIMEOUT` | `10s` | Client apply wait budget |
| `CLUSTER_SNAPSHOT_INTERVAL` | `5m` | Snapshot check interval |
| `CLUSTER_SNAPSHOT_MIN_ENTRIES` | `10000` | Minimum unapplied logs before snapshot |
| `CLUSTER_SNAPSHOT_RETAIN` | `2` | Number of snapshots to retain |
| `CLUSTER_TRAILING_LOGS` | `256` | Raft log entries kept after snapshot |
| `CLUSTER_SHARD_COUNT` | `1` | Number of local shards / Raft groups to host |
| `CLUSTER_SHARD_PORT_STRIDE` | `100` | Port offset between shard-local Raft listeners |
| `CLUSTER_ROUTING_SLOTS` | `256` | Fixed slot count for shared shard routing |
| `CLUSTER_REBALANCE_INTERVAL` | `30s` | Background interval for slot rebalancing |
| `CLUSTER_REBALANCE_THRESHOLD_BYTES` | `67108864` | Minimum shard byte skew before moving a slot |
| `CLUSTER_REBALANCE_MAX_SLOTS` | `1` | Max slots moved per rebalance pass |
| `CLUSTER_TLS_ENABLED` | unset | Enables TLS for inter-node Raft transport |
| `CLUSTER_TLS_CERT_FILE` | unset | PEM certificate for Raft transport |
| `CLUSTER_TLS_KEY_FILE` | unset | PEM private key for Raft transport |
| `CLUSTER_TLS_CA_FILE` | unset | PEM CA bundle for peer verification / mTLS |
| `CLUSTER_TLS_SERVER_NAME` | unset | Explicit TLS server name override |
| `CLUSTER_TLS_INSECURE_SKIP_VERIFY` | unset | Disables peer verification; debugging only |

### Frontend / BFF

| Variable | Default | Purpose |
|---|---|---|
| `HOST` | `127.0.0.1` | BFF bind host |
| `PORT` | `3001` | BFF port |
| `BACKEND_URL` | `http://127.0.0.1:8080` | Backend HTTP origin |
| `BACKEND_WS_URL` | `ws://127.0.0.1:8080/ws` | Backend WebSocket origin |
| `API_TOKEN` | unset | Backend bearer token to forward |
| `BACKEND_API_TOKEN` | falls back to `API_TOKEN` | Explicit backend bearer token override |
| `BFF_BASIC_AUTH` | unset | HTTP Basic auth in `user:password` form |
| `BACKEND_REQUEST_TIMEOUT_MS` | `15000` | Backend proxy timeout |
| `ALLOW_REMOTE_BFF` | unset | Allows non-loopback BFF bind |
| `ALLOW_INSECURE_REMOTE_BFF` | unset | Allows remote BFF exposure without `BFF_BASIC_AUTH` |

## Probes

- `/health`
  - shallow process liveness
  - always returns `200` when the HTTP server is alive
- `/ready`
  - readiness signal backed by engine state
  - returns `503` when the manifest or active WAL is missing, immutable backlog exceeds config, or L0 has reached the stop-writes threshold
- `/metrics`
  - Prometheus endpoint
  - bearer-protected when `METRICS_TOKEN` or `API_TOKEN` is set
- `/cluster/status`
  - role, term, commit index, last applied index, and leader identity
- `/cluster/leader`
  - leader ID and client address
- `/cluster/peers`
  - current peer registry and observed suffrage
- `/cluster/shards`
  - role and leader information per shard
- `/cluster/membership/add`
  - leader-only runtime add-voter endpoint
- `/cluster/membership/remove`
  - leader-only runtime remove-server endpoint
- `/cluster/readiness`
  - combined engine and cluster readiness
- `/db/get?consistency=linearizable|eventual`
  - `linearizable` is the default and may route to the leader
  - `eventual` may be served from the local follower state after the last applied log entry

## Logging

- Backend request logs are structured JSON by default.
- Each HTTP response includes `X-Request-ID`.
- Standard-library `log.Printf` paths are bridged into structured logs so flush and compaction warnings still surface in one log stream.

## Metrics

Prometheus metrics include:

- HTTP request totals and durations by route/method/status
- engine readiness
- sequence number
- SSTable file and byte counts
- memtable size
- immutable memtable backlog
- WAL file count
- cache size and hit rate
- WebSocket client count
- published engine event totals by type
- cluster enabled flag
- current term
- commit index
- last applied index
- one-hot role gauges
- shard-aware status via `/cluster/shards`

## Backup

Create a backup archive:

```bash
./scripts/backup.sh ./data ./backups
```

This writes:

- `lsm-backup-<timestamp>.tar.gz`
- `lsm-backup-<timestamp>.tar.gz.sha256`

The archive includes:

- `data/`
- `metadata.json`
- `config.yaml` when present

## Restore

Restore into an empty target directory:

```bash
./scripts/restore.sh ./backups/lsm-backup-20260526T000000Z.tar.gz ./restored-data
```

Replace an existing target directory:

```bash
./scripts/restore.sh ./backups/lsm-backup-20260526T000000Z.tar.gz ./data --force
```

## Load / Soak Validation

Run mixed read/write load through the browser-facing BFF:

```bash
TARGET=http://127.0.0.1:3001 \
DURATION=60s \
CONCURRENCY=16 \
WRITE_PERCENT=40 \
./scripts/loadtest.sh
```

Direct backend load with bearer auth:

```bash
TARGET=http://127.0.0.1:8080/api/v1 \
API_TOKEN=change-me \
go run ./cmd/loadtest -target http://127.0.0.1:8080/api/v1
```

Note: the backend exposes `/db/*` directly at both the legacy root and `/api/v1/*`.

## Cluster Caveats

The clustered mode is intentionally conservative:

- cross-shard write batches now use a local transaction coordinator across the shard groups on a node
- shard count can scale the keyspace across multiple local Raft groups, and hot slots are automatically rebalanced through a shared slot map stored in cluster metadata
- TLS is optional and should be enabled outside trusted lab networks
