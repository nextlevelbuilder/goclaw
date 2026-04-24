package store

import (
	"context"
	"time"
)

// VaultDocument is a registered document in the Knowledge Vault.
type VaultDocument struct {
	ID           string         `json:"id" db:"id"`
	TenantID     string         `json:"tenant_id" db:"tenant_id"`
	AgentID      *string        `json:"agent_id,omitempty" db:"agent_id"`
	TeamID       *string        `json:"team_id,omitempty" db:"team_id"`
	ChatID       *string        `json:"chat_id,omitempty" db:"chat_id"` // nil = team-wide (shared / legacy); non-nil = scoped to specific chat in isolated teams
	Scope        string         `json:"scope" db:"scope"` // personal, team, shared
	CustomScope  *string        `json:"custom_scope,omitempty" db:"custom_scope"`
	Path         string         `json:"path" db:"path"`                             // workspace-relative path
	PathBasename string         `json:"path_basename,omitempty" db:"path_basename"` // lowercased basename (PG GENERATED, SQLite app-populated)
	Title        string         `json:"title" db:"title"`
	DocType      string         `json:"doc_type" db:"doc_type"`         // context, memory, note, skill, episodic, media, document
	ContentHash  string         `json:"content_hash" db:"content_hash"` // SHA-256 hex digest
	Summary      string         `json:"summary" db:"summary"`           // LLM-generated or synthesized summary for embedding/search
	Metadata     map[string]any `json:"metadata,omitempty" db:"metadata"`
	CreatedAt    time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at" db:"updated_at"`
}

// VaultLink is a directed link between two vault documents.
type VaultLink struct {
	ID        string         `json:"id" db:"id"`
	FromDocID string         `json:"from_doc_id" db:"from_doc_id"`
	ToDocID   string         `json:"to_doc_id" db:"to_doc_id"`
	LinkType  string         `json:"link_type" db:"link_type"` // wikilink, reference, task_attachment, delegation_attachment, ...
	Context   string         `json:"context" db:"context"`     // surrounding text snippet or source reference
	Metadata  map[string]any `json:"metadata,omitempty" db:"metadata"` // {"source": "task:{id}"} etc., used by cleanup paths
	CreatedAt time.Time      `json:"created_at" db:"created_at"`
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
	TeamIDs    []string // non-nil = personal (NULL) + these team UUIDs (used for "all accessible" view)
	ChatID     *string  // isolated-team scope: when non-nil + TeamIsolated, filter (chat_id = ChatID OR chat_id IS NULL)
	TeamIsolated bool   // true = apply ChatID filter; false = shared/no-team mode (ignore ChatID)
	Scope      string   // empty = all scopes
	DocTypes   []string // empty = all types
	MaxResults int      // default 10
	MinScore   float64  // default 0.0
}

// VaultListOptions configures a list query for vault documents.
type VaultListOptions struct {
	TeamID   *string  // nil = no filter, ptr-to-empty = personal (NULL team_id), ptr-to-uuid = specific team
	TeamIDs  []string // non-nil = personal (NULL) + these team UUIDs (used for "all accessible" view)
	Scope    string   // empty = all
	DocTypes []string // empty = all
	Limit    int
	Offset   int
}

// VaultTreeEntry represents a file or virtual folder in the vault tree.
type VaultTreeEntry struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	IsDir       bool       `json:"isDir"`
	HasChildren bool       `json:"hasChildren,omitempty"`
	DocID       string     `json:"docId,omitempty"`
	DocType     string     `json:"docType,omitempty"`
	Scope       string     `json:"scope,omitempty"`
	Title       string     `json:"title,omitempty"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
}

// VaultTreeOptions configures a vault tree listing query.
type VaultTreeOptions struct {
	Path     string
	AgentID  string   // optional agent filter
	TeamID   *string
	TeamIDs  []string
	Scope    string
	DocTypes []string
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

	// ListTreeEntries returns immediate children (files + virtual folders) under the given path prefix.
	ListTreeEntries(ctx context.Context, tenantID string, opts VaultTreeOptions) ([]VaultTreeEntry, error)

	// GetDocumentsByIDs returns documents matching the given IDs with tenant isolation.
	GetDocumentsByIDs(ctx context.Context, tenantID string, docIDs []string) ([]VaultDocument, error)
	// GetDocumentByBasename finds a document by path basename (case-insensitive).
	GetDocumentByBasename(ctx context.Context, tenantID, agentID, basename string) (*VaultDocument, error)

	// Search (FTS + vector hybrid)
	Search(ctx context.Context, opts VaultSearchOptions) ([]VaultSearchResult, error)

	// Links
	CreateLinks(ctx context.Context, links []VaultLink) error
	CreateLink(ctx context.Context, link *VaultLink) error
	DeleteLink(ctx context.Context, tenantID, id string) error
	GetOutLinks(ctx context.Context, tenantID, docID string) ([]VaultLink, error)
	GetOutLinksBatch(ctx context.Context, tenantID string, docIDs []string) ([]VaultLink, error)
	GetBacklinks(ctx context.Context, tenantID, docID string) ([]VaultBacklink, error)
	DeleteDocLinks(ctx context.Context, tenantID, docID string) error
	DeleteDocLinksByType(ctx context.Context, tenantID, docID, linkType string) error
	DeleteDocLinksByTypes(ctx context.Context, tenantID, docID string, types []string) error

	// DeleteLinksBySource removes vault_links rows where metadata->>'source'
	// equals the given source key (e.g. "task:{uuid}", "delegation:{uuid}").
	// Used by cleanup paths (DetachFileFromTask, DeleteTask, bulk task delete)
	// to surgically remove Phase-04/05 auto-links without touching classify-
	// owned links. Returns the number of rows deleted. Tenant isolation is
	// enforced by joining vault_documents on tenant_id.
	DeleteLinksBySource(ctx context.Context, tenantID, source string) (int64, error)

	// Enrichment
	// ListUnenrichedDocs returns documents with empty summary for re-enrichment.
	// Used after rescan to retry failed enrichments.
	ListUnenrichedDocs(ctx context.Context, tenantID string, limit int) ([]VaultDocument, error)
	// UpdateSummaryAndReembed updates summary text and re-generates embedding from title+path+summary.
	UpdateSummaryAndReembed(ctx context.Context, tenantID, docID, summary string) error
	// FindSimilarDocs finds documents with similar embeddings to the given docID.
	// Returns top-N neighbors excluding the source doc. Score = cosine similarity.
	FindSimilarDocs(ctx context.Context, tenantID, agentID, docID string, limit int) ([]VaultSearchResult, error)
	// BatchFindByDelegationIDs returns vault docs sharing any of the given
	// delegation_ids in their metadata, keyed by delegation_id. Each
	// delegation's bucket is capped at `limit` (ordered by created_at DESC).
	// excludeDocIDs is applied as a NOT-IN filter to prevent self-links.
	// Single SQL query — uses ROW_NUMBER() PARTITION BY delegation_id over
	// the partial index added by migration 000048.
	BatchFindByDelegationIDs(
		ctx context.Context,
		tenantID string,
		delegationIDs []string,
		limit int,
		excludeDocIDs []string,
	) (map[string][]VaultDocument, error)

	// Embedding
	SetEmbeddingProvider(provider EmbeddingProvider)
	Close() error
}
