package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"lsm-engine/internal/cluster"
	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openGatewayTestServer(t *testing.T, allowedOrigins []string) (*engine.LSMEngine, *httptest.Server) {
	return openGatewayTestServerWithToken(t, allowedOrigins, "")
}

func openGatewayTestServerWithToken(t *testing.T, allowedOrigins []string, apiToken string) (*engine.LSMEngine, *httptest.Server) {
	t.Helper()

	eng, err := engine.Open(engine.Config{
		DataDir:      t.TempDir(),
		MemTableSize: 8 * 1024,
		SyncWAL:      false,
	})
	require.NoError(t, err)

	hub := NewWSHub(eng.EventBus(), HubOptions{
		AllowedOrigins: allowedOrigins,
		APIToken:       apiToken,
	})
	handler := NewHandler(cluster.NewStandaloneNode("test-node", eng), hub, HandlerOptions{
		AllowedOrigins: allowedOrigins,
		APIToken:       apiToken,
	})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		_ = eng.Close()
	})

	return eng, srv
}

func TestHandler_SameOriginAllowedByDefault(t *testing.T) {
	eng, srv := openGatewayTestServer(t, nil)
	require.NoError(t, eng.Put([]byte("alpha"), []byte("beta")))

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/db/open", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "open", body["status"])
}

func TestHandler_RejectsUnexpectedOriginByDefault(t *testing.T) {
	_, srv := openGatewayTestServer(t, nil)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/health", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://evil.example")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHandler_ReadyReportsEngineState(t *testing.T) {
	_, srv := openGatewayTestServer(t, nil)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/ready", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Ready           bool     `json:"ready"`
		ManifestExists  bool     `json:"manifest_exists"`
		ActiveWALExists bool     `json:"active_wal_exists"`
		Reasons         []string `json:"reasons"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.True(t, body.Ready)
	assert.True(t, body.ManifestExists)
	assert.True(t, body.ActiveWALExists)
	assert.Empty(t, body.Reasons)
}

func TestHandler_ExposesWALAndMemtableSnapshots(t *testing.T) {
	eng, srv := openGatewayTestServer(t, nil)
	require.NoError(t, eng.Put([]byte("alpha"), []byte("beta")))
	require.NoError(t, eng.Delete([]byte("gone")))

	walReq, err := http.NewRequest(http.MethodGet, srv.URL+"/wal/entries?limit=4", nil)
	require.NoError(t, err)
	walReq.Header.Set("Origin", srv.URL)

	walResp, err := http.DefaultClient.Do(walReq)
	require.NoError(t, err)
	defer walResp.Body.Close()
	assert.Equal(t, http.StatusOK, walResp.StatusCode)

	var walBody struct {
		Entries []map[string]interface{} `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(walResp.Body).Decode(&walBody))
	require.NotEmpty(t, walBody.Entries)

	memReq, err := http.NewRequest(http.MethodGet, srv.URL+"/memtable/snapshot?limit=10", nil)
	require.NoError(t, err)
	memReq.Header.Set("Origin", srv.URL)

	memResp, err := http.DefaultClient.Do(memReq)
	require.NoError(t, err)
	defer memResp.Body.Close()
	assert.Equal(t, http.StatusOK, memResp.StatusCode)

	var memBody struct {
		Mutable struct {
			Entries []struct {
				Key  string `json:"key"`
				Type string `json:"type"`
			} `json:"entries"`
		} `json:"mutable"`
	}
	require.NoError(t, json.NewDecoder(memResp.Body).Decode(&memBody))
	assert.GreaterOrEqual(t, len(memBody.Mutable.Entries), 2)
}

func TestHandler_ExposesClusterStatus(t *testing.T) {
	_, srv := openGatewayTestServer(t, nil)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/cluster/status", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Cluster struct {
			NodeID string `json:"node_id"`
			Role   string `json:"role"`
		} `json:"cluster"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "test-node", body.Cluster.NodeID)
	assert.Equal(t, "standalone", body.Cluster.Role)
}

func TestHandler_CloseIsExplicitlyUnsupported(t *testing.T) {
	_, srv := openGatewayTestServer(t, nil)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/db/close", strings.NewReader(""))
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestHandler_RequiresAuthWhenConfigured(t *testing.T) {
	_, srv := openGatewayTestServerWithToken(t, nil, "secret-token")

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/db/put", strings.NewReader(`{"key":"alpha","value":"beta"}`))
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	req, err = http.NewRequest(http.MethodPost, srv.URL+"/db/put", strings.NewReader(`{"key":"alpha","value":"beta"}`))
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestHandler_RejectsUnknownJSONFields(t *testing.T) {
	_, srv := openGatewayTestServerWithToken(t, nil, "secret-token")

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/db/put", strings.NewReader(`{"key":"alpha","value":"beta","extra":"nope"}`))
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandler_RejectsOversizedBodies(t *testing.T) {
	_, srv := openGatewayTestServerWithToken(t, nil, "secret-token")

	oversized := `{"key":"alpha","value":"` + strings.Repeat("x", maxWriteRequestBytes) + `"}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/db/put", bytes.NewBufferString(oversized))
	require.NoError(t, err)
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestHandler_ForwardsFollowerGetToLeader(t *testing.T) {
	leader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, getResponse{Key: "alpha", Value: "beta", Found: true})
	}))
	defer leader.Close()

	node := &redirectNode{leaderURL: leader.URL}
	handler := NewHandler(node, NewWSHub(nil, HubOptions{}), HandlerOptions{})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/db/get?key=alpha", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var body getResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.True(t, body.Found)
	assert.Equal(t, "beta", body.Value)
}

