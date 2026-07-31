# LSM Storage Engine — Monitoring Stack

Complete observability stack for the LSM-Tree storage engine using Prometheus, Grafana, and Alertmanager.

## Components

| Service | Port | Purpose |
|---|---|---|
| Prometheus | 9090 | Metrics collection, alerting rules, SLO recording rules |
| Grafana | 3000 | Dashboards (auto-provisioned from `grafana/`) |
| Alertmanager | 9093 | Alert routing, deduplication, silences |

## Quick Start

### Prerequisites

Your `.env` file (project root) must contain:
```
API_TOKEN=...
METRICS_TOKEN=...
```

`METRICS_TOKEN` is the bearer token the backend requires on its `/metrics` endpoint. Both tokens are already required by the main `docker-compose.yml`.

### Start the full stack

```bash
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d
```

### Access the UIs

| UI | URL | Default credentials |
|---|---|---|
| Grafana | http://localhost:3000 | admin / admin |
| Prometheus | http://localhost:9090 | none |
| Alertmanager | http://localhost:9093 | none |

Change the Grafana admin password on first login or set `GRAFANA_ADMIN_PASSWORD` in `.env` before starting.

### Stop the monitoring stack only

```bash
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml stop prometheus grafana alertmanager
```

### Stop everything

```bash
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml down
```

Add `-v` to also remove persistent volumes (Prometheus TSDB, Grafana state).

---

## Metrics Scraped

The backend exposes Prometheus metrics at `http://backend:8080/metrics` (bearer-token protected).

**HTTP layer**
- `lsm_http_requests_total{method, route, status}` — request counter
- `lsm_http_request_duration_seconds{method, route}` — latency histogram

**Engine internals**
- `lsm_engine_ready` — readiness flag (1 = ready)
- `lsm_engine_seq_no` — global sequence number
- `lsm_engine_total_sst_files` — total SSTable file count
- `lsm_engine_total_sst_bytes` — total SSTable data size
- `lsm_engine_memtable_size_bytes` — mutable memtable footprint
- `lsm_engine_immutables` — immutable memtables awaiting flush
- `lsm_engine_wal_files` — WAL file count

**Block cache**
- `lsm_cache_entries` — entry count
- `lsm_cache_hit_rate` — hit rate (0–1)

**Cluster (Raft)**
- `lsm_cluster_enabled`, `lsm_cluster_term`, `lsm_cluster_commit_index`, `lsm_cluster_last_applied_index`
- `lsm_cluster_role_{standalone,leader,follower,candidate}`

**Events**
- `lsm_events_total{type}` — engine event counter (compaction, flush, WAL sync, etc.)

**Go runtime** (from `GoCollector` + `ProcessCollector`)
- `go_goroutines`, `go_gc_duration_seconds`, `go_memstats_*`
- `process_resident_memory_bytes`, `process_open_fds`, `process_max_fds`

---

## Alerts

Alert rules live in `monitoring/rules/alerts.yml`. They are grouped by domain:

### Availability
| Alert | Condition | Severity |
|---|---|---|
| `LSMBackendDown` | `up == 0` for > 1 m | critical |
| `LSMHighErrorRate` | 5xx rate > 1% over 5 m | critical |
| `LSMBgErrorActive` | `lsm_bg_error == 1` | critical |

### Latency
| Alert | Condition | Severity |
|---|---|---|
| `LSMHighWriteLatencyP99` | P99 write latency > 100 ms for 5 m | warning |
| `LSMHighReadLatencyP99` | P99 read latency > 50 ms for 5 m | warning |

### Storage Health
| Alert | Condition | Severity |
|---|---|---|
| `LSML0FileCountHigh` | L0 files > 8 | warning |
| `LSML0FileCountCritical` | L0 files > 10 | critical |
| `LSMFlushQueueDeep` | Immutables > 4 for > 2 m | warning |
| `LSMWALFilesHigh` | WAL files > 5 | warning |
| `LSMMemtableSizeHigh` | Memtable > 80% of 64 MiB | warning |
| `LSMImmutableCountHigh` | Immutables > 1 for > 5 m | warning |

### Compaction
| Alert | Condition | Severity |
|---|---|---|
| `LSMCompactionLagging` | Total SSTable bytes > 9.7 GiB | warning |
| `LSMNoCompactionActivity` | No compaction events for 30 m while files > 2 | warning |

