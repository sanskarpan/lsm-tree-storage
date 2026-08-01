# Quick Start

## Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| Go | 1.26.3+ | Required for current security patches |
| Bun | Latest | JavaScript runtime + package manager |
| Docker | 20+ | Required for `docker compose` path |
| Make | Any | Optional; targets wrap common commands |

---

## Single node — Docker Compose

```bash
# Start the full stack (backend + frontend + monitoring)
docker compose up -d

# Check backend health
curl http://localhost:8080/health
# {"status":"ok"}

# Open the dashboard
open http://localhost:3001
```

The backend binds to `127.0.0.1:8080` by default. Set `ADDR=0.0.0.0:8080` to expose it on all interfaces (requires `API_TOKEN` to be set first).

---

## Single node — bare metal

```bash
# 1. Start the Go backend
make run
# OR
go run ./cmd/server/main.go

# 2. In a separate terminal, start the BFF + dashboard
cd frontend
bun install
bun run index.ts
```

---

## First write

```bash
curl -s -X POST http://localhost:8080/db/put \
  -H 'Content-Type: application/json' \
  -d '{"key": "hello", "value": "world"}'
# {"ok":true}
```

---

## First read

```bash
curl -s 'http://localhost:8080/db/get?key=hello'
# {"key":"hello","value":"world","found":true}
```

---

## First scan

```bash
curl -s 'http://localhost:8080/db/scan?start=a&end=z&limit=10'
# {"results":[{"key":"hello","value":"world"}],"count":1}
```

---

## Dashboard

Open [http://localhost:3001](http://localhost:3001) in a browser. The 7-panel React dashboard connects via WebSocket and updates in real time as the engine runs.

---

## Required environment variables

| Variable | Example | When required |
|----------|---------|---------------|
| `DATA_DIR` | `/data/lsm` | Production deployments; defaults to `./data` |
| `API_TOKEN` | `change-me` | Any non-loopback deployment; enables Bearer auth |

---

## TLS (production)

```bash
TLS_CERT_FILE=/etc/tls/cert.pem \
TLS_KEY_FILE=/etc/tls/key.pem \
go run ./cmd/server/main.go
```

When `TLS_CERT_FILE` and `TLS_KEY_FILE` are set, the server calls `ListenAndServeTLS`. If they are not set, a warning is logged:

```text
WARNING: TLS not configured; running in plaintext HTTP mode. Set TLS_CERT_FILE and TLS_KEY_FILE for production.
```

---

## Cluster mode (3-node example)

```bash
# Node 1 (bootstrap leader)
CLUSTER_ENABLED=1 \
CLUSTER_NODE_ID=node-1 \
CLUSTER_BIND_ADDR=127.0.0.1:7001 \
CLUSTER_ADVERTISE_ADDR=127.0.0.1:7001 \
CLUSTER_CLIENT_ADDR=http://127.0.0.1:8080 \
CLUSTER_BOOTSTRAP=1 \
CLUSTER_PEERS='node-2@127.0.0.1:7002@http://127.0.0.1:8081,node-3@127.0.0.1:7003@http://127.0.0.1:8082' \
go run ./cmd/server/main.go
```

See [Deployment](deployment.md) for the full 3-node setup and Kubernetes.
