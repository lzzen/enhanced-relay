// Package event implements the asynchronous, out-of-path extension points.
// See docs/plugin-architecture.md §3. Guarantees:
//   - Publish never blocks the data plane;
//   - each subscriber has its own bounded queue and worker, so one slow
//     subscriber cannot stall others or the request path;
//   - overflow drops are counted (never silently lost) for alerting.
//
// This in-memory bus is at-most-once. Durable, at-least-once delivery for
// money-related events (settlement) is a separate outbox concern (Phase 5).
package event

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Event is an immutable value carrying a redacted snapshot of request state.
type Event struct {
	Type    string
	Time    time.Time
	Payload any
}

// Handler consumes events for a single subscriber.
type Handler func(Event)

type subscriber struct {
	name    string
	topics  map[string]struct{}
	queue   chan Event
	handler Handler
	dropped atomic.Int64
}

func (s *subscriber) wants(topic string) bool {
	if len(s.topics) == 0 {
		return true // subscribe to all
	}
	_, ok := s.topics[topic]
	return ok
}

// Bus is a fan-out event bus with per-subscriber isolation.
type Bus struct {
	mu          sync.RWMutex
	subscribers []*subscriber
	queueSize   int

	wg      sync.WaitGroup
	started bool
	closed  atomic.Bool
}

// New returns a Bus whose subscribers each get a queue of the given size.
func New(perSubscriberQueue int) *Bus {
	if perSubscriberQueue <= 0 {
		perSubscriberQueue = 1024
	}
	return &Bus{queueSize: perSubscriberQueue}
}

// Subscribe registers a handler for the given topics (empty = all topics).
// Must be called before Start.
func (b *Bus) Subscribe(name string, topics []string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	set := make(map[string]struct{}, len(topics))
	for _, t := range topics {
		set[t] = struct{}{}
	}
	b.subscribers = append(b.subscribers, &subscriber{
		name:    name,
		topics:  set,
		queue:   make(chan Event, b.queueSize),
		handler: h,
	})
}

// Start launches one worker goroutine per subscriber.
func (b *Bus) Start() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return
	}
	b.started = true
	for _, s := range b.subscribers {
		b.wg.Add(1)
		go b.worker(s)
	}
}

func (b *Bus) worker(s *subscriber) {
	defer b.wg.Done()
	for e := range s.queue {
		func() {
			defer func() { _ = recover() }() // one bad event never kills the worker
			s.handler(e)
		}()
	}
}

// Publish delivers e to matching subscribers without ever blocking. If a
// subscriber's queue is full, the event is dropped for that subscriber and its
// drop counter is incremented.
func (b *Bus) Publish(e Event) {
	if b.closed.Load() {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subscribers {
		if !s.wants(e.Type) {
			continue
		}
		select {
		case s.queue <- e:
		default:
			s.dropped.Add(1)
		}
	}
}

// Dropped returns how many events were dropped for a named subscriber.
func (b *Bus) Dropped(name string) int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subscribers {
		if s.name == name {
			return s.dropped.Load()
		}
	}
	return 0
}

// Drain stops accepting new events, lets workers finish queued events, and
// waits (until ctx is done) for them to exit. Used during graceful shutdown.
func (b *Bus) Drain(ctx context.Context) {
	if b.closed.Swap(true) {
		return
	}
	b.mu.RLock()
	for _, s := range b.subscribers {
		close(s.queue)
	}
	b.mu.RUnlock()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
