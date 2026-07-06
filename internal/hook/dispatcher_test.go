package hook_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzzen/enhanced-relay/internal/clock"
	"github.com/lzzen/enhanced-relay/internal/hook"
	"github.com/lzzen/enhanced-relay/internal/idgen"
	"github.com/lzzen/enhanced-relay/internal/reqctx"
	"github.com/lzzen/enhanced-relay/internal/testutil/req"
)

// funcHook adapts a func into a Hook for tests.
type funcHook struct {
	name   string
	stages []hook.Stage
	fn     func(ctx context.Context, rc *reqctx.RequestContext) (hook.Result, error)
}

func (h funcHook) Name() string         { return h.name }
func (h funcHook) Version() string      { return "test" }
func (h funcHook) Stages() []hook.Stage { return h.stages }
func (h funcHook) Handle(ctx context.Context, rc *reqctx.RequestContext) (hook.Result, error) {
	return h.fn(ctx, rc)
}

func newRC() *reqctx.RequestContext {
	return reqctx.New(clock.NewFake(time.Unix(0, 0)), idgen.NewSequence("trace"))
}

func TestDispatcher_FastPath_NoHooks_ZeroAlloc(t *testing.T) {
	req.Covers(t, "REQ-EXT-FASTPATH-001")
	d := hook.NewDispatcher(nil, nil)
	rc := newRC()
	ctx := context.Background()

	allocs := testing.AllocsPerRun(1000, func() {
		if res := d.Run(ctx, hook.StageBeforeUpstream, rc); res.Decision != hook.Continue {
			t.Fatalf("expected Continue, got %v", res.Decision)
		}
	})
	if allocs != 0 {
		t.Fatalf("fast path must not allocate, got %v allocs/op", allocs)
	}
}

func TestDispatcher_Ordering_ByPriority(t *testing.T) {
	req.Covers(t, "REQ-EXT-HOOK-ORDER-001")
	var order []string
	mk := func(name string) hook.Hook {
		return funcHook{name: name, stages: []hook.Stage{hook.StageAfterParse}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			order = append(order, name)
			return hook.Result{Decision: hook.Continue}, nil
		}}
	}
	d := hook.NewDispatcher([]hook.Registration{
		{Hook: mk("b"), Priority: 10},
		{Hook: mk("a"), Priority: 1},
		{Hook: mk("c"), Priority: 100},
	}, nil)

	d.Run(context.Background(), hook.StageAfterParse, newRC())
	if got := order; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("expected [a b c], got %v", got)
	}
}

func TestDispatcher_Reject_StopsChain(t *testing.T) {
	req.Covers(t, "REQ-EXT-HOOK-REJECT-001")
	ran := false
	d := hook.NewDispatcher([]hook.Registration{
		{Hook: funcHook{name: "deny", stages: []hook.Stage{hook.StageBeforeRoute}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			return hook.Result{Decision: hook.Reject, StatusCode: 403, Reason: "nope"}, nil
		}}, Priority: 1},
		{Hook: funcHook{name: "after", stages: []hook.Stage{hook.StageBeforeRoute}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			ran = true
			return hook.Result{Decision: hook.Continue}, nil
		}}, Priority: 2},
	}, nil)

	res := d.Run(context.Background(), hook.StageBeforeRoute, newRC())
	if res.Decision != hook.Reject || res.StatusCode != 403 {
		t.Fatalf("expected reject 403, got %+v", res)
	}
	if ran {
		t.Fatal("subsequent hook must not run after reject")
	}
}

func TestDispatcher_Panic_Isolated_FailOpen(t *testing.T) {
	req.Covers(t, "REQ-EXT-HOOK-ISOLATION-001")
	var obs hook.Observation
	d := hook.NewDispatcher([]hook.Registration{
		{Hook: funcHook{name: "boom", stages: []hook.Stage{hook.StageAfterParse}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			panic("kaboom")
		}}, FailurePolicy: hook.FailOpen},
	}, func(o hook.Observation) { obs = o })

	res := d.Run(context.Background(), hook.StageAfterParse, newRC())
	if res.Decision != hook.Continue {
		t.Fatalf("fail-open panic should continue, got %v", res.Decision)
	}
	if obs.Err == nil || !errors.Is(obs.Err, hook.ErrHookPanic) {
		t.Fatalf("expected recovered panic error, got %v", obs.Err)
	}
}

func TestDispatcher_Panic_FailClosed_Rejects(t *testing.T) {
	req.Covers(t, "REQ-EXT-HOOK-ISOLATION-002")
	d := hook.NewDispatcher([]hook.Registration{
		{Hook: funcHook{name: "boom", stages: []hook.Stage{hook.StageAfterParse}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			panic("kaboom")
		}}, FailurePolicy: hook.FailClosed},
	}, nil)

	res := d.Run(context.Background(), hook.StageAfterParse, newRC())
	if res.Decision != hook.Reject {
		t.Fatalf("fail-closed panic should reject, got %v", res.Decision)
	}
}

func TestDispatcher_Timeout_FailClosed_Rejects(t *testing.T) {
	req.Covers(t, "REQ-EXT-HOOK-TIMEOUT-001")
	d := hook.NewDispatcher([]hook.Registration{
		{Hook: funcHook{name: "slow", stages: []hook.Stage{hook.StageBeforeUpstream}, fn: func(ctx context.Context, _ *reqctx.RequestContext) (hook.Result, error) {
			<-ctx.Done()
			return hook.Result{Decision: hook.Continue}, nil
		}}, Timeout: 20 * time.Millisecond, FailurePolicy: hook.FailClosed},
	}, nil)

	res := d.Run(context.Background(), hook.StageBeforeUpstream, newRC())
	if res.Decision != hook.Reject {
		t.Fatalf("timed-out fail-closed hook should reject, got %v", res.Decision)
	}
}

func TestDispatcher_RecordsActivePluginVersions(t *testing.T) {
	req.Covers(t, "REQ-EXT-AUDIT-VERSIONS-001")
	rc := newRC()
	d := hook.NewDispatcher([]hook.Registration{
		{Hook: funcHook{name: "v", stages: []hook.Stage{hook.StageAfterParse}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			return hook.Result{Decision: hook.Continue}, nil
		}}},
	}, nil)
	d.Run(context.Background(), hook.StageAfterParse, rc)
	if got := rc.ActivePlugins(); len(got) != 1 || got[0].Name != "v" {
		t.Fatalf("expected active plugin recorded, got %v", got)
	}
}
