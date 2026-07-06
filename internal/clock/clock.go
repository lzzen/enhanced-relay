// Package clock provides an injectable time source so business logic never
// calls time.Now() directly. This is a pillar of the deterministic test harness
// (see docs/ai-testing-acceptance.md §3.2).
package clock

import (
	"sync"
	"time"
)

// Clock is the minimal time source used across the gateway.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// System is the production clock backed by the real wall clock.
type System struct{}

// New returns the production system clock.
func New() Clock { return System{} }

func (System) Now() time.Time                  { return time.Now() }
func (System) Since(t time.Time) time.Duration { return time.Since(t) }

// Fake is a controllable clock for tests. It is safe for concurrent use.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake clock anchored at the given instant.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) Since(t time.Time) time.Duration {
	return f.Now().Sub(t)
}

// Advance moves the fake clock forward. Tests drive elapsed time with this
// instead of sleeping (docs/testing.md: never actually wait hundreds of seconds).
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
