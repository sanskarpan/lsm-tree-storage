// Package main is the entry point for the LSM engine HTTP server.
// It opens the storage engine, wires up the REST and WebSocket gateways,
// and listens on :8080 until an OS interrupt signal is received.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lsm-engine/gateway"
	"lsm-engine/internal/cluster"
	"lsm-engine/internal/engine"
	"lsm-engine/internal/observability"

	"gopkg.in/yaml.v3"
)

func main() {
	logger := newLogger()
	log.SetFlags(0)
	log.SetOutput(observability.NewLegacyLogBridge(logger.With("component", "stdlib"), slog.LevelWarn))

	cfg, err := loadConfig()
	if err != nil {
		fatal(logger, "load_config_failed", err)
	}
	addr := serverAddr()
	apiToken := strings.TrimSpace(os.Getenv("API_TOKEN"))
	if isRemoteBind(addr) && apiToken == "" && os.Getenv("ALLOW_INSECURE_REMOTE") != "1" {
		fatal(logger, "remote_bind_rejected", nil, slog.String("addr", addr))
	}

	clusterCfg, err := loadClusterConfig(cfg.DataDir, addr)
	if err != nil {
		fatal(logger, "load_cluster_config_failed", err)
	}
	if clusterCfg.AuthToken == "" {
		clusterCfg.AuthToken = apiToken
	}
	node, err := openNode(cfg, clusterCfg)
	if err != nil {
		fatal(logger, "open_node_failed", err)
	}
	defer func() { _ = node.Close() }()

	bus := node.EventBus()
	allowedOrigins := parseAllowedOrigins(os.Getenv("ALLOWED_ORIGINS"))
	hub := gateway.NewWSHub(bus, gateway.HubOptions{
		AllowedOrigins: allowedOrigins,
		APIToken:       apiToken,
	})
	handler := gateway.NewHandler(node, hub, gateway.HandlerOptions{
		AllowedOrigins: allowedOrigins,
		APIToken:       apiToken,
	})
	metrics := observability.NewMetrics(node, hub.ClientCount)

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler(metricsToken(apiToken)))
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              addr,
		Handler:           observability.Middleware(logger, metrics, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("server_listening",
			slog.String("addr", addr),
			slog.String("data_dir", cfg.DataDir),
			slog.Bool("auth_enabled", apiToken != ""),
			slog.String("compaction_style", cfg.CompactionStyle),
			slog.Bool("cluster_enabled", clusterCfg.Enabled),
			slog.String("cluster_node_id", node.Status(context.Background()).NodeID),
			slog.String("cluster_role", string(node.Status(context.Background()).Role)),
		)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("server_error", slog.String("addr", addr), slog.Any("error", err))
		}
	}()

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("shutdown_start")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown_failed", slog.Any("error", err))
		return
	}
	logger.Info("shutdown_complete")
}

type fileConfig struct {
	DataDir                        string            `yaml:"data_dir"`
	MemTableSize                   int64             `yaml:"mem_table_size"`
	BlockSize                      int               `yaml:"block_size"`
	BloomBitsPerKey                int               `yaml:"bloom_bits_per_key"`
	SSTMaxSize                     uint64            `yaml:"sst_max_size"`
	SyncWAL                        *bool             `yaml:"sync_wal"`
	MaxOpenFiles                   int               `yaml:"max_open_files"`
	BlockCacheSize                 int64             `yaml:"block_cache_size"`
	MaxLevels                      int               `yaml:"max_levels"`
	LevelSizeMultiplier            int               `yaml:"level_size_multiplier"`
	Level0FileNumCompactionTrigger int               `yaml:"level0_file_num_compaction_trigger"`
	Level0StopWritesTrigger        int               `yaml:"level0_stop_writes_trigger"`
	MaxImmutableMemTables          int               `yaml:"max_immutable_memtables"`
	CompactionStyle                string            `yaml:"compaction_style"`
	TimeWindowSize                 string            `yaml:"time_window_size"`
	Cluster                        clusterFileConfig `yaml:"cluster"`
}

