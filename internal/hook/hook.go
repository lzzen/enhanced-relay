// Package hook implements the synchronous, in-path extension points of the
// pipeline. See docs/plugin-architecture.md §2. Design guarantees:
//   - zero overhead when a stage has no registered hooks (fast path);
//   - per-hook timeout deducted from the request budget;
//   - panic isolation so one hook never crashes the process;
//   - fail-open vs fail-closed policy per hook.
package hook

import (
	"context"
	"fmt"
	"time"

	"github.com/lzzen/enhanced-relay/internal/reqctx"
)

// Stage identifies a point in the request lifecycle where hooks may run.
type Stage int

const (
	StageRequestStart Stage = iota
	StageAfterParse
	StageBeforeRoute
	StageBeforeUpstream // per upstream attempt
	StageAfterUpstream  // per upstream attempt
	StageBeforeResponse
	StageRequestEnd
	StageError
)

func (s Stage) String() string {
	switch s {
	case StageRequestStart:
		return "on_request_start"
	case StageAfterParse:
		return "after_parse"
	case StageBeforeRoute:
		return "before_route"
	case StageBeforeUpstream:
		return "before_upstream"
	case StageAfterUpstream:
		return "after_upstream"
	case StageBeforeResponse:
		return "before_response"
	case StageRequestEnd:
		return "on_request_end"
	case StageError:
		return "on_error"
	default:
		return fmt.Sprintf("stage(%d)", int(s))
	}
}

// Decision is what a hook tells the pipeline to do next.
type Decision int

const (
	Continue     Decision = iota // proceed unchanged
	Modified                     // context mutated, proceed
	Reject                       // reject the request with StatusCode/Reason
	ShortCircuit                 // stop and return a hook-produced response
)

func (d Decision) String() string {
	switch d {
	case Continue:
		return "continue"
	case Modified:
		return "modified"
	case Reject:
		return "reject"
	case ShortCircuit:
		return "short_circuit"
	default:
		return fmt.Sprintf("decision(%d)", int(d))
	}
}

// Result is a hook's outcome.
type Result struct {
	Decision   Decision
	Reason     string
	StatusCode int // meaningful for Reject
}

// FailurePolicy controls what happens when a hook errors, times out or panics.
type FailurePolicy int

const (
	// FailClosed rejects the request on hook failure. Use for auth/billing.
	FailClosed FailurePolicy = iota
	// FailOpen skips the failed hook and continues. Use for observability.
	FailOpen
)

// Hook is a single in-path extension unit.
type Hook interface {
	Name() string
	Version() string
	Stages() []Stage
	// Handle must honor ctx cancellation/deadline and use the injected
	// clock/HTTP client on rc; direct time/net calls are forbidden.
	Handle(ctx context.Context, rc *reqctx.RequestContext) (Result, error)
}

// Registration binds a Hook with its execution policy.
type Registration struct {
	Hook          Hook
	Priority      int // lower runs first
	Timeout       time.Duration
	FailurePolicy FailurePolicy
}