func TestHandler_EventualReadUsesFollowerLocalState(t *testing.T) {
	node := &redirectNode{
		leaderURL:     "http://leader.example",
		eventualValue: []byte("local-beta"),
		eventualReads: true,
	}
	handler := NewHandler(node, NewWSHub(nil, HubOptions{}), HandlerOptions{})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/db/get?key=alpha&consistency=eventual", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, node.eventualGetCalls)
	var body getResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.True(t, body.Found)
	assert.Equal(t, "local-beta", body.Value)
}

func TestHandler_ClusterMembershipAdminRoutes(t *testing.T) {
	node := &adminNode{}
	handler := NewHandler(node, NewWSHub(nil, HubOptions{}), HandlerOptions{})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addReq, err := http.NewRequest(http.MethodPost, srv.URL+"/cluster/membership/add", strings.NewReader(`{"node_id":"node-2","rpc_address":"127.0.0.1:7002","client_address":"http://127.0.0.1:8082"}`))
	require.NoError(t, err)
	addReq.Header.Set("Content-Type", "application/json")
	addResp, err := http.DefaultClient.Do(addReq)
	require.NoError(t, err)
	defer addResp.Body.Close()
	assert.Equal(t, http.StatusOK, addResp.StatusCode)
	assert.Equal(t, "node-2", node.added.NodeID)

	removeReq, err := http.NewRequest(http.MethodPost, srv.URL+"/cluster/membership/remove", strings.NewReader(`{"node_id":"node-2"}`))
	require.NoError(t, err)
	removeReq.Header.Set("Content-Type", "application/json")
	removeResp, err := http.DefaultClient.Do(removeReq)
	require.NoError(t, err)
	defer removeResp.Body.Close()
	assert.Equal(t, http.StatusOK, removeResp.StatusCode)
	assert.Equal(t, "node-2", node.removedNodeID)
}

func TestWSHub_SameOriginRequired(t *testing.T) {
	eng, srv := openGatewayTestServer(t, nil)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	dialer := websocket.Dialer{}

	_, resp, err := dialer.Dial(wsURL, http.Header{"Origin": []string{"http://evil.example"}})
	require.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}

	conn, resp, err := dialer.Dial(wsURL, http.Header{"Origin": []string{srv.URL}})
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer conn.Close()

	require.NoError(t, eng.Put([]byte("ws-key"), []byte("value")))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var evt struct {
		Type string `json:"type"`
	}
	require.NoError(t, conn.ReadJSON(&evt))
	assert.Equal(t, "wal.append", evt.Type)
}

type redirectNode struct {
	leaderURL         string
	eventualValue     []byte
	eventualReads     bool
	eventualGetCalls  int
	eventualScanCalls int
}

