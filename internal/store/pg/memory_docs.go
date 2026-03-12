package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGMemoryStore implements store.MemoryStore backed by Postgres.
type PGMemoryStore struct {
	db       *sql.DB
	provider store.EmbeddingProvider
	cfg      PGMemoryConfig
}

// PGMemoryConfig configures the PG memory store.
type PGMemoryConfig struct {
	MaxChunkLen  int
	MaxResults   int
	VectorWeight float64
	TextWeight   float64
}

// DefaultPGMemoryConfig returns sensible defaults.
func DefaultPGMemoryConfig() PGMemoryConfig {
	return PGMemoryConfig{
		MaxChunkLen:  1000,
		MaxResults:   6,
		VectorWeight: 0.7,
		TextWeight:   0.3,
	}
}

func NewPGMemoryStore(db *sql.DB, cfg PGMemoryConfig) *PGMemoryStore {
	return &PGMemoryStore{db: db, cfg: cfg}
}

func (s *PGMemoryStore) GetDocument(ctx context.Context, agentID, userID, path string) (string, error) {
	aid := mustParseUUID(agentID)
	var content string

	var err error
	if userID == "" {
		err = s.db.QueryRowContext(ctx,
			"SELECT content FROM memory_documents WHERE agent_id = $1 AND path = $2 AND user_id IS NULL",
			aid, path).Scan(&content)
	} else {
		err = s.db.QueryRowContext(ctx,
			"SELECT content FROM memory_documents WHERE agent_id = $1 AND path = $2 AND user_id = $3",
			aid, path, userID).Scan(&content)
	}
	if err != nil {
		return "", err
	}
	return content, nil
}

