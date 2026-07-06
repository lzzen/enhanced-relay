// Package builtin holds first-party plugins shipped in the binary. This stamp
// plugin is a minimal reference implementation proving the hook + plugin wiring
// end to end; real plugins (rules, media, oss, billing) follow the same shape.
package builtin

import (
	"context"
	"time"

	"github.com/lzzen/enhanced-relay/internal/hook"
	"github.com/lzzen/enhanced-relay/internal/plugin"
	"github.com/lzzen/enhanced-relay/internal/reqctx"
)

// StampName is the registry name of the stamp plugin.
const StampName = "stamp"

// Stamp is a trivial hook plugin that records a request-start attribute. It
// declares only the read_request_meta capability.
type Stamp struct{}

// NewStamp is the plugin.Factory for the stamp plugin.
func NewStamp() plugin.Plugin { return &Stamp{} }

func (s *Stamp) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:          StampName,
		Version:       "0.1.0",
		Kind:          plugin.KindHook,
		Capabilities:  []plugin.Capability{plugin.CapReadRequestMeta},
		FailurePolicy: hook.FailOpen,
		Priority:      0,
		Timeout:       50 * time.Millisecond,
	}
}

func (s *Stamp) Init(plugin.InitContext) error  { return nil }
func (s *Stamp) Shutdown(context.Context) error { return nil }
func (s *Stamp) Name() string                   { return StampName }
func (s *Stamp) Version() string                { return "0.1.0" }
func (s *Stamp) Stages() []hook.Stage           { return []hook.Stage{hook.StageRequestStart} }

func (s *Stamp) Handle(_ context.Context, rc *reqctx.RequestContext) (hook.Result, error) {
	rc.SetAttr("stamped", true)
	return hook.Result{Decision: hook.Continue}, nil
}
