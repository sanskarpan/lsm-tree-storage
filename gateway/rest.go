// Package gateway exposes the LSM engine over HTTP REST and WebSocket endpoints.
// REST handlers are registered on a standard http.ServeMux; the WebSocket hub
// fans out all engine events to connected browser clients.
package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"lsm-engine/internal/cluster"
	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/simulation"
	"lsm-engine/internal/sstable"
)

const (
	maxWriteRequestBytes = 8 << 20
	maxAdminRequestBytes = 1 << 20
	maxScanLimit         = 5000
	maxBenchKeys         = 1_000_000
	maxValueSizeBytes    = 1 << 20
)

// HandlerOptions configures request policy for the REST gateway.
type HandlerOptions struct {
	AllowedOrigins []string
	APIToken       string
}

// Handler wires REST routes to the LSM engine.
type Handler struct {
	node            cluster.Node
	hub             *WSHub
	httpClient      *http.Client
	lastBenchResult interface{}
	allowedOrigins  []string
	apiToken        string
	walMu           sync.Mutex
	recentWAL       []walEventSnapshot
}

type walEventSnapshot struct {
	Type      string `json:"type"`
	Key       string `json:"key,omitempty"`
	ValueLen  int    `json:"value_len,omitempty"`
	SeqNo     uint64 `json:"seq_no,omitempty"`
	Timestamp int64  `json:"timestamp_unix_nano"`
}

// NewHandler creates a Handler backed by eng and hub.
func NewHandler(node cluster.Node, hub *WSHub, opts HandlerOptions) *Handler {
	h := &Handler{
		node:           node,
		hub:            hub,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		allowedOrigins: append([]string(nil), opts.AllowedOrigins...),
		apiToken:       strings.TrimSpace(opts.APIToken),
	}
	if bus := node.EventBus(); bus != nil {
		bus.Subscribe(events.EvtWALAppend, h.recordWALEvent)
		bus.Subscribe(events.EvtWALSync, h.recordWALEvent)
	}
	return h
}

// RegisterRoutes registers all REST and WebSocket routes on mux.
// All routes are registered under both the legacy path and /api/v1/<path> for
// backwards compatibility. WebSocket stays at /ws only (no /api/v1 prefix).
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	routes := map[string]http.HandlerFunc{
		"/db/put":                    h.handlePut,
		"/db/get":                    h.handleGet,
		"/db/delete":                 h.handleDelete,
		"/db/scan":                   h.handleScan,
		"/db/batch":                  h.handleBatch,
		"/db/open":                   h.handleOpen,
		"/db/close":                  h.handleClose,
		"/db/stats":                  h.handleDBStats,
		"/levels":                    h.handleLevels,
		"/bloom/":                    h.handleBloom,
		"/stats":                     h.handleStats,
		"/health":                    h.handleHealth,
		"/ready":                     h.handleReady,
		"/amplification":             h.handleAmplification,
		"/compaction/force":          h.handleCompactionForce,
		"/compaction/style":          h.handleCompactionStyle,
		"/compaction/stats":          h.handleCompactionStats,
		"/bench/run":                 h.handleBenchRun,
		"/bench/result":              h.handleBenchResult,
		"/scenarios":                 h.handleScenariosList,
		"/scenarios/":                h.handleScenarioRun,
		"/wal/entries":               h.handleWALEntries,
		"/memtable/snapshot":         h.handleMemTableSnapshot,
		"/cache/stats":               h.handleCacheStats,
		"/cluster/status":            h.handleClusterStatus,
		"/cluster/leader":            h.handleClusterLeader,
		"/cluster/peers":             h.handleClusterPeers,
		"/cluster/shards":            h.handleClusterShards,
		"/cluster/membership":        h.handleClusterPeers,
		"/cluster/membership/add":    h.handleClusterAddPeer,
		"/cluster/membership/remove": h.handleClusterRemovePeer,
		"/cluster/readiness":         h.handleClusterReadiness,
		"/ws":                        h.hub.ServeWS,
	}
	for path, fn := range routes {
		mux.HandleFunc(path, fn)
		if path != "/ws" {
			mux.HandleFunc("/api/v1"+path, fn)
		}
	}
}

func (h *Handler) requestContext(r *http.Request) context.Context {
	if r == nil || r.Context() == nil {
		return context.Background()
	}
	return r.Context()
}

