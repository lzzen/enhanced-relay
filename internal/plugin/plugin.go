// Package plugin defines the unified extension model. All core enhancements
// (rules, media, oss, routing, billing, upstream adapters) are plugins. The
// first-class loader is a compile-time registry (docs/plugin-architecture.md
// §4): zero hot-path overhead and compatible with a static single binary.
package plugin

import (
	"context"
	"fmt"
	"time"

	"github.com/lzzen/enhanced-relay/internal/hook"
)

// Kind categorizes what a plugin extends.
type Kind string

const (
	KindHook            Kind = "hook"
	KindEventSubscriber Kind = "event_subscriber"
	KindUpstreamAdapter Kind = "upstream_adapter"
	KindMedia           Kind = "media"
	KindStorage         Kind = "storage"
	KindBilling         Kind = "billing"
)

// Capability is a declarative permission. Plugins start with zero capabilities
// and may only perform actions they declared and were granted.
type Capability string

const (
	CapReadRequestMeta Capability = "read_request_meta"
	CapMutateRequest   Capability = "mutate_request"
	CapReadBody        Capability = "read_body"
	CapMutateBody      Capability = "mutate_body"
	CapOutboundHTTP    Capability = "outbound_http"
	CapReadSecret      Capability = "read_secret"
	CapEmitEvent       Capability = "emit_event"
	CapPersist         Capability = "persist"
)

// Manifest is a plugin's self-declaration. It is validated at publish time and
// frozen into the immutable config snapshot.
type Manifest struct {
	Name          string
	Version       string
	Kind          Kind
	Capabilities  []Capability
	FailurePolicy hook.FailurePolicy
	// Priority and Timeout apply to hook-kind plugins.
	Priority int
	Timeout  time.Duration
}

// Validate checks the manifest for the minimum required fields.
func (m Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("plugin: manifest missing name")
	}
	if m.Version == "" {
		return fmt.Errorf("plugin %q: manifest missing version", m.Name)
	}
	if m.Kind == "" {
		return fmt.Errorf("plugin %q: manifest missing kind", m.Name)
	}
	return nil
}

// HasCapability reports whether the manifest declared the given capability.
func (m Manifest) HasCapability(c Capability) bool {
	for _, got := range m.Capabilities {
		if got == c {
			return true
		}
	}
	return false
}

// Plugin is the base lifecycle contract every plugin implements.
type Plugin interface {
	Manifest() Manifest
	Init(InitContext) error
	Shutdown(context.Context) error
}

// HookPlugin is a plugin that contributes an in-path hook.
type HookPlugin interface {
	Plugin
	hook.Hook
}

// InitContext carries dependencies granted to a plugin at startup. Real builds
// pass capability-gated accessors (SSRF-safe HTTP client, secret broker, event
// emitter) here — only for capabilities the plugin declared.
type InitContext struct {
	Config map[string]any
}
