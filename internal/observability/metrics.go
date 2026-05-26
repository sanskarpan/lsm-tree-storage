package observability

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"lsm-engine/internal/cluster"
	"lsm-engine/internal/events"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the Prometheus registry and application collectors.
type Metrics struct {
	registry           *prometheus.Registry
	httpRequestsTotal  *prometheus.CounterVec
	httpRequestSeconds *prometheus.HistogramVec
	eventTotals        *prometheus.CounterVec
}

func NewMetrics(node cluster.Node, clientCount func() int) *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		registry: registry,
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "lsm_http_requests_total",
				Help: "Total number of HTTP requests served by route, method, and status.",
			},
			[]string{"method", "route", "status"},
		),
		httpRequestSeconds: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "lsm_http_request_duration_seconds",
				Help:    "HTTP request latency by route and method.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "route"},
		),
		eventTotals: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "lsm_events_total",
				Help: "Total published engine events by type.",
			},
			[]string{"type"},
		),
	}

	registry.MustRegister(
		m.httpRequestsTotal,
		m.httpRequestSeconds,
		m.eventTotals,
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_engine_ready",
				Help: "Whether the engine currently reports ready for service.",
			},
			func() float64 {
				if node.HealthStatus(nil).Ready {
					return 1
				}
				return 0
			},
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_engine_seq_no",
				Help: "Current global engine sequence number.",
			},
			func() float64 { return float64(toUint64(node.Stats(nil)["seq_no"])) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_engine_total_sst_files",
				Help: "Current number of SSTable files in the manifest.",
			},
			func() float64 { return float64(toInt(node.Stats(nil)["total_sst_files"])) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_engine_total_sst_bytes",
				Help: "Current total SSTable bytes managed by the manifest.",
			},
			func() float64 { return float64(toInt(node.Stats(nil)["total_sst_bytes"])) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_engine_memtable_size_bytes",
				Help: "Approximate mutable memtable size in bytes.",
			},
			func() float64 { return float64(toInt64(node.Stats(nil)["memtable_size"])) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_engine_immutables",
				Help: "Current number of immutable memtables waiting to flush.",
			},
			func() float64 { return float64(toInt(node.Stats(nil)["num_immutables"])) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_engine_wal_files",
				Help: "Current number of WAL files tracked by the engine.",
			},
			func() float64 { return float64(toInt(node.Stats(nil)["wal_files"])) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_cache_entries",
				Help: "Current block-cache entry count.",
			},
			func() float64 { return float64(toInt(node.Stats(nil)["cache_size"])) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_cache_hit_rate",
				Help: "Current block-cache hit rate ratio.",
			},
			func() float64 { return toFloat64(node.Stats(nil)["cache_hit_rate"]) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_ws_clients",
				Help: "Current number of connected WebSocket clients.",
			},
			func() float64 {
				if clientCount == nil {
					return 0
				}
				return float64(clientCount())
			},
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_cluster_enabled",
				Help: "Whether clustered mode is enabled for this node.",
			},
			func() float64 {
				if node.Status(nil).Enabled {
					return 1
				}
				return 0
			},
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_cluster_term",
				Help: "Current Raft term for the local node.",
			},
			func() float64 { return float64(node.Status(nil).Term) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_cluster_commit_index",
				Help: "Current committed Raft log index.",
			},
			func() float64 { return float64(node.Status(nil).CommitIndex) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "lsm_cluster_last_applied_index",
				Help: "Last applied Raft log index recorded by the FSM.",
			},
			func() float64 { return float64(node.Status(nil).LastApplied) },
		),
	)

	if bus := node.EventBus(); bus != nil {
		bus.SubscribeAll(func(evt events.Event) {
			m.eventTotals.WithLabelValues(string(evt.Type)).Inc()
		})
	}
	for _, role := range []string{"standalone", "follower", "candidate", "leader"} {
		roleName := role
		registry.MustRegister(prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name:        "lsm_cluster_role_" + strings.ReplaceAll(roleName, "-", "_"),
				Help:        "Whether the node currently has the " + roleName + " role.",
				ConstLabels: prometheus.Labels{"role": roleName},
			},
			func() float64 {
				if string(node.Status(nil).Role) == roleName {
					return 1
				}
				return 0
			},
		))
	}

	return m
}

func (m *Metrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	code := strconv.Itoa(status)
	m.httpRequestsTotal.WithLabelValues(method, route, code).Inc()
	m.httpRequestSeconds.WithLabelValues(method, route).Observe(duration.Seconds())
}

func (m *Metrics) Handler(authToken string) http.Handler {
	handler := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
	authToken = strings.TrimSpace(authToken)
	if authToken == "" {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := parseBearerToken(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare([]byte(authToken), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lsm-metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func parseBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func toInt(v interface{}) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func toInt64(v interface{}) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func toUint64(v interface{}) uint64 {
	switch value := v.(type) {
	case uint64:
		return value
	case int:
		return uint64(value)
	case int64:
		return uint64(value)
	case float64:
		return uint64(value)
	default:
		return 0
	}
}

func toFloat64(v interface{}) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case uint64:
		return float64(value)
	default:
		return 0
	}
}