func (h *Handler) writeNodeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var notLeader *cluster.NotLeaderError
	if errors.As(err, &notLeader) {
		status := h.node.Status(context.Background())
		leaderAddress := notLeader.LeaderAddress
		if leaderAddress == "" {
			leaderAddress = h.node.LeaderAddress(context.Background())
		}
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":          "not_leader",
			"message":        err.Error(),
			"leader_id":      notLeader.LeaderID,
			"leader_address": leaderAddress,
			"cluster":        status,
		})
		return true
	}
	return false
}

func (h *Handler) maybeForwardRead(w http.ResponseWriter, r *http.Request, err error) bool {
	var notLeader *cluster.NotLeaderError
	if !errors.As(err, &notLeader) {
		return false
	}
	if r.Method != http.MethodGet {
		return false
	}
	if r.Header.Get("X-LSM-Forwarded") == "1" {
		return false
	}
	targetBase := strings.TrimSpace(notLeader.LeaderAddress)
	if targetBase == "" {
		targetBase = strings.TrimSpace(h.node.LeaderAddress(h.requestContext(r)))
	}
	if targetBase == "" {
		return false
	}
	targetURL := strings.TrimRight(targetBase, "/") + r.URL.RequestURI()
	req, reqErr := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)
	if reqErr != nil {
		http.Error(w, reqErr.Error(), http.StatusBadGateway)
		return true
	}
	req.Header = r.Header.Clone()
	req.Header.Set("X-LSM-Forwarded", "1")
	resp, respErr := h.httpClient.Do(req)
	if respErr != nil {
		http.Error(w, respErr.Error(), http.StatusBadGateway)
		return true
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return true
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func corsHeaders(w http.ResponseWriter, origin string) {
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Read-Consistency")
}

func (h *Handler) applyCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if !originAllowed(origin, requestScheme(r), r.Host, h.allowedOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return false
	}
	corsHeaders(w, origin)
	return true
}

func parseBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" || len(header) < len("Bearer ")+1 {
		return ""
	}
	if !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func tokenAuthorized(expected, actual string) bool {
	if expected == "" || actual == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.apiToken == "" {
		return true
	}
	token := parseBearerToken(r.Header.Get("Authorization"))
	if tokenAuthorized(h.apiToken, token) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="lsm-engine"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	var trailing struct{}
	if err := dec.Decode(&trailing); err != io.EOF {
		http.Error(w, "request body must contain a single JSON object", http.StatusBadRequest)
		return false
	}
	return true
}

