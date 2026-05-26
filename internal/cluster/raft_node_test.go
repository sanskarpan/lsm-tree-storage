package cluster

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"

	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRaftNode_ElectionReplicationAndFailover(t *testing.T) {
	nodes, cleanup := openTestCluster(t, 3)
	defer cleanup()

	leader := waitForLeader(t, nodes, 15*time.Second)
	require.NotNil(t, leader)

	var follower *RaftNode
	for _, node := range nodes {
		if node != leader {
			follower = node
			break
		}
	}
	require.NotNil(t, follower)

	err := follower.Put(context.Background(), []byte("follower-write"), []byte("nope"))
	var notLeader *NotLeaderError
	require.ErrorAs(t, err, &notLeader)

	require.NoError(t, leader.Put(context.Background(), []byte("alpha"), []byte("one")))
	waitForReplicatedValue(t, nodes, []byte("alpha"), []byte("one"), 10*time.Second)

	require.NoError(t, leader.Close())

	var survivors []*RaftNode
	for _, node := range nodes {
		if node != leader {
			survivors = append(survivors, node)
		}
	}
	newLeader := waitForLeader(t, survivors, 15*time.Second)
	require.NotNil(t, newLeader)
	require.NoError(t, newLeader.Put(context.Background(), []byte("beta"), []byte("two")))
	waitForReplicatedValue(t, survivors, []byte("beta"), []byte("two"), 10*time.Second)
	waitForReplicatedValue(t, survivors, []byte("alpha"), []byte("one"), 10*time.Second)
}

func TestRaftNode_SnapshotRecoveryWithoutLocalEngineFiles(t *testing.T) {
	nodes, cleanup := openTestCluster(t, 1)
	defer cleanup()

	node := waitForLeader(t, nodes, 10*time.Second)
	require.NotNil(t, node)

	for i := 0; i < 20; i++ {
		key := []byte("snap-key-" + strconv.Itoa(i))
		val := []byte("snap-val-" + strconv.Itoa(i))
		require.NoError(t, node.Put(context.Background(), key, val))
	}

	require.NoError(t, node.raft.Snapshot().Error())

	engineDir := node.engineCfg.DataDir
	clusterDir := filepath.Join(engineDir, clusterDirName)
	require.NoError(t, node.Close())
	removeNonClusterEntries(t, engineDir)
	_, err := os.Stat(clusterDir)
	require.NoError(t, err)

	reopened, err := OpenRaftNode(node.cfg, node.engineCfg)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()

	waitForLeader(t, []*RaftNode{reopened}, 10*time.Second)
	value, err := reopened.Get(context.Background(), []byte("snap-key-3"))
	require.NoError(t, err)
	assert.Equal(t, []byte("snap-val-3"), value)
}

func TestRaftNode_DynamicMembershipAddAndRemove(t *testing.T) {
	nodes, cleanup := openTestCluster(t, 1)
	defer cleanup()

	leader := waitForLeader(t, nodes, 10*time.Second)
	require.NotNil(t, leader)

	joinAddr := freeTCPAddr(t)
	joinDir := filepath.Join(t.TempDir(), "engine")
	joinCfg := DefaultConfig()
	joinCfg.Enabled = true
	joinCfg.NodeID = "node-2"
	joinCfg.BindAddress = joinAddr
	joinCfg.AdvertiseAddress = joinAddr
	joinCfg.ClientAddress = "http://" + joinAddr
	joinCfg.DataDir = filepath.Join(joinDir, clusterDirName)
	joinCfg.ElectionTimeout = 800 * time.Millisecond
	joinCfg.HeartbeatInterval = 250 * time.Millisecond
	joinCfg.CommitTimeout = 50 * time.Millisecond
	joinCfg.ApplyTimeout = 5 * time.Second
	joinCfg.SnapshotInterval = 2 * time.Second
	joinCfg.SnapshotMinEntries = 10

	joinEngCfg := engine.DefaultConfig(joinDir)
	joinEngCfg.MemTableSize = 4 * 1024
	joinEngCfg.SyncWAL = true

	joiner, err := OpenRaftNode(joinCfg, joinEngCfg)
	require.NoError(t, err)
	defer func() { _ = joiner.Close() }()

	require.NoError(t, leader.AddPeer(context.Background(), Peer{
		NodeID:        "node-2",
		RPCAddress:    joinAddr,
		ClientAddress: "http://" + joinAddr,
	}))
	waitForPeerCount(t, leader, 2, 10*time.Second)

	require.NoError(t, leader.Put(context.Background(), []byte("join-key"), []byte("join-val")))
	waitForReplicatedValue(t, []*RaftNode{leader, joiner}, []byte("join-key"), []byte("join-val"), 10*time.Second)

	require.NoError(t, leader.RemovePeer(context.Background(), "node-2"))
	waitForPeerCount(t, leader, 1, 10*time.Second)

	require.NoError(t, leader.Put(context.Background(), []byte("post-remove"), []byte("v2")))
	waitForMissingValue(t, joiner, []byte("post-remove"), 3*time.Second)
}

