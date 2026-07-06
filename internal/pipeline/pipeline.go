// Package pipeline orchestrates the request lifecycle stages, dispatching hooks
// at each stage and honoring their decisions. The HTTP transport, parsing,
// routing and upstream calls are filled in by later phases; this is the stable
// spine that sequences stages and enforces short-circuit/reject semantics.
package pipeline

import (
	"context"

	"github.com/lzzen/enhanced-relay/internal/event"
	"github.com/lzzen/enhanced-relay/internal/hook"
	"github.com/lzzen/enhanced-relay/internal/reqctx"
)

// Outcome summarizes how a request ended.
type Outcome struct {
	Decision   hook.Decision
	Reason     string
	StatusCode int
}

// Pipeline holds the immutable dispatcher and the event bus.
type Pipeline struct {
	dispatcher *hook.Dispatcher
	bus        *event.Bus
}

// New builds a Pipeline from an (immutable) dispatcher and event bus.
func New(d *hook.Dispatcher, bus *event.Bus) *Pipeline {
	return &Pipeline{dispatcher: d, bus: bus}
}

// requestStages is the ordered spine excluding per-attempt and terminal stages,
// which are driven separately once routing/upstream exist.
var requestStages = []hook.Stage{
	hook.StageRequestStart,
	hook.StageAfterParse,
	hook.StageBeforeRoute,
	hook.StageBeforeUpstream,
	hook.StageAfterUpstream,
	hook.StageBeforeResponse,
}

// Execute runs the request through the stage spine. Any Reject or ShortCircuit
// stops progression; on_request_end always runs (best-effort) afterwards.
func (p *Pipeline) Execute(ctx context.Context, rc *reqctx.RequestContext) Outcome {
	outcome := Outcome{Decision: hook.Continue}

	for _, stage := range requestStages {
		res := p.dispatcher.Run(ctx, stage, rc)
		if res.Decision == hook.Reject || res.Decision == hook.ShortCircuit {
			outcome = Outcome{Decision: res.Decision, Reason: res.Reason, StatusCode: res.StatusCode}
			break
		}
	}

	// Terminal stage always runs so cleanup/audit hooks fire regardless.
	p.dispatcher.Run(ctx, hook.StageRequestEnd, rc)

	if p.bus != nil {
		p.bus.Publish(event.Event{
			Type:    "RequestCompleted",
			Time:    rc.Clock.Now(),
			Payload: map[string]any{"trace_id": rc.TraceID, "decision": outcome.Decision.String()},
		})
	}
	return outcome
}
