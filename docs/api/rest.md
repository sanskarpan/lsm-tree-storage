# REST API Reference

All routes are available at both the legacy root path (`/db/put`) and the versioned path (`/api/v1/db/put`). The WebSocket endpoint is at `/ws` only (no `/api/v1` prefix).

---

## Authentication

Three token tiers are supported. Set the tokens as environment variables on the backend:

| Token variable | Role | Access |
|----------------|------|--------|
| `API_TOKEN` | `roleAdmin` (3) | All routes |
| `API_TOKEN_READWRITE` | `roleReadWrite` (2) | Write + read; no admin operations |
| `API_TOKEN_READONLY` | `roleReadOnly` (1) | Get, Scan, Stats, Health |

Supply the token in the `Authorization` header:

```bash
Authorization: Bearer <token>
```

When no tokens are configured, all routes are unauthenticated. When `API_TOKEN` is set but `API_TOKEN_READWRITE` and `API_TOKEN_READONLY` are not, the admin token grants all access.

Token comparison uses `crypto/subtle.ConstantTimeCompare` to prevent timing attacks.

The `/health` endpoint is always unauthenticated.

---

## Core engine operations

### PUT — write a key-value pair

```
POST /db/put
Role: roleReadWrite
Content-Type: application/json
Body size limit: ~1 MB (key ≤ 8 KB, value ≤ 1 MB)
```

Request:

```json
{"key": "hello", "value": "world"}
```

Response `200 OK`:

```json
{"ok": true}
```

```bash
curl -s -X POST http://localhost:8080/db/put \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"key":"hello","value":"world"}'
```

---

### GET — read a key

```
GET /db/get?key=<key>[&consistency=linearizable|eventual]
Role: roleReadOnly
```

Response `200 OK` (found):

```json
{"key": "hello", "value": "world", "found": true}
```

Response `200 OK` (not found):

```json
{"key": "hello", "found": false}
```

```bash
curl -s 'http://localhost:8080/db/get?key=hello' \
  -H 'Authorization: Bearer <token>'
```

In cluster mode, `?consistency=eventual` may serve from follower state without a `VerifyLeader` round-trip.

---

### DELETE — delete a key

```
DELETE /db/delete?key=<key>
Role: roleReadWrite
```

Response `200 OK`:

```json
{"ok": true}
```

```bash
curl -s -X DELETE 'http://localhost:8080/db/delete?key=hello' \
  -H 'Authorization: Bearer <token>'
```

---

### SCAN — range scan

```
GET /db/scan?start=<start>&end=<end>&limit=<n>
Role: roleReadOnly
Max limit: 5000
```

Response `200 OK`:

```json
{
  "results": [
    {"key": "apple", "value": "red"},
    {"key": "banana", "value": "yellow"}
  ],
  "count": 2
}
```

```bash
curl -s 'http://localhost:8080/db/scan?start=a&end=z&limit=100' \
  -H 'Authorization: Bearer <token>'
```

---

### BATCH — write batch

```
POST /db/batch
Role: roleReadWrite
Content-Type: application/json
Body size limit: ~500 MB (500 entries × max key+value)
```

Request:

```json
{
  "entries": [
    {"key": "k1", "value": "v1"},
    {"key": "k2", "value": "v2"},
    {"key": "old-key", "delete": true}
  ]
}
```

Response `200 OK`:

```json
{"ok": true, "applied": 3}
```

```bash
curl -s -X POST http://localhost:8080/db/batch \
  -H 'Authorization: Bearer <token>' \
  -H 'Content-Type: application/json' \
  -d '{"entries":[{"key":"k1","value":"v1"},{"key":"k2","delete":true}]}'
```

---

## Diagnostics

### Stats

```
GET /stats
Role: roleReadOnly
```

Returns a full engine stats snapshot: `seq_no`, `total_sst_files`, `total_sst_bytes`, `memtable_size`, `num_immutables`, `wal_files`, `cache_size`, `cache_hit_rate`.

```bash
curl -s 'http://localhost:8080/stats' -H 'Authorization: Bearer <token>'
```

### Health

```
GET /health
Role: none (always unauthenticated)
```

```json
{"status": "ok"}
```

Returns `200` as long as the HTTP server is alive.

### Ready

```
GET /ready
Role: none
```

Returns `200` when the engine is ready to serve requests. Returns `503` when:
- MANIFEST or WAL is missing
- Immutable backlog exceeds `MaxImmutableMemTables`
- L0 has reached `Level0StopWritesTrigger`

### Levels

```
GET /levels
Role: roleReadOnly
```

Returns all 7 levels with the SSTable list, key ranges, and file sizes for each level.

### Metrics

```
GET /metrics
Role: requires METRICS_TOKEN (or API_TOKEN as fallback)
```

Prometheus text/OpenMetrics exposition format. Bearer-token authenticated when `METRICS_TOKEN` or `API_TOKEN` is configured.

---

## Compaction control

### Force compaction

```
POST /compaction/force
Role: roleAdmin
Content-Type: application/json
```

Request:

```json
{"level": 0}
```

```bash
curl -s -X POST http://localhost:8080/compaction/force \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{"level":0}'
```

### Change compaction style

```
POST /compaction/style
Role: roleAdmin
Content-Type: application/json
```

Valid styles: `leveled`, `size-tiered`, `time-window`.

```bash
curl -s -X POST http://localhost:8080/compaction/style \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{"style":"size-tiered"}'
```

---

## Admin

### Consistent snapshot

```
POST /admin/snapshot
Role: roleAdmin
```

Flushes the active MemTable, waits for the flush to complete, and returns the list of live SSTable files to copy for a consistent backup.

```bash
curl -s -X POST http://localhost:8080/admin/snapshot \
  -H 'Authorization: Bearer <admin-token>' \
  -H 'Content-Type: application/json' \
  -d '{}'
```

Response:

```json
{
  "ok": true,
  "data_dir": "./data",
  "snapshot_files": ["./data/MANIFEST", "./data/000001.sst"],
  "message": "engine flushed; copy all snapshot_files to take a consistent backup"
}
```

---

## Cluster routes

### Cluster status

```
GET /cluster/status
Role: roleReadOnly
```

Returns `role`, `term`, `commit_index`, `last_applied_index`, `leader_id`.

### Cluster leader

```
GET /cluster/leader
Role: roleReadOnly
```

Returns `leader_id` and `client_address`.

### Cluster peers

```
GET /cluster/peers
Role: roleReadOnly
```

Returns current peer registry with node IDs, RPC addresses, and suffrage.

### Add peer

```
POST /cluster/membership/add
Role: roleAdmin
Content-Type: application/json
```

```json
{
  "node_id": "node-4",
  "rpc_address": "127.0.0.1:7004",
  "client_address": "http://127.0.0.1:8083"
}
```

Must be sent to the current leader; non-leaders return `409 Conflict` with `leader_address`.

### Remove peer

```
POST /cluster/membership/remove
Role: roleAdmin
Content-Type: application/json
```

```json
{"node_id": "node-4"}
```

---

## WebSocket

```
ws://localhost:8080/ws
```

Emits a stream of JSON engine events. Every event has the shape:

```json
{"type": "<event_type>", "ts": 1234567890123456789, "extra": {}}
```

Event types include: `wal_append`, `wal_sync`, `memtable_insert`, `memtable_full`, `flush_start`, `flush_complete`, `sstable_created`, `compaction_start`, `compaction_complete`, `bloom_check`, `cache_hit`, `cache_miss`, `amplification`, and others.

When `API_TOKEN` is set, WebSocket clients must supply it via the `Authorization` header or the `?access_token=` query parameter.
