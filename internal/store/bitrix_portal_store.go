package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BitrixPortalData represents a Bitrix24 portal row.
//
// credentials + state are stored AES-256-GCM encrypted on disk
// (via internal/crypto/aes.go). The store layer handles encrypt/decrypt
// so callers deal with plaintext []byte payloads.
type BitrixPortalData struct {
	BaseModel
	TenantID    uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Name        string    `json:"name" db:"name"`
	Domain      string    `json:"domain" db:"domain"`
	Credentials []byte    `json:"-" db:"credentials"` // plaintext after decrypt; never serialized
	State       []byte    `json:"-" db:"state"`       // plaintext after decrypt; never serialized
}

// BitrixPortalCredentials is the decoded JSON payload of the `credentials`
// column. It carries the Bitrix24 app client_id / client_secret pair the
// Portal uses for the OAuth2 exchange + refresh flow.
type BitrixPortalCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// BitrixPortalState is the decoded JSON payload of the `state` column.
// It holds everything the Portal runtime persists between restarts:
// active OAuth token, refresh token, bot/media caches, and refresh bookkeeping.
type BitrixPortalState struct {
	AccessToken      string            `json:"access_token,omitempty"`
	RefreshToken     string            `json:"refresh_token,omitempty"`
	ExpiresAt        time.Time         `json:"expires_at,omitempty"`
	MemberID         string            `json:"member_id,omitempty"`
	AppToken         string            `json:"app_token,omitempty"` // auth.application_token from OAuth response
	Scope            string            `json:"scope,omitempty"`
	ClientEndpoint   string            `json:"client_endpoint,omitempty"`
	RegisteredBots   map[string]int    `json:"registered_bots,omitempty"` // bot_code → bot_id
	MediaFolders     map[string]string `json:"media_folders,omitempty"`   // bot_code → disk folder id
	LastRefreshAt    time.Time         `json:"last_refresh_at,omitempty"`
	LastRefreshError string            `json:"last_refresh_error,omitempty"`
	ConsecutiveFail  int               `json:"consecutive_fail,omitempty"`

	// PublicURL is the gateway's externally reachable base URL, captured from
	// the request hitting /bitrix24/install. Channels use this when registering
	// imbot event handler URLs with Bitrix24. Replaces the deprecated per-channel
	// public_url config.
	PublicURL string `json:"public_url,omitempty"`
}

// BitrixPortalStore manages bitrix_portals rows.
//
// All methods except ListAllForLoader must be called on a context carrying
// either a matching TenantID (store.WithTenantID) or master scope — the impls
// verify via store.IsMasterScope. ListAllForLoader is an internal startup
// helper that returns rows across all tenants and must never be exposed via RPC.
type BitrixPortalStore interface {
	Create(ctx context.Context, p *BitrixPortalData) error
	GetByName(ctx context.Context, tenantID uuid.UUID, name string) (*BitrixPortalData, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]BitrixPortalData, error)
	ListAllForLoader(ctx context.Context) ([]BitrixPortalData, error)
	UpdateCredentials(ctx context.Context, tenantID uuid.UUID, name string, creds []byte) error
	UpdateState(ctx context.Context, tenantID uuid.UUID, name string, state []byte) error
	Delete(ctx context.Context, tenantID uuid.UUID, name string) error
}
