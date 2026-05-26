package cluster

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"lsm-engine/internal/engine"
	"lsm-engine/internal/events"
	"lsm-engine/internal/manifest"
)

type ShardedNode struct {
	mu            sync.RWMutex
	cfg           Config
	shards        []Node
	bus           *events.EventBus
	txJournal     *txJournalStore
	rebalanceStop chan struct{}
	rebalanceWG   sync.WaitGroup
}

func OpenShardedNode(cfg Config, baseEngineCfg engine.Config) (*ShardedNode, error) {
	if cfg.ShardCount <= 1 {
		return nil, fmt.Errorf("cluster: shard count must be > 1 for sharded node")
	}
	node := &ShardedNode{
		cfg:       cfg,
		shards:    make([]Node, 0, cfg.ShardCount),
		bus:       events.NewEventBus(),
		txJournal: newTxJournalStore(filepath.Join(cfg.DataDir, "txns")),
	}

	for shard := 0; shard < cfg.ShardCount; shard++ {
		shardCfg := deriveShardConfig(cfg, shard)
		engCfg := baseEngineCfg
		engCfg.DataDir = filepath.Join(baseEngineCfg.DataDir, "shards", shardName(shard))

		var child Node
		var err error
		if cfg.Enabled {
			child, err = OpenRaftNode(shardCfg, engCfg)
		} else {
			eng, openErr := engine.Open(engCfg)
			if openErr != nil {
				err = openErr
			} else {
				child = NewStandaloneNode(shardCfg.NodeID, eng)
			}
		}
		if err != nil {
			_ = node.Close()
			return nil, err
		}
		node.shards = append(node.shards, child)
		if bus := child.EventBus(); bus != nil {
			bus.SubscribeAll(func(evt events.Event) {
				node.bus.Publish(evt)
			})
		}
	}
	if err := node.recoverTransactions(context.Background()); err != nil {
		_ = node.Close()
		return nil, err
	}
	if err := node.ensureRoutingPlan(context.Background()); err != nil {
		_ = node.Close()
		return nil, err
	}
	if cfg.Enabled {
		node.startRebalancer()
	}
	return node, nil
}

func (n *ShardedNode) EventBus() *events.EventBus {
	return n.bus
}

func (n *ShardedNode) Status(ctx context.Context) Status {
	n.mu.RLock()
	defer n.mu.RUnlock()
	shards := n.Shards(ctx)
	status := Status{
		Enabled:    n.cfg.Enabled,
		NodeID:     n.cfg.NodeID,
		PeerCount:  len(n.Peers(ctx)),
		ShardCount: len(shards),
		Shards:     shards,
	}
	if len(shards) == 0 {
		status.Role = RoleStandalone
		return status
	}
	status.Role = shards[0].Role
	status.LeaderID = shards[0].LeaderID
	status.Term = shards[0].Term
	status.CommitIndex = shards[0].CommitIndex
	status.LastApplied = shards[0].LastApplied
	for _, shard := range shards[1:] {
		if shard.Role != status.Role || shard.LeaderID != status.LeaderID {
			status.Role = RoleMixed
			status.LeaderID = ""
		}
		if shard.Term > status.Term {
			status.Term = shard.Term
		}
		if shard.CommitIndex > status.CommitIndex {
			status.CommitIndex = shard.CommitIndex
		}
		if shard.LastApplied > status.LastApplied {
			status.LastApplied = shard.LastApplied
		}
	}
	if status.Role != RoleLeader && status.Role != RoleStandalone {
		status.ReadOnlyCause = "shards may have different leaders; per-key requests are routed by the shared slot map"
	}
	return status
}

func (n *ShardedNode) Shards(ctx context.Context) []ShardStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()
	shards := make([]ShardStatus, 0, len(n.shards))
	for i, shard := range n.shards {
		shardStatuses := shard.Shards(ctx)
		if len(shardStatuses) == 0 {
			continue
		}
		item := shardStatuses[0]
		item.ShardID = shardName(i)
		item.KeyspaceHint = fmt.Sprintf("slot-map[%d]", n.routingSlotCount())
		shards = append(shards, item)
	}
	sort.Slice(shards, func(i, j int) bool {
		return shards[i].ShardID < shards[j].ShardID
	})
	return shards
}