func requestScheme(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		return strings.ToLower(forwarded)
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func originAllowed(origin, requestScheme, requestHost string, allowedOrigins []string) bool {
	if origin == "" {
		return true
	}
	if len(allowedOrigins) == 1 && allowedOrigins[0] == "*" {
		return true
	}
	u, err := url.Parse(origin)
	if err == nil && strings.EqualFold(u.Host, requestHost) && strings.EqualFold(u.Scheme, requestScheme) {
		return true
	}
	for _, allowed := range allowedOrigins {
		if strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}

func parseReadConsistency(r *http.Request) (cluster.ReadConsistency, error) {
	mode := strings.TrimSpace(r.URL.Query().Get("consistency"))
	if mode == "" {
		mode = strings.TrimSpace(r.Header.Get("X-Read-Consistency"))
	}
	switch strings.ToLower(mode) {
	case "", string(cluster.ReadConsistencyLinearizable):
		return cluster.ReadConsistencyLinearizable, nil
	case string(cluster.ReadConsistencyEventual):
		return cluster.ReadConsistencyEventual, nil
	default:
		return "", errors.New("invalid read consistency; use linearizable|eventual")
	}
}

func (h *Handler) recordWALEvent(evt events.Event) {
	entry := walEventSnapshot{
		Type:      string(evt.Type),
		Timestamp: time.Now().UnixNano(),
	}
	if seq, ok := evt.Extra["seq_no"].(uint64); ok {
		entry.SeqNo = seq
	} else if seq, ok := evt.Extra["seq"].(uint64); ok {
		entry.SeqNo = seq
	}
	if key, ok := evt.Extra["key"].(string); ok {
		entry.Key = key
	}
	if valLen, ok := evt.Extra["val_len"].(int); ok {
		entry.ValueLen = valLen
	}

	h.walMu.Lock()
	defer h.walMu.Unlock()
	h.recentWAL = append(h.recentWAL, entry)
	if len(h.recentWAL) > 256 {
		h.recentWAL = append([]walEventSnapshot(nil), h.recentWAL[len(h.recentWAL)-256:]...)
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

type putRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (h *Handler) handlePut(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req putRequest
	if !h.requireAuth(w, r) {
		return
	}
	if !decodeJSONBody(w, r, &req, maxWriteRequestBytes) {
		return
	}
	if req.Key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if err := h.node.Put(h.requestContext(r), []byte(req.Key), []byte(req.Value)); err != nil {
		if h.writeNodeError(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type getResponse struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	Found bool   `json:"found"`
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	consistency, err := parseReadConsistency(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	val, err := h.node.GetWithConsistency(h.requestContext(r), []byte(key), consistency)
	if err == engine.ErrNotFound {
		writeJSON(w, http.StatusNotFound, getResponse{Key: key, Found: false})
		return
	}
	if err != nil {
		if h.maybeForwardRead(w, r, err) {
			return
		}
		if h.writeNodeError(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, getResponse{Key: key, Value: string(val), Found: true})
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if err := h.node.Delete(h.requestContext(r), []byte(key)); err != nil {
		if h.writeNodeError(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type scanEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// handleScan does a key-range scan using the engine's MergeIterator.
// This is a basic linear scan across all levels — sufficient for correctness.
func (h *Handler) handleScan(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")
	limitStr := r.URL.Query().Get("limit")
	limit := 1000
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= maxScanLimit {
			limit = n
		}
	}
	consistency, err := parseReadConsistency(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Use a simple approach: scan by iterating the engine's public API
	// via a range scan helper that internally uses a merge iterator
	results, err := h.node.ScanWithConsistency(h.requestContext(r), []byte(start), []byte(end), limit, consistency)
	if err != nil {
		if h.maybeForwardRead(w, r, err) {
			return
		}
		if h.writeNodeError(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	entries := make([]scanEntry, 0, len(results))
	for _, kv := range results {
		entries = append(entries, scanEntry{Key: kv[0], Value: kv[1]})
	}
	writeJSON(w, http.StatusOK, entries)
}

// ── Level info ────────────────────────────────────────────────────────────────

type levelInfo struct {
	Level     int           `json:"level"`
	NumFiles  int           `json:"num_files"`
	TotalSize uint64        `json:"total_size"`
	Files     []sstableInfo `json:"files"`
}

type sstableInfo struct {
	FileID   uint64 `json:"file_id"`
	FirstKey string `json:"first_key"`
	LastKey  string `json:"last_key"`
	FileSize uint64 `json:"file_size"`
	NumKeys  uint64 `json:"num_keys"`
}

func (h *Handler) handleLevels(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	version := h.node.Version(h.requestContext(r))
	levels := make([]levelInfo, 0, 7)
	for i, level := range version.Levels {
		info := levelInfo{Level: i, NumFiles: len(level)}
		for _, meta := range level {
			info.TotalSize += meta.FileSize
			info.Files = append(info.Files, sstableInfo{
				FileID:   meta.FileID,
				FirstKey: string(meta.FirstKey),
				LastKey:  string(meta.LastKey),
				FileSize: meta.FileSize,
				NumKeys:  meta.NumKeys,
			})
		}
		levels = append(levels, info)
	}
	writeJSON(w, http.StatusOK, levels)
}

// ── Bloom stats ────────────────────────────────────────────────────────────────

type bloomStats struct {
	FileID     uint64  `json:"file_id"`
	BitsPerKey int     `json:"bits_per_key"`
	EstFPRate  float64 `json:"estimated_fp_rate"`
	Status     string  `json:"status"`
}

func (h *Handler) handleBloom(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	// Extract fileID from /bloom/{fileID} or /api/v1/bloom/{fileID}
	p := r.URL.Path
	for _, pfx := range []string{"/api/v1/bloom/", "/bloom/"} {
		if strings.HasPrefix(p, pfx) {
			p = strings.TrimPrefix(p, pfx)
			break
		}
	}
	parts := strings.Split(p, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "fileID required", http.StatusBadRequest)
		return
	}
	fileID, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid fileID", http.StatusBadRequest)
		return
	}

	// Find the SSTable in any level
	version := h.node.Version(h.requestContext(r))
	bpk := h.node.EngineConfig(h.requestContext(r)).BloomBitsPerKey
	for _, level := range version.Levels {
		for _, meta := range level {
			if meta.FileID == fileID {
				// Estimated FP rate for bitsPerKey=10: (1 - e^(-kn/m))^k ≈ 0.0082
				estFP := estimateFPRate(bpk)
				writeJSON(w, http.StatusOK, bloomStats{
					FileID:     fileID,
					BitsPerKey: bpk,
					EstFPRate:  estFP,
					Status:     "loaded",
				})
				return
			}
		}
	}
	http.Error(w, "SSTable not found", http.StatusNotFound)
}

func estimateFPRate(bitsPerKey int) float64 {
	// Approximate false positive rate: (0.6185)^bitsPerKey
	rate := 1.0
	base := 0.6185
	for i := 0; i < bitsPerKey; i++ {
		rate *= base
	}
	return rate
}

// ── Stats ──────────────────────────────────────────────────────────────────────

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, h.node.Stats(h.requestContext(r)))
}

// ── Health ─────────────────────────────────────────────────────────────────────

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := h.node.HealthStatus(h.requestContext(r))
	code := http.StatusOK
	if !status.Ready {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, status)
}

// ── Batch write ────────────────────────────────────────────────────────────────

type batchEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Delete bool   `json:"delete"`
}

type batchRequest struct {
	Entries []batchEntry `json:"entries"`
}

func (h *Handler) handleBatch(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req batchRequest
	if !h.requireAuth(w, r) {
		return
	}
	if !decodeJSONBody(w, r, &req, maxWriteRequestBytes) {
		return
	}
	batch := &engine.WriteBatch{}
	for _, e := range req.Entries {
		if e.Key == "" {
			http.Error(w, "entries[].key required", http.StatusBadRequest)
			return
		}
		if e.Delete {
			batch.Delete([]byte(e.Key))
		} else {
			batch.Put([]byte(e.Key), []byte(e.Value))
		}
	}
	if err := h.node.Write(h.requestContext(r), batch); err != nil {
		if h.writeNodeError(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Amplification ──────────────────────────────────────────────────────────────

func (h *Handler) handleAmplification(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	stats := h.node.Stats(h.requestContext(r))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_sst_bytes": stats["total_sst_bytes"],
		"total_sst_files": stats["total_sst_files"],
		"seq_no":          stats["seq_no"],
	})
}

// ── Compaction control ─────────────────────────────────────────────────────────

type compactionForceRequest struct {
	Level int `json:"level"`
}

func (h *Handler) handleCompactionForce(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req compactionForceRequest
	if !h.requireAuth(w, r) {
		return
	}
	if !decodeJSONBody(w, r, &req, maxAdminRequestBytes) {
		return
	}
	if req.Level < 0 {
		http.Error(w, "level must be >= 0", http.StatusBadRequest)
		return
	}
	h.node.ForceCompaction(h.requestContext(r), req.Level)
	writeJSON(w, http.StatusOK, map[string]string{"status": "triggered"})
}

type compactionStyleRequest struct {
	Style string `json:"style"`
}

func (h *Handler) handleCompactionStyle(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req compactionStyleRequest
	if !h.requireAuth(w, r) {
		return
	}
	if !decodeJSONBody(w, r, &req, maxAdminRequestBytes) {
		return
	}
	valid := map[string]bool{"leveled": true, "size-tiered": true, "time-window": true}
	if !valid[req.Style] {
		http.Error(w, "invalid style; use leveled|size-tiered|time-window", http.StatusBadRequest)
		return
	}
	h.node.SetCompactionStyle(h.requestContext(r), req.Style)
	writeJSON(w, http.StatusOK, map[string]string{"style": req.Style})
}

// ── Benchmark ──────────────────────────────────────────────────────────────────

type benchRequest struct {
	Type           string  `json:"type"`
	NumKeys        int     `json:"num_keys"`
	ValueSize      int     `json:"value_size"`
	ReadWriteRatio float64 `json:"read_write_ratio"`
}

func (h *Handler) handleBenchRun(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req benchRequest
	if !h.requireAuth(w, r) {
		return
	}
	if !decodeJSONBody(w, r, &req, maxAdminRequestBytes) {
		return
	}
	if req.NumKeys <= 0 || req.NumKeys > maxBenchKeys {
		http.Error(w, "num_keys must be between 1 and 1000000", http.StatusBadRequest)
		return
	}
	if req.ValueSize <= 0 || req.ValueSize > maxValueSizeBytes {
		http.Error(w, "value_size must be between 1 and 1048576", http.StatusBadRequest)
		return
	}
	if req.ReadWriteRatio < 0 || req.ReadWriteRatio > 1 {
		http.Error(w, "read_write_ratio must be between 0 and 1", http.StatusBadRequest)
		return
	}
	cfg := simulation.WorkloadConfig{
		Type:           simulation.WorkloadType(req.Type),
		NumKeys:        req.NumKeys,
		ValueSize:      req.ValueSize,
		ReadWriteRatio: req.ReadWriteRatio,
	}
	if cfg.Type == "" {
		cfg.Type = simulation.WorkloadSequentialWrite
	}
	validTypes := map[simulation.WorkloadType]bool{
		simulation.WorkloadSequentialWrite:  true,
		simulation.WorkloadRandomWrite:      true,
		simulation.WorkloadZipfRead:         true,
		simulation.WorkloadMixed:            true,
		simulation.WorkloadCompactionStress: true,
		simulation.WorkloadPointDelete:      true,
	}
	if !validTypes[cfg.Type] {
		http.Error(w, "invalid benchmark type", http.StatusBadRequest)
		return
	}
	status := h.node.Status(h.requestContext(r))
	if status.Role != cluster.RoleLeader && status.Role != cluster.RoleStandalone {
		if h.writeNodeError(w, &cluster.NotLeaderError{
			LeaderID:      status.LeaderID,
			LeaderAddress: h.node.LeaderAddress(h.requestContext(r)),
		}) {
			return
		}
	}
	result := simulation.RunWorkload(&nodeEngineAdapter{node: h.node, ctx: h.requestContext(r)}, cfg)
	out := map[string]interface{}{
		"total_ops":    result.TotalOps,
		"duration_ms":  result.Duration.Milliseconds(),
		"ops_per_sec":  result.OpsPerSec,
		"p50_write_us": result.WriteLatency.Percentile(50).Microseconds(),
		"p99_write_us": result.WriteLatency.Percentile(99).Microseconds(),
		"p50_read_us":  result.ReadLatency.Percentile(50).Microseconds(),
		"p99_read_us":  result.ReadLatency.Percentile(99).Microseconds(),
	}
	h.lastBenchResult = out
	writeJSON(w, http.StatusOK, out)
}

// ── Scenarios ──────────────────────────────────────────────────────────────────

func (h *Handler) handleScenariosList(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	type scenarioInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	list := make([]scenarioInfo, 0, len(simulation.AllScenarios))
	for _, s := range simulation.AllScenarios {
		list = append(list, scenarioInfo{
			Name:        string(s),
			Description: simulation.ScenarioDescription[s],
		})
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) handleScenarioRun(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	// Path: /scenarios/{name}/run or /api/v1/scenarios/{name}/run
	p := r.URL.Path
	for _, pfx := range []string{"/api/v1/scenarios/", "/scenarios/"} {
		if strings.HasPrefix(p, pfx) {
			p = strings.TrimPrefix(p, pfx)
			break
		}
	}
	parts := strings.Split(p, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "scenario name required", http.StatusBadRequest)
		return
	}
	name := simulation.ScenarioName(parts[0])
	status := h.node.Status(h.requestContext(r))
	if status.Role != cluster.RoleLeader && status.Role != cluster.RoleStandalone {
		if h.writeNodeError(w, &cluster.NotLeaderError{
			LeaderID:      status.LeaderID,
			LeaderAddress: h.node.LeaderAddress(h.requestContext(r)),
		}) {
			return
		}
	}
	if err := simulation.RunScenario(&nodeEngineAdapter{node: h.node, ctx: h.requestContext(r)}, name); err != nil {
		if h.writeNodeError(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "done", "scenario": string(name)})
}

// ── Engine lifecycle stubs ─────────────────────────────────────────────────────

// handleOpen returns engine status (engine is always open when server is running).
func (h *Handler) handleOpen(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "open",
		"state":   h.node.RuntimeState(h.requestContext(r)),
		"stats":   h.node.Stats(h.requestContext(r)),
		"config":  h.node.EngineConfig(h.requestContext(r)),
		"cluster": h.node.Status(h.requestContext(r)),
	})
}

// handleClose makes it explicit that engine lifecycle is process-managed.
func (h *Handler) handleClose(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusConflict, map[string]interface{}{
		"status":    "rejected",
		"supported": false,
		"reason":    "engine lifecycle is managed by the server process; remote close is not supported",
	})
}

// handleDBStats returns engine stats at /db/stats (alias of /stats).
func (h *Handler) handleDBStats(w http.ResponseWriter, r *http.Request) {
	h.handleStats(w, r)
}

// ── Compaction stats ──────────────────────────────────────────────────────────

// handleCompactionStats returns per-level compaction statistics.
func (h *Handler) handleCompactionStats(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	version := h.node.Version(h.requestContext(r))
	type levelStat struct {
		Level     int    `json:"level"`
		NumFiles  int    `json:"num_files"`
		TotalSize uint64 `json:"total_size"`
	}
	stats := make([]levelStat, 0, 7)
	for i, level := range version.Levels {
		var sz uint64
		for _, m := range level {
			sz += m.FileSize
		}
		stats = append(stats, levelStat{Level: i, NumFiles: len(level), TotalSize: sz})
	}
	writeJSON(w, http.StatusOK, stats)
}

// ── Bench result ──────────────────────────────────────────────────────────────

func (h *Handler) handleBenchResult(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	if h.lastBenchResult == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no_result"})
		return
	}
	writeJSON(w, http.StatusOK, h.lastBenchResult)
}

func (h *Handler) handleWALEntries(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 256 {
			limit = parsed
		}
	}

	h.walMu.Lock()
	defer h.walMu.Unlock()

	start := len(h.recentWAL) - limit
	if start < 0 {
		start = 0
	}
	entries := append([]walEventSnapshot(nil), h.recentWAL[start:]...)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"entries": entries,
		"count":   len(entries),
		"limit":   limit,
		"state":   h.node.RuntimeState(h.requestContext(r)),
	})
}

func (h *Handler) handleMemTableSnapshot(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	type entryView struct {
		Key      string `json:"key"`
		SeqNo    uint64 `json:"seq_no"`
		Type     string `json:"type"`
		Value    string `json:"value,omitempty"`
		ValueLen int    `json:"value_len,omitempty"`
	}
	type tableView struct {
		ApproximateSize int64       `json:"approximate_size"`
		WALSeqNo        uint64      `json:"wal_seq_no"`
		Truncated       bool        `json:"truncated"`
		Entries         []entryView `json:"entries"`
	}
	type immutableView struct {
		LogNumber uint64    `json:"log_number"`
		WALPath   string    `json:"wal_path"`
		Table     tableView `json:"table"`
	}

	toTableView := func(snapshot engine.TableSnapshot) tableView {
		out := tableView{
			ApproximateSize: snapshot.ApproximateSize,
			WALSeqNo:        snapshot.WALSeqNo,
			Truncated:       snapshot.Truncated,
			Entries:         make([]entryView, 0, len(snapshot.Entries)),
		}
		for _, entry := range snapshot.Entries {
			item := entryView{
				Key:      string(entry.UserKey),
				SeqNo:    entry.SeqNo,
				Value:    string(entry.Value),
				ValueLen: len(entry.Value),
			}
			if entry.Type == sstable.TypeDeletion {
				item.Type = "delete"
				item.Value = ""
				item.ValueLen = 0
			} else {
				item.Type = "put"
			}
			out.Entries = append(out.Entries, item)
		}
		return out
	}

	snap := h.node.MemTableSnapshot(h.requestContext(r), limit)
	immutables := make([]immutableView, 0, len(snap.Immutables))
	for _, imm := range snap.Immutables {
		immutables = append(immutables, immutableView{
			LogNumber: imm.LogNumber,
			WALPath:   imm.WALPath,
			Table:     toTableView(imm.Table),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mutable":           toTableView(snap.Mutable),
		"immutables":        immutables,
		"active_wal_path":   snap.ActiveWALPath,
		"active_log_number": snap.ActiveLogNumber,
		"limit":             limit,
	})
}

// ── Cache stats ───────────────────────────────────────────────────────────────

func (h *Handler) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	stats := h.node.Stats(h.requestContext(r))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hits":     stats["cache_hits"],
		"misses":   stats["cache_misses"],
		"hit_rate": stats["cache_hit_rate"],
		"size":     stats["cache_size"],
	})
}

