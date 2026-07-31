# Kubernetes Deployment Guide

This directory contains documentation for deploying the LSM-Tree storage engine on Kubernetes using Helm.

## Prerequisites

- [Helm 3.x](https://helm.sh/docs/intro/install/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/) configured against your target cluster
- A container registry containing the `lsm-storage-backend` and `lsm-storage-frontend` images

## Quick Start

### 1. Create the Authentication Secret

The backend requires an API token and the frontend BFF requires basic-auth credentials.
Create a Kubernetes Secret before installing the chart:

```bash
kubectl create secret generic my-lsm-secrets \
  --from-literal=API_TOKEN=<your-strong-api-token> \
  --from-literal=METRICS_TOKEN=<your-metrics-token> \
  --from-literal=BFF_BASIC_AUTH=admin:supersecret
```

> The `METRICS_TOKEN` protects the `/metrics` endpoint. If omitted, `API_TOKEN` is used as a fallback.

### 2. Install the Chart

```bash
helm install lsm-storage ./helm/lsm-storage \
  --set auth.existingSecret=my-lsm-secrets \
  --set image.backend.repository=<your-registry>/lsm-storage-backend \
  --set image.backend.tag=<version> \
  --set image.frontend.repository=<your-registry>/lsm-storage-frontend \
  --set image.frontend.tag=<version>
```

### 3. Verify the Deployment

```bash
kubectl get pods -l app.kubernetes.io/name=lsm-storage
kubectl get svc  -l app.kubernetes.io/name=lsm-storage
```

Port-forward to test locally:

```bash
kubectl port-forward svc/lsm-storage-lsm-storage-backend  8080:8080 &
kubectl port-forward svc/lsm-storage-lsm-storage-frontend 3001:3001 &

curl http://localhost:8080/ready          # should return {"status":"ok",...}
open http://localhost:3001                # open the dashboard
```

## Upgrading

```bash
helm upgrade lsm-storage ./helm/lsm-storage
```

To pass additional overrides at upgrade time use the same `--set` flags as install.

## Common Configurations

### Enable Ingress

```bash
helm upgrade lsm-storage ./helm/lsm-storage \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set "ingress.hosts[0].host=lsm.example.com" \
  --set "ingress.hosts[0].paths[0].path=/" \
  --set "ingress.hosts[0].paths[0].pathType=Prefix" \
  --set "ingress.hosts[0].paths[0].service=frontend" \
  --set "ingress.hosts[0].paths[1].path=/api/" \
  --set "ingress.hosts[0].paths[1].pathType=Prefix" \
  --set "ingress.hosts[0].paths[1].service=backend" \
  --set "ingress.annotations.nginx\.ingress\.kubernetes\.io/rewrite-target=/$2"
```

### Enable TLS on the Backend

```bash
# Create a TLS secret (or use cert-manager)
kubectl create secret tls lsm-tls \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key

helm upgrade lsm-storage ./helm/lsm-storage \
  --set tls.enabled=true \
  --set tls.certSecretName=lsm-tls
```

### Enable Prometheus Monitoring

Requires the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) to be installed.

```bash
helm upgrade lsm-storage ./helm/lsm-storage \
  --set metrics.serviceMonitor.enabled=true \
  --set "metrics.serviceMonitor.additionalLabels.release=prometheus"
```

The `ServiceMonitor` scrapes `/metrics` every 15 s and authenticates using `METRICS_TOKEN` from the auth Secret.

### Enable Horizontal Pod Autoscaling

```bash
helm upgrade lsm-storage ./helm/lsm-storage \
  --set autoscaling.enabled=true \
  --set autoscaling.minReplicas=2 \
  --set autoscaling.maxReplicas=5 \
  --set autoscaling.targetCPUUtilizationPercentage=70
```

### Use a Specific Storage Class

```bash
helm upgrade lsm-storage ./helm/lsm-storage \
  --set persistence.storageClass=fast-ssd \
  --set persistence.size=50Gi
```

## Uninstalling

```bash
helm uninstall lsm-storage
```

> The PersistentVolumeClaim is **not** deleted automatically (Helm resource policy: keep). Delete it manually if you no longer need the data:
>
> ```bash
> kubectl delete pvc lsm-storage-lsm-storage-data
> ```

## Production Checklist

| Item | Notes |
|------|-------|
| Auth credentials | Replace placeholder Secret values with strong, unique tokens |
| Persistent storage | Set `persistence.storageClass` to a reliable block-storage class |
| Resource limits | Tune `resources.backend` and `resources.frontend` to your workload |
| TLS | Enable `tls.enabled=true` or terminate TLS at the Ingress layer |
| Ingress | Configure with TLS for external HTTPS access |
| PodDisruptionBudget | Enabled by default; ensure `minAvailable` fits your replica count |
| HPA | Enable for production workloads with variable traffic |
| Monitoring | Enable `metrics.serviceMonitor.enabled=true` if you use Prometheus Operator |
| Dedicated nodes | Use `nodeSelector` / `tolerations` to schedule on storage-optimised nodes |
| Image tags | Pin `image.backend.tag` and `image.frontend.tag` to specific versions |

## values.yaml Reference

Key values and their defaults:

| Key | Default | Description |
|-----|---------|-------------|
| `replicaCount` | `1` | Number of backend (and frontend) pod replicas |
| `image.backend.repository` | `lsm-storage-backend` | Backend container image |
| `image.backend.tag` | `latest` | Backend image tag |
| `image.frontend.repository` | `lsm-storage-frontend` | Frontend container image |
| `image.frontend.tag` | `latest` | Frontend image tag |
| `persistence.enabled` | `true` | Mount a PVC at `/data` |
| `persistence.size` | `10Gi` | PVC storage request |
| `persistence.storageClass` | `""` | StorageClass name (empty = cluster default) |
| `auth.existingSecret` | `""` | Name of a pre-existing Secret with auth keys |
| `config.syncWAL` | `true` | Flush WAL to disk on every write |
| `config.blockCacheSize` | `134217728` | Block cache size in bytes (128 MB) |
| `config.compactionStyle` | `leveled` | `leveled`, `size-tiered`, or `time-window` |
| `tls.enabled` | `false` | Enable TLS on the backend gRPC/HTTP server |
| `metrics.serviceMonitor.enabled` | `false` | Create a Prometheus Operator ServiceMonitor |
| `autoscaling.enabled` | `false` | Enable HPA for the backend Deployment |
| `podDisruptionBudget.enabled` | `true` | Create a PodDisruptionBudget |