func TestRaftNode_TLSEnabledClusterReplicates(t *testing.T) {
	certFile, keyFile, caFile := writeTLSMaterials(t, t.TempDir())
	nodes, cleanup := openTestClusterWithMutator(t, 3, func(cfg *Config) {
		cfg.TLS.Enabled = true
		cfg.TLS.CertFile = certFile
		cfg.TLS.KeyFile = keyFile
		cfg.TLS.CAFile = caFile
	})
	defer cleanup()

	leader := waitForLeader(t, nodes, 15*time.Second)
	require.NotNil(t, leader)
	require.NoError(t, leader.Put(context.Background(), []byte("tls-key"), []byte("tls-val")))
	waitForReplicatedValue(t, nodes, []byte("tls-key"), []byte("tls-val"), 10*time.Second)
}

func TestRaftNode_EventualReadServedByFollower(t *testing.T) {
	nodes, cleanup := openTestCluster(t, 3)
	defer cleanup()

	leader := waitForLeader(t, nodes, 15*time.Second)
	require.NotNil(t, leader)
	require.NoError(t, leader.Put(context.Background(), []byte("eventual-key"), []byte("eventual-val")))
	waitForReplicatedValue(t, nodes, []byte("eventual-key"), []byte("eventual-val"), 10*time.Second)

	var follower *RaftNode
	for _, node := range nodes {
		if node != leader {
			follower = node
			break
		}
	}
	require.NotNil(t, follower)

	value, err := follower.GetWithConsistency(context.Background(), []byte("eventual-key"), ReadConsistencyEventual)
	require.NoError(t, err)
	assert.Equal(t, []byte("eventual-val"), value)
}

func TestShardedNode_RoutesKeysAndCommitsCrossShardBatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NodeID = "local"
	cfg.ShardCount = 4

	engCfg := engine.DefaultConfig(t.TempDir())
	engCfg.MemTableSize = 4 * 1024
	engCfg.SyncWAL = true

	node, err := OpenShardedNode(cfg, engCfg)
	require.NoError(t, err)
	defer func() { _ = node.Close() }()

	keyA := []byte("alpha")
	keyB := []byte("bravo")
	for node.shardForKey(keyB) == node.shardForKey(keyA) {
		keyB = append(keyB, 'x')
	}
	require.NoError(t, node.Put(context.Background(), keyA, []byte("one")))
	require.NoError(t, node.Put(context.Background(), keyB, []byte("two")))

	valueA, err := node.Get(context.Background(), keyA)
	require.NoError(t, err)
	assert.Equal(t, []byte("one"), valueA)

	shardA := node.shardForKey(keyA)
	shardNodeA, ok := node.shards[shardA].(*StandaloneNode)
	require.True(t, ok)
	_, err = shardNodeA.eng.Get(keyA)
	require.NoError(t, err)

	batch := &engine.WriteBatch{}
	batch.Put(keyA, []byte("v1"))
	batch.Put(keyB, []byte("v2"))
	require.NoError(t, node.Write(context.Background(), batch))
	valueA, err = node.Get(context.Background(), keyA)
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), valueA)
	valueB, err := node.Get(context.Background(), keyB)
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), valueB)
	assert.Len(t, node.Shards(context.Background()), 4)
}

