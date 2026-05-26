# Production Audit TODO

This file tracks the issues found during the end-to-end audit, why they matter,
their current status, and the follow-up work still required.

## Planned Architecture Work

The repo is operationally mature for single-node use, but HA is still a planned
architectural program. The design and migration phases are captured in
[docs/ha-architecture.md](docs/ha-architecture.md).

### HA Phase 0
- Status: complete
- Scope: cluster config types, replicated logical command model, cluster status types, and repo-level architecture plan

### HA Phase 1
- Status: complete
- Scope: the gateway now routes through a node abstraction instead of writing directly to the raw engine

### HA Phase 2
- Status: complete
- Scope: Raft consensus, peer transport, leader election, leader redirects, quorum writes, and runtime membership changes

### HA Phase 3
- Status: complete
- Scope: deterministic apply into local engines, durable applied-index checkpoints, and non-leader read forwarding

### HA Phase 4
- Status: complete
- Scope: snapshot creation, snapshot restore, and bounded replay using retained Raft snapshots

### HA Phase 5
- Status: complete
- Scope: cluster metrics, readiness endpoints, TLS-capable transport, shard-aware routing, automatic shard rebalancing, integration coverage for failover and snapshot recovery, plus operator documentation

## Fixed

### 1. WAL rotation could lose live writes during flush
- Root cause: the engine kept appending new mutable writes to the same WAL used by the immutable memtable being flushed. After the flush finished, that WAL was deleted even though it still contained the newest unflushed mutable writes.
- Impact: a crash after a flush, but before the next flush, could permanently lose acknowledged writes.
- Fix: memtable rotation now creates a new WAL immediately. Each immutable memtable keeps ownership of the WAL that contains only its data, and that WAL is deleted only after the immutable flush completes.

### 2. Recovery only replayed a single WAL file
- Root cause: startup replayed `manifest.LogNumber` only, even though the runtime could legitimately have multiple WAL files on disk.
- Impact: crash recovery could miss data from older immutable WALs and restore a stale state.
- Fix: recovery now scans and replays all WAL files, sorts them by file ID, rebuilds the memtable, rewrites a canonical active WAL, and updates the manifest log number.

### 3. Manifest could reference SSTables before readers existed
- Root cause: flush and compaction published new SSTables to the manifest before their readers were registered.
- Impact: compaction could select inputs that were present in metadata but unreadable in memory, causing silent correctness drift and flaky compaction results.
- Fix: output readers are opened and registered before manifest visibility, with cleanup rollback if publishing fails.

### 4. `Scan` was not snapshot-safe under concurrent writes
- Root cause: `Scan` iterated the mutable memtable after releasing its lock, even though the iterator walked the live skiplist structure directly.
- Impact: data races and undefined results under concurrent `Put`/`Delete`/`Scan`.
- Fix: memtable snapshots are copied before `Scan` overlays them onto SSTable results. A concurrent scan/write integration test now covers this.

### 5. `MaxImmutableMemTables` was ignored
- Root cause: backpressure was effectively controlled by the flush channel buffer, not the configured immutable memtable cap.
- Impact: the engine could accumulate more immutable memtables than configured and violate its own write-stall contract.
- Fix: memtable rotation now blocks on `flushCond` when the configured immutable limit is reached.

### 6. Memtable retained caller-owned byte slices
- Root cause: `Put` and `Delete` stored caller-provided slices without copying them.
- Impact: callers could mutate keys/values after write and corrupt in-memory state.
- Fix: keys and values are copied on insert, and memtable snapshots deep-copy records.

### 7. Backend startup ignored `config.yaml`
- Root cause: the server always used `DefaultConfig("./data")` plus `DATA_DIR`, despite the README claiming YAML-backed configuration.
- Impact: documented settings were not actually applied, including compaction and durability knobs.
- Fix: the server now loads `config.yaml` by default, supports `CONFIG_PATH`, honors environment overrides, and parses `time_window_size`.

