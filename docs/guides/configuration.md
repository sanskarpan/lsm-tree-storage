# Configuration Reference

Configuration is loaded from `config.yaml` at startup. Every field has a corresponding environment variable for container deployments. Environment variables take precedence over the YAML file.

---

## Engine config (`internal/engine/config.go`)

The `Config` struct in `internal/engine/config.go` controls all engine behaviour. Defaults are provided by `DefaultConfig(dataDir)`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `DataDir` | `string` | `./data` | Root directory for SSTables, MANIFEST, WAL, and CURRENT file |
| `MemTableSize` | `int64` | 67108864 (64 MB) | MemTable flush threshold in bytes; when `ApproximateSize()` reaches this value, the mutable MemTable is promoted to the immutable queue |
| `BlockSize` | `int` | 4096 (4 KB) | Target size for each SSTable data block; the last block in a file may be larger |
| `BloomBitsPerKey` | `int` | 10 | Bits allocated per key in the per-SSTable Bloom filter; 10 → k≈6–7 hash functions → ~1% false positive rate |
| `SSTMaxSize` | `uint64` | 67108864 (64 MB) | Maximum size of a single SSTable file; the builder starts a new file when this is exceeded |
| `SyncWAL` | `bool` | `true` | Whether `WAL.Append` calls `file.Sync()` after every write; set to `false` for higher throughput at the cost of up-to-one-block durability |
| `MaxOpenFiles` | `int` | 1000 | Bound on simultaneously open `SSTableReader` file descriptors (LRU table cache) |
| `BlockCacheSize` | `int64` | 134217728 (128 MB) | LRU block cache capacity in bytes; shared across all SSTable readers |
| `MaxLevels` | `int` | 7 | Number of LSM levels (L0 through L6) |
| `LevelSizeMultiplier` | `int` | 10 | Size ratio between consecutive levels; L1 target = `SSTMaxSize`, L2 = 10×, L3 = 100×, etc. |
| `Level0FileNumCompactionTrigger` | `int` | 4 | Number of L0 SSTables that triggers a compaction cycle |
| `Level0StopWritesTrigger` | `int` | 12 | Number of L0 SSTables at which all writes stall until compaction catches up |
| `MaxImmutableMemTables` | `int` | 2 | Maximum immutable MemTables queued before `rotateMemTable` blocks (write stall) |
| `MaxValueSize` | `int64` | 1048576 (1 MB) | Maximum value size in bytes; `Put` and `Write` reject larger values |
| `CompactionStyle` | `string` | `"leveled"` | Active compaction strategy: `"leveled"`, `"size-tiered"`, or `"time-window"` |
| `TimeWindowSize` | `time.Duration` | `1h` | Window size for TWCS; only meaningful when `CompactionStyle="time-window"` |
| `WriteStallTimeout` | `time.Duration` | 30s | Maximum wait in `rotateMemTable` for the flush worker to drain an immutable MemTable before returning an error |

---

## Environment variables

### Backend server

| Variable | Default | Description |
|----------|---------|-------------|
| `ADDR` | `127.0.0.1:8080` | HTTP bind address |
| `PORT` | unset | Alternative port-only input (overrides port in `ADDR`) |
| `DATA_DIR` | `./data` | Storage root; sets `Config.DataDir` |
| `CONFIG_PATH` | `config.yaml` | Path to the YAML configuration file |
| `API_TOKEN` | unset | Admin bearer token; when set, all routes except `/health` require `Authorization: Bearer <token>` |
| `API_TOKEN_READWRITE` | unset | Read-write bearer token; grants write + read but not admin operations |
| `API_TOKEN_READONLY` | unset | Read-only bearer token; grants `GET /db/get`, `GET /db/scan`, `GET /stats`, `GET /health` |
| `METRICS_TOKEN` | falls back to `API_TOKEN` | Bearer token for `GET /metrics`; allows a separate token for Prometheus scraping |
| `TLS_CERT_FILE` | unset | PEM certificate file; enables `ListenAndServeTLS` when set together with `TLS_KEY_FILE` |
| `TLS_KEY_FILE` | unset | PEM private key file |
| `ALLOWED_ORIGINS` | same-origin only | Comma-separated allowed CORS origins |
| `LOG_FORMAT` | `json` | Log output format: `json` or `text` |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `ALLOW_INSECURE_REMOTE` | unset | Allows non-loopback bind without `API_TOKEN`; explicitly opt-in |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | OTLP gRPC endpoint (e.g. `localhost:4317`); tracing disabled if unset |