func (h *Handler) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": h.node.Status(h.requestContext(r)),
		"leader":  h.node.LeaderAddress(h.requestContext(r)),
	})
}

func (h *Handler) handleClusterLeader(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	status := h.node.Status(h.requestContext(r))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"leader_id":      status.LeaderID,
		"leader_address": h.node.LeaderAddress(h.requestContext(r)),
		"role":           status.Role,
	})
}

func (h *Handler) handleClusterPeers(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"peers": h.node.Peers(h.requestContext(r)),
	})
}

func (h *Handler) handleClusterShards(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"shards": h.node.Shards(h.requestContext(r)),
	})
}

type addPeerRequest struct {
	NodeID        string `json:"node_id"`
	RPCAddress    string `json:"rpc_address"`
	ClientAddress string `json:"client_address"`
}

func (h *Handler) handleClusterAddPeer(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	var req addPeerRequest
	if !decodeJSONBody(w, r, &req, maxAdminRequestBytes) {
		return
	}
	peer := cluster.Peer{
		NodeID:        strings.TrimSpace(req.NodeID),
		RPCAddress:    strings.TrimSpace(req.RPCAddress),
		ClientAddress: strings.TrimSpace(req.ClientAddress),
	}
	if err := h.node.AddPeer(h.requestContext(r), peer); err != nil {
		if h.writeNodeError(w, err) {
			return
		}
		if errors.Is(err, cluster.ErrUnsupported) {
			http.Error(w, err.Error(), http.StatusNotImplemented)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "added",
		"peer":   peer,
	})
}

