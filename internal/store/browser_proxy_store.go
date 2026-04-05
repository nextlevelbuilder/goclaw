package store

import (
	"context"
	"time"
)

// BrowserProxy represents a proxy in the pool.
type BrowserProxy struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenantId"`
	Name            string    `json:"name"`
	URL             string    `json:"url"`
	Username        string    `json:"username,omitempty"`
	Password        string    `json:"password,omitempty"` // encrypted via crypto.Encrypt
	Geo             string    `json:"geo,omitempty"`      // country code: VN, US, JP
	Tags            []string  `json:"tags,omitempty"`
	IsEnabled       bool      `json:"isEnabled"`
	IsHealthy       bool      `json:"isHealthy"`
	FailCount       int       `json:"failCount"`
	LastHealthCheck *time.Time `json:"lastHealthCheck,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// BrowserProxyStore manages browser proxies.
type BrowserProxyStore interface {
	List(ctx context.Context, tenantID string) ([]*BrowserProxy, error)
	Get(ctx context.Context, id string) (*BrowserProxy, error)
	Create(ctx context.Context, p *BrowserProxy) error
	Update(ctx context.Context, p *BrowserProxy) error
	Delete(ctx context.Context, id, tenantID string) error
	ListHealthy(ctx context.Context, tenantID, geo string) ([]*BrowserProxy, error)
	UpdateHealth(ctx context.Context, id string, healthy bool, failCount int) error
	SetEnabled(ctx context.Context, id, tenantID string, enabled bool) error
}

// ProxyProfileAssignment tracks sticky proxy-to-profile mapping.
type ProxyProfileAssignment struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenantId"`
	ProxyID    string    `json:"proxyId"`
	ProfileDir string    `json:"profileDir"`
	AssignedAt time.Time `json:"assignedAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

// BrowserProxyAssignmentStore manages sticky proxy-profile assignments.
type BrowserProxyAssignmentStore interface {
	GetByProfile(ctx context.Context, tenantID, profileDir string) (*ProxyProfileAssignment, error)
	Upsert(ctx context.Context, a *ProxyProfileAssignment) error
	CountByProxy(ctx context.Context, proxyID string) (int, error)
	DeleteByProxy(ctx context.Context, proxyID string) error
}
