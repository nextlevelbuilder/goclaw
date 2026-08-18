package gc

import "sync"

// Registry maps /gc: command kinds to skill slugs within the go-claw-engineer
// kit. The mapping is what makes /gc:plan resolve to the plan skill's SKILL.md.
// A registry with no entries resolves nothing, so an unconfigured registry
// behaves as passthrough for all commands.
type Registry struct {
	mu     sync.RWMutex
	byKind map[CommandKind]string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byKind: make(map[CommandKind]string)}
}

// Register maps kind to its skill slug. Registering a kind that already has a
// slug replaces it.
func (r *Registry) Register(kind CommandKind, slug string) {
	if kind == "" || slug == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKind[kind] = slug
}

// Lookup returns the skill slug for kind, and whether one is registered.
func (r *Registry) Lookup(kind CommandKind) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	slug, ok := r.byKind[kind]
	return slug, ok
}

// KnownKinds returns the command kinds that currently have a registered slug.
// Order is deterministic (plan, fix, cook, review, test, debug, docs, architect, uiux)
// for stable listings.
func (r *Registry) KnownKinds() []CommandKind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []CommandKind
	for _, k := range knownKinds {
		if _, ok := r.byKind[k]; ok {
			out = append(out, k)
		}
	}
	return out
}