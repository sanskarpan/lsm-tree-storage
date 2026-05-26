package cluster

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"lsm-engine/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShardedNode_RoutingPlanInitializesAndHidesReservedKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NodeID = "local"
	cfg.ShardCount = 2
	cfg.RoutingSlots = 32
	cfg.RebalanceInterval = 0

	engCfg := engine.DefaultConfig(t.TempDir())
	engCfg.MemTableSize = 4 * 1024
	engCfg.SyncWAL = true

	node, err := OpenShardedNode(cfg, engCfg)
	require.NoError(t, err)
	defer func() { _ = node.Close() }()

	plan, err := node.loadRoutingPlan(context.Background())
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Equal(t, 32, len(plan.Slots))

	err = node.Put(context.Background(), []byte(routingPlanKey), []byte("forbidden"))
	require.Error(t, err)

	items, err := node.Scan(context.Background(), nil, nil, 0)
	require.NoError(t, err)
	for _, item := range items {
		require.NotEqual(t, routingPlanKey, item[0])
	}
}

func TestShardedNode_MigrateSlotMovesKeysAndCleansSource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NodeID = "local"
	cfg.ShardCount = 2
	cfg.RoutingSlots = 32
	cfg.RebalanceInterval = 0

	engCfg := engine.DefaultConfig(t.TempDir())
	engCfg.MemTableSize = 4 * 1024
	engCfg.SyncWAL = true

	node, err := OpenShardedNode(cfg, engCfg)
	require.NoError(t, err)
	defer func() { _ = node.Close() }()

	plan, err := node.loadRoutingPlan(context.Background())
	require.NoError(t, err)

	keys := keysForSlot(t, plan, 0, 8)
	values := make(map[string]string, len(keys))
	for i, key := range keys {
		value := fmt.Sprintf("value-%d", i)
		values[string(key)] = value
		require.NoError(t, node.Put(context.Background(), key, []byte(value)))
	}

	slot, count := node.heaviestSlotOnShard(context.Background(), plan, 0)
	require.Equal(t, 0, slot)
	require.Greater(t, count, 0)

	require.NoError(t, node.migrateSlot(context.Background(), plan, slot, 0, 1))

	updated, err := node.loadRoutingPlan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, updated.Slots[slot].Owner)
	assert.False(t, updated.Slots[slot].Migrating)

	for _, key := range keys {
		value, err := node.Get(context.Background(), key)
		require.NoError(t, err)
		assert.Equal(t, values[string(key)], string(value))
		assert.Equal(t, 1, node.shardForKey(key))
	}

	source, ok := node.shards[0].(*StandaloneNode)
	require.True(t, ok)
	for _, key := range keys {
		_, err := source.eng.Get(key)
		require.ErrorIs(t, err, engine.ErrNotFound)
	}
}

func TestShardedNode_RebalanceOnceMovesHotSlot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.NodeID = "local"
	cfg.ShardCount = 2
	cfg.RoutingSlots = 32
	cfg.RebalanceInterval = 0
	cfg.RebalanceThresholdBytes = 1
	cfg.RebalanceMaxSlots = 1

	engCfg := engine.DefaultConfig(t.TempDir())
	engCfg.MemTableSize = 4 * 1024
	engCfg.SyncWAL = true

	node, err := OpenShardedNode(cfg, engCfg)
	require.NoError(t, err)
	defer func() { _ = node.Close() }()

	plan, err := node.loadRoutingPlan(context.Background())
	require.NoError(t, err)

	keys := keysForSlot(t, plan, 0, 12)
	for i, key := range keys {
		payload := strings.Repeat(fmt.Sprintf("hot-%d", i), 32)
		require.NoError(t, node.Put(context.Background(), key, []byte(payload)))
	}

	slot, count := node.heaviestSlotOnShard(context.Background(), plan, 0)
	require.Equal(t, 0, slot)
	require.Greater(t, count, 0)

	require.NoError(t, node.rebalanceOnce(context.Background()))

	updated, err := node.loadRoutingPlan(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, updated.Slots[slot].Owner)

	for _, key := range keys {
		value, err := node.Get(context.Background(), key)
		require.NoError(t, err)
		require.NotEmpty(t, value)
		assert.Equal(t, 1, node.shardForKey(key))
	}
}

func keysForSlot(t *testing.T, plan *routingPlan, slot, count int) [][]byte {
	t.Helper()
	keys := make([][]byte, 0, count)
	for i := 0; len(keys) < count && i < 100000; i++ {
		key := []byte(fmt.Sprintf("slot-%d-%d", slot, i))
		if plan.slotForKey(key) != slot {
			continue
		}
		keys = append(keys, key)
	}
	require.Len(t, keys, count)
	return keys
}
