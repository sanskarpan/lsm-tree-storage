package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
)

type RaftNode struct {
	mu sync.RWMutex

	cfg        Config
	engineCfg  engine.Config
	clusterDir string

	eng *engine.LSMEngine
	bus *events.EventBus

	raft          *raft.Raft
	transport     *raft.NetworkTransport
	logStore      *raftboltdb.BoltStore
	stableStore   *raftboltdb.BoltStore
	snapshotStore raft.SnapshotStore

	appliedStore   *appliedStateStore
	peerStore      *peerRegistryStore
	applied        appliedState
	peerRegistry   map[string]Peer
	httpClient     *http.Client
	readLeaseMu    sync.RWMutex
	readLeaseUntil time.Time
}

func OpenRaftNode(cfg Config, engCfg engine.Config) (*RaftNode, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, fmt.Errorf("cluster: node_id required")
	}
	if strings.TrimSpace(cfg.BindAddress) == "" {
		return nil, fmt.Errorf("cluster: bind_address required")
	}
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(engCfg.DataDir, clusterDirName)
	}
	if cfg.AdvertiseAddress == "" {
		cfg.AdvertiseAddress = cfg.BindAddress
	}
	if cfg.ApplyTimeout <= 0 {
		cfg.ApplyTimeout = 10 * time.Second
	}
	if cfg.SnapshotRetain <= 0 {
		cfg.SnapshotRetain = 2
	}
	if cfg.TrailingLogs == 0 {
		cfg.TrailingLogs = 256
	}

	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, err
	}
	eng, err := engine.Open(engCfg)
	if err != nil {
		return nil, err
	}

	node := &RaftNode{
		cfg:          cfg,
		engineCfg:    engCfg,
		clusterDir:   cfg.DataDir,
		eng:          eng,
		bus:          events.NewEventBus(),
		appliedStore: newAppliedStateStore(filepath.Join(cfg.DataDir, "applied-state.json")),
		peerStore:    newPeerRegistryStore(filepath.Join(cfg.DataDir, "peers.json")),
		peerRegistry: map[string]Peer{},
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
	node.bridgeEngineBus(eng)

	if node.applied, err = node.appliedStore.Load(); err != nil {
		_ = eng.Close()
		return nil, fmt.Errorf("cluster: load applied state: %w", err)
	}
	if err := node.seedPeerRegistry(); err != nil {
		_ = eng.Close()
		return nil, fmt.Errorf("cluster: load peer registry: %w", err)
	}

	if err := node.openRaft(); err != nil {
		_ = eng.Close()
		return nil, err
	}
	return node, nil
}