type clusterFileConfig struct {
	Enabled                 *bool                `yaml:"enabled"`
	NodeID                  string               `yaml:"node_id"`
	DataDir                 string               `yaml:"data_dir"`
	BindAddress             string               `yaml:"bind_address"`
	AdvertiseAddress        string               `yaml:"advertise_address"`
	ClientAddress           string               `yaml:"client_address"`
	Bootstrap               *bool                `yaml:"bootstrap"`
	ElectionTimeout         string               `yaml:"election_timeout"`
	HeartbeatInterval       string               `yaml:"heartbeat_interval"`
	CommitTimeout           string               `yaml:"commit_timeout"`
	ApplyTimeout            string               `yaml:"apply_timeout"`
	SnapshotInterval        string               `yaml:"snapshot_interval"`
	SnapshotMinEntries      uint64               `yaml:"snapshot_min_entries"`
	SnapshotRetain          int                  `yaml:"snapshot_retain"`
	TrailingLogs            uint64               `yaml:"trailing_logs"`
	ShardCount              int                  `yaml:"shard_count"`
	ShardPortStride         int                  `yaml:"shard_port_stride"`
	RoutingSlots            int                  `yaml:"routing_slots"`
	RebalanceInterval       string               `yaml:"rebalance_interval"`
	RebalanceThresholdBytes int64                `yaml:"rebalance_threshold_bytes"`
	RebalanceMaxSlots       int                  `yaml:"rebalance_max_slots"`
	TLS                     clusterTLSFileConfig `yaml:"tls"`
	Peers                   []cluster.Peer       `yaml:"peers"`
}

type clusterTLSFileConfig struct {
	Enabled            *bool  `yaml:"enabled"`
	CertFile           string `yaml:"cert_file"`
	KeyFile            string `yaml:"key_file"`
	CAFile             string `yaml:"ca_file"`
	ServerName         string `yaml:"server_name"`
	InsecureSkipVerify *bool  `yaml:"insecure_skip_verify"`
}

func loadConfig() (engine.Config, error) {
	cfg := engine.DefaultConfig("./data")

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	if data, err := os.ReadFile(configPath); err == nil {
		var raw fileConfig
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return engine.Config{}, err
		}
		if raw.DataDir != "" {
			cfg.DataDir = raw.DataDir
		}
		if raw.MemTableSize > 0 {
			cfg.MemTableSize = raw.MemTableSize
		}
		if raw.BlockSize > 0 {
			cfg.BlockSize = raw.BlockSize
		}
		if raw.BloomBitsPerKey > 0 {
			cfg.BloomBitsPerKey = raw.BloomBitsPerKey
		}
		if raw.SSTMaxSize > 0 {
			cfg.SSTMaxSize = raw.SSTMaxSize
		}
		if raw.SyncWAL != nil {
			cfg.SyncWAL = *raw.SyncWAL
		}
		if raw.MaxOpenFiles > 0 {
			cfg.MaxOpenFiles = raw.MaxOpenFiles
		}
		if raw.BlockCacheSize > 0 {
			cfg.BlockCacheSize = raw.BlockCacheSize
		}
		if raw.MaxLevels > 0 {
			cfg.MaxLevels = raw.MaxLevels
		}
		if raw.LevelSizeMultiplier > 0 {
			cfg.LevelSizeMultiplier = raw.LevelSizeMultiplier
		}
		if raw.Level0FileNumCompactionTrigger > 0 {
			cfg.Level0FileNumCompactionTrigger = raw.Level0FileNumCompactionTrigger
		}
		if raw.Level0StopWritesTrigger > 0 {
			cfg.Level0StopWritesTrigger = raw.Level0StopWritesTrigger
		}
		if raw.MaxImmutableMemTables > 0 {
			cfg.MaxImmutableMemTables = raw.MaxImmutableMemTables
		}
		if raw.CompactionStyle != "" {
			cfg.CompactionStyle = raw.CompactionStyle
		}
		if raw.TimeWindowSize != "" {
			d, err := time.ParseDuration(raw.TimeWindowSize)
			if err != nil {
				return engine.Config{}, err
			}
			cfg.TimeWindowSize = d
		}
	} else if !os.IsNotExist(err) {
		return engine.Config{}, err
	}

	if dir := os.Getenv("DATA_DIR"); dir != "" {
		cfg.DataDir = dir
	}

	return cfg, nil
}

