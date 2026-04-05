package store

import (
	"context"
	"time"
)

// BrowserExtension represents a Chrome extension registered in the system.
type BrowserExtension struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenantId"`
	Name      string    `json:"name"`
	Path      string    `json:"path"` // absolute path to unpacked extension dir
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
}

// BrowserExtensionStore manages browser extensions.
type BrowserExtensionStore interface {
	List(ctx context.Context, tenantID string) ([]*BrowserExtension, error)
	Create(ctx context.Context, e *BrowserExtension) error
	Delete(ctx context.Context, id string) error
	ListEnabled(ctx context.Context, tenantID string) ([]*BrowserExtension, error)
}
