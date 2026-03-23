package providers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// MasterTenantID is the default tenant for config-based providers.
var MasterTenantID = uuid.Must(uuid.Parse("0193a5b0-7000-7000-8000-000000000001"))

// Registry manages available LLM providers with per-tenant isolation.
// Providers are keyed by "tenantID/name". Get falls back to master tenant.
type Registry struct {
	providers     map[string]Provider
	mu            sync.RWMutex
	tenantFromCtx func(context.Context) uuid.UUID // injected to avoid circular import with store
}

// NewRegistry creates a provider registry.
// tenantFromCtx extracts tenant UUID from context (pass store.TenantIDFromContext).
func NewRegistry(tenantFromCtx func(context.Context) uuid.UUID) *Registry {
	return &Registry{
		providers:     make(map[string]Provider),
		tenantFromCtx: tenantFromCtx,
	}
}

// compoundKey returns "tenantID/name" for registry lookup.
func compoundKey(tenantID uuid.UUID, name string) string {
	return tenantID.String() + "/" + name
}

// tenantFromContext resolves the tenant UUID from context, defaulting to MasterTenantID.
func (r *Registry) tenantFromContext(ctx context.Context) uuid.UUID {
	if r.tenantFromCtx != nil {
		if t := r.tenantFromCtx(ctx); t != uuid.Nil {
			return t
		}
	}
	return MasterTenantID
}

// Register adds a provider to the registry under the master tenant.
func (r *Registry) Register(provider Provider) {
	r.RegisterForTenant(MasterTenantID, provider)
}

// RegisterForTenant adds a provider under a specific tenant.
// If a provider with the same tenant+name already exists, it is closed before replacement.
func (r *Registry) RegisterForTenant(tenantID uuid.UUID, provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := compoundKey(tenantID, provider.Name())
	if old, ok := r.providers[key]; ok {
		if c, ok := old.(io.Closer); ok {
			c.Close()
		}
	}
	r.providers[key] = provider
}

// Unregister removes a provider from the master tenant.
func (r *Registry) Unregister(name string) {
	r.UnregisterForTenant(MasterTenantID, name)
}

// UnregisterForTenant removes a provider from a specific tenant.
func (r *Registry) UnregisterForTenant(tenantID uuid.UUID, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := compoundKey(tenantID, name)
	if old, ok := r.providers[key]; ok {
		if c, ok := old.(io.Closer); ok {
			c.Close()
		}
		delete(r.providers, key)
	}
}

// Get returns a provider by name, using tenant from context (falls back to master).
func (r *Registry) Get(ctx context.Context, name string) (Provider, error) {
	return r.GetForTenant(r.tenantFromContext(ctx), name)
}

// GetForTenant returns a provider by name for a specific tenant.
// Falls back to master tenant if not found for the given tenant.
func (r *Registry) GetForTenant(tenantID uuid.UUID, name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try tenant-specific first
	if tenantID != MasterTenantID {
		if p, ok := r.providers[compoundKey(tenantID, name)]; ok {
			return p, nil
		}
	}
	// Fallback to master tenant
	if p, ok := r.providers[compoundKey(MasterTenantID, name)]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("provider not found: %s", name)
}

// Close calls Close() on all providers that implement io.Closer.
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, p := range r.providers {
		if c, ok := p.(io.Closer); ok {
			if err := c.Close(); err != nil {
				slog.Warn("provider close error", "key", key, "error", err)
			}
		}
	}
}

// List returns provider names visible to the tenant in context (tenant-specific + master defaults).
func (r *Registry) List(ctx context.Context) []string {
	return r.ListForTenant(r.tenantFromContext(ctx))
}

// ListForTenant returns provider names available to a tenant (tenant-specific + master defaults).
func (r *Registry) ListForTenant(tenantID uuid.UUID) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var names []string

	masterPrefix := MasterTenantID.String() + "/"
	tenantPrefix := tenantID.String() + "/"

	// Add tenant-specific providers first
	if tenantID != MasterTenantID {
		for key := range r.providers {
			if strings.HasPrefix(key, tenantPrefix) {
				name := strings.TrimPrefix(key, tenantPrefix)
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
	}

	// Add master tenant providers (not already overridden)
	for key := range r.providers {
		if strings.HasPrefix(key, masterPrefix) {
			name := strings.TrimPrefix(key, masterPrefix)
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}

	return names
}
