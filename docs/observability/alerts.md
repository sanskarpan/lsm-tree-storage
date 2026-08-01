# Alert Rules Reference

Alert rules live in `monitoring/rules/alerts.yml` (domain alerts) and `monitoring/rules/slos.yml` (SLO burn-rate alerts). They are auto-loaded by Prometheus via `docker-compose.monitoring.yml`.

Reload without restart:

```bash
curl -X POST http://localhost:9090/-/reload
```

---

## Availability alerts

| Alert name | Severity | Condition | Meaning |
|------------|----------|-----------|---------|
| `LSMBackendDown` | critical | `up == 0` for > 1 min | Prometheus cannot scrape the backend at all — process is likely dead |
| `LSMHighErrorRate` | critical | 5xx rate > 1% of requests over 5 min | More than 1 in 100 requests are failing with server errors |
| `LSMBgErrorActive` | critical | `lsm_bg_error == 1` | A background flush or compaction error has stalled the engine; all writes will fail until resolved |

An inhibition rule in `monitoring/alertmanager.yml` silences all non-critical alerts when `LSMBackendDown` fires, to avoid alert noise while the node is down.

---

## Latency alerts

| Alert name | Severity | Condition | Meaning |
|------------|----------|-----------|---------|
| `LSMHighWriteLatencyP99` | warning | P99 write latency > 100 ms for 5 min | Writes are slow; check WAL I/O, compaction backlog, or write stall |
| `LSMHighReadLatencyP99` | warning | P99 read latency > 50 ms for 5 min | Reads are slow; check block cache hit rate, L0 file count, or disk I/O |

Write routes: `/db/put` and `/db/batch`.
Read routes: `/db/get` and `/db/scan`.

---

## Storage health alerts

| Alert name | Severity | Condition | Meaning |
|------------|----------|-----------|---------|
| `LSML0FileCountHigh` | warning | `lsm_engine_l0_sst_files > 8` | L0 file accumulation is above normal; compaction is behind |
| `LSML0FileCountCritical` | critical | `lsm_engine_l0_sst_files > 10` | Approaching the `Level0StopWritesTrigger` (default 12); writes may stall soon |
| `LSMFlushQueueDeep` | warning | `lsm_engine_immutables > 4` for > 2 min | Immutable MemTable backlog is growing; flush worker may be falling behind |
| `LSMWALFilesHigh` | warning | `lsm_engine_wal_files > 5` | Multiple WAL files accumulating; possible flush stall |
| `LSMMemtableSizeHigh` | warning | `lsm_engine_memtable_size_bytes > 80% of 64 MiB` | MemTable is near its flush threshold (> 51 MiB) |
| `LSMImmutableCountHigh` | warning | `lsm_engine_immutables > 1` for > 5 min | Persistent immutable backlog; investigate flush worker health |

---

## Compaction alerts

| Alert name | Severity | Condition | Meaning |
|------------|----------|-----------|---------|
| `LSMCompactionLagging` | warning | `lsm_engine_total_sst_bytes > 9.7 GiB` | Total SSTable bytes are unusually high; compaction is not keeping up |
| `LSMNoCompactionActivity` | warning | No `compaction_complete` events in 30 min while `lsm_engine_total_sst_files > 2` | Compaction has stalled despite having files to compact |

---

## Resource alerts

| Alert name | Severity | Condition | Meaning |
|------------|----------|-----------|---------|
| `LSMDiskUsageHigh` | warning | `lsm_engine_total_sst_bytes > 8 GiB` | SSTable data is approaching disk capacity thresholds |
| `LSMHighMemoryUsage` | warning | `process_resident_memory_bytes > 2 GiB` | Process RSS is high; may indicate block cache misconfiguration or leak |

---

## SLO definitions

Defined in `monitoring/rules/slos.yml`.

| SLO | Target | Error budget (30 days) |
|-----|--------|------------------------|
| Write availability | 99.9% of writes succeed (< 0.1% 5xx) | 43.8 minutes |
| Read availability | 99.9% of reads succeed (< 0.1% 5xx) | 43.8 minutes |
| Write latency P99 | 99% of writes complete in < 100 ms | 7.3 hours |
| Read latency P99 | 99% of reads complete in < 50 ms | 7.3 hours |

---

## SLO burn-rate alerts

Each SLO generates two multi-window burn-rate alerts. The double-window approach reduces false positives from short-lived spikes while still catching sustained degradation.

| Alert suffix | Short window | Long window | Burn rate | Interpretation |
|--------------|-------------|-------------|-----------|----------------|
| `FastBurn` | 5 minutes | 1 hour | 14.4× | Budget exhausted in ~2 hours — page immediately |
| `SlowBurn` | 30 minutes | 6 hours | 6.0× | Budget exhausted in ~5 days — open a ticket |

**FastBurn** fires when the 5-minute error rate **and** the 1-hour error rate both exceed 14.4× the budget consumption rate. Both windows must be above threshold to reduce alert flapping.

**SlowBurn** fires when the 30-minute error rate **and** the 6-hour error rate both exceed 6.0× the budget consumption rate.

Example alert names generated:

- `LSMWriteAvailabilityFastBurn`
- `LSMWriteAvailabilitySlowBurn`
- `LSMReadLatencyP99FastBurn`
- `LSMReadLatencyP99SlowBurn`

---

## Alertmanager configuration

Edit `monitoring/alertmanager.yml` for notification destinations.

| Environment variable | Purpose |
|----------------------|---------|
| `ALERTMANAGER_SMTP_PASSWORD` | SMTP auth password |
| `ALERTMANAGER_WEBHOOK_URL` | Default catch-all webhook |
| `ALERTMANAGER_PAGERDUTY_URL` | Critical-alert webhook |
| `ALERTMANAGER_SLO_WEBHOOK_URL` | SLO burn-rate notification webhook |