### Resources
| Alert | Condition | Severity |
|---|---|---|
| `LSMDiskUsageHigh` | Total SSTable bytes > 8 GiB | warning |
| `LSMHighMemoryUsage` | Process RSS > 2 GiB | warning |

> **Note:** `LSMBgErrorActive`, `LSML0FileCountHigh`, and `LSML0FileCountCritical` reference metrics
> (`lsm_bg_error`, `lsm_engine_l0_sst_files`) that are not yet exposed by the engine.
> These alerts will remain inactive until those gauges are added to
> `internal/observability/metrics.go`.

---

## SLO Definitions

Defined in `monitoring/rules/slos.yml`.

| SLO | Target | Error Budget |
|---|---|---|
| Write Availability | 99.9% of writes succeed (< 0.1% 5xx) | 43.8 min / 30 days |
| Read Availability | 99.9% of reads succeed (< 0.1% 5xx) | 43.8 min / 30 days |
| Write Latency P99 | 99% of writes complete in < 100 ms | 7.3 h / 30 days |
| Read Latency P99 | 99% of reads complete in < 50 ms | 7.3 h / 30 days |

### Burn-Rate Alerts

Each SLO has two multi-window burn-rate alerts:

| Alert suffix | Windows | Burn rate | Meaning |
|---|---|---|---|
| `FastBurn` | 1 h + 5 m | 14.4× | Budget exhausted in ~2 h — page immediately |
| `SlowBurn` | 6 h + 30 m | 6.0× | Budget exhausted in ~5 d — open a ticket |

"Write" requests: routes `/db/put` and `/db/batch`
"Read"  requests: routes `/db/get` and `/db/scan`

---

## Adding or Editing Alerts

1. Edit `monitoring/rules/alerts.yml` or `monitoring/rules/slos.yml`.
2. Validate with `promtool check rules monitoring/rules/*.yml` (install `promtool` via the Prometheus binary bundle).
3. Reload Prometheus without restart:
   ```bash
   curl -X POST http://localhost:9090/-/reload
   ```
   This works because `--web.enable-lifecycle` is passed in `docker-compose.monitoring.yml`.

---

## Grafana Dashboard

The `LSM Storage Engine — Overview` dashboard (UID `lsm-overview`) is auto-provisioned at startup from `monitoring/grafana/dashboards/lsm-overview.json`.

Rows:
1. **Overview** — total request rate, error %, P99 latency, backend readiness
2. **Write Path** — write throughput, write latency heatmap, WAL files, memtable state
3. **Read Path** — read throughput, read latency heatmap, cache hit rate
4. **Storage** — SSTable file count (with thresholds), immutable memtable count, data size, engine events
5. **System** — process RSS, GC pause, goroutine count, file descriptors

To persist dashboard edits back to disk: export the JSON from Grafana (Dashboard menu → Share → Export) and replace `monitoring/grafana/dashboards/lsm-overview.json`.

---

## Alertmanager Configuration

Edit `monitoring/alertmanager.yml` to configure real notification destinations.

The file ships with:
- Placeholder SMTP settings for `oncall@example.com` / `ops@example.com`
- Placeholder webhook URLs (configurable via environment variables)
- An inhibition rule that silences non-critical alerts when `LSMBackendDown` fires

Environment variables recognised by `alertmanager.yml`:
| Variable | Purpose |
|---|---|
| `ALERTMANAGER_SMTP_PASSWORD` | SMTP auth password |
| `ALERTMANAGER_WEBHOOK_URL` | Default/catch-all webhook |
| `ALERTMANAGER_PAGERDUTY_URL` | Critical-page webhook (or use `pagerduty_configs`) |
| `ALERTMANAGER_SLO_WEBHOOK_URL` | SLO burn-rate notification webhook |

Add them to `.env` or pass via `environment:` in `docker-compose.monitoring.yml`.

---

## Data Retention

Prometheus TSDB is configured to retain 15 days of metrics (`--storage.tsdb.retention.time=15d`).
Adjust in `docker-compose.monitoring.yml` under the `prometheus.command` section.

For longer retention, consider adding a remote-write target (Thanos, Cortex, VictoriaMetrics).
