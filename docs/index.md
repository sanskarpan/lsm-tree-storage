# LSM-Tree Storage Engine

A from-scratch Log-Structured Merge-Tree storage engine in Go, with Raft replication and a live React dashboard.

---

## What this is

This project is an educational, production-hardened implementation of a Log-Structured Merge-Tree (LSM-Tree) database storage engine. It covers the complete write and read paths — WAL append with CRC32 and fsync, MemTable skip-list insertion, immutable flush, and SSTable construction — plus three pluggable compaction strategies (Leveled, Size-Tiered, Time-Window), per-SSTable Bloom filters, an LRU block cache, and MANIFEST-driven crash recovery. Every component is implemented from scratch in Go with no external database dependencies.

The engine is wrapped in an HTTP/WebSocket gateway and paired with a Bun + Elysia BFF that serves a 7-panel React dashboard. Raft-backed multi-node replication is available via the cluster layer (`internal/cluster`), providing leader election, quorum commit, runtime membership changes, and snapshot-based follower restore. The codebase includes fuzz tests, chaos tests, load tests, an OTel tracing integration, a Prometheus metrics stack, a Grafana dashboard, and a Helm chart for Kubernetes deployment.

---

## Start here

- [Quick start](guides/quick-start.md) — run a single node in two commands, write your first key
- [Architecture overview](architecture/overview.md) — system diagram, write path, read path, component table
- [Configuration reference](guides/configuration.md) — every `Config` field and environment variable
- [REST API](api/rest.md) — all endpoints, auth tiers, request/response schemas

---

## Design decisions

- [ADR-0001: LSM-Tree over B-Tree](adr/0001-lsm-over-btree.md) — why sequential I/O wins for write-heavy workloads
- [ADR-0002: Replicate logical operations, not SSTables](adr/0002-raft-replication.md) — Raft over Put/Delete commands
- [ADR-0003: Write-before-disk atomicity in MANIFEST](adr/0003-write-before-disk.md) — crash-safe version edit ordering
- [ADR-0004: Persist MaxSeqNo to MANIFEST on every flush](adr/0004-seqno-persistence.md) — why data disappeared after a clean restart

---

## Observability

- [Prometheus metrics](observability/metrics.md) — full metric catalogue with labels and types
- [Alert rules](observability/alerts.md) — 15 alert rules across availability, latency, storage, and cluster domains
- [OTel tracing](observability/tracing.md) — OTLP setup, span attributes, W3C TraceContext propagation
- [Grafana dashboard](observability/grafana.md) — 25-panel overview, import path, datasource config

---

## Operations

- [Deployment](guides/deployment.md) — single node, 3-node cluster, Kubernetes Helm chart
- [Configuration](guides/configuration.md) — engine `Config` struct defaults and all environment variables
- [Backup and recovery](guides/backup-recovery.md) — consistent snapshot, crash replay, CRC failure handling

---

## API

- [REST API reference](api/rest.md) — all routes, roles, curl examples

---

!!! note "Built with"
    Documentation generated with [MkDocs](https://www.mkdocs.org/) and the [Material theme](https://squidfunk.github.io/mkdocs-material/).
