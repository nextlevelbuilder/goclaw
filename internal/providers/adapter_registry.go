package providers

import (
	"fmt"
	"sync"
)

// AdapterRegistry maps provider names to adapter factories.
// Enables plugin-style provider registration.
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[string]AdapterFactory
}

// NewAdapterRegistry creates an empty registry.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: make(map[string]AdapterFactory)}
}

// Register adds a provider adapter factory.
func (r *AdapterRegistry) Register(name string, factory AdapterFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = factory
}

// Get returns adapter for provider name. Error if not registered.
func (r *AdapterRegistry) Get(name string, cfg ProviderConfig) (ProviderAdapter, error) {
	r.mu.RLock()
	factory, ok := r.adapters[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown provider adapter: %s", name)
	}
	return factory(cfg)
}