func TestShardedNode_CrossShardBatchRollsBackOnFailure(t *testing.T) {
	okShard := &fakeClusterNode{
		values: map[string]string{},
	}
	failShard := &fakeClusterNode{
		values:      map[string]string{},
		failOnWrite: true,
	}
	node := &ShardedNode{
		cfg:       DefaultConfig(),
		shards:    []Node{okShard, failShard},
		bus:       events.NewEventBus(),
		txJournal: newTxJournalStore(filepath.Join(t.TempDir(), "txns")),
	}

	keyA := []byte("alpha")
	shardA := node.shardForKey(keyA)
	var keyB []byte
	var shardB int
	for i := 0; i < 1000; i++ {
		candidate := []byte(fmt.Sprintf("bravo-%d", i))
		candidateShard := node.shardForKey(candidate)
		if candidateShard != shardA {
			keyB = candidate
			shardB = candidateShard
			break
		}
	}
	require.NotNil(t, keyB)
	node.shards[shardA].(*fakeClusterNode).values[string(keyA)] = "a"
	node.shards[shardB].(*fakeClusterNode).values[string(keyB)] = "b"

	batch := &engine.WriteBatch{}
	batch.Put(keyA, []byte("one"))
	batch.Put(keyB, []byte("two"))
	err := node.Write(context.Background(), batch)
	require.Error(t, err)
	value, err := node.shards[shardA].Get(context.Background(), keyA)
	require.NoError(t, err)
	assert.Equal(t, []byte("a"), value)
	value, err = node.shards[shardB].Get(context.Background(), keyB)
	require.NoError(t, err)
	assert.Equal(t, []byte("b"), value)
}

func TestDeriveShardAddress_IPv6(t *testing.T) {
	addr := "[::1]:7000"
	assert.Equal(t, "[::1]:7200", deriveShardAddress(addr, 2, 100))
	assert.Equal(t, "[::1]:7000", baseAddressFromShardAddress("[::1]:7200", 2, 100))
}

func openTestCluster(t *testing.T, size int) ([]*RaftNode, func()) {
	return openTestClusterWithMutator(t, size, nil)
}

func openTestClusterWithMutator(t *testing.T, size int, mutate func(*Config)) ([]*RaftNode, func()) {
	t.Helper()

	addrs := make([]string, size)
	for i := range addrs {
		addrs[i] = freeTCPAddr(t)
	}

	cfgs := make([]Config, 0, size)
	engCfgs := make([]engine.Config, 0, size)
	for i := 0; i < size; i++ {
		dataDir := filepath.Join(t.TempDir(), "engine")
		engCfg := engine.DefaultConfig(dataDir)
		engCfg.MemTableSize = 4 * 1024
		engCfg.SyncWAL = true

		peerList := make([]Peer, 0, size-1)
		for j := 0; j < size; j++ {
			if i == j {
				continue
			}
			peerList = append(peerList, Peer{
				NodeID:        "node-" + strconv.Itoa(j+1),
				RPCAddress:    addrs[j],
				ClientAddress: "http://" + addrs[j],
			})
		}

		clusterCfg := DefaultConfig()
		clusterCfg.Enabled = true
		clusterCfg.NodeID = "node-" + strconv.Itoa(i+1)
		clusterCfg.BindAddress = addrs[i]
		clusterCfg.AdvertiseAddress = addrs[i]
		clusterCfg.ClientAddress = "http://" + addrs[i]
		clusterCfg.DataDir = filepath.Join(dataDir, clusterDirName)
		clusterCfg.Peers = peerList
		clusterCfg.Bootstrap = i == 0
		clusterCfg.ElectionTimeout = 800 * time.Millisecond
		clusterCfg.HeartbeatInterval = 250 * time.Millisecond
		clusterCfg.CommitTimeout = 50 * time.Millisecond
		clusterCfg.ApplyTimeout = 5 * time.Second
		clusterCfg.SnapshotInterval = 2 * time.Second
		clusterCfg.SnapshotMinEntries = 10
		clusterCfg.SnapshotRetain = 2
		if mutate != nil {
			mutate(&clusterCfg)
		}

		cfgs = append(cfgs, clusterCfg)
		engCfgs = append(engCfgs, engCfg)
	}

	nodes := make([]*RaftNode, 0, size)
	for i := 0; i < size; i++ {
		node, err := OpenRaftNode(cfgs[i], engCfgs[i])
		require.NoError(t, err)
		nodes = append(nodes, node)
	}

	cleanup := func() {
		for _, node := range nodes {
			if node != nil {
				_ = node.Close()
			}
		}
	}
	return nodes, cleanup
}