func (n *RaftNode) openRaft() error {
	logStore, err := raftboltdb.NewBoltStore(filepath.Join(n.clusterDir, "raft.db"))
	if err != nil {
		return fmt.Errorf("cluster: open raft log store: %w", err)
	}
	snapshots, err := raft.NewFileSnapshotStore(filepath.Join(n.clusterDir, "snapshots"), n.cfg.SnapshotRetain, os.Stderr)
	if err != nil {
		_ = logStore.Close()
		return fmt.Errorf("cluster: open snapshot store: %w", err)
	}

	stream, err := newStreamLayer(n.cfg)
	if err != nil {
		_ = logStore.Close()
		return fmt.Errorf("cluster: open transport stream: %w", err)
	}
	transport := raft.NewNetworkTransportWithConfig(&raft.NetworkTransportConfig{
		Stream:          stream,
		MaxPool:         3,
		MaxRPCsInFlight: 2,
		Timeout:         10 * time.Second,
	})
	transport.SetHeartbeatHandler(nil)

	raftCfg := raft.DefaultConfig()
	raftCfg.LocalID = raft.ServerID(n.cfg.NodeID)
	if n.cfg.HeartbeatInterval > 0 {
		raftCfg.HeartbeatTimeout = n.cfg.HeartbeatInterval
		raftCfg.LeaderLeaseTimeout = n.cfg.HeartbeatInterval
	}
	if n.cfg.ElectionTimeout > 0 {
		raftCfg.ElectionTimeout = n.cfg.ElectionTimeout
	}
	if n.cfg.CommitTimeout > 0 {
		raftCfg.CommitTimeout = n.cfg.CommitTimeout
	}
	if n.cfg.SnapshotInterval > 0 {
		raftCfg.SnapshotInterval = n.cfg.SnapshotInterval
	}
	if n.cfg.SnapshotMinEntries > 0 {
		raftCfg.SnapshotThreshold = n.cfg.SnapshotMinEntries
	}
	raftCfg.TrailingLogs = n.cfg.TrailingLogs
	raftCfg.ShutdownOnRemove = true

	existing, err := raft.HasExistingState(logStore, logStore, snapshots)
	if err != nil {
		_ = transport.Close()
		_ = logStore.Close()
		return fmt.Errorf("cluster: inspect existing state: %w", err)
	}
	if !existing && n.cfg.Bootstrap {
		if err := raft.BootstrapCluster(raftCfg, logStore, logStore, snapshots, transport, n.bootstrapConfiguration()); err != nil && !strings.Contains(err.Error(), "bootstrap only works on new clusters") {
			_ = transport.Close()
			_ = logStore.Close()
			return fmt.Errorf("cluster: bootstrap: %w", err)
		}
	}

	fsm := &raftFSM{node: n}
	raftNode, err := raft.NewRaft(raftCfg, fsm, logStore, logStore, snapshots, transport)
	if err != nil {
		_ = transport.Close()
		_ = logStore.Close()
		return fmt.Errorf("cluster: open raft: %w", err)
	}

	n.logStore = logStore
	n.stableStore = logStore
	n.snapshotStore = snapshots
	n.transport = transport
	n.raft = raftNode
	return nil
}

func (n *RaftNode) bootstrapConfiguration() raft.Configuration {
	servers := []raft.Server{{
		ID:       raft.ServerID(n.cfg.NodeID),
		Address:  raft.ServerAddress(n.cfg.AdvertiseAddress),
		Suffrage: raft.Voter,
	}}
	seen := map[string]struct{}{n.cfg.NodeID: {}}
	for _, peer := range n.cfg.Peers {
		if peer.NodeID == "" || peer.RPCAddress == "" {
			continue
		}
		if _, ok := seen[peer.NodeID]; ok {
			continue
		}
		seen[peer.NodeID] = struct{}{}
		servers = append(servers, raft.Server{
			ID:       raft.ServerID(peer.NodeID),
			Address:  raft.ServerAddress(peer.RPCAddress),
			Suffrage: raft.Voter,
		})
	}
	return raft.Configuration{Servers: servers}
}

func (n *RaftNode) bridgeEngineBus(eng *engine.LSMEngine) {
	if eng == nil {
		return
	}
	if bus := eng.EventBus(); bus != nil {
		bus.SubscribeAll(func(evt events.Event) {
			n.bus.Publish(evt)
		})
	}
}

func (n *RaftNode) currentEngine() *engine.LSMEngine {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.eng
}

func (n *RaftNode) EventBus() *events.EventBus {
	return n.bus
}

func (n *RaftNode) Status(context.Context) Status {
	stats := n.raft.Stats()
	role := mapRaftRole(n.raft.State())
	_, leaderID := n.raft.LeaderWithID()
	appliedIndex := parseUint64(stats["applied_index"])
	if appliedIndex < n.applied.LastAppliedIndex {
		appliedIndex = n.applied.LastAppliedIndex
	}
	return Status{
		Enabled:       true,
		NodeID:        n.cfg.NodeID,
		Role:          role,
		LeaderID:      string(leaderID),
		Term:          parseUint64(stats["term"]),
		CommitIndex:   parseUint64(stats["commit_index"]),
		LastApplied:   appliedIndex,
		PeerCount:     int(parseUint64(stats["num_peers"])) + 1,
		ReadOnlyCause: n.readOnlyCause(role),
		ShardCount:    1,
		Shards:        n.Shards(context.Background()),
	}
}

