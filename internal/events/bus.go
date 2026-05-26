// Package events provides a lightweight publish-subscribe event bus used by
// all components of the LSM engine to emit observable lifecycle events.
package events

import "sync"

// EventType is a string identifier for a published event.
type EventType string

const (
	// WAL events
	EvtWALAppend EventType = "wal.append"
	EvtWALSync   EventType = "wal.sync"

	// MemTable events
	EvtMemTablePut  EventType = "memtable.put"
	EvtMemTableFull EventType = "memtable.full"
	// EvtMemTableRotate is emitted when the mutable MemTable is rotated to immutable.
	EvtMemTableRotate EventType = "memtable.rotate"

	// Flush events
	EvtFlushStart    EventType = "flush.start"
	EvtFlushComplete EventType = "flush.complete"

	// SSTable events
	EvtSSTableCreated EventType = "sstable.created"
	EvtSSTableDeleted EventType = "sstable.deleted"

	// Bloom filter events
	EvtBloomCheck EventType = "bloom.check"
	EvtBloomHit   EventType = "bloom.hit"
	EvtBloomMiss  EventType = "bloom.miss"

	// Cache events
	EvtCacheHit  EventType = "cache.hit"
	EvtCacheMiss EventType = "cache.miss"

	// Read events
	EvtReadStart    EventType = "read.start"
	EvtReadMemTable EventType = "read.memtable"
	EvtReadSSTable  EventType = "read.sstable"
	EvtReadComplete EventType = "read.complete"
	EvtBloomFP      EventType = "bloom.fp"

	// Compaction events
	EvtCompactionStart    EventType = "compaction.start"
	EvtCompactionPick     EventType = "compaction.pick"
	EvtCompactionMerge    EventType = "compaction.merge"
	EvtCompactionComplete EventType = "compaction.complete"
	EvtTombstoneDropped   EventType = "tombstone.dropped"

	// Block cache / disk read events
	EvtBlockRead EventType = "block.read"

	// MANIFEST
	EvtManifestApply EventType = "manifest.apply"

	// Amplification events
	EvtAmplification EventType = "amplification"

	// Scenario step
	EvtScenarioStep EventType = "scenario.step"
)

// Event is a single published notification carrying a type and optional metadata.
type Event struct {
	Type  EventType              `json:"type"`
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// EventPublisher is the interface implemented by any component that can publish events.
type EventPublisher interface {
	Publish(evt Event)
}

// Subscriber is a callback function invoked synchronously when a matching event is published.
type Subscriber func(evt Event)

// EventBus is a synchronous publish-subscribe event bus that fans out events to registered subscribers.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]Subscriber
	allSubs     []Subscriber
}

// NewEventBus creates an empty EventBus ready for use.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[EventType][]Subscriber),
	}
}

// Publish sends an event to all subscribers (non-blocking)
func (b *EventBus) Publish(evt Event) {
	b.mu.RLock()
	subs := make([]Subscriber, 0, len(b.subscribers[evt.Type])+len(b.allSubs))
	subs = append(subs, b.subscribers[evt.Type]...)
	subs = append(subs, b.allSubs...)
	b.mu.RUnlock()

	for _, sub := range subs {
		sub(evt)
	}
}

// Subscribe to a specific event type
func (b *EventBus) Subscribe(evtType EventType, sub Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[evtType] = append(b.subscribers[evtType], sub)
}

// SubscribeAll subscribes to all event types
func (b *EventBus) SubscribeAll(sub Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.allSubs = append(b.allSubs, sub)
}

// ChannelBus is a non-blocking channel-based event publisher
// that can be used to fan out to WebSocket clients.
type ChannelBus struct {
	ch chan Event
}

// NewChannelBus creates a ChannelBus with a buffer of bufSize events.
func NewChannelBus(bufSize int) *ChannelBus {
	return &ChannelBus{ch: make(chan Event, bufSize)}
}

// Publish sends evt to the channel. It drops the event silently if the buffer is full.
func (c *ChannelBus) Publish(evt Event) {
	select {
	case c.ch <- evt:
	default: // drop if full; never block storage goroutines
	}
}

// Chan returns the read-only channel that delivers published events.
func (c *ChannelBus) Chan() <-chan Event {
	return c.ch
}

// NoopBus is a no-op publisher (for tests)
type NoopBus struct{}

// Publish discards the event. NoopBus satisfies EventPublisher with zero overhead.
func (n *NoopBus) Publish(_ Event) {}