func (n *redirectNode) EventBus() *events.EventBus { return nil }
func (n *redirectNode) Status(context.Context) cluster.Status {
	return cluster.Status{Enabled: true, NodeID: "node-2", Role: cluster.RoleFollower, LeaderID: "node-1", PeerCount: 2}
}
func (n *redirectNode) Shards(context.Context) []cluster.ShardStatus { return nil }
func (n *redirectNode) LeaderAddress(context.Context) string         { return n.leaderURL }
func (n *redirectNode) Peers(context.Context) []cluster.Peer         { return nil }
func (n *redirectNode) AddPeer(context.Context, cluster.Peer) error  { return cluster.ErrUnsupported }
func (n *redirectNode) RemovePeer(context.Context, string) error     { return cluster.ErrUnsupported }
func (n *redirectNode) Put(context.Context, []byte, []byte) error    { return cluster.ErrUnsupported }
func (n *redirectNode) Delete(context.Context, []byte) error         { return cluster.ErrUnsupported }
func (n *redirectNode) Write(context.Context, *engine.WriteBatch) error {
	return cluster.ErrUnsupported
}
func (n *redirectNode) Get(context.Context, []byte) ([]byte, error) {
	return nil, &cluster.NotLeaderError{LeaderID: "node-1", LeaderAddress: n.leaderURL}
}
func (n *redirectNode) Scan(context.Context, []byte, []byte, int) ([][2]string, error) {
	return nil, &cluster.NotLeaderError{LeaderID: "node-1", LeaderAddress: n.leaderURL}
}
func (n *redirectNode) GetWithConsistency(_ context.Context, _ []byte, consistency cluster.ReadConsistency) ([]byte, error) {
	if consistency == cluster.ReadConsistencyEventual && n.eventualReads {
		n.eventualGetCalls++
		return append([]byte(nil), n.eventualValue...), nil
	}
	return n.Get(context.Background(), nil)
}
func (n *redirectNode) ScanWithConsistency(_ context.Context, _ []byte, _ []byte, _ int, consistency cluster.ReadConsistency) ([][2]string, error) {
	if consistency == cluster.ReadConsistencyEventual && n.eventualReads {
		n.eventualScanCalls++
		return [][2]string{{"alpha", string(n.eventualValue)}}, nil
	}
	return n.Scan(context.Background(), nil, nil, 0)
}
func (n *redirectNode) Version(context.Context) *manifest.Version    { return &manifest.Version{} }
func (n *redirectNode) Stats(context.Context) map[string]interface{} { return map[string]interface{}{} }
func (n *redirectNode) HealthStatus(context.Context) engine.HealthStatus {
	return engine.HealthStatus{Ready: true}
}
func (n *redirectNode) RuntimeState(context.Context) engine.RuntimeState {
	return engine.RuntimeState{Open: true}
}
func (n *redirectNode) EngineConfig(context.Context) engine.Config { return engine.Config{} }
func (n *redirectNode) MemTableSnapshot(context.Context, int) engine.MemTablesSnapshot {
	return engine.MemTablesSnapshot{}
}
func (n *redirectNode) ForceFlush(context.Context)                 {}
func (n *redirectNode) ForceCompaction(context.Context, int)       {}
func (n *redirectNode) SetCompactionStyle(context.Context, string) {}
func (n *redirectNode) Close() error                               { return nil }

type adminNode struct {
	added         cluster.Peer
	removedNodeID string
}

func (n *adminNode) EventBus() *events.EventBus { return nil }
func (n *adminNode) Status(context.Context) cluster.Status {
	return cluster.Status{Enabled: false, NodeID: "test-admin", Role: cluster.RoleStandalone, PeerCount: 1}
}
func (n *adminNode) Shards(context.Context) []cluster.ShardStatus { return nil }
func (n *adminNode) LeaderAddress(context.Context) string         { return "" }
func (n *adminNode) Peers(context.Context) []cluster.Peer         { return nil }
func (n *adminNode) AddPeer(_ context.Context, peer cluster.Peer) error {
	n.added = peer
	return nil
}
func (n *adminNode) RemovePeer(_ context.Context, nodeID string) error {
	n.removedNodeID = nodeID
	return nil
}
func (n *adminNode) Put(context.Context, []byte, []byte) error { return nil }
func (n *adminNode) Delete(context.Context, []byte) error      { return nil }
func (n *adminNode) Write(context.Context, *engine.WriteBatch) error {
	return nil
}
func (n *adminNode) Get(context.Context, []byte) ([]byte, error) { return nil, engine.ErrNotFound }
func (n *adminNode) Scan(context.Context, []byte, []byte, int) ([][2]string, error) {
	return nil, nil
}
func (n *adminNode) GetWithConsistency(ctx context.Context, key []byte, _ cluster.ReadConsistency) ([]byte, error) {
	return n.Get(ctx, key)
}
func (n *adminNode) ScanWithConsistency(ctx context.Context, start, end []byte, limit int, _ cluster.ReadConsistency) ([][2]string, error) {
	return n.Scan(ctx, start, end, limit)
}
func (n *adminNode) Version(context.Context) *manifest.Version    { return &manifest.Version{} }
func (n *adminNode) Stats(context.Context) map[string]interface{} { return map[string]interface{}{} }
func (n *adminNode) HealthStatus(context.Context) engine.HealthStatus {
	return engine.HealthStatus{Ready: true}
}
func (n *adminNode) RuntimeState(context.Context) engine.RuntimeState {
	return engine.RuntimeState{Open: true}
}
func (n *adminNode) EngineConfig(context.Context) engine.Config { return engine.Config{} }
func (n *adminNode) MemTableSnapshot(context.Context, int) engine.MemTablesSnapshot {
	return engine.MemTablesSnapshot{}
}
func (n *adminNode) ForceFlush(context.Context)                 {}
func (n *adminNode) ForceCompaction(context.Context, int)       {}
func (n *adminNode) SetCompactionStyle(context.Context, string) {}
func (n *adminNode) Close() error                               { return nil }

func TestWSHub_RequiresAuthWhenConfigured(t *testing.T) {
	_, srv := openGatewayTestServerWithToken(t, nil, "secret-token")

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	dialer := websocket.Dialer{}

	_, resp, err := dialer.Dial(wsURL, http.Header{"Origin": []string{srv.URL}})
	require.Error(t, err)
	if resp != nil {
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}

	conn, resp, err := dialer.Dial(wsURL, http.Header{
		"Origin":        []string{srv.URL},
		"Authorization": []string{"Bearer secret-token"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer conn.Close()
}