func (n *RaftNode) Shards(context.Context) []ShardStatus {
	status := ShardStatus{
		ShardID:      "shard-0",
		NodeID:       n.cfg.NodeID,
		Role:         mapRaftRole(n.raft.State()),
		Term:         parseUint64(n.raft.Stats()["term"]),
		CommitIndex:  parseUint64(n.raft.Stats()["commit_index"]),
		LastApplied:  maxUint64(parseUint64(n.raft.Stats()["applied_index"]), n.applied.LastAppliedIndex),
		PeerCount:    int(parseUint64(n.raft.Stats()["num_peers"])) + 1,
		KeyspaceHint: "all keys",
	}
	leaderAddr, leaderID := n.raft.LeaderWithID()
	status.LeaderID = string(leaderID)
	status.Leader = n.resolveClientAddress(string(leaderID), string(leaderAddr))
	return []ShardStatus{status}
}

func (n *RaftNode) readOnlyCause(role Role) string {
	if role == RoleLeader {
		return ""
	}
	if n.raft.State() == raft.Shutdown {
		return "node is shutting down"
	}
	return "reads and writes are leader-routed in clustered mode"
}

func (n *RaftNode) LeaderAddress(context.Context) string {
	leaderAddr, leaderID := n.raft.LeaderWithID()
	return n.resolveClientAddress(string(leaderID), string(leaderAddr))
}

func (n *RaftNode) Peers(context.Context) []Peer {
	future := n.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		n.mu.RLock()
		defer n.mu.RUnlock()
		return peersToSortedSlice(n.peerRegistry)
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	peers := make([]Peer, 0, len(future.Configuration().Servers))
	for _, server := range future.Configuration().Servers {
		peer := Peer{
			NodeID:     string(server.ID),
			RPCAddress: string(server.Address),
			Suffrage:   fmt.Sprintf("%v", server.Suffrage),
		}
		if known, ok := n.peerRegistry[peer.NodeID]; ok {
			if known.ClientAddress != "" {
				peer.ClientAddress = known.ClientAddress
			}
		}
		if peer.NodeID == n.cfg.NodeID && peer.ClientAddress == "" {
			peer.ClientAddress = n.cfg.ClientAddress
		}
		peers = append(peers, peer)
	}
	return peers
}

func (n *RaftNode) AddPeer(ctx context.Context, peer Peer) error {
	if strings.TrimSpace(peer.NodeID) == "" || strings.TrimSpace(peer.RPCAddress) == "" {
		return fmt.Errorf("cluster: node_id and rpc_address required")
	}
	if err := n.requireLeader(); err != nil {
		return err
	}
	timeout := n.cfg.ApplyTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	future := n.raft.AddVoter(raft.ServerID(peer.NodeID), raft.ServerAddress(peer.RPCAddress), 0, timeout)
	if err := future.Error(); err != nil {
		return n.mapRaftError(err)
	}
	n.mu.Lock()
	n.peerRegistry[peer.NodeID] = peer
	err := n.peerStore.Save(n.peerRegistry)
	n.mu.Unlock()
	return err
}

func (n *RaftNode) RemovePeer(ctx context.Context, nodeID string) error {
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("cluster: node_id required")
	}
	if err := n.requireLeader(); err != nil {
		return err
	}
	timeout := n.cfg.ApplyTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	future := n.raft.RemoveServer(raft.ServerID(nodeID), 0, timeout)
	if err := future.Error(); err != nil {
		return n.mapRaftError(err)
	}
	n.mu.Lock()
	delete(n.peerRegistry, nodeID)
	err := n.peerStore.Save(n.peerRegistry)
	n.mu.Unlock()
	return err
}