type removePeerRequest struct {
	NodeID string `json:"node_id"`
}

func (h *Handler) handleClusterRemovePeer(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	var req removePeerRequest
	if !decodeJSONBody(w, r, &req, maxAdminRequestBytes) {
		return
	}
	if err := h.node.RemovePeer(h.requestContext(r), strings.TrimSpace(req.NodeID)); err != nil {
		if h.writeNodeError(w, err) {
			return
		}
		if errors.Is(err, cluster.ErrUnsupported) {
			http.Error(w, err.Error(), http.StatusNotImplemented)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "removed",
		"node_id": strings.TrimSpace(req.NodeID),
	})
}

func (h *Handler) handleClusterReadiness(w http.ResponseWriter, r *http.Request) {
	if !h.applyCORS(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.requireAuth(w, r) {
		return
	}
	health := h.node.HealthStatus(h.requestContext(r))
	clusterStatus := h.node.Status(h.requestContext(r))
	code := http.StatusOK
	ready := health.Ready && (clusterStatus.Role == cluster.RoleStandalone || clusterStatus.LeaderID != "" || clusterStatus.Role == cluster.RoleLeader)
	if !ready {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]interface{}{
		"ready":   ready,
		"engine":  health,
		"cluster": clusterStatus,
		"leader":  h.node.LeaderAddress(h.requestContext(r)),
	})
}

type nodeEngineAdapter struct {
	node cluster.Node
	ctx  context.Context
}

func (a *nodeEngineAdapter) Put(key, value []byte) error {
	return a.node.Put(a.ctx, key, value)
}

func (a *nodeEngineAdapter) Delete(key []byte) error {
	return a.node.Delete(a.ctx, key)
}

func (a *nodeEngineAdapter) Get(key []byte) ([]byte, error) {
	return a.node.Get(a.ctx, key)
}

func (a *nodeEngineAdapter) ForceCompaction(level int) {
	a.node.ForceCompaction(a.ctx, level)
}

func (a *nodeEngineAdapter) ForceFlush() {
	a.node.ForceFlush(a.ctx)
}

func (a *nodeEngineAdapter) Scan(start, end []byte, limit int) [][2]string {
	results, _ := a.node.Scan(a.ctx, start, end, limit)
	return results
}

func (a *nodeEngineAdapter) Stats() map[string]interface{} {
	return a.node.Stats(a.ctx)
}

func (a *nodeEngineAdapter) EventBus() *events.EventBus {
	return a.node.EventBus()
}
