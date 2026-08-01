# Contributing

---

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.26.3+ | Backend engine and server |
| Bun | Latest | Frontend BFF and React dashboard |
| Docker | 20+ | Integration tests, Docker Compose stack |
| golangci-lint | Latest | Required for `make lint` |

Install Bun:

```bash
curl -fsSL https://bun.sh/install | bash
```

---

## Run tests

```bash
# All unit and integration tests, single run
go test ./... -race -count=1

# With race detector, 3 repetitions (catches flaky concurrency bugs)
make test-race

# Specific package
go test ./internal/engine/... -race -v
go test ./internal/wal/... -race -v
go test ./internal/manifest/... -race -v
```

The test suite covers: WAL round-trip, skip-list correctness, SSTable builder/reader, Bloom filter, MANIFEST apply/recover, compaction correctness (all three strategies), and crash recovery.

---

## Run fuzz tests

```bash
# WAL entry binary encoding round-trip
go test ./internal/wal/... -fuzz=FuzzWALEntryRoundtrip -fuzztime=30s

# VersionEdit encoding round-trip
go test ./internal/manifest/... -fuzz=FuzzVersionEditRoundtrip -fuzztime=30s
```

Fuzz tests use Go's built-in fuzzer (`testing.F`). Add new corpus seeds in `testdata/fuzz/<TestName>/` to improve coverage of known edge cases.

---

## Run the frontend

```bash
cd frontend
bun install
bun run dev     # hot-reload development server at :3001
# OR
bun run index.ts  # production BFF server
```

The BFF proxies all REST and WebSocket traffic to the Go backend at `http://localhost:8080`.

---

## CI requirements

All pull requests must pass the following jobs:

| Job | Command | What it checks |
|-----|---------|----------------|
| `go` | `go test ./... -race -count=1` + `govulncheck ./...` | Unit/integration tests with race detector; known CVEs |
| `frontend` | `bun run build` (inside `frontend/`) | TypeScript compilation, no type errors |
| `docker` | `make docker-build` | Backend and frontend images build successfully |

`govulncheck` is run automatically in CI. To run locally:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

---

## Good first issues

| Area | Description |
|------|-------------|
| Benchmarks | Add benchmark coverage for `Scan` and `WriteBatch` operations |
| Fuzz corpus | Add more seed inputs to `testdata/fuzz/` for WAL and MANIFEST fuzzers |
| Compaction | Implement a new heuristic for LCS SSTable pick order (e.g., key-range coverage, not just age) |
| Dashboard | Add a new panel visualising the MANIFEST VersionEdit log |
| Docs | Add examples to the API reference for less-common routes (`/amplification`, `/bloom/:fileID`) |

---

## ADR process

When making a significant architectural decision:

1. Create `docs/adr/NNNN-title.md` using the Nygard format (Status / Context / Decision / Consequences).
2. Add a row to `docs/adr/index.md`.
3. Reference the ADR from the relevant component docs page.

Use the existing ADRs (`0001`–`0004`) as style examples.

---

## Code style

- Follow standard `gofmt` / `goimports` formatting. The CI `golangci-lint` run enforces this.
- Exported functions must have doc comments.
- Error strings must be lowercase and not end with punctuation (Go convention).
- Background goroutines must have a documented exit condition (channel close, context cancel, or engine close).
