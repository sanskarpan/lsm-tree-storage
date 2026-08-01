# Security Overview

---

## RBAC — three token tiers

The gateway (`gateway/rest.go`) implements role-based access control with three bearer token tiers:

| Role | Value | Token variable | Routes granted |
|------|-------|----------------|----------------|
| `roleAdmin` | 3 | `API_TOKEN` | All routes including `/admin/snapshot`, `/compaction/*`, `/cluster/membership/*` |
| `roleReadWrite` | 2 | `API_TOKEN_READWRITE` | `/db/put`, `/db/delete`, `/db/batch`, `/db/get`, `/db/scan`, `/stats`, `/health` |
| `roleReadOnly` | 1 | `API_TOKEN_READONLY` | `/db/get`, `/db/scan`, `/stats`, `/health`, `/ready` |

Token comparison uses `crypto/subtle.ConstantTimeCompare`, which runs in constant time regardless of token length. This prevents timing-oracle attacks.

`/health` is always unauthenticated regardless of token configuration.

---

## TLS

Set `TLS_CERT_FILE` and `TLS_KEY_FILE` to enable HTTPS:

```bash
TLS_CERT_FILE=/etc/tls/cert.pem \
TLS_KEY_FILE=/etc/tls/key.pem \
./server
```

When both variables are set, the server calls `net/http.ListenAndServeTLS`. When they are absent, a warning is logged and the server starts in plaintext HTTP mode:

```text
WARNING: TLS not configured; running in plaintext HTTP mode. Set TLS_CERT_FILE and TLS_KEY_FILE for production.
```

!!! warning
    Never expose the backend on a non-loopback address without TLS in production. The `ALLOW_INSECURE_REMOTE` flag is explicitly an opt-in escape hatch, not a recommended configuration.

---

## Rate limiting

The gateway implements per-IP token bucket rate limiting (`gateway/rest.go ipRateLimiter`):

| Parameter | Value |
|-----------|-------|
| Burst limit | 100 requests per window |
| Window | 1 second |
| State cleanup interval | 5 minutes |
| Cleanup threshold | Entries idle for > 10 minutes are removed |

Requests that exceed the rate limit receive `429 Too Many Requests`. The rate limiter state is in-process; it does not persist across restarts.

---

## SSRF protection — cluster leader forwarding

When a follower receives a write request, it may forward to the current leader. Before forwarding, the leader's HTTP address is validated against the in-memory `peerRegistry` loaded from `CLUSTER_PEERS` and `/cluster/membership/add` calls. Addresses not in the registry are rejected — the request is not forwarded. This prevents a compromised leader hint (e.g., from a Byzantine peer) from causing an SSRF to an arbitrary host.

---

## BFF Basic Auth

When `BFF_BASIC_AUTH=user:password` is set on the Elysia BFF (`frontend/server/bff.ts`), every browser-facing BFF route requires HTTP Basic authentication. The password comparison uses `timingSafeEqual` (Node.js `crypto` module's timing-safe buffer comparison), which is the equivalent of `crypto/subtle.ConstantTimeCompare` in Go. There is no timing oracle on the BFF side.

When the BFF is bound to a non-loopback host, it refuses to start unless either `BFF_BASIC_AUTH` or `ALLOW_INSECURE_REMOTE_BFF=1` is set.

---

## Body size limits

Oversized request bodies are rejected by `MaxBytesReader` **before** JSON decoding, preventing memory exhaustion from large payloads:

| Route | Limit | Constant in `gateway/rest.go` |
|-------|-------|-------------------------------|
| `/db/put`, `/db/delete` | ~1 MB (key 8 KB + value 1 MB + framing) | `maxSingleWriteRequestBytes` |
| `/db/batch` | ~500 MB (500 entries × max key+value + framing) | `maxBatchRequestBytes` |
| `/admin/*`, `/compaction/*`, `/cluster/membership/*` | 1 MB | `maxAdminRequestBytes` |

---

## Container security

Both Docker images run as non-root users:

| Image | Username | UID |
|-------|----------|-----|
| Backend (`Dockerfile.backend`) | `lsm` | 1001 |
| Frontend | `appuser` | 1001 |

The backend `Dockerfile.backend` uses `useradd` (Debian-style), not `adduser`, because the `oven/bun` base image is Debian slim which does not include `adduser`.

---

## Raft inter-node TLS

Set `CLUSTER_TLS_ENABLED=1` to encrypt inter-node Raft transport. For mutual TLS (mTLS), set `CLUSTER_TLS_CA_FILE` to a PEM CA bundle:

| Variable | Purpose |
|----------|---------|
| `CLUSTER_TLS_CERT_FILE` | Server certificate |
| `CLUSTER_TLS_KEY_FILE` | Private key |
| `CLUSTER_TLS_CA_FILE` | CA bundle for peer verification (enables mTLS) |

`CLUSTER_TLS_INSECURE_SKIP_VERIFY=1` disables peer certificate verification. Only for debugging — never use in production.
