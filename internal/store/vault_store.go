package store

import (
	"context"
	"time"
)

// VaultDocument is a registered document in the Knowledge Vault.
type VaultDocument struct {
	ID          string         `json:"id" db:"id"`
	TenantID    string         `json:"tenant_id" db:"tenant_id"`
	AgentID     string         `json:"agent_id" db:"agent_id"`
	TeamID      *string        `json:"team_id,omitempty" db:"team_id"`
	Scope       string         `json:"scope" db:"scope"`             // personal, team, shared
	CustomScope *string        `json:"custom_scope,omitempty" db:"custom_scope"`
	Path        string         `json:"path" db:"path"`               // workspace-relative path
	Title       string         `json:"title" db:"title"`
	DocType     string         `json:"doc_type" db:"doc_type"`       // context, memory, note, skill, episodic, media
	ContentHash string         `json:"content_hash" db:"content_hash"` // SHA-256 hex digest
	Summary     string         `json:"summary" db:"summary"`           // LLM-generated summary for richer embedding/search
	Metadata    map[string]any `json:"metadata,omitempty" db:"metadata"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

// VaultLink is a directed link between two vault documents.
type VaultLink struct {
	ID        string    `json:"id" db:"id"`
	FromDocID string    `json:"from_doc_id" db:"from_doc_id"`
	ToDocID   string    `json:"to_doc_id" db:"to_doc_id"`
	LinkType  string    `json:"link_type" db:"link_type"` // wikilink, reference, etc.
	Context   string    `json:"context" db:"context"`     // surrounding text snippet
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// VaultBacklink is an enriched backlink with source doc metadata (single JOIN query).
type VaultBacklink struct {
	FromDocID string  `json:"from_doc_id"`
	Context   string  `json:"context"`
	Title     string  `json:"title"`
	Path      string  `json:"path"`
	TeamID    *string `json:"team_id,omitempty"`
}

// VaultSearchResult is a single result from vault search.
type VaultSearchResult struct {
	Document VaultDocument `json:"document" db:"-"`
	Score    float64       `json:"score" db:"-"`
	Source   string        `json:"source" db:"-"` // vault, episodic, kg
}

// VaultSearchOptions configures a vault search query.
type VaultSearchOptions struct {
	Query      string
	AgentID    string
	TenantID   string
	TeamID     *string  // nil = no filter, ptr-to-empty = personal (NULL team_id), ptr-to-uuid = specific team
	Scope      string   // empty = all scopes
	DocTypes   []string // empty = all types
	MaxResults int      // default 10
	MinScore   float64  // default 0.0
}

// VaultListOptions configures a list query for vault documents.
type VaultListOptions struct {
	TeamID   *string  // nil = no filter, ptr-to-empty = personal (NULL team_id), ptr-to-uuid = specific team
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
	CountDocuments(ctx context.Context, tenantID, agentID string, opts VaultListOptions) (int, error)
	UpdateHash(ctx context.Context, tenantID, id, newHash string) error

	// Search (FTS + vector hybrid)
	Search(ctx context.Context, opts VaultSearchOptions) ([]VaultSearchResult, error)

	// Links
	CreateLink(ctx context.Context, link *VaultLink) error
	DeleteLink(ctx context.Context, tenantID, id string) error
	GetOutLinks(ctx context.Context, tenantID, docID string) ([]VaultLink, error)
	GetBacklinks(ctx context.Context, tenantID, docID string) ([]VaultBacklink, error)
	DeleteDocLinks(ctx context.Context, tenantID, docID string) error

	// Enrichment
	// UpdateSummaryAndReembed updates summary text and re-generates embedding from title+path+summary.
	UpdateSummaryAndReembed(ctx context.Context, tenantID, docID, summary string) error
	// FindSimilarDocs finds documents with similar embeddings to the given docID.
	// Returns top-N neighbors excluding the source doc. Score = cosine similarity.
	FindSimilarDocs(ctx context.Context, tenantID, agentID, docID string, limit int) ([]VaultSearchResult, error)

	// Embedding
	SetEmbeddingProvider(provider EmbeddingProvider)
	Close() error
}