func (n *ShardedNode) LeaderAddress(ctx context.Context) string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	shards := n.Shards(ctx)
	if len(shards) == 0 {
		return ""
	}
	leader := shards[0].Leader
	for _, shard := range shards[1:] {
		if shard.Leader != leader {
			return ""
		}
	}
	return leader
}

func (n *ShardedNode) Peers(context.Context) []Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if len(n.shards) > 0 {
		seen := make(map[string]Peer)
		for shardIdx, shard := range n.shards {
			for _, peer := range shard.Peers(context.Background()) {
				base := Peer{
					NodeID:        baseNodeIDFromShardNodeID(peer.NodeID, shardIdx),
					RPCAddress:    baseAddressFromShardAddress(peer.RPCAddress, shardIdx, n.cfg.ShardPortStride),
					ClientAddress: peer.ClientAddress,
					Suffrage:      peer.Suffrage,
				}
				if existing, ok := seen[base.NodeID]; ok {
					if existing.ClientAddress == "" && base.ClientAddress != "" {
						existing.ClientAddress = base.ClientAddress
					}
					if existing.RPCAddress == "" && base.RPCAddress != "" {
						existing.RPCAddress = base.RPCAddress
					}
					if existing.Suffrage == "" && base.Suffrage != "" {
						existing.Suffrage = base.Suffrage
					}
					seen[base.NodeID] = existing
					continue
				}
				seen[base.NodeID] = base
			}
		}
		if len(seen) > 0 {
			return peersToSortedSlice(seen)
		}
	}
	seen := make(map[string]Peer)
	seen[n.cfg.NodeID] = Peer{
		NodeID:        n.cfg.NodeID,
		ClientAddress: n.cfg.ClientAddress,
	}
	for _, peer := range n.cfg.Peers {
		seen[peer.NodeID] = Peer{
			NodeID:        peer.NodeID,
			RPCAddress:    peer.RPCAddress,
			ClientAddress: peer.ClientAddress,
			Suffrage:      peer.Suffrage,
		}
	}
	return peersToSortedSlice(seen)
}

func (n *ShardedNode) AddPeer(ctx context.Context, peer Peer) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	for shard := range n.shards {
		if err := n.shards[shard].AddPeer(ctx, deriveShardPeer(peer, shard, n.cfg.ShardPortStride)); err != nil {
			return err
		}
	}
	n.cfg.Peers = append(n.cfg.Peers, peer)
	return nil
}

func (n *ShardedNode) RemovePeer(ctx context.Context, nodeID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	for shard := range n.shards {
		if err := n.shards[shard].RemovePeer(ctx, deriveShardNodeID(nodeID, shard)); err != nil {
			return err
		}
	}
	filtered := n.cfg.Peers[:0]
	for _, peer := range n.cfg.Peers {
		if peer.NodeID != nodeID {
			filtered = append(filtered, peer)
		}
	}
	n.cfg.Peers = filtered
	return nil
}

func (n *ShardedNode) Put(ctx context.Context, key, value []byte) error {
	batch := &engine.WriteBatch{}
	batch.Put(key, value)
	return n.Write(ctx, batch)
}

func (n *ShardedNode) Delete(ctx context.Context, key []byte) error {
	batch := &engine.WriteBatch{}
	batch.Delete(key)
	return n.Write(ctx, batch)
}

