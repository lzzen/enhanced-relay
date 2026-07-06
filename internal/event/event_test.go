package event_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzzen/enhanced-relay/internal/event"
	"github.com/lzzen/enhanced-relay/internal/testutil/req"
)

func TestBus_DeliversToMatchingSubscribers(t *testing.T) {
	req.Covers(t, "REQ-EXT-EVENT-DELIVER-001")
	b := event.New(16)
	var got atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	b.Subscribe("audit", []string{"RequestCompleted"}, func(e event.Event) {
		got.Add(1)
		wg.Done()
	})
	b.Start()

	b.Publish(event.Event{Type: "RequestCompleted"})
	b.Publish(event.Event{Type: "SomethingElse"}) // must not match

	wg.Wait()
	b.Drain(context.Background())
	if got.Load() != 1 {
		t.Fatalf("expected exactly 1 matched event, got %d", got.Load())
	}
}

func TestBus_NonPositiveQueue_DefaultsToBuffered(t *testing.T) {
	req.Covers(t, "REQ-EXT-EVENT-DEFAULTQ-001")
	// New(0) must default to a buffered queue. Without starting the bus (no
	// worker draining), a single publish should be buffered, not dropped. If the
	// default were an unbuffered (size 0) queue, the publish would drop.
	b := event.New(0)
	b.Subscribe("s", nil, func(event.Event) {})
	b.Publish(event.Event{Type: "x"})
	if got := b.Dropped("s"); got != 0 {
		t.Fatalf("New(0) must default to a buffered queue; got %d drops", got)
	}
}

func TestBus_PublishNeverBlocks_DropsOnOverflow(t *testing.T) {
	req.Covers(t, "REQ-EXT-EVENT-NONBLOCK-001")
	b := event.New(1)
	release := make(chan struct{})
	b.Subscribe("slow", nil, func(event.Event) {
		<-release // block the single worker so the queue fills
	})
	b.Start()

	// Publish must return promptly even though the subscriber is stuck.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			b.Publish(event.Event{Type: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked: data plane would stall")
	}

	if b.Dropped("slow") == 0 {
		t.Fatal("expected overflow drops to be counted")
	}
	close(release)
	b.Drain(context.Background())
}

func TestBus_SlowSubscriberDoesNotStallOthers(t *testing.T) {
	req.Covers(t, "REQ-EXT-EVENT-ISOLATION-001")
	b := event.New(1)
	release := make(chan struct{})
	b.Subscribe("slow", nil, func(event.Event) { <-release })

	var fast atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	b.Subscribe("fast", nil, func(event.Event) {
		if fast.Add(1) == 1 {
			wg.Done()
		}
	})
	b.Start()

	b.Publish(event.Event{Type: "x"})
	// Fast subscriber processes despite the slow one being blocked.
	waitTimeout(t, &wg, 2*time.Second)

	close(release)
	b.Drain(context.Background())
}

func waitTimeout(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("timed out waiting for isolated subscriber")
	}
}
