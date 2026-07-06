package hook

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/lzzen/enhanced-relay/internal/reqctx"
)

// ErrHookPanic is returned (to the observer) when a hook panics.
var ErrHookPanic = errors.New("hook: panic recovered")

// Observation is emitted after each hook runs, for tracing/metrics.
type Observation struct {
	Stage    Stage
	Hook     string
	Version  string
	Decision Decision
	Err      error
	// Elapsed is measured on rc.Clock so it is deterministic under test.
	ElapsedNanos int64
}

// Observer receives one Observation per hook execution. It must not block.
type Observer func(Observation)

// Dispatcher holds hooks grouped and pre-sorted by stage. It is immutable after
// Build; the pipeline reads from an immutable config snapshot.
type Dispatcher struct {
	byStage  map[Stage][]Registration
	observer Observer
}

// NewDispatcher builds a Dispatcher from registrations. Hooks are grouped by
// stage and sorted by priority once, so dispatch does no sorting on the hot path.
func NewDispatcher(regs []Registration, observer Observer) *Dispatcher {
	byStage := make(map[Stage][]Registration)
	for _, r := range regs {
		for _, st := range r.Hook.Stages() {
			byStage[st] = append(byStage[st], r)
		}
	}
	for st := range byStage {
		regs := byStage[st]
		sort.SliceStable(regs, func(i, j int) bool { return regs[i].Priority < regs[j].Priority })
		byStage[st] = regs
	}
	if observer == nil {
		observer = func(Observation) {}
	}
	return &Dispatcher{byStage: byStage, observer: observer}
}

// Run executes all hooks registered for stage in priority order.
//
// Fast path: if no hooks are registered for the stage, this returns immediately
// with a single map lookup and length check — no allocation, no goroutine.
func (d *Dispatcher) Run(ctx context.Context, stage Stage, rc *reqctx.RequestContext) Result {
	regs := d.byStage[stage]
	if len(regs) == 0 {
		return Result{Decision: Continue}
	}
	for _, r := range regs {
		res := d.runOne(ctx, stage, r, rc)
		rc.RecordPlugin(reqctx.PluginRef{Name: r.Hook.Name(), Version: r.Hook.Version()})
		switch res.Decision {
		case Reject, ShortCircuit:
			return res
		default:
			// Continue / Modified -> next hook
		}
	}
	return Result{Decision: Continue}
}

func (d *Dispatcher) runOne(ctx context.Context, stage Stage, r Registration, rc *reqctx.RequestContext) (res Result) {
	start := rc.Clock.Now()

	hctx := ctx
	var cancel context.CancelFunc
	if r.Timeout > 0 {
		hctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("%w: %v", ErrHookPanic, rec)
				res = Result{Decision: Continue}
			}
		}()
		res, err = r.Hook.Handle(hctx, rc)
	}()

	// A deadline exceeded during the hook counts as a hook failure.
	if err == nil && hctx.Err() != nil {
		err = hctx.Err()
	}

	if err != nil {
		res = applyFailurePolicy(r, err)
	}

	d.observer(Observation{
		Stage:        stage,
		Hook:         r.Hook.Name(),
		Version:      r.Hook.Version(),
		Decision:     res.Decision,
		Err:          err,
		ElapsedNanos: rc.Clock.Since(start).Nanoseconds(),
	})
	return res
}

func applyFailurePolicy(r Registration, err error) Result {
	if r.FailurePolicy == FailClosed {
		return Result{
			Decision:   Reject,
			Reason:     fmt.Sprintf("hook %q failed (fail-closed): %v", r.Hook.Name(), err),
			StatusCode: 502,
		}
	}
	// FailOpen: skip this hook, continue the pipeline.
	return Result{Decision: Continue}
}
