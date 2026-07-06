package reqctx

import (
	"sync"
	"time"

	"github.com/lzzen/enhanced-relay/internal/clock"
)

// Timings records per-stage durations so any slow request can be explained
// segment by segment (docs/observability-security.md §1).
type Timings struct {
	clk clock.Clock

	mu    sync.Mutex
	spans map[string]time.Duration
}

// NewTimings creates a Timings recorder bound to a clock.
func NewTimings(clk clock.Clock) *Timings {
	return &Timings{clk: clk, spans: make(map[string]time.Duration)}
}

// Measure runs fn and records its duration under the given segment name.
func (t *Timings) Measure(segment string, fn func()) {
	start := t.clk.Now()
	fn()
	t.record(segment, t.clk.Since(start))
}

// Start returns a stop func that records elapsed time under segment when called.
func (t *Timings) Start(segment string) (stop func()) {
	start := t.clk.Now()
	return func() { t.record(segment, t.clk.Since(start)) }
}

func (t *Timings) record(segment string, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.spans[segment] += d
}

// Snapshot returns a copy of all recorded segment durations.
func (t *Timings) Snapshot() map[string]time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]time.Duration, len(t.spans))
	for k, v := range t.spans {
		out[k] = v
	}
	return out
}
