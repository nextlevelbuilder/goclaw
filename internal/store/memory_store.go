package store

import "context"

// DocumentInfo describes a memory document.
type DocumentInfo struct {
	Path      string `json:"path" db:"path"`
	Hash      string `json:"hash" db:"hash"`
	AgentID   string `json:"agent_id,omitempty" db:"agent_id"`
	UserID    string `json:"user_id,omitempty" db:"user_id"`
	UpdatedAt int64  `json:"updated_at" db:"updated_at"`
}

// MemorySearchResult is a single result from memory search.
type MemorySearchResult struct {
	Path      string  `json:"path" db:"-"`
	StartLine int     `json:"start_line" db:"-"`
	EndLine   int     `json:"end_line" db:"-"`
	Score     float64 `json:"score" db:"-"`
	Snippet   string  `json:"snippet" db:"-"`
	Source    string  `json:"source" db:"-"`
	Scope     string  `json:"scope,omitempty" db:"-"` // "global" or "personal"
}

// MemorySearchOptions configures a memory search query.
type MemorySearchOptions struct {
	MaxResults   int
	MinScore     float64
	Source       string  // "memory", "sessions", ""
	PathPrefix   string
	VectorWeight float64 // per-agent override (0 = use store default)
	TextWeight   float64 // per-agent override (0 = use store default)
}

// EmbeddingProvider generates vector embeddings for text.
type EmbeddingProvider interface {
	Name() string
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// DocumentDetail provides full document info including chunk/embedding stats.
type DocumentDetail struct {
	Path          string `json:"path" db:"path"`
	Content       string `json:"content" db:"content"`
	Hash          string `json:"hash" db:"hash"`
	UserID        string `json:"user_id,omitempty" db:"user_id"`
	ChunkCount    int    `json:"chunk_count" db:"chunk_count"`
	EmbeddedCount int    `json:"embedded_count" db:"embedded_count"`
	CreatedAt     int64  `json:"created_at" db:"created_at"`
	UpdatedAt     int64  `json:"updated_at" db:"updated_at"`
}

// ChunkInfo describes a single memory chunk.
type ChunkInfo struct {
	ID           string `json:"id" db:"id"`
	StartLine    int    `json:"start_line" db:"start_line"`
	EndLine      int    `json:"end_line" db:"end_line"`
	TextPreview  string `json:"text_preview" db:"text_preview"`
	HasEmbedding bool   `json:"has_embedding" db:"has_embedding"`
}

// MemoryStore manages memory documents and search.
type MemoryStore interface {
	// Document CRUD
	GetDocument(ctx context.Context, agentID, userID, path string) (string, error)
	PutDocument(ctx context.Context, agentID, userID, path, content string) error
	DeleteDocument(ctx context.Context, agentID, userID, path string) error
	ListDocuments(ctx context.Context, agentID, userID string) ([]DocumentInfo, error)

	// Admin queries
	ListAllDocumentsGlobal(ctx context.Context) ([]DocumentInfo, error)
	ListAllDocuments(ctx context.Context, agentID string) ([]DocumentInfo, error)
	GetDocumentDetail(ctx context.Context, agentID, userID, path string) (*DocumentDetail, error)
	ListChunks(ctx context.Context, agentID, userID, path string) ([]ChunkInfo, error)

	// Search
	Search(ctx context.Context, query string, agentID, userID string, opts MemorySearchOptions) ([]MemorySearchResult, error)

	// Indexing
	IndexDocument(ctx context.Context, agentID, userID, path string) error
	IndexAll(ctx context.Context, agentID, userID string) error

	SetEmbeddingProvider(provider EmbeddingProvider)
	Close() error
}