func (n *ShardedNode) Write(ctx context.Context, batch *engine.WriteBatch) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if batch == nil {
		return nil
	}
	entries := batch.Entries()
	if len(entries) == 0 {
		return nil
	}
	shardBatches := make(map[int]*engine.WriteBatch)
	for _, entry := range entries {
		if isReservedClusterKey(entry.Key) {
			return fmt.Errorf("cluster: key %q is reserved", string(entry.Key))
		}
		primary, secondary, migrating, err := n.routeForKey(ctx, entry.Key)
		if err != nil {
			return err
		}
		if shardBatches[primary] == nil {
			shardBatches[primary] = &engine.WriteBatch{}
		}
		if entry.Delete {
			shardBatches[primary].Delete(entry.Key)
		} else {
			shardBatches[primary].Put(entry.Key, entry.Value)
		}
		if migrating && secondary != primary {
			if shardBatches[secondary] == nil {
				shardBatches[secondary] = &engine.WriteBatch{}
			}
			if entry.Delete {
				shardBatches[secondary].Delete(entry.Key)
			} else {
				shardBatches[secondary].Put(entry.Key, entry.Value)
			}
		}
	}
	if len(shardBatches) == 1 {
		for shardID, shardBatch := range shardBatches {
			return n.shards[shardID].Write(ctx, shardBatch)
		}
	}
	return n.writeCrossShardBatch(ctx, shardBatches)
}

func (n *ShardedNode) Get(ctx context.Context, key []byte) ([]byte, error) {
	return n.GetWithConsistency(ctx, key, ReadConsistencyLinearizable)
}

func (n *ShardedNode) Scan(ctx context.Context, start, end []byte, limit int) ([][2]string, error) {
	return n.ScanWithConsistency(ctx, start, end, limit, ReadConsistencyLinearizable)
}

func (n *ShardedNode) GetWithConsistency(ctx context.Context, key []byte, consistency ReadConsistency) ([]byte, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if isReservedClusterKey(key) {
		return nil, engine.ErrNotFound
	}
	primary, _, _, err := n.routeForKey(ctx, key)
	if err != nil {
		return nil, err
	}
	return n.shards[primary].GetWithConsistency(ctx, key, consistency)
}

func (n *ShardedNode) ScanWithConsistency(ctx context.Context, start, end []byte, limit int, consistency ReadConsistency) ([][2]string, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	type scanValue struct {
		value string
		owner int
	}
	all := make(map[string]scanValue)
	plan, err := n.loadRoutingPlan(ctx)
	if err != nil {
		plan = newRoutingPlan(len(n.shards), n.routingSlotCount())
	}
	for shardID, shard := range n.shards {
		perShardLimit := limit
		if limit > 0 {
			perShardLimit = 0
		}
		items, err := shard.ScanWithConsistency(ctx, start, end, perShardLimit, consistency)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if isReservedClusterKey([]byte(item[0])) {
				continue
			}
			primary, _, _ := plan.routeForKey([]byte(item[0]))
			if existing, ok := all[item[0]]; ok {
				if shardID != primary && existing.owner == primary {
					continue
				}
				if shardID != primary && existing.owner != primary {
					continue
				}
			}
			all[item[0]] = scanValue{value: item[1], owner: shardID}
		}
	}
	results := make([][2]string, 0, len(all))
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		results = append(results, [2]string{key, all[key].value})
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (n *ShardedNode) Version(context.Context) *manifest.Version {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if len(n.shards) == 0 {
		return nil
	}
	return n.shards[0].Version(context.Background())
}

func (n *ShardedNode) Stats(ctx context.Context) map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()
	total := map[string]interface{}{
		"cluster_enabled": n.cfg.Enabled,
		"shard_count":     len(n.shards),
	}
	perShard := make([]map[string]interface{}, 0, len(n.shards))
	var totalFiles, totalBytes, memtableSize, numImmutables, walFiles int
	var seqNo uint64
	for i, shard := range n.shards {
		stats := shard.Stats(ctx)
		perShard = append(perShard, stats)
		totalFiles += toInt(stats["total_sst_files"])
		totalBytes += toInt(stats["total_sst_bytes"])
		memtableSize += int(toInt64(stats["memtable_size"]))
		numImmutables += toInt(stats["num_immutables"])
		walFiles += toInt(stats["wal_files"])
		seqNo += toUint64(stats["seq_no"])
		perShard[i]["shard_id"] = shardName(i)
	}
	total["total_sst_files"] = totalFiles
	total["total_sst_bytes"] = totalBytes
	total["memtable_size"] = memtableSize
	total["num_immutables"] = numImmutables
	total["wal_files"] = walFiles
	total["seq_no"] = seqNo
	total["shards"] = perShard
	return total
}

