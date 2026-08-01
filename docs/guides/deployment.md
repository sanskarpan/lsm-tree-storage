# Deployment

---

## 1. Single node — Docker Compose

The simplest deployment uses the provided `docker-compose.yml`. It runs the Go backend and the Bun BFF frontend as separate containers with a shared named volume for the data directory.

```bash
# Create an .env file with the required secrets
cat > .env <<'EOF'
API_TOKEN=change-me-strong-token
METRICS_TOKEN=change-me-metrics-token
EOF

# Start
docker compose up -d

# Verify
curl http://localhost:8080/ready
# {"status":"ok","engine_ready":true,...}

# Dashboard
open http://localhost:3001
```

Key environment variables for single-node Docker:

| Variable | Recommended value | Notes |
|----------|------------------|-------|
| `API_TOKEN` | strong random string | Protects all routes except `/health` |
| `METRICS_TOKEN` | separate token | Protects `/metrics`; falls back to `API_TOKEN` |
| `DATA_DIR` | `/data` | Mounted as a Docker volume |
| `TLS_CERT_FILE` | `/etc/tls/cert.pem` | For production HTTPS |
| `TLS_KEY_FILE` | `/etc/tls/key.pem` | For production HTTPS |

The data volume persists SSTables and MANIFEST between container restarts. Always mount it on reliable block storage.

---

## 2. Cluster — 3-node Raft

### Bootstrap procedure

Start all three nodes simultaneously. The node with `CLUSTER_BOOTSTRAP=1` proposes the initial voter set. The other nodes join as voters during the bootstrap handshake.

```bash
# Node 1 — bootstrap leader
CLUSTER_ENABLED=1 \
CLUSTER_NODE_ID=node-1 \
CLUSTER_BIND_ADDR=127.0.0.1:7001 \
CLUSTER_ADVERTISE_ADDR=127.0.0.1:7001 \
CLUSTER_CLIENT_ADDR=http://127.0.0.1:8080 \
CLUSTER_BOOTSTRAP=1 \
CLUSTER_PEERS='node-2@127.0.0.1:7002@http://127.0.0.1:8081,node-3@127.0.0.1:7003@http://127.0.0.1:8082' \
DATA_DIR=./data/node-1 \
go run ./cmd/server/main.go

# Node 2
CLUSTER_ENABLED=1 \
CLUSTER_NODE_ID=node-2 \
CLUSTER_BIND_ADDR=127.0.0.1:7002 \
CLUSTER_ADVERTISE_ADDR=127.0.0.1:7002 \
CLUSTER_CLIENT_ADDR=http://127.0.0.1:8081 \
CLUSTER_PEERS='node-1@127.0.0.1:7001@http://127.0.0.1:8080,node-3@127.0.0.1:7003@http://127.0.0.1:8082' \
DATA_DIR=./data/node-2 \
ADDR=127.0.0.1:8081 \
go run ./cmd/server/main.go

# Node 3
CLUSTER_ENABLED=1 \
CLUSTER_NODE_ID=node-3 \
CLUSTER_BIND_ADDR=127.0.0.1:7003 \
CLUSTER_ADVERTISE_ADDR=127.0.0.1:7003 \
CLUSTER_CLIENT_ADDR=http://127.0.0.1:8082 \
CLUSTER_PEERS='node-1@127.0.0.1:7001@http://127.0.0.1:8080,node-2@127.0.0.1:7002@http://127.0.0.1:8081' \
DATA_DIR=./data/node-3 \
ADDR=127.0.0.1:8082 \
go run ./cmd/server/main.go
```

### Peer list format

```text
nodeID@rpcAddr[@clientAddr]

nodeID      — unique string identifier (matches CLUSTER_NODE_ID)
rpcAddr     — Raft TCP address (host:port)
clientAddr  — HTTP address for client redirects (optional; defaults to CLUSTER_ADVERTISE_ADDR)
```

### Leader failure

When the leader dies:

1. Remaining nodes detect the absence of heartbeats after `CLUSTER_ELECTION_TIMEOUT` (default 3s).
2. A follower starts a new election; the node with the most up-to-date log wins.
3. The new leader accepts writes after receiving votes from a quorum (⌊N/2⌋ + 1 nodes).
4. Clients that cached the old leader address receive a `409 Conflict` with the new leader's `CLUSTER_CLIENT_ADDR` in the response body.

!!! note "Three-node quorum"
    A 3-node cluster tolerates the loss of exactly 1 node. A 5-node cluster tolerates 2. Never run a cluster with 2 nodes — the quorum requirement is 2 nodes, so losing 1 node halts writes.

### Adding a node at runtime

```bash
curl -X POST http://localhost:8080/cluster/membership/add \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{"node_id":"node-4","rpc_address":"127.0.0.1:7004","client_address":"http://127.0.0.1:8083"}'
```

The new node will receive a Raft snapshot if it is too far behind the committed log.

---

## 3. Kubernetes — Helm chart

The Helm chart lives in `helm/lsm-storage`. It creates a `Deployment`, `Service`, `PersistentVolumeClaim`, optional `Ingress`, `HorizontalPodAutoscaler`, `PodDisruptionBudget`, and `ServiceMonitor` for Prometheus Operator.

### Prerequisites

- Helm 3.x
- kubectl configured against your cluster
- Container images pushed to an accessible registry

### Install

```bash
# 1. Create auth secret
kubectl create secret generic my-lsm-secrets \
  --from-literal=API_TOKEN=<admin-token> \
  --from-literal=METRICS_TOKEN=<metrics-token> \
  --from-literal=BFF_BASIC_AUTH=admin:supersecret

# 2. Install chart
helm install lsm-storage ./helm/lsm-storage \
  --set auth.existingSecret=my-lsm-secrets \
  --set image.backend.repository=<registry>/lsm-storage-backend \
  --set image.backend.tag=<version> \
  --set image.frontend.repository=<registry>/lsm-storage-frontend \
  --set image.frontend.tag=<version>

# 3. Verify
kubectl get pods -l app.kubernetes.io/name=lsm-storage
kubectl port-forward svc/lsm-storage-lsm-storage-backend 8080:8080
curl http://localhost:8080/ready
```

### Key values.yaml fields

| Key | Default | Description |
|-----|---------|-------------|
| `replicaCount` | `1` | Backend replica count |
| `persistence.enabled` | `true` | Mount a PVC at `/data` |
| `persistence.size` | `10Gi` | PVC storage request |
| `persistence.storageClass` | `""` | StorageClass name (empty = cluster default) |
| `config.syncWAL` | `true` | Sync WAL on every write |
| `config.blockCacheSize` | `134217728` | Block cache size (128 MB) |
| `config.compactionStyle` | `leveled` | Compaction strategy |
| `tls.enabled` | `false` | TLS on the backend |
| `metrics.serviceMonitor.enabled` | `false` | Create Prometheus Operator `ServiceMonitor` |
| `autoscaling.enabled` | `false` | Enable HPA |
| `podDisruptionBudget.enabled` | `true` | Create PDB |

### Enable Prometheus monitoring

Requires [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) in the cluster:

```bash
helm upgrade lsm-storage ./helm/lsm-storage \
  --set metrics.serviceMonitor.enabled=true \
  --set "metrics.serviceMonitor.additionalLabels.release=prometheus"
```

The `ServiceMonitor` scrapes `/metrics` every 15 seconds using `METRICS_TOKEN` from the auth Secret.

### Enable HPA

```bash
helm upgrade lsm-storage ./helm/lsm-storage \
  --set autoscaling.enabled=true \
  --set autoscaling.minReplicas=2 \
  --set autoscaling.maxReplicas=5 \
  --set autoscaling.targetCPUUtilizationPercentage=70
```

### PodDisruptionBudget

Enabled by default. The default `minAvailable` is 1. Adjust to match your replica count before performing node drains.

### Upgrade

```bash
helm upgrade lsm-storage ./helm/lsm-storage
```

### Uninstall

```bash
helm uninstall lsm-storage
# PVC is NOT deleted automatically (resource policy: keep)
kubectl delete pvc lsm-storage-lsm-storage-data  # only if data is no longer needed
```