func waitForPeerCount(t *testing.T, node *RaftNode, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(node.Peers(context.Background())) == expected {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("peer count did not reach %d within %s", expected, timeout)
}

func waitForLeader(t *testing.T, nodes []*RaftNode, timeout time.Duration) *RaftNode {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, node := range nodes {
			if node == nil || node.raft == nil {
				continue
			}
			if node.raft.State() == raft.Leader {
				return node
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("leader not elected within %s", timeout)
	return nil
}

func waitForReplicatedValue(t *testing.T, nodes []*RaftNode, key, expected []byte, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allMatch := true
		for _, node := range nodes {
			val, err := node.currentEngine().Get(key)
			if err != nil || string(val) != string(expected) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("value %q=%q not replicated within %s", key, expected, timeout)
}

func waitForMissingValue(t *testing.T, node *RaftNode, key []byte, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := node.currentEngine().Get(key)
		if err == engine.ErrNotFound {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("key %q unexpectedly replicated", key)
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

func removeNonClusterEntries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.Name() == clusterDirName {
			continue
		}
		require.NoError(t, os.RemoveAll(filepath.Join(dir, entry.Name())))
	}
}

func writeTLSMaterials(t *testing.T, dir string) (string, string, string) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "lsm-test-ca",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "127.0.0.1",
		},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		SubjectKeyId: []byte{1, 2, 3, 4},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	caFile := filepath.Join(dir, "ca.pem")
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0644))
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0644))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)}), 0600))
	return certFile, keyFile, caFile
}

type fakeClusterNode struct {
	values      map[string]string
	failOnWrite bool
}

func (n *fakeClusterNode) EventBus() *events.EventBus { return nil }
func (n *fakeClusterNode) Status(context.Context) Status {
	return Status{Enabled: false, NodeID: "fake", Role: RoleStandalone, LeaderID: "fake", PeerCount: 1, ShardCount: 1}
}
func (n *fakeClusterNode) Shards(context.Context) []ShardStatus {
	return []ShardStatus{{ShardID: "shard-0", Role: RoleStandalone, LeaderID: "fake", PeerCount: 1}}
}
func (n *fakeClusterNode) LeaderAddress(context.Context) string { return "" }
func (n *fakeClusterNode) Peers(context.Context) []Peer         { return nil }
func (n *fakeClusterNode) AddPeer(context.Context, Peer) error  { return ErrUnsupported }
func (n *fakeClusterNode) RemovePeer(context.Context, string) error {
	return ErrUnsupported
}
func (n *fakeClusterNode) Put(_ context.Context, key, value []byte) error {
	if n.failOnWrite {
		return fmt.Errorf("forced put failure")
	}
	if n.values == nil {
		n.values = map[string]string{}
	}
	n.values[string(key)] = string(value)
	return nil
}
func (n *fakeClusterNode) Delete(_ context.Context, key []byte) error {
	if n.failOnWrite {
		return fmt.Errorf("forced delete failure")
	}
	delete(n.values, string(key))
	return nil
}
func (n *fakeClusterNode) Write(_ context.Context, batch *engine.WriteBatch) error {
	if n.failOnWrite {
		return fmt.Errorf("forced write failure")
	}
	for _, entry := range batch.Entries() {
		if entry.Delete {
			delete(n.values, string(entry.Key))
			continue
		}
		if n.values == nil {
			n.values = map[string]string{}
		}
		n.values[string(entry.Key)] = string(entry.Value)
	}
	return nil
}
func (n *fakeClusterNode) Get(_ context.Context, key []byte) ([]byte, error) {
	val, ok := n.values[string(key)]
	if !ok {
		return nil, engine.ErrNotFound
	}
	return []byte(val), nil
}
func (n *fakeClusterNode) GetWithConsistency(ctx context.Context, key []byte, _ ReadConsistency) ([]byte, error) {
	return n.Get(ctx, key)
}
func (n *fakeClusterNode) Scan(context.Context, []byte, []byte, int) ([][2]string, error) {
	return nil, nil
}
func (n *fakeClusterNode) ScanWithConsistency(ctx context.Context, start, end []byte, limit int, _ ReadConsistency) ([][2]string, error) {
	return n.Scan(ctx, start, end, limit)
}
func (n *fakeClusterNode) Version(context.Context) *manifest.Version { return &manifest.Version{} }
func (n *fakeClusterNode) Stats(context.Context) map[string]interface{} {
	return map[string]interface{}{}
}
func (n *fakeClusterNode) HealthStatus(context.Context) engine.HealthStatus {
	return engine.HealthStatus{Ready: true}
}
func (n *fakeClusterNode) RuntimeState(context.Context) engine.RuntimeState {
	return engine.RuntimeState{Open: true}
}
func (n *fakeClusterNode) EngineConfig(context.Context) engine.Config { return engine.Config{} }
func (n *fakeClusterNode) MemTableSnapshot(context.Context, int) engine.MemTablesSnapshot {
	return engine.MemTablesSnapshot{}
}
func (n *fakeClusterNode) ForceFlush(context.Context)                 {}
func (n *fakeClusterNode) ForceCompaction(context.Context, int)       {}
func (n *fakeClusterNode) SetCompactionStyle(context.Context, string) {}
func (n *fakeClusterNode) Close() error                               { return nil }