func loadClusterConfig(engineDataDir, serverAddr string) (cluster.Config, error) {
	cfg := cluster.DefaultConfig()
	cfg.NodeID = "standalone"
	cfg.ClientAddress = serverAddr
	cfg.DataDir = filepath.Join(engineDataDir, "_cluster")

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	if data, err := os.ReadFile(configPath); err == nil {
		var raw fileConfig
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return cluster.Config{}, err
		}
		if raw.Cluster.Enabled != nil {
			cfg.Enabled = *raw.Cluster.Enabled
		}
		if raw.Cluster.NodeID != "" {
			cfg.NodeID = raw.Cluster.NodeID
		}
		if raw.Cluster.DataDir != "" {
			cfg.DataDir = raw.Cluster.DataDir
		}
		if raw.Cluster.BindAddress != "" {
			cfg.BindAddress = raw.Cluster.BindAddress
		}
		if raw.Cluster.AdvertiseAddress != "" {
			cfg.AdvertiseAddress = raw.Cluster.AdvertiseAddress
		}
		if raw.Cluster.ClientAddress != "" {
			cfg.ClientAddress = raw.Cluster.ClientAddress
		}
		if raw.Cluster.Bootstrap != nil {
			cfg.Bootstrap = *raw.Cluster.Bootstrap
		}
		if raw.Cluster.ElectionTimeout != "" {
			d, err := time.ParseDuration(raw.Cluster.ElectionTimeout)
			if err != nil {
				return cluster.Config{}, err
			}
			cfg.ElectionTimeout = d
		}
		if raw.Cluster.HeartbeatInterval != "" {
			d, err := time.ParseDuration(raw.Cluster.HeartbeatInterval)
			if err != nil {
				return cluster.Config{}, err
			}
			cfg.HeartbeatInterval = d
		}
		if raw.Cluster.CommitTimeout != "" {
			d, err := time.ParseDuration(raw.Cluster.CommitTimeout)
			if err != nil {
				return cluster.Config{}, err
			}
			cfg.CommitTimeout = d
		}
		if raw.Cluster.ApplyTimeout != "" {
			d, err := time.ParseDuration(raw.Cluster.ApplyTimeout)
			if err != nil {
				return cluster.Config{}, err
			}
			cfg.ApplyTimeout = d
		}
		if raw.Cluster.SnapshotInterval != "" {
			d, err := time.ParseDuration(raw.Cluster.SnapshotInterval)
			if err != nil {
				return cluster.Config{}, err
			}
			cfg.SnapshotInterval = d
		}
		if raw.Cluster.SnapshotMinEntries > 0 {
			cfg.SnapshotMinEntries = raw.Cluster.SnapshotMinEntries
		}
		if raw.Cluster.SnapshotRetain > 0 {
			cfg.SnapshotRetain = raw.Cluster.SnapshotRetain
		}
		if raw.Cluster.TrailingLogs > 0 {
			cfg.TrailingLogs = raw.Cluster.TrailingLogs
		}
		if raw.Cluster.ShardCount > 0 {
			cfg.ShardCount = raw.Cluster.ShardCount
		}
		if raw.Cluster.ShardPortStride > 0 {
			cfg.ShardPortStride = raw.Cluster.ShardPortStride
		}
		if raw.Cluster.RoutingSlots > 0 {
			cfg.RoutingSlots = raw.Cluster.RoutingSlots
		}
		if raw.Cluster.RebalanceInterval != "" {
			d, err := time.ParseDuration(raw.Cluster.RebalanceInterval)
			if err != nil {
				return cluster.Config{}, err
			}
			cfg.RebalanceInterval = d
		}
		if raw.Cluster.RebalanceThresholdBytes > 0 {
			cfg.RebalanceThresholdBytes = raw.Cluster.RebalanceThresholdBytes
		}
		if raw.Cluster.RebalanceMaxSlots > 0 {
			cfg.RebalanceMaxSlots = raw.Cluster.RebalanceMaxSlots
		}
		if raw.Cluster.TLS.Enabled != nil {
			cfg.TLS.Enabled = *raw.Cluster.TLS.Enabled
		}
		if raw.Cluster.TLS.CertFile != "" {
			cfg.TLS.CertFile = raw.Cluster.TLS.CertFile
		}
		if raw.Cluster.TLS.KeyFile != "" {
			cfg.TLS.KeyFile = raw.Cluster.TLS.KeyFile
		}
		if raw.Cluster.TLS.CAFile != "" {
			cfg.TLS.CAFile = raw.Cluster.TLS.CAFile
		}
		if raw.Cluster.TLS.ServerName != "" {
			cfg.TLS.ServerName = raw.Cluster.TLS.ServerName
		}
		if raw.Cluster.TLS.InsecureSkipVerify != nil {
			cfg.TLS.InsecureSkipVerify = *raw.Cluster.TLS.InsecureSkipVerify
		}
		if len(raw.Cluster.Peers) > 0 {
			cfg.Peers = append([]cluster.Peer(nil), raw.Cluster.Peers...)
		}
	} else if !os.IsNotExist(err) {
		return cluster.Config{}, err
	}

	if raw := strings.TrimSpace(os.Getenv("CLUSTER_ENABLED")); raw != "" {
		cfg.Enabled = raw == "1" || strings.EqualFold(raw, "true")
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_NODE_ID")); raw != "" {
		cfg.NodeID = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_DATA_DIR")); raw != "" {
		cfg.DataDir = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_BIND_ADDR")); raw != "" {
		cfg.BindAddress = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_ADVERTISE_ADDR")); raw != "" {
		cfg.AdvertiseAddress = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_CLIENT_ADDR")); raw != "" {
		cfg.ClientAddress = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_BOOTSTRAP")); raw != "" {
		cfg.Bootstrap = raw == "1" || strings.EqualFold(raw, "true")
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_ELECTION_TIMEOUT")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.ElectionTimeout = d
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_HEARTBEAT_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.HeartbeatInterval = d
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_COMMIT_TIMEOUT")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.CommitTimeout = d
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_APPLY_TIMEOUT")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.ApplyTimeout = d
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_SNAPSHOT_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.SnapshotInterval = d
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_SNAPSHOT_MIN_ENTRIES")); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.SnapshotMinEntries = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_SNAPSHOT_RETAIN")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.SnapshotRetain = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_TRAILING_LOGS")); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.TrailingLogs = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_SHARD_COUNT")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.ShardCount = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_SHARD_PORT_STRIDE")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.ShardPortStride = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_ROUTING_SLOTS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.RoutingSlots = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_REBALANCE_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.RebalanceInterval = d
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_REBALANCE_THRESHOLD_BYTES")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.RebalanceThresholdBytes = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_REBALANCE_MAX_SLOTS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.RebalanceMaxSlots = n
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_TLS_ENABLED")); raw != "" {
		cfg.TLS.Enabled = raw == "1" || strings.EqualFold(raw, "true")
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_TLS_CERT_FILE")); raw != "" {
		cfg.TLS.CertFile = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_TLS_KEY_FILE")); raw != "" {
		cfg.TLS.KeyFile = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_TLS_CA_FILE")); raw != "" {
		cfg.TLS.CAFile = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_TLS_SERVER_NAME")); raw != "" {
		cfg.TLS.ServerName = raw
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_TLS_INSECURE_SKIP_VERIFY")); raw != "" {
		cfg.TLS.InsecureSkipVerify = raw == "1" || strings.EqualFold(raw, "true")
	}
	if raw := strings.TrimSpace(os.Getenv("CLUSTER_PEERS")); raw != "" {
		peers, err := parseClusterPeers(raw)
		if err != nil {
			return cluster.Config{}, err
		}
		cfg.Peers = peers
	}

	if cfg.Enabled && cfg.BindAddress == "" {
		return cluster.Config{}, fmt.Errorf("cluster bind address required when clustering is enabled")
	}
	return cfg, nil
}

