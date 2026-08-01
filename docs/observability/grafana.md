# Grafana Dashboard

The `LSM Storage Engine — Overview` dashboard (UID `lsm-overview`) is auto-provisioned at startup from `monitoring/grafana/dashboards/lsm-overview.json`.

---

## Importing

The dashboard is provisioned automatically when you start the monitoring stack:

```bash
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d
```

To import manually into an existing Grafana instance:

1. Open Grafana → Dashboards → Import
2. Upload `monitoring/grafana/dashboards/lsm-overview.json`
3. Set datasource to your Prometheus instance

---

## Datasource

| Field | Value |
|-------|-------|
| Type | Prometheus |
| URL | `http://prometheus:9090` (Docker network) or your Prometheus address |
| Default | Yes (used by the auto-provisioned dashboard) |

---

## Dashboard structure (25 panels, 5 rows)

### Row 1 — Overview

| Panel | Type | Metric(s) |
|-------|------|-----------|
| Backend readiness | Stat | `lsm_engine_ready` |
| Total request rate | Graph | `rate(lsm_http_requests_total[5m])` |
| Error rate % | Stat | `rate(lsm_http_requests_total{status=~"5.."}[5m])` |
| P99 latency | Stat | `histogram_quantile(0.99, rate(lsm_http_request_duration_seconds_bucket[5m]))` |

### Row 2 — Write Path

| Panel | Type | Metric(s) |
|-------|------|-----------|
| MemTable size | Gauge | `lsm_engine_memtable_size_bytes` |
| Immutable MemTables | Stat | `lsm_engine_immutables` |
| WAL files | Stat | `lsm_engine_wal_files` |
| L0 SSTable files | Graph | `lsm_engine_l0_sst_files` |
| Write throughput | Graph | `rate(lsm_http_requests_total{route="/db/put"}[1m])` |
| Write latency heatmap | Heatmap | `lsm_http_request_duration_seconds_bucket{route="/db/put"}` |

### Row 3 — Read Path

| Panel | Type | Metric(s) |
|-------|------|-----------|
| Block cache hit rate | Gauge | `lsm_cache_hit_rate` |
| Cache entries | Stat | `lsm_cache_entries` |
| Total SSTable files | Stat | `lsm_engine_total_sst_files` |
| Total SSTable bytes | Stat | `lsm_engine_total_sst_bytes` |
| Read throughput | Graph | `rate(lsm_http_requests_total{route="/db/get"}[1m])` |
| Read latency heatmap | Heatmap | `lsm_http_request_duration_seconds_bucket{route="/db/get"}` |

### Row 4 — HTTP

| Panel | Type | Metric(s) |
|-------|------|-----------|
| Request rate by route | Graph | `rate(lsm_http_requests_total[5m])` by `route` |
| P50 latency | Graph | `histogram_quantile(0.50, rate(...))` by `route` |
| P95 latency | Graph | `histogram_quantile(0.95, rate(...))` by `route` |
| P99 latency | Graph | `histogram_quantile(0.99, rate(...))` by `route` |
| Error rate by route | Graph | `rate(lsm_http_requests_total{status=~"5.."}[5m])` by `route` |
| Background error | Stat | `lsm_bg_error` (alert threshold at 1) |

### Row 5 — Cluster (Raft)

| Panel | Type | Metric(s) |
|-------|------|-----------|
| Raft term | Stat | `lsm_cluster_term` |
| Commit index | Graph | `lsm_cluster_commit_index` |
| Applied index | Graph | `lsm_cluster_last_applied_index` |
| Replication lag | Graph | `lsm_cluster_commit_index - lsm_cluster_last_applied_index` |
| Node role | Stat | `lsm_cluster_role_leader`, `lsm_cluster_role_follower`, `lsm_cluster_role_candidate` |

---

## Access

| UI | URL | Default credentials |
|----|-----|---------------------|
| Grafana | http://localhost:3000 | `admin` / `admin` |
| Prometheus | http://localhost:9090 | none |
| Alertmanager | http://localhost:9093 | none |

Change the Grafana admin password on first login or set `GRAFANA_ADMIN_PASSWORD` in `.env` before starting the stack.

---

## Persisting dashboard edits

1. Edit panels in Grafana.
2. Dashboard menu → Share → Export.
3. Replace `monitoring/grafana/dashboards/lsm-overview.json` with the exported JSON.
4. Commit the change; the dashboard will be auto-provisioned on the next stack restart.

---

## Data retention

Prometheus TSDB is configured with `--storage.tsdb.retention.time=15d`. Adjust in `docker-compose.monitoring.yml` under the `prometheus.command` section. For longer retention, configure a remote-write target (Thanos, Cortex, VictoriaMetrics).
