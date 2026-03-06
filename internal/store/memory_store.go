package store

import "context"

// DocumentInfo describes a memory document.
type DocumentInfo struct {
	Path      string `json:"path"`
	Hash      string `json:"hash"`
	UserID    string `json:"user_id,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

// MemorySearchResult is a single result from memory search.
type MemorySearchResult struct {
	Path      string  `json:"path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float64 `json:"score"`
	Snippet   string  `json:"snippet"`
	Source    string  `json:"source"`
	Scope     string  `json:"scope,omitempty"` // "global" or "personal"
}

// MemorySearchOptions configures a memory search query.
type MemorySearchOptions struct {
	MaxResults int
	MinScore   float64
	Source     string // "memory", "sessions", ""
	PathPrefix string
}

// EmbeddingProvider generates vector embeddings for text.
type EmbeddingProvider interface {
	Name() string
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// MemoryStore manages memory documents and search.
type MemoryStore interface {
	// Document CRUD
	GetDocument(ctx context.Context, agentID, userID, path string) (string, error)
	PutDocument(ctx context.Context, agentID, userID, path, content string) error
	DeleteDocument(ctx context.Context, agentID, userID, path string) error
	ListDocuments(ctx context.Context, agentID, userID string) ([]DocumentInfo, error)

	// Search
	Search(ctx context.Context, query string, agentID, userID string, opts MemorySearchOptions) ([]MemorySearchResult, error)

	// Indexing
	IndexDocument(ctx context.Context, agentID, userID, path string) error
	IndexAll(ctx context.Context, agentID, userID string) error

	SetEmbeddingProvider(provider EmbeddingProvider)
	Close() error
}