func (n *ShardedNode) HealthStatus(ctx context.Context) engine.HealthStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()
	shards := n.Shards(ctx)
	status := engine.HealthStatus{
		Ready: true,
		State: engine.RuntimeState{
			Open:            true,
			DataDir:         strings.TrimSpace(n.cfg.DataDir),
			CompactionStyle: "",
		},
		Reasons: []string{},
	}
	for _, shard := range shards {
		if shard.Role == RoleCandidate {
			status.Ready = false
			status.Reasons = append(status.Reasons, shard.ShardID+": candidate")
		}
		if shard.LeaderID == "" {
			status.Ready = false
			status.Reasons = append(status.Reasons, shard.ShardID+": no leader")
		}
	}
	return status
}

func (n *ShardedNode) RuntimeState(context.Context) engine.RuntimeState {
	return engine.RuntimeState{
		Open:          true,
		DataDir:       n.cfg.DataDir,
		SyncWAL:       true,
		ActiveWALPath: "",
	}
}

func (n *ShardedNode) EngineConfig(context.Context) engine.Config {
	return engine.Config{}
}

func (n *ShardedNode) MemTableSnapshot(ctx context.Context, limit int) engine.MemTablesSnapshot {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if len(n.shards) == 0 {
		return engine.MemTablesSnapshot{}
	}
	return n.shards[0].MemTableSnapshot(ctx, limit)
}

func (n *ShardedNode) ForceFlush(ctx context.Context) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, shard := range n.shards {
		shard.ForceFlush(ctx)
	}
}

func (n *ShardedNode) ForceCompaction(ctx context.Context, level int) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, shard := range n.shards {
		shard.ForceCompaction(ctx, level)
	}
}

func (n *ShardedNode) SetCompactionStyle(ctx context.Context, style string) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, shard := range n.shards {
		shard.SetCompactionStyle(ctx, style)
	}
}

