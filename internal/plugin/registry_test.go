package plugin_test

import (
	"testing"

	"github.com/lzzen/enhanced-relay/internal/hook"
	"github.com/lzzen/enhanced-relay/internal/plugin"
	"github.com/lzzen/enhanced-relay/internal/plugin/builtin"
	"github.com/lzzen/enhanced-relay/internal/testutil/req"
)

func TestRegistry_BuildUnknown_Errors(t *testing.T) {
	req.Covers(t, "REQ-EXT-PLUGIN-REGISTRY-001")
	r := plugin.NewRegistry()
	if _, err := r.Build([]string{"missing"}); err == nil {
		t.Fatal("expected error for unknown plugin")
	}
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	req.Covers(t, "REQ-EXT-PLUGIN-REGISTRY-002")
	r := plugin.NewRegistry()
	r.Register("dup", builtin.NewStamp)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r.Register("dup", builtin.NewStamp)
}

func TestRegistry_BuildAndHookRegistrations(t *testing.T) {
	req.Covers(t, "REQ-EXT-PLUGIN-REGISTRY-003", "REQ-EXT-AUDIT-VERSIONS-001")
	r := plugin.NewRegistry()
	r.Register(builtin.StampName, builtin.NewStamp)

	plugins, err := r.Build([]string{builtin.StampName})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	regs := plugin.HookRegistrations(plugins)
	if len(regs) != 1 {
		t.Fatalf("expected 1 hook registration, got %d", len(regs))
	}
	if regs[0].Timeout <= 0 || regs[0].FailurePolicy != hook.FailOpen {
		t.Fatalf("manifest policy not propagated: %+v", regs[0])
	}
}

func TestManifest_Validate(t *testing.T) {
	req.Covers(t, "REQ-EXT-PLUGIN-MANIFEST-001")
	bad := plugin.Manifest{Name: "x"} // missing version + kind
	if err := bad.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
	good := builtin.NewStamp().Manifest()
	if err := good.Validate(); err != nil {
		t.Fatalf("stamp manifest should be valid: %v", err)
	}
	if !good.HasCapability(plugin.CapReadRequestMeta) {
		t.Fatal("stamp should declare read_request_meta capability")
	}
}