func openNode(cfg engine.Config, clusterCfg cluster.Config) (cluster.Node, error) {
	if clusterCfg.ShardCount > 1 {
		return cluster.OpenShardedNode(clusterCfg, cfg)
	}
	if !clusterCfg.Enabled {
		eng, err := engine.Open(cfg)
		if err != nil {
			return nil, err
		}
		return cluster.NewStandaloneNode(clusterCfg.NodeID, eng), nil
	}
	return cluster.OpenRaftNode(clusterCfg, cfg)
}

func parseClusterPeers(raw string) ([]cluster.Peer, error) {
	parts := strings.Split(raw, ",")
	peers := make([]cluster.Peer, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, "@")
		if len(fields) < 2 || len(fields) > 3 {
			return nil, fmt.Errorf("invalid cluster peer %q; want nodeID@rpcAddr[@clientAddr]", part)
		}
		peer := cluster.Peer{
			NodeID:     strings.TrimSpace(fields[0]),
			RPCAddress: strings.TrimSpace(fields[1]),
		}
		if len(fields) == 3 {
			peer.ClientAddress = strings.TrimSpace(fields[2])
		}
		if peer.NodeID == "" || peer.RPCAddress == "" {
			return nil, fmt.Errorf("invalid cluster peer %q", part)
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

func serverAddr() string {
	if addr := os.Getenv("ADDR"); addr != "" {
		return addr
	}
	if port := os.Getenv("PORT"); port != "" {
		if _, err := strconv.Atoi(port); err == nil {
			return "127.0.0.1:" + port
		}
		return port
	}
	return "127.0.0.1:8080"
}

func parseAllowedOrigins(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func metricsToken(apiToken string) string {
	if token := strings.TrimSpace(os.Getenv("METRICS_TOKEN")); token != "" {
		return token
	}
	return apiToken
}

func isRemoteBind(addr string) bool {
	host := addr
	if strings.HasPrefix(addr, ":") {
		return true
	}
	if parsedHost, _, err := net.SplitHostPort(addr); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	return true
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("LOG_FORMAT")), "text") {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func fatal(logger *slog.Logger, msg string, err error, attrs ...slog.Attr) {
	args := make([]any, 0, len(attrs)+2)
	if err != nil {
		args = append(args, slog.Any("error", err))
	}
	for _, attr := range attrs {
		args = append(args, attr)
	}
	logger.Error(msg, args...)
	os.Exit(1)
}
