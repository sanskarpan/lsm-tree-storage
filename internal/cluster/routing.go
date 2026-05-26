package cluster

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"
)

const (
	routingPlanKey            = "__lsm_cluster__/routing_plan"
	routingReservedPrefix     = "__lsm_cluster__/"
	defaultRoutingSlots       = 256
	defaultRebalanceThreshold = 64 << 20
	defaultRebalanceMaxSlots  = 1
)

type routingPlan struct {
	Version   uint64        `json:"version"`
	SlotCount int           `json:"slot_count"`
	UpdatedAt time.Time     `json:"updated_at"`
	Slots     []routingSlot `json:"slots"`
}

type routingSlot struct {
	Owner     int  `json:"owner"`
	Migrating bool `json:"migrating,omitempty"`
	Target    int  `json:"target,omitempty"`
}

func newRoutingPlan(shardCount, slotCount int) *routingPlan {
	if shardCount <= 0 {
		shardCount = 1
	}
	if slotCount <= 0 {
		slotCount = defaultRoutingSlots
	}
	plan := &routingPlan{
		Version:   1,
		SlotCount: slotCount,
		UpdatedAt: time.Now().UTC(),
		Slots:     make([]routingSlot, slotCount),
	}
	for slot := range plan.Slots {
		plan.Slots[slot].Owner = slot % shardCount
	}
	return plan
}

func loadRoutingPlan(data []byte) (*routingPlan, error) {
	var plan routingPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	if plan.SlotCount <= 0 {
		return nil, fmt.Errorf("cluster: routing plan missing slot count")
	}
	if len(plan.Slots) != plan.SlotCount {
		return nil, fmt.Errorf("cluster: routing plan slot count mismatch")
	}
	return &plan, nil
}

func (p *routingPlan) clone() *routingPlan {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Slots = append([]routingSlot(nil), p.Slots...)
	return &cp
}

func (p *routingPlan) marshal() ([]byte, error) {
	return json.Marshal(p)
}

func (p *routingPlan) slotForKey(key []byte) int {
	if p == nil || p.SlotCount <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write(key)
	return int(h.Sum32() % uint32(p.SlotCount))
}

func (p *routingPlan) routeForKey(key []byte) (owner int, target int, migrating bool) {
	if p == nil || len(p.Slots) == 0 {
		return 0, 0, false
	}
	slot := p.slotForKey(key)
	if slot < 0 || slot >= len(p.Slots) {
		return 0, 0, false
	}
	assignment := p.Slots[slot]
	if assignment.Owner < 0 {
		assignment.Owner = 0
	}
	if assignment.Owner >= len(p.Slots) {
		assignment.Owner = assignment.Owner % len(p.Slots)
	}
	if assignment.Migrating {
		if assignment.Target < 0 {
			assignment.Target = assignment.Owner
		}
		if assignment.Target >= len(p.Slots) {
			assignment.Target = assignment.Target % len(p.Slots)
		}
		return assignment.Owner, assignment.Target, true
	}
	return assignment.Owner, assignment.Owner, false
}

func (p *routingPlan) ownerForSlot(slot int) (owner int, target int, migrating bool) {
	if p == nil || slot < 0 || slot >= len(p.Slots) {
		return 0, 0, false
	}
	assignment := p.Slots[slot]
	if assignment.Migrating {
		return assignment.Owner, assignment.Target, true
	}
	return assignment.Owner, assignment.Owner, false
}

func (p *routingPlan) normalize(shardCount int) {
	if p == nil || shardCount <= 0 {
		return
	}
	if p.SlotCount <= 0 {
		p.SlotCount = defaultRoutingSlots
	}
	if len(p.Slots) != p.SlotCount {
		slots := make([]routingSlot, p.SlotCount)
		copy(slots, p.Slots)
		p.Slots = slots
	}
	for i := range p.Slots {
		if p.Slots[i].Owner < 0 || p.Slots[i].Owner >= shardCount {
			p.Slots[i].Owner = i % shardCount
		}
		if p.Slots[i].Target < 0 || p.Slots[i].Target >= shardCount {
			p.Slots[i].Target = 0
		}
	}
}

func isReservedClusterKey(key []byte) bool {
	return len(key) > 0 && hasClusterReservedPrefix(key)
}

func hasClusterReservedPrefix(key []byte) bool {
	return len(key) >= len(routingReservedPrefix) && string(key[:len(routingReservedPrefix)]) == routingReservedPrefix
}
