package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/lzzen/enhanced-relay/internal/clock"
	"github.com/lzzen/enhanced-relay/internal/event"
	"github.com/lzzen/enhanced-relay/internal/hook"
	"github.com/lzzen/enhanced-relay/internal/idgen"
	"github.com/lzzen/enhanced-relay/internal/pipeline"
	"github.com/lzzen/enhanced-relay/internal/plugin"
	"github.com/lzzen/enhanced-relay/internal/plugin/builtin"
	"github.com/lzzen/enhanced-relay/internal/reqctx"
	"github.com/lzzen/enhanced-relay/internal/testutil/req"
)

type funcHook struct {
	name   string
	stages []hook.Stage
	fn     func(context.Context, *reqctx.RequestContext) (hook.Result, error)
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

func TestPipeline_RunsStagesInOrder_AndBuiltinStampFires(t *testing.T) {
	req.Covers(t, "REQ-EXT-PIPELINE-001")
	reg := plugin.NewRegistry()
	reg.Register(builtin.StampName, builtin.NewStamp)
	plugins, err := reg.Build([]string{builtin.StampName})
	if err != nil {
		t.Fatal(err)
	}
	d := hook.NewDispatcher(plugin.HookRegistrations(plugins), nil)
	bus := event.New(8)
	bus.Start()
	p := pipeline.New(d, bus)

	rc := newRC()
	out := p.Execute(context.Background(), rc)
	if out.Decision != hook.Continue {
		t.Fatalf("expected Continue, got %v", out.Decision)
	}
	if v, ok := rc.Attr("stamped"); !ok || v != true {
		t.Fatal("builtin stamp hook should have run at request start")
	}
	bus.Drain(context.Background())
}

func TestPipeline_RejectShortCircuitsRemainingStages(t *testing.T) {
	req.Covers(t, "REQ-EXT-PIPELINE-002")
	reached := false
	d := hook.NewDispatcher([]hook.Registration{
		{Hook: funcHook{name: "deny", stages: []hook.Stage{hook.StageBeforeRoute}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			return hook.Result{Decision: hook.Reject, StatusCode: 403}, nil
		}}},
		{Hook: funcHook{name: "later", stages: []hook.Stage{hook.StageBeforeUpstream}, fn: func(context.Context, *reqctx.RequestContext) (hook.Result, error) {
			reached = true
			return hook.Result{Decision: hook.Continue}, nil
		}}},
	}, nil)
	p := pipeline.New(d, nil)

	out := p.Execute(context.Background(), newRC())
	if out.Decision != hook.Reject || out.StatusCode != 403 {
		t.Fatalf("expected reject 403, got %+v", out)
	}
	if reached {
		t.Fatal("stages after reject must not run")
	}
}
