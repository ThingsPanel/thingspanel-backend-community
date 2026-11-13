package uplinkcontract

import (
	"container/list"
	"context"
	"sync"
	"time"

	"project/internal/brokersemantic"
)

type contractLoader func(context.Context, Pins) (*brokersemantic.UplinkContract, error)
type cacheEntry struct {
	pins     Pins
	contract *brokersemantic.UplinkContract
	cost     int
}
type loadFlight struct {
	done     chan struct{}
	contract *brokersemantic.UplinkContract
	err      error
}
type contractCache struct {
	mu                          sync.Mutex
	entries                     map[Pins]*list.Element
	lru                         *list.List
	flights                     map[Pins]*loadFlight
	slots                       chan struct{}
	bytes, maxBytes, maxEntries int
	load                        contractLoader
}

func newContractCache(load contractLoader, entries, bytes, concurrent int) *contractCache {
	return &contractCache{entries: map[Pins]*list.Element{}, lru: list.New(), flights: map[Pins]*loadFlight{}, slots: make(chan struct{}, concurrent), maxEntries: entries, maxBytes: bytes, load: load}
}

func (c *contractCache) get(ctx context.Context, pins Pins) (*brokersemantic.UplinkContract, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if item := c.entries[pins]; item != nil {
		c.lru.MoveToFront(item)
		value := item.Value.(*cacheEntry).contract
		c.mu.Unlock()
		return value, nil
	}
	flight := c.flights[pins]
	if flight == nil {
		// No unbounded queue/goroutines for different cold publications. Callers
		// retain their existing bounded retry/backpressure behavior on saturation.
		select {
		case c.slots <- struct{}{}:
		default:
			c.mu.Unlock()
			return nil, ErrUnavailable
		}
		flight = &loadFlight{done: make(chan struct{})}
		c.flights[pins] = flight
		// A cancelled first waiter must not cancel other same-key waiters. The
		// shared load is independently bounded; it never holds the cache mutex.
		go c.fetch(context.WithoutCancel(ctx), pins, flight)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.contract, flight.err
	}
}

func (c *contractCache) fetch(parent context.Context, pins Pins, flight *loadFlight) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	value, err := c.load(ctx, pins)
	if err == nil && (value == nil || ctx.Err() != nil) {
		value = nil
		err = ErrUnavailable
	}
	c.mu.Lock()
	if err == nil && value.CacheCost() <= c.maxBytes {
		cost := value.CacheCost() + len(pins.TenantID) + len(pins.ModelID) + len(pins.SnapshotID) + 128
		if cost <= c.maxBytes {
			for c.lru.Len() >= c.maxEntries || c.bytes+cost > c.maxBytes {
				item := c.lru.Back()
				if item == nil {
					break
				}
				old := item.Value.(*cacheEntry)
				c.bytes -= old.cost
				delete(c.entries, old.pins)
				c.lru.Remove(item)
			}
			c.entries[pins] = c.lru.PushFront(&cacheEntry{pins, value, cost})
			c.bytes += cost
		}
	}
	flight.contract, flight.err = value, err
	delete(c.flights, pins)
	<-c.slots
	close(flight.done)
	c.mu.Unlock()
}