func (n *ShardedNode) Close() error {
	stop := n.rebalanceStop
	n.rebalanceStop = nil
	if stop != nil {
		close(stop)
		n.rebalanceWG.Wait()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var firstErr error
	for _, shard := range n.shards {
		if err := shard.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (n *ShardedNode) writeCrossShardBatch(ctx context.Context, shardBatches map[int]*engine.WriteBatch) error {
	txID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), len(shardBatches))
	record, applied, err := n.prepareCrossShardTransaction(ctx, txID, shardBatches)
	if err != nil {
		return err
	}
	if err := n.txJournal.Save(record); err != nil {
		return err
	}
	if err := n.applyCrossShardTransaction(ctx, record, shardBatches, applied); err != nil {
		_ = n.txJournal.Delete(txID)
		return err
	}
	record.Committed = true
	if err := n.txJournal.Save(record); err != nil {
		_ = n.txJournal.Delete(txID)
		return err
	}
	if err := n.txJournal.Delete(txID); err != nil {
		return err
	}
	return nil
}

func (n *ShardedNode) prepareCrossShardTransaction(ctx context.Context, txID string, shardBatches map[int]*engine.WriteBatch) (txJournalEntry, []int, error) {
	preImages := make([]txPreImage, 0)
	applied := make([]int, 0, len(shardBatches))
	shardIDs := make([]int, 0, len(shardBatches))
	for shardID := range shardBatches {
		shardIDs = append(shardIDs, shardID)
	}
	sort.Ints(shardIDs)
	seen := make(map[string]struct{})
	for _, shardID := range shardIDs {
		batch := shardBatches[shardID]
		if batch == nil {
			continue
		}
		for _, entry := range batch.Entries() {
			keyID := fmt.Sprintf("%d:%s", shardID, string(entry.Key))
			if _, ok := seen[keyID]; ok {
				continue
			}
			seen[keyID] = struct{}{}
			value, err := n.shards[shardID].GetWithConsistency(ctx, entry.Key, ReadConsistencyLinearizable)
			if err != nil && err != engine.ErrNotFound {
				return txJournalEntry{}, nil, err
			}
			img := txPreImage{Shard: shardID, Key: string(entry.Key), Present: err == nil}
			if err == nil {
				img.Value = string(value)
			}
			preImages = append(preImages, img)
		}
	}
	return txJournalEntry{
		ID:         txID,
		CreatedAt:  time.Now().UTC(),
		PreImages:  preImages,
		EntryCount: len(preImages),
	}, applied, nil
}

func (n *ShardedNode) applyCrossShardTransaction(ctx context.Context, record txJournalEntry, shardBatches map[int]*engine.WriteBatch, _ []int) error {
	shardIDs := make([]int, 0, len(shardBatches))
	for shardID := range shardBatches {
		shardIDs = append(shardIDs, shardID)
	}
	sort.Ints(shardIDs)
	applied := make([]int, 0, len(shardIDs))
	for _, shardID := range shardIDs {
		if err := n.shards[shardID].Write(ctx, shardBatches[shardID]); err != nil {
			if rollbackErr := n.rollbackCrossShardTransaction(ctx, record, applied); rollbackErr != nil {
				return fmt.Errorf("cluster: cross-shard batch failed: %v (rollback error: %w)", err, rollbackErr)
			}
			return err
		}
		applied = append(applied, shardID)
	}
	return nil
}

func (n *ShardedNode) rollbackCrossShardTransaction(ctx context.Context, record txJournalEntry, applied []int) error {
	byShard := make(map[int]*engine.WriteBatch)
	for i := len(record.PreImages) - 1; i >= 0; i-- {
		pre := record.PreImages[i]
		batch := byShard[pre.Shard]
		if batch == nil {
			batch = &engine.WriteBatch{}
			byShard[pre.Shard] = batch
		}
		if pre.Present {
			batch.Put([]byte(pre.Key), []byte(pre.Value))
		} else {
			batch.Delete([]byte(pre.Key))
		}
	}
	for i := len(applied) - 1; i >= 0; i-- {
		shardID := applied[i]
		if batch := byShard[shardID]; batch != nil {
			if err := n.shards[shardID].Write(ctx, batch); err != nil {
				return err
			}
		}
	}
	return nil
}

func (n *ShardedNode) recoverTransactions(ctx context.Context) error {
	pending, err := n.txJournal.LoadAll()
	if err != nil {
		return err
	}
	for _, record := range pending {
		if record.Committed {
			if err := n.txJournal.Delete(record.ID); err != nil {
				return err
			}
			continue
		}
		applied := make([]int, 0, len(record.PreImages))
		seen := make(map[int]struct{})
		for _, pre := range record.PreImages {
			if _, ok := seen[pre.Shard]; ok {
				continue
			}
			seen[pre.Shard] = struct{}{}
			applied = append(applied, pre.Shard)
		}
		if err := n.rollbackCrossShardTransaction(ctx, record, applied); err != nil {
			return fmt.Errorf("cluster: recover transaction %s: %w", record.ID, err)
		}
		if err := n.txJournal.Delete(record.ID); err != nil {
			return err
		}
	}
	return nil
}

func (n *ShardedNode) routingSlotCount() int {
	if n.cfg.RoutingSlots > 0 {
		return n.cfg.RoutingSlots
	}
	return defaultRoutingSlots
}

func (n *ShardedNode) loadRoutingPlan(ctx context.Context) (*routingPlan, error) {
	if len(n.shards) == 0 {
		return newRoutingPlan(1, n.routingSlotCount()), nil
	}
	data, err := n.shards[0].GetWithConsistency(ctx, []byte(routingPlanKey), ReadConsistencyLinearizable)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) {
			return newRoutingPlan(len(n.shards), n.routingSlotCount()), nil
		}
		return nil, err
	}
	plan, err := loadRoutingPlan(data)
	if err != nil {
		return nil, err
	}
	plan.normalize(len(n.shards))
	return plan, nil
}

