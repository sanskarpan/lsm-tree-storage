# Prometheus Metrics Reference

Metrics are exposed at `GET /metrics` in Prometheus text/OpenMetrics format. The endpoint is bearer-token authenticated when `METRICS_TOKEN` or `API_TOKEN` is configured.

Metrics are registered in `internal/observability/metrics.go` using the `prometheus/client_golang` library.

---

## Metrics endpoint

```bash
curl -s http://localhost:8080/metrics \
  -H 'Authorization: Bearer <metrics-token>'
```

Prometheus scrape config (see also `deploy/prometheus/prometheus.yml`):

```yaml
scrape_configs:
  - job_name: lsm-storage
    static_configs:
      - targets: ['backend:8080']
    authorization:
      credentials: <METRICS_TOKEN>
```

---

## Engine internals

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lsm_engine_ready` | Gauge | — | 1 when the engine reports ready; 0 otherwise |
| `lsm_engine_seq_no` | Gauge | — | Current global sequence number |
| `lsm_engine_total_sst_files` | Gauge | — | Total SSTable files tracked by the MANIFEST |
| `lsm_engine_total_sst_bytes` | Gauge | — | Total SSTable data size in bytes |
| `lsm_engine_memtable_size_bytes` | Gauge | — | Approximate mutable MemTable size in bytes |
| `lsm_engine_immutables` | Gauge | — | Number of immutable MemTables waiting to flush |
| `lsm_engine_wal_files` | Gauge | — | Number of WAL files currently tracked |
| `lsm_engine_l0_sst_files` | Gauge | — | Number of SSTable files in L0 (pre-compaction level) |
| `lsm_bg_error` | Gauge | — | 1 when a background flush or compaction error has stalled the engine; 0 otherwise |

!!! note
    `lsm_engine_l0_sst_files` and `lsm_bg_error` are derived from `node.Version()` and `node.HealthStatus()` respectively. Both are now registered in `internal/observability/metrics.go`.

---

## Block cache

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lsm_cache_entries` | Gauge | — | Current block-cache entry count |
| `lsm_cache_hit_rate` | Gauge | — | Block-cache hit rate ratio (0.0 – 1.0) |

---

## WebSocket

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lsm_ws_clients` | Gauge | — | Number of currently connected WebSocket clients |

---

## Cluster (Raft)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lsm_cluster_enabled` | Gauge | — | 1 when clustered mode is active |
| `lsm_cluster_term` | Gauge | — | Current Raft term |
| `lsm_cluster_commit_index` | Gauge | — | Committed Raft log index |
| `lsm_cluster_last_applied_index` | Gauge | — | Last applied log index recorded by the FSM |
| `lsm_cluster_role_standalone` | Gauge | `role="standalone"` | 1 when node role is standalone |
| `lsm_cluster_role_leader` | Gauge | `role="leader"` | 1 when node is the Raft leader |
| `lsm_cluster_role_follower` | Gauge | `role="follower"` | 1 when node is a follower |
| `lsm_cluster_role_candidate` | Gauge | `role="candidate"` | 1 when node is a candidate |

The four role gauges form a one-hot vector: exactly one is 1 at any time.

---

## HTTP layer

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lsm_http_requests_total` | Counter | `method`, `route`, `status` | Total HTTP requests served |
| `lsm_http_request_duration_seconds` | Histogram | `method`, `route` | Request latency (standard Prometheus buckets) |

`route` is the registered path pattern (e.g., `/db/put`), not the full URL. This prevents cardinality explosion from key values in query strings.

---

## Events

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `lsm_events_total` | Counter | `type` | Total published engine events by type |

Event types tracked: `wal_append`, `wal_sync`, `memtable_insert`, `memtable_full`, `memtable_rotate`, `flush_start`, `flush_complete`, `sstable_created`, `sstable_deleted`, `compaction_start`, `compaction_pick`, `compaction_complete`, `tombstone_dropped`, `bloom_check`, `bloom_hit`, `bloom_miss`, `bloom_fp`, `cache_hit`, `cache_miss`, `amplification`, and others.

---

## Go runtime and process metrics

Registered automatically via `collectors.NewGoCollector()` and `collectors.NewProcessCollector()`:

| Metric prefix | Description |
|---------------|-------------|
| `go_goroutines` | Number of goroutines |
| `go_gc_duration_seconds` | GC pause duration histogram |
| `go_memstats_*` | Go memory allocator statistics |
| `process_resident_memory_bytes` | Process RSS |
| `process_open_fds` | Open file descriptors |
| `process_max_fds` | File descriptor limit |