func (s *PGMemoryStore) PutDocument(ctx context.Context, agentID, userID, path, content string) error {
	aid := mustParseUUID(agentID)
	hash := memory.ContentHash(content)
	id := uuid.Must(uuid.NewV7())
	now := time.Now()

	var uid *string
	if userID != "" {
		uid = &userID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_documents (id, agent_id, user_id, path, content, hash, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (agent_id, COALESCE(user_id, ''), path)
		 DO UPDATE SET content = EXCLUDED.content, hash = EXCLUDED.hash, updated_at = EXCLUDED.updated_at`,
		id, aid, uid, path, content, hash, now,
	)
	return err
}

func (s *PGMemoryStore) DeleteDocument(ctx context.Context, agentID, userID, path string) error {
	aid := mustParseUUID(agentID)
	if userID == "" {
		_, err := s.db.ExecContext(ctx,
			"DELETE FROM memory_documents WHERE agent_id = $1 AND path = $2 AND user_id IS NULL",
			aid, path)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM memory_documents WHERE agent_id = $1 AND path = $2 AND user_id = $3",
		aid, path, userID)
	return err
}

func (s *PGMemoryStore) ListDocuments(ctx context.Context, agentID, userID string) ([]store.DocumentInfo, error) {
	aid := mustParseUUID(agentID)

	var rows *sql.Rows
	var err error
	if userID == "" {
		rows, err = s.db.QueryContext(ctx,
			"SELECT path, hash, user_id, updated_at FROM memory_documents WHERE agent_id = $1 AND user_id IS NULL", aid)
	} else {
		rows, err = s.db.QueryContext(ctx,
			"SELECT path, hash, user_id, updated_at FROM memory_documents WHERE agent_id = $1 AND (user_id IS NULL OR user_id = $2)", aid, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.DocumentInfo
	for rows.Next() {
		var path, hash string
		var uid *string
		var updatedAt time.Time
		if err := rows.Scan(&path, &hash, &uid, &updatedAt); err != nil {
			continue
		}
		info := store.DocumentInfo{
			Path:      path,
			Hash:      hash,
			UpdatedAt: updatedAt.UnixMilli(),
		}
		if uid != nil {
			info.UserID = *uid
		}
		result = append(result, info)
	}
	return result, nil
}

// IndexDocument chunks a document and stores chunks with embeddings.
func (s *PGMemoryStore) IndexDocument(ctx context.Context, agentID, userID, path string) error {
	aid := mustParseUUID(agentID)

	// Get document content
	content, err := s.GetDocument(ctx, agentID, userID, path)
	if err != nil {
		return err
	}

	// Get document ID
	var docID uuid.UUID
	if userID == "" {
		err = s.db.QueryRowContext(ctx,
			"SELECT id FROM memory_documents WHERE agent_id = $1 AND path = $2 AND user_id IS NULL",
			aid, path).Scan(&docID)
	} else {
		err = s.db.QueryRowContext(ctx,
			"SELECT id FROM memory_documents WHERE agent_id = $1 AND path = $2 AND user_id = $3",
			aid, path, userID).Scan(&docID)
	}
	if err != nil {
		return err
	}

	// Delete old chunks
	s.db.ExecContext(ctx, "DELETE FROM memory_chunks WHERE document_id = $1", docID)

	// Chunk text
	chunks := memory.ChunkText(content, s.cfg.MaxChunkLen)
	if len(chunks) == 0 {
		return nil
	}

	// Generate embeddings
	var embeddings [][]float32
	if s.provider != nil {
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}
		var embErr error
		embeddings, embErr = s.provider.Embed(ctx, texts)
		if embErr != nil {
			slog.Warn("memory embedding failed, storing chunks without vectors",
				"path", path, "chunks", len(chunks), "error", embErr)
		}
	}

	// Insert chunks
	for i, tc := range chunks {
		hash := memory.ContentHash(tc.Text)
		chunkID := uuid.Must(uuid.NewV7())
		now := time.Now()

		var uid *string
		if userID != "" {
			uid = &userID
		}

		if embeddings != nil && i < len(embeddings) {
			// Insert with embedding via raw SQL (pgvector)
			s.db.ExecContext(ctx,
				`INSERT INTO memory_chunks (id, agent_id, document_id, user_id, path, start_line, end_line, hash, text, embedding, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::vector, $11)`,
				chunkID, aid, docID, uid, path, tc.StartLine, tc.EndLine, hash, tc.Text,
				vectorToString(embeddings[i]), now,
			)
		} else {
			s.db.ExecContext(ctx,
				`INSERT INTO memory_chunks (id, agent_id, document_id, user_id, path, start_line, end_line, hash, text, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				 ON CONFLICT DO NOTHING`,
				chunkID, aid, docID, uid, path, tc.StartLine, tc.EndLine, hash, tc.Text, now,
			)
		}
	}

	return nil
}

func (s *PGMemoryStore) IndexAll(ctx context.Context, agentID, userID string) error {
	docs, err := s.ListDocuments(ctx, agentID, userID)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		s.IndexDocument(ctx, agentID, doc.UserID, doc.Path)
	}
	return nil
}

func (s *PGMemoryStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.provider = provider
}

// BackfillEmbeddings finds all chunks without embeddings and generates them.
// Processes in batches to avoid memory spikes. Safe to call multiple times.
func (s *PGMemoryStore) BackfillEmbeddings(ctx context.Context) (int, error) {
	if s.provider == nil {
		return 0, fmt.Errorf("no embedding provider configured")
	}

	const batchSize = 50
	total := 0

	for {
		rows, err := s.db.QueryContext(ctx,
			"SELECT id, text FROM memory_chunks WHERE embedding IS NULL ORDER BY id ASC LIMIT $1", batchSize)
		if err != nil {
			return total, fmt.Errorf("query chunks without embeddings: %w", err)
		}

		type chunkRow struct {
			ID   uuid.UUID
			Text string
		}
		var chunks []chunkRow
		for rows.Next() {
			var c chunkRow
			if err := rows.Scan(&c.ID, &c.Text); err != nil {
				continue
			}
			chunks = append(chunks, c)
		}
		rows.Close()

		if len(chunks) == 0 {
			break
		}

		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
		}

		embeddings, err := s.provider.Embed(ctx, texts)
		if err != nil {
			return total, fmt.Errorf("generate embeddings: %w", err)
		}

		for i, chunk := range chunks {
			if i >= len(embeddings) {
				break
			}
			vecStr := vectorToString(embeddings[i])
			if _, err := s.db.ExecContext(ctx,
				"UPDATE memory_chunks SET embedding = $1::vector WHERE id = $2",
				vecStr, chunk.ID,
			); err != nil {
				return total, fmt.Errorf("update chunk embedding id=%s: %w", chunk.ID, err)
			}
			total++
		}

		if len(chunks) < batchSize {
			break
		}
	}

	return total, nil
}

func (s *PGMemoryStore) Close() error { return nil }

// --- Helpers ---

func mustParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func vectorToString(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	buf := make([]byte, 0, len(v)*10)
	buf = append(buf, '[')
	for i, f := range v {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, fmt.Appendf(nil, "%g", f)...)
	}
	buf = append(buf, ']')
	return string(buf)
}