### 8. Backend and BFF ports were hardcoded
- Root cause: the Go server always bound to `:8080`, while the BFF always targeted `localhost:8080` and listened on `3001`.
- Impact: the app could not be brought up reliably in a clean environment if those ports were already occupied.
- Fix: the Go server now supports `ADDR`/`PORT`; the BFF now supports `PORT`, `BACKEND_URL`, and `BACKEND_WS_URL`.

### 9. Frontend entrypoint was a placeholder
- Root cause: `frontend/index.ts` only logged `"Hello via Bun!"` while the actual server lived in `frontend/server/bff.ts`.
- Impact: the documented `bun run index.ts` boot path was broken.
- Fix: `frontend/index.ts` now imports the BFF entrypoint directly.

### 10. Frontend dashboard hardcoded `localhost:3001`
- Root cause: the inline dashboard JS called `http://localhost:3001/...` and `ws://localhost:3001/ws` directly.
- Impact: serving the BFF on any non-default host/port broke the UI even though the backend and BFF themselves started successfully.
- Fix: the dashboard now derives API and WebSocket origins from `window.location`.

### 11. WAL append events were missing from normal writes
- Root cause: the engine used `AppendWithSeqNo` for real writes, but only `Append` emitted `wal.append` events.
- Impact: the dashboard, amplification tracker, and general observability missed most write activity.
- Fix: both append paths now emit consistent `wal.append` events with `seq`, `seq_no`, `key_len`, and `val_len`.

### 12. Write batches were not truly atomic
- Root cause: `WriteBatch` used one WAL record per entry and updated the memtable incrementally.
- Impact: a torn batch could be partially visible after an error or crash, despite the API promising atomic application.
- Fix: batches now reserve sequence numbers up front, write one atomic WAL batch record, sync once if configured, and only then mutate the memtable. Recovery replays the batch as all-or-nothing. Regression coverage now includes batch crash recovery and truncated-batch replay.

### 13. SSTable format dropped sequence numbers on disk
- Root cause: encoded SSTable keys omitted `SeqNo`, so compaction and reads relied on file ordering heuristics instead of explicit version metadata.
- Impact: snapshot reads and version ordering across compaction were weaker than production LSM semantics.
- Fix: SSTable keys now encode `UserKey + SeqNo + Type`, block iterators decode sequence numbers from disk, and readers honor `readSeqNo` correctly from persisted data. Regression coverage now checks multi-version snapshot reads directly from an SSTable.

### 14. Several REST endpoints were stubs or misleading placeholders
- Root cause: `/db/open`, `/db/close`, `/wal/entries`, and `/memtable/snapshot` returned placeholder text instead of real state or explicit rejection semantics.
- Impact: operators and integrations could not rely on those endpoints for observability or lifecycle behavior.
- Fix: `/db/open` now returns runtime state, config, and stats; `/db/close` explicitly rejects remote lifecycle control; `/wal/entries` returns bounded recent WAL activity; `/memtable/snapshot` returns bounded mutable and immutable memtable contents with sequence numbers, tombstones, and WAL ownership.

### 15. Manifest replay could mis-handle truncated reads
- Root cause: manifest recovery used non-filling reads for edit length/body parsing.
- Impact: partial reads could be mistaken for clean EOF or hide corruption.
- Fix: manifest replay now uses `io.ReadFull` and returns explicit truncation errors for both the length prefix and edit body.

### 16. Security defaults were wide open
- Root cause: REST accepted any origin and the WebSocket upgrader unconditionally allowed cross-origin connections.
- Impact: safe enough for localhost demos, but unsafe for shared environments or deployments.
- Fix: REST and WebSocket access now default to same-origin only, with optional `ALLOWED_ORIGINS` overrides for approved cross-origin clients. Regression coverage now checks both rejection and allowed-origin paths.

### 17. Frontend codebase had two overlapping implementations
- Root cause: the served dashboard lived in `public/index.html`, while `frontend/src/` contained an unused alternate client/store tree.
- Impact: duplicated logic and high drift risk.
- Fix: the unused alternate tree was removed and the frontend README now documents the single supported path through the Bun BFF plus static dashboard.

