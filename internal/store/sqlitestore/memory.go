//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteMemoryStore implements store.MemoryStore backed by SQLite.
// Vector search is not available in the Lite edition (VectorSearch: false).
// FTS uses simple LIKE queries instead of tsvector.
type SQLiteMemoryStore struct {
	db       *sql.DB
	provider store.EmbeddingProvider
	mu       sync.RWMutex
	cfg      SQLiteMemoryConfig
}

// SQLiteMemoryConfig configures the SQLite memory store.
type SQLiteMemoryConfig struct {
	MaxChunkLen  int
	ChunkOverlap int
	MaxResults   int
	TextWeight   float64
	VectorWeight float64
}

// DefaultSQLiteMemoryConfig returns sensible defaults.
func DefaultSQLiteMemoryConfig() SQLiteMemoryConfig {
	return SQLiteMemoryConfig{
		MaxChunkLen:  1000,
		ChunkOverlap: 200,
		MaxResults:   6,
		TextWeight:   1.0,
		VectorWeight: 0.0, // no vector search in SQLite edition
	}
}

// NewSQLiteMemoryStore creates a new SQLite-backed memory store.
func NewSQLiteMemoryStore(db *sql.DB) *SQLiteMemoryStore {
	return &SQLiteMemoryStore{db: db, cfg: DefaultSQLiteMemoryConfig()}
}

// SetEmbeddingProvider stores the provider reference but embeddings are not
// persisted in SQLite (no vector column). Kept for interface compatibility.
func (s *SQLiteMemoryStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.provider = provider
}

// UpdateChunkConfig updates chunk splitting parameters at runtime.
func (s *SQLiteMemoryStore) UpdateChunkConfig(maxChunkLen, chunkOverlap int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxChunkLen > 0 {
		s.cfg.MaxChunkLen = maxChunkLen
	}
	if chunkOverlap >= 0 {
		s.cfg.ChunkOverlap = chunkOverlap
	}
}

// chunkConfig returns current chunk parameters (thread-safe).
func (s *SQLiteMemoryStore) chunkConfig() (maxLen, overlap int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.MaxChunkLen, s.cfg.ChunkOverlap
}

func (s *SQLiteMemoryStore) Close() error { return nil }

// scanDocumentRow scans (path, hash, user_id, updated_at) into DocumentInfo.
func scanDocumentRow(path, hash string, uid *string, updatedAt time.Time) store.DocumentInfo {
	info := store.DocumentInfo{
		Path:      path,
		Hash:      hash,
		UpdatedAt: updatedAt.UnixMilli(),
	}
	if uid != nil {
		info.UserID = *uid
	}
	return info
}
