package store

import (
	"context"
	"time"
)

// VaultDocument is a registered document in the Knowledge Vault.
type VaultDocument struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	AgentID     string         `json:"agent_id"`
	Scope       string         `json:"scope"`       // personal, team, shared
	Path        string         `json:"path"`         // workspace-relative path
	Title       string         `json:"title"`
	DocType     string         `json:"doc_type"`     // context, memory, note, skill, episodic
	ContentHash string         `json:"content_hash"` // SHA-256 hex digest
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// VaultLink is a directed link between two vault documents.
type VaultLink struct {
	ID        string    `json:"id"`
	FromDocID string    `json:"from_doc_id"`
	ToDocID   string    `json:"to_doc_id"`
	LinkType  string    `json:"link_type"` // wikilink, reference, etc.
	Context   string    `json:"context"`   // surrounding text snippet
	CreatedAt time.Time `json:"created_at"`
}

// VaultSearchResult is a single result from vault search.
type VaultSearchResult struct {
	Document VaultDocument `json:"document"`
	Score    float64       `json:"score"`
	Source   string        `json:"source"` // vault, episodic, kg
}

// VaultSearchOptions configures a vault search query.
type VaultSearchOptions struct {
	Query      string
	AgentID    string
	TenantID   string
	Scope      string   // empty = all scopes
	DocTypes   []string // empty = all types
	MaxResults int      // default 10
	MinScore   float64  // default 0.0
}

// VaultListOptions configures a list query for vault documents.
type VaultListOptions struct {
	Scope    string   // empty = all
	DocTypes []string // empty = all
	Limit    int
	Offset   int
}

// VaultStore manages the Knowledge Vault document registry and links.
type VaultStore interface {
	// Document CRUD
	UpsertDocument(ctx context.Context, doc *VaultDocument) error
	GetDocument(ctx context.Context, tenantID, agentID, path string) (*VaultDocument, error)
	GetDocumentByID(ctx context.Context, tenantID, id string) (*VaultDocument, error)
	DeleteDocument(ctx context.Context, tenantID, agentID, path string) error
	ListDocuments(ctx context.Context, tenantID, agentID string, opts VaultListOptions) ([]VaultDocument, error)
	UpdateHash(ctx context.Context, tenantID, id, newHash string) error

	// Search (FTS + vector hybrid)
	Search(ctx context.Context, opts VaultSearchOptions) ([]VaultSearchResult, error)

	// Links
	CreateLink(ctx context.Context, link *VaultLink) error
	DeleteLink(ctx context.Context, tenantID, id string) error
	GetOutLinks(ctx context.Context, tenantID, docID string) ([]VaultLink, error)
	GetBacklinks(ctx context.Context, tenantID, docID string) ([]VaultLink, error)
	DeleteDocLinks(ctx context.Context, tenantID, docID string) error

	// Embedding
	SetEmbeddingProvider(provider EmbeddingProvider)
	Close() error
}