func (n *ShardedNode) ensureRoutingPlan(ctx context.Context) error {
	if len(n.shards) == 0 {
		return nil
	}
	plan, err := n.loadRoutingPlan(ctx)
	if err != nil {
		return err
	}
	if plan == nil {
		plan = newRoutingPlan(len(n.shards), n.routingSlotCount())
	}
	return n.persistRoutingPlan(ctx, plan)
}

func (n *ShardedNode) persistRoutingPlan(ctx context.Context, plan *routingPlan) error {
	if len(n.shards) == 0 || plan == nil {
		return nil
	}
	plan.normalize(len(n.shards))
	plan.UpdatedAt = time.Now().UTC()
	data, err := plan.marshal()
	if err != nil {
		return err
	}
	batch := &engine.WriteBatch{}
	batch.Put([]byte(routingPlanKey), data)
	return n.shards[0].Write(ctx, batch)
}

func (n *ShardedNode) routeForKey(ctx context.Context, key []byte) (int, int, bool, error) {
	if len(n.shards) == 0 {
		return 0, 0, false, fmt.Errorf("cluster: no shards configured")
	}
	plan, err := n.loadRoutingPlan(ctx)
	if err != nil {
		return n.legacyShardForKey(key), n.legacyShardForKey(key), false, nil
	}
	primary, secondary, migrating := plan.routeForKey(key)
	if primary < 0 || primary >= len(n.shards) {
		primary = n.legacyShardForKey(key)
	}
	if secondary < 0 || secondary >= len(n.shards) {
		secondary = primary
	}
	return primary, secondary, migrating, nil
}

func (n *ShardedNode) legacyShardForKey(key []byte) int {
	if len(n.shards) == 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write(key)
	return int(h.Sum32() % uint32(len(n.shards)))
}

func (n *ShardedNode) shardForKey(key []byte) int {
	primary, _, _, err := n.routeForKey(context.Background(), key)
	if err != nil {
		return n.legacyShardForKey(key)
	}
	return primary
}

func (n *ShardedNode) startRebalancer() {
	if n.cfg.RebalanceInterval <= 0 || len(n.shards) <= 1 {
		return
	}
	stop := make(chan struct{})
	n.rebalanceStop = stop
	n.rebalanceWG.Add(1)
	go func(stopCh <-chan struct{}) {
		defer n.rebalanceWG.Done()
		ticker := time.NewTicker(n.cfg.RebalanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				timeout := n.cfg.ApplyTimeout
				if timeout <= 0 {
					timeout = 30 * time.Second
				}
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				_ = n.rebalanceOnce(ctx)
				cancel()
			case <-stopCh:
				return
			}
		}
	}(stop)
}