### 18. Point and range reads were still expensive at scale
- Root cause: `Scan` merged the entire keyspace in memory, and L1+ point reads walked files linearly.
- Impact: correctness was acceptable, but latency and memory use degraded quickly as the dataset grew.
- Fix: manifest-managed L1+ levels are now kept sorted by key range, point reads binary-search a candidate SSTable per level, and scans only visit SSTables and in-memory entries that overlap the requested range instead of rebuilding the whole database snapshot.

### 19. Backend was exposed with permissive transport defaults
- Root cause: the Go server bound to all interfaces by default and used `http.Server` without read, write, header, or idle timeouts.
- Impact: accidental network exposure was too easy, and the service was vulnerable to slowloris-style connection exhaustion or hung clients.
- Fix: the backend now binds to `127.0.0.1` by default, supports explicit `ADDR` overrides for deployments, refuses remote binds without `API_TOKEN` unless `ALLOW_INSECURE_REMOTE=1` is set, and configures `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, and `IdleTimeout`.

### 20. Backend APIs accepted unbounded or weakly-validated JSON
- Root cause: mutating/admin handlers decoded request bodies directly from `r.Body`, accepted unknown fields, and did not reject trailing JSON values.
- Impact: large-body memory abuse, confusing silent field drops, and inconsistent request semantics were possible.
- Fix: JSON handlers now use bounded `http.MaxBytesReader`, `DisallowUnknownFields`, and single-object enforcement. Range and benchmark inputs are also bounded and validated.

### 21. API and observability endpoints had no authentication boundary
- Root cause: every REST endpoint and the backend WebSocket stream were accessible to any client that could reach the port.
- Impact: unauthorized reads, writes, benchmark execution, compaction control, and event-stream data exfiltration were possible once the service was reachable.
- Fix: when `API_TOKEN` is set, all backend endpoints except `/health` require `Authorization: Bearer <token>`, and backend WebSocket clients must provide the token by header or `access_token` query param. The Bun BFF forwards the configured token automatically.

### 22. WebSocket clients could consume resources indefinitely
- Root cause: the WebSocket hub had no handshake timeout, read limit, heartbeat, deadline extension, or slow-client eviction.
- Impact: stale, malicious, or slow clients could hold sockets forever or backpressure the broadcast path.
- Fix: the hub now enforces handshake/read/write deadlines, ping/pong heartbeats, a max inbound message size, and automatic eviction of slow clients.

### 23. The Bun BFF still had proxy-level hardening gaps
- Root cause: the BFF did not bind to loopback explicitly, did not forward backend auth, and retried backend WebSocket connections with a fixed reconnect loop.
- Impact: the BFF could become an inconsistent security boundary and could hang on backend failures.
- Fix: the BFF now binds to `127.0.0.1` by default, refuses non-loopback binds unless `ALLOW_REMOTE_BFF=1` is set, forwards backend bearer auth for all proxied REST traffic, uses bounded backend fetch timeouts, and reconnects its backend WebSocket with capped exponential backoff. Static asset responses also send `X-Content-Type-Options: nosniff`.

### 24. Standard-library vulnerabilities depended on the caller's Go runtime
- Root cause: `govulncheck` previously flagged reachable vulnerabilities in the local `go1.26.1` standard library (`net`, `crypto/tls`, and `crypto/x509`).
- Impact: even a correct application build could inherit runtime-level CVEs from an outdated Go toolchain.
- Fix: the module now requires `toolchain go1.26.3`, and validation was rerun with `GOVERSION=go1.26.3`, after which `govulncheck` reported no vulnerabilities found.

### 25. Remote BFF exposure still depended on external auth and header policy
- Root cause: after backend hardening, the Bun BFF was still a browser-facing trusted proxy with no first-class user auth or browser response hardening of its own.
- Impact: a remotely exposed BFF could rely too heavily on infrastructure assumptions, leaving the UI, proxied API surface, and event stream less protected than the backend.
- Fix: the BFF now supports built-in `BFF_BASIC_AUTH`, refuses remote exposure without either that auth or an explicit insecure override, applies CSP and other browser security headers globally, and keeps the HTML shell `no-store` so authenticated control-room pages are not cached casually.

### 26. Runtime observability was not production-friendly
- Root cause: the server had shallow health reporting, no readiness signal, no Prometheus endpoint, and no structured access logs.
- Impact: operators could not distinguish liveness from readiness, scrape first-class metrics, or correlate HTTP traffic reliably in production incidents.
- Fix: the backend now emits structured JSON access logs with `X-Request-ID`, bridges legacy `log.Printf` paths into the same log stream, exposes `/ready` from real engine state, and exports Prometheus metrics at `/metrics` with optional bearer auth.

### 27. There was no repo-native backup or restore workflow
- Root cause: the project previously relied on ad hoc file copies with no checksum verification or documented archive format.
- Impact: recovery procedures were manual, brittle, and easy to get wrong under pressure.
- Fix: `scripts/backup.sh` now creates timestamped tarball backups plus SHA-256 checksums and metadata, and `scripts/restore.sh` verifies integrity before restoring into a target directory.

### 28. There was no sustained HTTP load or soak harness
- Root cause: tests covered correctness, but the repo lacked a repeatable tool for concurrent mixed traffic against the running service.
- Impact: performance regressions and operational timeout behavior were harder to validate before deployment.
- Fix: `cmd/loadtest` and `scripts/loadtest.sh` now provide a configurable concurrent read/write harness with auth support and percentile summaries for repeatable soak-style validation.

### 29. Deployment artifacts were incomplete
- Root cause: the repository lacked first-class container build files, a composed stack definition, and an example metrics scraper configuration.
- Impact: deployment setup was still mostly tribal knowledge even after the code hardening work.
- Fix: the repo now includes `Dockerfile.backend`, `frontend/Dockerfile`, `docker-compose.yml`, `.dockerignore` files, and `deploy/prometheus/prometheus.yml`. Both images were built successfully during validation.

### 30. CI automation did not enforce the new quality bar
- Root cause: none of the test, race, vuln, build, or packaging checks were encoded as repository automation.
- Impact: regressions could re-enter the repo without a repeatable gate.
- Fix: `.github/workflows/ci.yml` now runs Go tests, race tests, `govulncheck`, frontend typecheck/build/audit, the loadtest build, Docker image builds, and `docker compose config`.

## Validation Notes

- Unit tests pass.
- Integration tests pass.
- Race detector passes.
- Clean-environment boot has been validated with custom backend and BFF ports.
- Direct backend API, BFF proxy API, and WebSocket event delivery have been rechecked after the fixes above.
- `bun audit` reports no frontend dependency vulnerabilities.
- `govulncheck` reports no vulnerabilities with the enforced `go1.26.3` toolchain.
- Live validation with `API_TOKEN` enabled confirmed:
  - unauthenticated backend writes are rejected with `401`
  - authenticated backend writes succeed
  - the BFF can proxy authenticated backend reads and writes without exposing the token to the browser
  - both direct backend WebSocket access and BFF WebSocket fan-out behave correctly under the new auth policy
- Live validation with `BFF_BASIC_AUTH` enabled confirmed:
  - unauthenticated BFF HTML requests are challenged with `401` and `WWW-Authenticate: Basic`
  - authenticated BFF requests receive CSP and the other browser security headers
  - authenticated BFF API writes succeed
  - unauthenticated BFF WebSocket upgrades are rejected
  - authenticated BFF WebSocket upgrades receive backend event fan-out correctly
- Live validation after the production-pass additions confirmed:
  - `/ready` returns engine-backed readiness state
  - `/metrics` rejects unauthenticated scrapes and serves Prometheus metrics with the configured bearer token
  - backup archives and checksum files are produced successfully
  - restore verification succeeds and recreates the backed-up data directory contents
  - the load harness runs against the secured BFF and reports clean success/failure accounting
  - `docker compose config` validates
  - backend and frontend container images build successfully
