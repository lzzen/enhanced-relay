// Package req provides the requirement-to-test traceability primitive used by
// the AI acceptance harness (docs/ai-testing-acceptance.md §2). Tests call
// Covers to bind themselves to requirement IDs. When REQ_COVERAGE_DIR is set
// (by cmd/acceptance), each test process appends its bindings to a per-process
// JSONL file so the acceptance runner can aggregate them across packages and
// gate on requirement coverage.
package req

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// CoverageDirEnv is the env var pointing at the directory where per-process
// coverage files are written.
const CoverageDirEnv = "REQ_COVERAGE_DIR"

var (
	mu       sync.Mutex
	covered  = map[string][]string{} // REQ-ID -> test names (in-process)
	sink     *os.File
	sinkInit bool
)

// Binding is one requirement<->test link, as persisted to the coverage file.
type Binding struct {
	Req  string `json:"req"`
	Test string `json:"test"`
}

// Covers records that the current test exercises the given requirement IDs.
func Covers(t *testing.T, ids ...string) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for _, id := range ids {
		covered[id] = append(covered[id], t.Name())
	}
	persist(t.Name(), ids)
	t.Logf("covers %v", ids)
}

// persist appends bindings to the per-process coverage file. Caller holds mu.
func persist(test string, ids []string) {
	dir := os.Getenv(CoverageDirEnv)
	if dir == "" {
		return
	}
	if !sinkInit {
		sinkInit = true
		name := filepath.Join(dir, fmt.Sprintf("cov-%d.jsonl", os.Getpid()))
		f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			sink = f
		}
	}
	if sink == nil {
		return
	}
	enc := json.NewEncoder(sink)
	for _, id := range ids {
		_ = enc.Encode(Binding{Req: id, Test: test})
	}
}

// Coverage returns a copy of the requirement->tests map recorded in-process.
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
