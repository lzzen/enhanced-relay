package clock_test

import (
	"testing"
	"time"

	"github.com/lzzen/enhanced-relay/internal/clock"
	"github.com/lzzen/enhanced-relay/internal/idgen"
	"github.com/lzzen/enhanced-relay/internal/testutil/req"
)

func TestFakeClock_Advance_Deterministic(t *testing.T) {
	req.Covers(t, "REQ-TEST-DETERMINISM-CLOCK-001")
	start := time.Unix(1000, 0)
	f := clock.NewFake(start)
	if !f.Now().Equal(start) {
		t.Fatalf("expected anchored start, got %v", f.Now())
	}
	f.Advance(90 * time.Second)
	if got := f.Since(start); got != 90*time.Second {
		t.Fatalf("expected 90s elapsed, got %v", got)
	}
}

func TestSequenceIDs_Deterministic(t *testing.T) {
	req.Covers(t, "REQ-TEST-DETERMINISM-ID-001")
	g := idgen.NewSequence("trace")
	if got := g.NewID(); got != "trace-000001" {
		t.Fatalf("expected trace-000001, got %s", got)
	}
	if got := g.NewID(); got != "trace-000002" {
		t.Fatalf("expected trace-000002, got %s", got)
	}
}

func TestRandomIDs_UniqueAndHex(t *testing.T) {
	req.Covers(t, "REQ-TEST-DETERMINISM-ID-002")
	g := idgen.New()
	seen := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		id := g.NewID()
		if len(id) != 32 {
			t.Fatalf("expected 32 hex chars, got %q", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
	}
}
