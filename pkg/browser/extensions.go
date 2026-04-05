package browser

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ExtensionManager manages Chrome extension registration and loading.
type ExtensionManager struct {
	store  store.BrowserExtensionStore
	logger *slog.Logger
}

// NewExtensionManager creates an ExtensionManager.
func NewExtensionManager(s store.BrowserExtensionStore, l *slog.Logger) *ExtensionManager {
	if l == nil {
		l = slog.Default()
	}
	return &ExtensionManager{store: s, logger: l}
}

// ExtensionPaths returns filesystem paths for all enabled extensions.
// Used for Chrome --load-extension flag.
func (em *ExtensionManager) ExtensionPaths(ctx context.Context, tenantID string) ([]string, error) {
	exts, err := em.store.ListEnabled(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(exts))
	for i, e := range exts {
		paths[i] = e.Path
	}
	return paths, nil
}

// List returns all extensions for a tenant.
func (em *ExtensionManager) List(ctx context.Context, tenantID string) ([]*store.BrowserExtension, error) {
	return em.store.List(ctx, tenantID)
}

// Add registers a new extension.
func (em *ExtensionManager) Add(ctx context.Context, e *store.BrowserExtension) error {
	return em.store.Create(ctx, e)
}

// Remove deletes an extension.
func (em *ExtensionManager) Remove(ctx context.Context, id string) error {
	return em.store.Delete(ctx, id)
}
