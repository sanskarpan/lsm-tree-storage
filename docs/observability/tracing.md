# OTel Tracing

Distributed tracing is implemented in `internal/observability/tracing.go` using the OpenTelemetry Go SDK with an OTLP/gRPC exporter.

---

## Enabling tracing

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to the gRPC address of your OTLP collector:

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
go run ./cmd/server/main.go
```

When the variable is unset (the default), tracing is disabled completely. The no-op tracer provider has zero performance impact — no spans are created, no allocations, no network I/O.

```text
# Log message when endpoint is set:
tracing: OTLP exporter connected to localhost:4317

# Log message when endpoint is absent:
tracing: OTEL_EXPORTER_OTLP_ENDPOINT not set; tracing disabled
```

---

## Service identity

The tracer is initialised in `cmd/server/main.go`:

```go
shutdownTracing := observability.InitTracer("lsm-storage", "1.0.0")
```

| OTel attribute | Value |
|----------------|-------|
| `service.name` | `lsm-storage` |
| `service.version` | `1.0.0` |

These attributes are embedded in every span's resource and appear in trace backends under those names.

---

## Sampler

The SDK is configured with `sdktrace.AlwaysSample()`. Every request is traced. For production use, configure **head sampling at the collector side** (e.g., in the OpenTelemetry Collector's `tail_sampling` processor) rather than in the SDK. This avoids losing partial traces from probabilistic SDK-side sampling.

---

## Propagation

W3C TraceContext propagation is enabled via:

```go
otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
    propagation.TraceContext{},
    propagation.Baggage{},
))
```

Inbound HTTP requests are checked for `traceparent` and `tracestate` headers. If present, the span is created as a child of the incoming trace. If absent, a new root span is started.

Outbound requests (e.g., leader forwarding in cluster mode) propagate the trace context via the same `traceparent` header.

---

## Span per HTTP request

The HTTP middleware in `internal/observability/http.go` creates one span per request with attributes:

| Attribute | Example value |
|-----------|---------------|
| `http.method` | `POST` |
| `http.route` | `/db/put` |
| `http.status_code` | `200` |
| `net.peer.ip` | `127.0.0.1` |

The span name is `<METHOD> <route>` (e.g., `POST /db/put`).

---

## Compatible backends

Any OTLP-compatible backend works. Common options:

| Backend | Docker image / endpoint |
|---------|------------------------|
| Jaeger | `jaegertracing/all-in-one`; OTLP gRPC on `:4317` |
| Grafana Tempo | `grafana/tempo`; OTLP gRPC on `:4317` |
| Honeycomb | `api.honeycomb.io:443` (with API key header) |
| OpenTelemetry Collector | Acts as a fan-out proxy to multiple backends |

---

## Quick start with Jaeger

```bash
# Run Jaeger all-in-one with OTLP gRPC enabled
docker run -d --name jaeger \
  -p 4317:4317 \
  -p 16686:16686 \
  jaegertracing/all-in-one:latest

# Start the engine with tracing
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 go run ./cmd/server/main.go

# View traces
open http://localhost:16686
```

---

## Exporter configuration

The OTLP gRPC connection is created with insecure credentials by default:

```go
conn, err := grpc.NewClient(endpoint,
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

For TLS-secured OTLP endpoints (e.g., Honeycomb, cloud-hosted Tempo), use the standard `OTEL_EXPORTER_OTLP_HEADERS` environment variable to pass API keys, and configure TLS at the collector or via a sidecar proxy.
