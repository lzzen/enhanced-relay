// Package req provides the requirement-to-test traceability primitive used by
// the AI acceptance harness (docs/ai-testing-acceptance.md §2). Tests call
// Covers to bind themselves to requirement IDs; a later CI step aggregates
// these into build/traceability.json and fails if any P0/P1 requirement is
// unbound or its bound test is red.
package req

import (
	"sync"
	"testing"
)

var (
	mu      sync.Mutex
	covered = map[string][]string{} // REQ-ID -> test names
)

// Covers records that the current test exercises the given requirement IDs.
func Covers(t *testing.T, ids ...string) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for _, id := range ids {
		covered[id] = append(covered[id], t.Name())
	}
	t.Logf("covers %v", ids)
}

// Coverage returns a copy of the requirement->tests map recorded so far.
func Coverage() map[string][]string {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string][]string, len(covered))
	for k, v := range covered {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