func (n *RaftNode) Put(ctx context.Context, key, value []byte) error {
	if err := n.submit(ctx, Command{
		Type:  CommandPut,
		Key:   append([]byte(nil), key...),
		Value: append([]byte(nil), value...),
	}); err != nil {
		if _, forwardErr := n.forwardPut(ctx, key, value); forwardErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (n *RaftNode) Delete(ctx context.Context, key []byte) error {
	if err := n.submit(ctx, Command{
		Type: CommandDelete,
		Key:  append([]byte(nil), key...),
	}); err != nil {
		if _, forwardErr := n.forwardDelete(ctx, key); forwardErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (n *RaftNode) Write(ctx context.Context, batch *engine.WriteBatch) error {
	if batch == nil {
		return nil
	}
	entries := batch.Entries()
	cmd := Command{
		Type:    CommandWriteBatch,
		Entries: make([]BatchEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		cmd.Entries = append(cmd.Entries, BatchEntry{
			Key:    append([]byte(nil), entry.Key...),
			Value:  append([]byte(nil), entry.Value...),
			Delete: entry.Delete,
		})
	}
	if err := n.submit(ctx, cmd); err != nil {
		if _, forwardErr := n.forwardBatch(ctx, entries); forwardErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (n *RaftNode) submit(ctx context.Context, cmd Command) error {
	if err := n.requireLeader(); err != nil {
		return err
	}
	data, err := cmd.Marshal()
	if err != nil {
		return err
	}
	timeout := n.cfg.ApplyTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	future := n.raft.Apply(data, timeout)
	done := make(chan error, 1)
	go func() {
		done <- future.Error()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return n.mapRaftError(err)
		}
		if respErr, ok := future.Response().(error); ok && respErr != nil {
			return respErr
		}
		n.refreshReadLease()
		return nil
	}
}

func (n *RaftNode) mapRaftError(err error) error {
	switch {
	case errors.Is(err, raft.ErrNotLeader), errors.Is(err, raft.ErrLeadershipLost):
		leaderAddr, leaderID := n.raft.LeaderWithID()
		return &NotLeaderError{
			LeaderID:      string(leaderID),
			LeaderAddress: n.resolveClientAddress(string(leaderID), string(leaderAddr)),
		}
	case errors.Is(err, raft.ErrAbortedByRestore):
		return fmt.Errorf("cluster: write aborted by snapshot restore")
	default:
		return err
	}
}

func (n *RaftNode) requireLeader() error {
	if n.raft.State() == raft.Leader {
		return nil
	}
	leaderAddr, leaderID := n.raft.LeaderWithID()
	return &NotLeaderError{
		LeaderID:      string(leaderID),
		LeaderAddress: n.resolveClientAddress(string(leaderID), string(leaderAddr)),
	}
}

func (n *RaftNode) linearizableRead(ctx context.Context) error {
	if n.raft.State() != raft.Leader {
		return n.requireLeader()
	}
	if n.readLeaseValid() {
		return nil
	}
	verify := n.raft.VerifyLeader()
	done := make(chan error, 1)
	go func() {
		done <- verify.Error()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return n.mapRaftError(err)
		}
		n.refreshReadLease()
		return nil
	}
}

func (n *RaftNode) Get(ctx context.Context, key []byte) ([]byte, error) {
	return n.GetWithConsistency(ctx, key, ReadConsistencyLinearizable)
}

func (n *RaftNode) GetWithConsistency(ctx context.Context, key []byte, consistency ReadConsistency) ([]byte, error) {
	if normalizeReadConsistency(consistency) == ReadConsistencyEventual {
		return n.currentEngine().Get(key)
	}
	if err := n.linearizableRead(ctx); err != nil {
		if value, forwardErr := n.forwardGet(ctx, key); forwardErr == nil {
			return value, nil
		}
		return nil, err
	}
	return n.currentEngine().Get(key)
}

func (n *RaftNode) Scan(ctx context.Context, start, end []byte, limit int) ([][2]string, error) {
	return n.ScanWithConsistency(ctx, start, end, limit, ReadConsistencyLinearizable)
}

func (n *RaftNode) ScanWithConsistency(ctx context.Context, start, end []byte, limit int, consistency ReadConsistency) ([][2]string, error) {
	if normalizeReadConsistency(consistency) == ReadConsistencyEventual {
		return n.currentEngine().Scan(start, end, limit), nil
	}
	if err := n.linearizableRead(ctx); err != nil {
		if results, forwardErr := n.forwardScan(ctx, start, end, limit); forwardErr == nil {
			return results, nil
		}
		return nil, err
	}
	return n.currentEngine().Scan(start, end, limit), nil
}

func (n *RaftNode) Version(context.Context) *manifest.Version {
	return n.currentEngine().Manifest().Current()
}

func (n *RaftNode) Stats(context.Context) map[string]interface{} {
	stats := n.currentEngine().Stats()
	clusterStatus := n.Status(context.Background())
	stats["cluster_enabled"] = true
	stats["cluster_role"] = string(clusterStatus.Role)
	stats["cluster_term"] = clusterStatus.Term
	stats["cluster_commit_index"] = clusterStatus.CommitIndex
	stats["cluster_last_applied"] = clusterStatus.LastApplied
	return stats
}

func (n *RaftNode) HealthStatus(context.Context) engine.HealthStatus {
	status := n.currentEngine().HealthStatus()
	clusterStatus := n.Status(context.Background())
	if clusterStatus.Role != RoleLeader && clusterStatus.Role != RoleFollower {
		status.Ready = false
		status.Reasons = append(status.Reasons, "cluster has no stable leader")
	}
	if clusterStatus.Role == RoleFollower && clusterStatus.LeaderID == "" {
		status.Ready = false
		status.Reasons = append(status.Reasons, "follower has no known leader")
	}
	return status
}

func (n *RaftNode) RuntimeState(context.Context) engine.RuntimeState {
	return n.currentEngine().State()
}

func (n *RaftNode) EngineConfig(context.Context) engine.Config {
	return n.currentEngine().Config()
}

func (n *RaftNode) MemTableSnapshot(_ context.Context, limit int) engine.MemTablesSnapshot {
	return n.currentEngine().MemTableSnapshot(limit)
}

func (n *RaftNode) ForceFlush(context.Context) {
	n.currentEngine().ForceFlush()
}

func (n *RaftNode) ForceCompaction(_ context.Context, level int) {
	n.currentEngine().ForceCompaction(level)
}

func (n *RaftNode) SetCompactionStyle(_ context.Context, style string) {
	n.currentEngine().SetCompactionStyle(style)
}

func (n *RaftNode) Close() error {
	var firstErr error
	if n.raft != nil {
		if err := n.raft.Shutdown().Error(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if n.transport != nil {
		if err := n.transport.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if n.logStore != nil {
		if err := n.logStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.eng != nil {
		if err := n.eng.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (n *RaftNode) resolveClientAddress(leaderID, leaderRPC string) string {
	if leaderID == "" && leaderRPC == "" {
		return ""
	}
	if leaderID == n.cfg.NodeID || leaderRPC == n.cfg.AdvertiseAddress || leaderRPC == n.cfg.BindAddress {
		return normalizeClientAddress(n.cfg.ClientAddress)
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, peer := range n.peerRegistry {
		if peer.NodeID == leaderID || peer.RPCAddress == leaderRPC {
			if peer.ClientAddress != "" {
				return normalizeClientAddress(peer.ClientAddress)
			}
		}
	}
	return normalizeClientAddress(leaderRPC)
}

func (n *RaftNode) refreshReadLease() {
	lease := n.cfg.HeartbeatInterval
	if lease <= 0 {
		lease = 500 * time.Millisecond
	}
	n.readLeaseMu.Lock()
	n.readLeaseUntil = time.Now().Add(lease)
	n.readLeaseMu.Unlock()
}

func (n *RaftNode) readLeaseValid() bool {
	n.readLeaseMu.RLock()
	defer n.readLeaseMu.RUnlock()
	return time.Now().Before(n.readLeaseUntil)
}

func (n *RaftNode) seedPeerRegistry() error {
	loaded, err := n.peerStore.Load()
	if err != nil {
		return err
	}
	if loaded == nil {
		loaded = map[string]Peer{}
	}
	loaded[n.cfg.NodeID] = Peer{
		NodeID:        n.cfg.NodeID,
		RPCAddress:    n.cfg.AdvertiseAddress,
		ClientAddress: n.cfg.ClientAddress,
	}
	for _, peer := range n.cfg.Peers {
		loaded[peer.NodeID] = peer
	}
	n.peerRegistry = loaded
	return n.peerStore.Save(n.peerRegistry)
}

func normalizeClientAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func (n *RaftNode) forwardGet(ctx context.Context, key []byte) ([]byte, error) {
	values := url.Values{}
	values.Set("key", string(key))
	target := "/db/get?" + values.Encode()
	var resp struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	status, err := n.forwardRead(ctx, target, &resp)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound || !resp.Found {
		return nil, engine.ErrNotFound
	}
	return []byte(resp.Value), nil
}

func (n *RaftNode) forwardScan(ctx context.Context, start, end []byte, limit int) ([][2]string, error) {
	values := url.Values{}
	values.Set("start", string(start))
	values.Set("end", string(end))
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	var resp []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	status, err := n.forwardRead(ctx, "/db/scan?"+values.Encode(), &resp)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("cluster: forwarded scan returned status %d", status)
	}
	results := make([][2]string, 0, len(resp))
	for _, item := range resp {
		results = append(results, [2]string{item.Key, item.Value})
	}
	return results, nil
}

func (n *RaftNode) forwardRead(ctx context.Context, path string, dst interface{}) (int, error) {
	leader := strings.TrimSpace(n.LeaderAddress(ctx))
	if leader == "" {
		return 0, fmt.Errorf("cluster: leader address unknown")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(leader, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	if token := strings.TrimSpace(n.cfg.AuthToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("cluster: forwarded read failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (n *RaftNode) forwardPut(ctx context.Context, key, value []byte) (int, error) {
	payload := map[string]string{
		"key":   string(key),
		"value": string(value),
	}
	return n.forwardWriteJSON(ctx, http.MethodPost, "/db/put", payload, nil)
}

func (n *RaftNode) forwardDelete(ctx context.Context, key []byte) (int, error) {
	values := url.Values{}
	values.Set("key", string(key))
	return n.forwardWriteJSON(ctx, http.MethodDelete, "/db/delete?"+values.Encode(), nil, nil)
}

func (n *RaftNode) forwardBatch(ctx context.Context, entries []engine.BatchEntry) (int, error) {
	type batchEntry struct {
		Key    string `json:"key"`
		Value  string `json:"value,omitempty"`
		Delete bool   `json:"delete,omitempty"`
	}
	reqBody := struct {
		Entries []batchEntry `json:"entries"`
	}{Entries: make([]batchEntry, 0, len(entries))}
	for _, entry := range entries {
		reqBody.Entries = append(reqBody.Entries, batchEntry{
			Key:    string(entry.Key),
			Value:  string(entry.Value),
			Delete: entry.Delete,
		})
	}
	return n.forwardWriteJSON(ctx, http.MethodPost, "/db/batch", reqBody, nil)
}

func (n *RaftNode) forwardWriteJSON(ctx context.Context, method, path string, payload interface{}, dst interface{}) (int, error) {
	target := strings.TrimSpace(n.LeaderAddress(ctx))
	if target == "" {
		return 0, fmt.Errorf("cluster: leader address unknown")
	}
	return n.forwardJSON(ctx, method, target, path, payload, dst)
}

func (n *RaftNode) forwardJSON(ctx context.Context, method, targetBase, path string, payload interface{}, dst interface{}) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	target := strings.TrimRight(targetBase, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(n.cfg.AuthToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var forwardErr struct {
			LeaderAddress string `json:"leader_address"`
			Message       string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&forwardErr)
		if resp.StatusCode == http.StatusConflict && forwardErr.LeaderAddress != "" && forwardErr.LeaderAddress != targetBase {
			return n.forwardJSON(ctx, method, forwardErr.LeaderAddress, path, payload, dst)
		}
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("cluster: forwarded write failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil && err != io.EOF {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

type raftFSM struct {
	node *RaftNode
}

func (f *raftFSM) Apply(logEntry *raft.Log) interface{} {
	cmd, err := UnmarshalCommand(logEntry.Data)
	if err != nil {
		return err
	}

	f.node.mu.RLock()
	if logEntry.Index <= f.node.applied.LastAppliedIndex {
		f.node.mu.RUnlock()
		return nil
	}
	eng := f.node.eng
	f.node.mu.RUnlock()

	switch cmd.Type {
	case CommandPut:
		err = eng.Put(cmd.Key, cmd.Value)
	case CommandDelete:
		err = eng.Delete(cmd.Key)
	case CommandWriteBatch:
		batch := &engine.WriteBatch{}
		for _, entry := range cmd.Entries {
			if entry.Delete {
				batch.Delete(entry.Key)
			} else {
				batch.Put(entry.Key, entry.Value)
			}
		}
		err = eng.Write(batch)
	default:
		err = fmt.Errorf("cluster: unsupported command %q", cmd.Type)
	}
	if err != nil {
		return err
	}

	state := appliedState{
		LastAppliedIndex: logEntry.Index,
		LastAppliedTerm:  logEntry.Term,
	}
	if err := f.node.appliedStore.Save(state); err != nil {
		return err
	}
	f.node.mu.Lock()
	f.node.applied = state
	f.node.mu.Unlock()
	return nil
}

func (f *raftFSM) Snapshot() (raft.FSMSnapshot, error) {
	node := f.node
	stageRoot, err := os.MkdirTemp(node.clusterDir, snapshotStagingPrefix)
	if err != nil {
		return nil, err
	}
	engineStage := filepath.Join(stageRoot, "engine")

	node.mu.Lock()
	defer node.mu.Unlock()

	if node.eng != nil {
		if err := node.eng.Close(); err != nil {
			_ = os.RemoveAll(stageRoot)
			return nil, err
		}
	}
	if err := copyTree(engineStage, node.engineCfg.DataDir, func(rel string) bool {
		head := strings.Split(filepath.ToSlash(rel), "/")[0]
		return head == clusterDirName
	}); err != nil {
		_ = os.RemoveAll(stageRoot)
		return nil, err
	}
	reopened, err := engine.Open(node.engineCfg)
	if err != nil {
		_ = os.RemoveAll(stageRoot)
		return nil, err
	}
	node.eng = reopened
	node.bridgeEngineBus(reopened)

	return &engineSnapshot{
		root: engineStage,
		meta: snapshotMetadata{
			Applied: node.applied,
			NodeID:  node.cfg.NodeID,
			TakenAt: time.Now().UTC(),
		},
	}, nil
}

func (f *raftFSM) Restore(snapshot io.ReadCloser) error {
	defer snapshot.Close()

	node := f.node
	restoreRoot, err := os.MkdirTemp(node.clusterDir, "snapshot-restore-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(restoreRoot) }()

	meta, err := extractSnapshot(snapshot, restoreRoot)
	if err != nil {
		return err
	}

	node.mu.Lock()
	defer node.mu.Unlock()

	if node.eng != nil {
		if err := node.eng.Close(); err != nil {
			return err
		}
	}
	if err := replaceEngineDirContents(node.engineCfg.DataDir, restoreRoot); err != nil {
		return err
	}
	reopened, err := engine.Open(node.engineCfg)
	if err != nil {
		return err
	}
	node.eng = reopened
	node.bridgeEngineBus(reopened)
	if err := node.appliedStore.Save(meta.Applied); err != nil {
		return err
	}
	node.applied = meta.Applied
	return nil
}

func mapRaftRole(state raft.RaftState) Role {
	switch state {
	case raft.Leader:
		return RoleLeader
	case raft.Candidate:
		return RoleCandidate
	case raft.Follower:
		return RoleFollower
	default:
		return RoleFollower
	}
}

func parseUint64(raw string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	return n
}
