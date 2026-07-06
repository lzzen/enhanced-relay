// Package reqctx defines RequestContext, the single object that flows through
// the pipeline. Hooks read and mutate it; events carry redacted snapshots of it.
package reqctx

import (
	"sync"
	"time"

	"github.com/lzzen/enhanced-relay/internal/clock"
	"github.com/lzzen/enhanced-relay/internal/idgen"
)

// PluginRef records a plugin (and its version) that was active for a request,
// so decisions can be frozen and replayed for audit/billing recomputation.
type PluginRef struct {
	Name    string
	Version string
}

// RequestContext is the unified per-request state. In later phases this gains
// parsed body handles, routing decisions and billing snapshots; the shape here
// is the stable spine the extension framework binds to.
type RequestContext struct {
	TraceID   string
	StartedAt time.Time

	Method        string
	Path          string
	Model         string // original client-requested model
	UpstreamModel string // resolved upstream model

	Clock clock.Clock
	IDs   idgen.Generator

	Timings *Timings

	mu            sync.RWMutex
	attrs         map[string]any
	activePlugins []PluginRef
}

// New creates a RequestContext with an injected clock and ID generator and a
// freshly generated trace ID.
func New(clk clock.Clock, ids idgen.Generator) *RequestContext {
	return &RequestContext{
		TraceID:   ids.NewID(),
		StartedAt: clk.Now(),
		Clock:     clk,
		IDs:       ids,
		Timings:   NewTimings(clk),
		attrs:     make(map[string]any),
	}
}

// SetAttr stores a plugin-scoped attribute (real impl gates this by capability).
func (rc *RequestContext) SetAttr(key string, val any) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.attrs[key] = val
}

// Attr reads a previously stored attribute.
func (rc *RequestContext) Attr(key string) (any, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	v, ok := rc.attrs[key]
	return v, ok
}

// RecordPlugin marks a plugin as active for this request.
func (rc *RequestContext) RecordPlugin(ref PluginRef) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.activePlugins = append(rc.activePlugins, ref)
}

// ActivePlugins returns a copy of the plugins that acted on this request.
func (rc *RequestContext) ActivePlugins() []PluginRef {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	out := make([]PluginRef, len(rc.activePlugins))
	copy(out, rc.activePlugins)
	return out
}