func (n *ShardedNode) rebalanceOnce(ctx context.Context) error {
	if len(n.shards) <= 1 || len(n.shards) == 0 {
		return nil
	}
	status := n.shards[0].Status(ctx)
	if status.Role != RoleLeader && status.Role != RoleStandalone {
		return nil
	}
	plan, err := n.loadRoutingPlan(ctx)
	if err != nil {
		return err
	}
	threshold := n.cfg.RebalanceThresholdBytes
	if threshold <= 0 {
		threshold = defaultRebalanceThreshold
	}
	maxMoves := n.cfg.RebalanceMaxSlots
	if maxMoves <= 0 {
		maxMoves = defaultRebalanceMaxSlots
	}
	for moved := 0; moved < maxMoves; moved++ {
		loads := make([]int64, len(n.shards))
		for i, shard := range n.shards {
			stats := shard.Stats(ctx)
			loads[i] = toInt64(stats["total_sst_bytes"]) + toInt64(stats["memtable_size"])
		}
		source, target := mostImbalancedShards(loads)
		if loads[source]-loads[target] < threshold {
			return nil
		}
		slot, count := n.heaviestSlotOnShard(ctx, plan, source)
		if slot < 0 || count == 0 {
			return nil
		}
		if err := n.migrateSlot(ctx, plan, slot, source, target); err != nil {
			return err
		}
		plan, err = n.loadRoutingPlan(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func mostImbalancedShards(loads []int64) (source int, target int) {
	if len(loads) == 0 {
		return 0, 0
	}
	source, target = 0, 0
	for i := range loads {
		if loads[i] > loads[source] {
			source = i
		}
		if loads[i] < loads[target] {
			target = i
		}
	}
	return source, target
}

func (n *ShardedNode) heaviestSlotOnShard(ctx context.Context, plan *routingPlan, shardID int) (int, int) {
	if plan == nil || shardID < 0 || shardID >= len(n.shards) {
		return -1, 0
	}
	items, err := n.shards[shardID].ScanWithConsistency(ctx, nil, nil, 0, ReadConsistencyLinearizable)
	if err != nil {
		return -1, 0
	}
	counts := make(map[int]int)
	for _, item := range items {
		if isReservedClusterKey([]byte(item[0])) {
			continue
		}
		slot := plan.slotForKey([]byte(item[0]))
		if slot >= 0 && slot < len(plan.Slots) {
			if owner, _, migrating := plan.ownerForSlot(slot); owner == shardID && !migrating {
				counts[slot]++
			}
		}
	}
	bestSlot, bestCount := -1, 0
	for slot, count := range counts {
		if count > bestCount {
			bestSlot = slot
			bestCount = count
		}
	}
	return bestSlot, bestCount
}

func (n *ShardedNode) migrateSlot(ctx context.Context, plan *routingPlan, slot, source, target int) error {
	if plan == nil || slot < 0 || slot >= len(plan.Slots) {
		return fmt.Errorf("cluster: invalid routing slot %d", slot)
	}
	if source == target {
		return nil
	}
	current := plan.clone()
	current.Version++
	current.UpdatedAt = time.Now().UTC()
	current.Slots[slot].Owner = source
	current.Slots[slot].Migrating = true
	current.Slots[slot].Target = target
	if err := n.persistRoutingPlan(ctx, current); err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		if err := n.copySlotKeys(ctx, plan, slot, source, target); err != nil {
			revert := current.clone()
			revert.Version++
			revert.UpdatedAt = time.Now().UTC()
			revert.Slots[slot] = routingSlot{Owner: source}
			_ = n.persistRoutingPlan(context.Background(), revert)
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	final := current.clone()
	final.Version++
	final.UpdatedAt = time.Now().UTC()
	final.Slots[slot] = routingSlot{Owner: target}
	if err := n.persistRoutingPlan(ctx, final); err != nil {
		revert := current.clone()
		revert.Version++
		revert.UpdatedAt = time.Now().UTC()
		revert.Slots[slot] = routingSlot{Owner: source}
		_ = n.persistRoutingPlan(context.Background(), revert)
		return err
	}
	time.Sleep(250 * time.Millisecond)
	if err := n.copySlotKeys(ctx, plan, slot, source, target); err != nil {
		return err
	}
	if err := n.deleteSlotKeys(ctx, plan, slot, source); err != nil {
		return err
	}
	return nil
}

func (n *ShardedNode) copySlotKeys(ctx context.Context, plan *routingPlan, slot, source, target int) error {
	if plan == nil || source < 0 || source >= len(n.shards) || target < 0 || target >= len(n.shards) {
		return fmt.Errorf("cluster: invalid shard migration %d -> %d", source, target)
	}
	sourceItems, err := n.shards[source].ScanWithConsistency(ctx, nil, nil, 0, ReadConsistencyLinearizable)
	if err != nil {
		return err
	}
	targetItems, err := n.shards[target].ScanWithConsistency(ctx, nil, nil, 0, ReadConsistencyLinearizable)
	if err != nil {
		return err
	}
	sourceValues := make(map[string]string)
	for _, item := range sourceItems {
		if isReservedClusterKey([]byte(item[0])) {
			continue
		}
		if plan.slotForKey([]byte(item[0])) != slot {
			continue
		}
		sourceValues[item[0]] = item[1]
	}
	targetValues := make(map[string]struct{})
	for _, item := range targetItems {
		if isReservedClusterKey([]byte(item[0])) {
			continue
		}
		if plan.slotForKey([]byte(item[0])) != slot {
			continue
		}
		targetValues[item[0]] = struct{}{}
	}
	const batchSize = 128
	batch := &engine.WriteBatch{}
	flush := func() error {
		if len(batch.Entries()) == 0 {
			return nil
		}
		if err := n.writeDirectToShard(ctx, target, batch); err != nil {
			return err
		}
		batch = &engine.WriteBatch{}
		return nil
	}
	for key, value := range sourceValues {
		batch.Put([]byte(key), []byte(value))
		if len(batch.Entries()) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	for key := range targetValues {
		if _, ok := sourceValues[key]; ok {
			continue
		}
		batch.Delete([]byte(key))
		if len(batch.Entries()) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (n *ShardedNode) deleteSlotKeys(ctx context.Context, plan *routingPlan, slot, source int) error {
	if plan == nil || source < 0 || source >= len(n.shards) {
		return fmt.Errorf("cluster: invalid source shard %d", source)
	}
	sourceItems, err := n.shards[source].ScanWithConsistency(ctx, nil, nil, 0, ReadConsistencyLinearizable)
	if err != nil {
		return err
	}
	const batchSize = 128
	batch := &engine.WriteBatch{}
	flush := func() error {
		if len(batch.Entries()) == 0 {
			return nil
		}
		if err := n.writeDirectToShard(ctx, source, batch); err != nil {
			return err
		}
		batch = &engine.WriteBatch{}
		return nil
	}
	for _, item := range sourceItems {
		if isReservedClusterKey([]byte(item[0])) {
			continue
		}
		if plan.slotForKey([]byte(item[0])) != slot {
			continue
		}
		batch.Delete([]byte(item[0]))
		if len(batch.Entries()) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func (n *ShardedNode) writeDirectToShard(ctx context.Context, shard int, batch *engine.WriteBatch) error {
	if shard < 0 || shard >= len(n.shards) || batch == nil {
		return nil
	}
	return n.shards[shard].Write(ctx, batch)
}

func deriveShardConfig(cfg Config, shard int) Config {
	shardCfg := cfg
	shardCfg.NodeID = deriveShardNodeID(cfg.NodeID, shard)
	shardCfg.DataDir = filepath.Join(cfg.DataDir, "shards", shardName(shard))
	if cfg.Enabled {
		shardCfg.BindAddress = deriveShardAddress(cfg.BindAddress, shard, cfg.ShardPortStride)
		shardCfg.AdvertiseAddress = deriveShardAddress(cfg.AdvertiseAddress, shard, cfg.ShardPortStride)
	}
	shardCfg.Peers = make([]Peer, 0, len(cfg.Peers))
	for _, peer := range cfg.Peers {
		shardCfg.Peers = append(shardCfg.Peers, deriveShardPeer(peer, shard, cfg.ShardPortStride))
	}
	return shardCfg
}

func deriveShardPeer(peer Peer, shard, stride int) Peer {
	return Peer{
		NodeID:        deriveShardNodeID(peer.NodeID, shard),
		RPCAddress:    deriveShardAddress(peer.RPCAddress, shard, stride),
		ClientAddress: peer.ClientAddress,
		Suffrage:      peer.Suffrage,
	}
}

func deriveShardNodeID(nodeID string, shard int) string {
	return fmt.Sprintf("%s-%s", nodeID, shardName(shard))
}

func deriveShardAddress(addr string, shard, stride int) string {
	if addr == "" || stride <= 0 {
		return addr
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return addr
	}
	return net.JoinHostPort(host, strconv.Itoa(port+(shard*stride)))
}

func baseNodeIDFromShardNodeID(nodeID string, shard int) string {
	suffix := "-" + shardName(shard)
	if strings.HasSuffix(nodeID, suffix) {
		return strings.TrimSuffix(nodeID, suffix)
	}
	return nodeID
}

func baseAddressFromShardAddress(addr string, shard, stride int) string {
	if addr == "" || stride <= 0 {
		return addr
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return addr
	}
	basePort := port - (shard * stride)
	if basePort <= 0 {
		return addr
	}
	return net.JoinHostPort(host, strconv.Itoa(basePort))
}

func shardName(shard int) string {
	return fmt.Sprintf("shard-%02d", shard)
}

func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
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
