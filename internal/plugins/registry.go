// Package plugins — Plugin registry for managing plugin lifecycle (CP-08).
package plugins

import (
	"fmt"
	"sync"
	"time"
)

// PluginState represents the lifecycle state of a plugin.
type PluginState string

const (
	PluginInstalled PluginState = "installed"
	PluginEnabled   PluginState = "enabled"
	PluginDisabled  PluginState = "disabled"
	PluginError     PluginState = "error"
)

// Plugin represents a loaded plugin with its manifest and state.
type Plugin struct {
	Manifest PluginManifest
	State    PluginState
	Path     string // filesystem path to plugin directory
	Source   string // "local", "marketplace", "git"
	LoadedAt time.Time
	Error    string // error message if State == PluginError
}

// Registry manages the set of installed plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*Plugin // name → plugin
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]*Plugin),
	}
}

// Register adds a plugin to the registry.
func (r *Registry) Register(p *Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[p.Manifest.Name]; exists {
		return fmt.Errorf("plugin %q already registered", p.Manifest.Name)
	}
	r.plugins[p.Manifest.Name] = p
	return nil
}

// Upsert adds or replaces a plugin in the registry.
func (r *Registry) Upsert(p *Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Manifest.Name] = p
}

// Enable sets a plugin's state to enabled.
func (r *Registry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.State = PluginEnabled
	return nil
}

// Disable sets a plugin's state to disabled.
func (r *Registry) Disable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.State = PluginDisabled
	return nil
}

// Get returns a plugin by name (nil if not found).
func (r *Registry) Get(name string) *Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[name]
}

// List returns all registered plugins.
func (r *Registry) List() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		result = append(result, p)
	}
	return result
}

// EnabledPlugins returns only plugins in enabled state.
func (r *Registry) EnabledPlugins() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*Plugin
	for _, p := range r.plugins {
		if p.State == PluginEnabled {
			result = append(result, p)
		}
	}
	return result
}

// Commands returns all commands from enabled plugins.
func (r *Registry) Commands() []PluginCommand {
	var cmds []PluginCommand
	for _, p := range r.EnabledPlugins() {
		cmds = append(cmds, p.Manifest.Commands...)
	}
	return cmds
}

// Agents returns all agents from enabled plugins.
func (r *Registry) Agents() []PluginAgent {
	var agents []PluginAgent
	for _, p := range r.EnabledPlugins() {
		agents = append(agents, p.Manifest.Agents...)
	}
	return agents
}

// HooksFor returns all hooks matching a specific event from enabled plugins.
func (r *Registry) HooksFor(event string) []Hook {
	var hooks []Hook
	for _, p := range r.EnabledPlugins() {
		if eventHooks, ok := p.Manifest.Hooks[event]; ok {
			hooks = append(hooks, eventHooks...)
		}
	}
	return hooks
}

// Remove unregisters a plugin.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.plugins, name)
}

// Count returns the number of registered plugins.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}
