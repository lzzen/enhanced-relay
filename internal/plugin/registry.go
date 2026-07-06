package plugin

import (
	"fmt"
	"sort"
	"sync"

	"github.com/lzzen/enhanced-relay/internal/hook"
)

// Factory constructs a plugin instance. Plugins register a Factory from init().
type Factory func() Plugin

// Registry is the compile-time plugin catalog. Registration happens via init();
// Build() materializes only the plugins enabled by the config snapshot.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a factory under name. It panics on duplicate names because that
// is a programming error discoverable at startup.
func (r *Registry) Register(name string, f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		panic(fmt.Sprintf("plugin: duplicate registration for %q", name))
	}
	r.factories[name] = f
}

// Names returns the registered plugin names, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for name := range r.factories {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Build instantiates the named plugins, validates their manifests and returns
// them. Unknown names are an error.
func (r *Registry) Build(names []string) ([]Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Plugin, 0, len(names))
	for _, name := range names {
		f, ok := r.factories[name]
		if !ok {
			return nil, fmt.Errorf("plugin: %q not registered", name)
		}
		p := f()
		if err := p.Manifest().Validate(); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// HookRegistrations extracts hook.Registration entries from hook-kind plugins,
// wiring each plugin's manifest policy (priority, timeout, failure policy) into
// the dispatcher. Non-hook plugins are ignored here.
func HookRegistrations(plugins []Plugin) []hook.Registration {
	var regs []hook.Registration
	for _, p := range plugins {
		hp, ok := p.(HookPlugin)
		if !ok {
			continue
		}
		m := p.Manifest()
		regs = append(regs, hook.Registration{
			Hook:          hp,
			Priority:      m.Priority,
			Timeout:       m.Timeout,
			FailurePolicy: m.FailurePolicy,
		})
	}
	return regs
}