### Cluster variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLUSTER_ENABLED` | unset | Set to `1` to enable Raft-backed clustered mode |
| `CLUSTER_NODE_ID` | `standalone` | Unique identifier for this node in the cluster |
| `CLUSTER_DATA_DIR` | `<DATA_DIR>/_cluster` | Raft metadata, log, and snapshot store |
| `CLUSTER_BIND_ADDR` | unset | Local Raft TCP listen address (e.g. `127.0.0.1:7001`) |
| `CLUSTER_ADVERTISE_ADDR` | `CLUSTER_BIND_ADDR` | Peer-visible Raft address (use public IP in multi-host deployments) |
| `CLUSTER_CLIENT_ADDR` | backend bind addr | Client-visible HTTP address used in leader-redirect responses |
| `CLUSTER_BOOTSTRAP` | unset | Set to `1` on the first startup to bootstrap the initial voter set |
| `CLUSTER_PEERS` | unset | Peer list: `nodeID@rpcAddr[@clientAddr],...` |
| `CLUSTER_ELECTION_TIMEOUT` | `3s` | Raft leader election timeout |
| `CLUSTER_HEARTBEAT_INTERVAL` | `500ms` | Leader heartbeat interval |
| `CLUSTER_COMMIT_TIMEOUT` | `250ms` | AppendEntries commit tick |
| `CLUSTER_APPLY_TIMEOUT` | `10s` | Maximum wait for a client write to be applied |
| `CLUSTER_SNAPSHOT_INTERVAL` | `5m` | How often the FSM checks whether to take a snapshot |
| `CLUSTER_SNAPSHOT_MIN_ENTRIES` | `10000` | Minimum unapplied log entries before a snapshot is triggered |
| `CLUSTER_SNAPSHOT_RETAIN` | `2` | Number of snapshots to keep on disk |
| `CLUSTER_TRAILING_LOGS` | `256` | Raft log entries retained after each snapshot |
| `CLUSTER_SHARD_COUNT` | `1` | Number of local Raft groups (shards) |
| `CLUSTER_SHARD_PORT_STRIDE` | `100` | Port offset between per-shard Raft listeners |
| `CLUSTER_ROUTING_SLOTS` | `256` | Fixed slot count for the shared shard routing map |
| `CLUSTER_REBALANCE_INTERVAL` | `30s` | Background slot rebalancing interval |
| `CLUSTER_REBALANCE_THRESHOLD_BYTES` | `67108864` (64 MB) | Minimum byte skew before moving a slot |
| `CLUSTER_REBALANCE_MAX_SLOTS` | `1` | Max slots moved per rebalance pass |
| `CLUSTER_TLS_ENABLED` | unset | Set to `1` to enable TLS for inter-node Raft transport |
| `CLUSTER_TLS_CERT_FILE` | unset | PEM certificate for the Raft transport |
| `CLUSTER_TLS_KEY_FILE` | unset | PEM private key for the Raft transport |
| `CLUSTER_TLS_CA_FILE` | unset | PEM CA bundle for mTLS peer verification |
| `CLUSTER_TLS_SERVER_NAME` | unset | TLS server name override |
| `CLUSTER_TLS_INSECURE_SKIP_VERIFY` | unset | Disable peer TLS verification; debugging only |

### Frontend / BFF

| Variable | Default | Description |
|----------|---------|-------------|
| `HOST` | `127.0.0.1` | BFF bind host |
| `PORT` | `3001` | BFF port |
| `BACKEND_URL` | `http://127.0.0.1:8080` | Backend HTTP origin for proxying |
| `BACKEND_WS_URL` | `ws://127.0.0.1:8080/ws` | Backend WebSocket origin for bridging |
| `API_TOKEN` | unset | Backend bearer token forwarded by the BFF |
| `BACKEND_API_TOKEN` | falls back to `API_TOKEN` | Explicit override for backend token |
| `BFF_BASIC_AUTH` | unset | HTTP Basic auth in `user:password` form for browser-facing BFF routes |
| `BACKEND_REQUEST_TIMEOUT_MS` | `15000` | Proxy timeout in milliseconds |
| `ALLOW_REMOTE_BFF` | unset | Allows non-loopback BFF bind |
| `ALLOW_INSECURE_REMOTE_BFF` | unset | Allows remote BFF bind without `BFF_BASIC_AUTH` |

---

## config.yaml example

```yaml
data_dir: "./data"
mem_table_size: 67108864       # 64 MB
max_immutable_memtables: 2
block_size: 4096               # 4 KB
sst_max_size: 67108864         # 64 MB
max_open_files: 1000
bloom_bits_per_key: 10
block_cache_size: 134217728    # 128 MB
max_levels: 7
level_size_multiplier: 10
level0_file_num_compaction_trigger: 4
level0_stop_writes_trigger: 12
compaction_style: "leveled"    # leveled | size-tiered | time-window
time_window_size: "1h"
sync_wal: true

cluster:
  enabled: false
  node_id: "node-1"
  bind_address: "127.0.0.1:7001"
  bootstrap: false
```
