package builtin_test

import (
	"context"
	"testing"
	"time"

	"github.com/lzzen/enhanced-relay/internal/clock"
	"github.com/lzzen/enhanced-relay/internal/hook"
	"github.com/lzzen/enhanced-relay/internal/idgen"
	"github.com/lzzen/enhanced-relay/internal/plugin"
	"github.com/lzzen/enhanced-relay/internal/plugin/builtin"
	"github.com/lzzen/enhanced-relay/internal/reqctx"
	"github.com/lzzen/enhanced-relay/internal/testutil/req"
)

func TestStamp_Handle_SetsAttr(t *testing.T) {
	req.Covers(t, "REQ-EXT-PLUGIN-BUILTIN-001")
	s := &builtin.Stamp{}
	rc := reqctx.New(clock.NewFake(time.Unix(0, 0)), idgen.NewSequence("t"))

	res, err := s.Handle(context.Background(), rc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != hook.Continue {
		t.Fatalf("expected Continue, got %v", res.Decision)
	}
	if v, ok := rc.Attr("stamped"); !ok || v != true {
		t.Fatal("stamp hook should set the stamped attribute")
	}
}

func TestStamp_Manifest_Fields(t *testing.T) {
	req.Covers(t, "REQ-EXT-PLUGIN-BUILTIN-002")
	s := &builtin.Stamp{}
	m := s.Manifest()
	if m.Timeout != 50*time.Millisecond {
		t.Fatalf("timeout want 50ms, got %v", m.Timeout)
	}
	if m.Kind != plugin.KindHook {
		t.Fatalf("kind want hook, got %v", m.Kind)
	}
	if m.FailurePolicy != hook.FailOpen {
		t.Fatalf("failure policy want fail-open, got %v", m.FailurePolicy)
	}
	if s.Name() != builtin.StampName || s.Version() != m.Version {
		t.Fatalf("name/version mismatch: %s %s vs manifest %s", s.Name(), s.Version(), m.Version)
	}
	if len(s.Stages()) != 1 || s.Stages()[0] != hook.StageRequestStart {
		t.Fatalf("stamp should run at request start, got %v", s.Stages())
	}
}
