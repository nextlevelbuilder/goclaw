//go:build sqlite || sqliteonly

package sqlitestore

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

func (s *SQLiteMemoryStore) GetDocument(ctx context.Context, agentID, userID, path string) (string, error) {
	aid := agentID
	var content string
	var err error

	if userID == "" {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return "", tcErr
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT content FROM memory_documents WHERE agent_id = ? AND path = ? AND user_id IS NULL"+tc,
			append([]any{aid, path}, tcArgs...)...).Scan(&content)
	} else {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return "", tcErr
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT content FROM memory_documents WHERE agent_id = ? AND path = ? AND user_id = ?"+tc,
			append([]any{aid, path, userID}, tcArgs...)...).Scan(&content)
	}
	if err != nil {
		return "", err
	}
	return content, nil
}

func (s *SQLiteMemoryStore) PutDocument(ctx context.Context, agentID, userID, path, content string) error {
	hash := memory.ContentHash(content)
	id := uuid.Must(uuid.NewV7()).String()
	now := time.Now().UTC()
	tid := tenantIDForInsert(ctx).String()

	var uid *string
	if userID != "" {
		uid = &userID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_documents (id, agent_id, user_id, path, content, hash, tenant_id, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (agent_id, COALESCE(user_id, ''), path)
		 DO UPDATE SET content = excluded.content, hash = excluded.hash,
		               tenant_id = excluded.tenant_id, updated_at = excluded.updated_at`,
		id, agentID, uid, path, content, hash, tid, now,
	)
	return err
}

func (s *SQLiteMemoryStore) DeleteDocument(ctx context.Context, agentID, userID, path string) error {
	var res sql.Result
	var err error

	if userID == "" {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return tcErr
		}
		res, err = s.db.ExecContext(ctx,
			"DELETE FROM memory_documents WHERE agent_id = ? AND path = ? AND user_id IS NULL"+tc,
			append([]any{agentID, path}, tcArgs...)...)
	} else {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return tcErr
		}
		res, err = s.db.ExecContext(ctx,
			"DELETE FROM memory_documents WHERE agent_id = ? AND path = ? AND user_id = ?"+tc,
			append([]any{agentID, path, userID}, tcArgs...)...)
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("document not found: %s", path)
	}
	return nil
}

func (s *SQLiteMemoryStore) ListDocuments(ctx context.Context, agentID, userID string) ([]store.DocumentInfo, error) {
	var rows *sql.Rows
	var err error

	if userID == "" {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return nil, tcErr
		}
		rows, err = s.db.QueryContext(ctx,
			"SELECT path, hash, user_id, updated_at FROM memory_documents WHERE agent_id = ? AND user_id IS NULL"+tc,
			append([]any{agentID}, tcArgs...)...)
	} else {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return nil, tcErr
		}
		rows, err = s.db.QueryContext(ctx,
			"SELECT path, hash, user_id, updated_at FROM memory_documents WHERE agent_id = ? AND (user_id IS NULL OR user_id = ?)"+tc,
			append([]any{agentID, userID}, tcArgs...)...)
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
		result = append(result, scanDocumentRow(path, hash, uid, updatedAt))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// IndexDocument chunks a document and stores chunks (without embeddings in SQLite).
func (s *SQLiteMemoryStore) IndexDocument(ctx context.Context, agentID, userID, path string) error {
	content, err := s.GetDocument(ctx, agentID, userID, path)
	if err != nil {
		return err
	}

	// Get document ID
	var docID string
	if userID == "" {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return tcErr
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT id FROM memory_documents WHERE agent_id = ? AND path = ? AND user_id IS NULL"+tc,
			append([]any{agentID, path}, tcArgs...)...).Scan(&docID)
	} else {
		tc, tcArgs, tcErr := scopeClause(ctx)
		if tcErr != nil {
			return tcErr
		}
		err = s.db.QueryRowContext(ctx,
			"SELECT id FROM memory_documents WHERE agent_id = ? AND path = ? AND user_id = ?"+tc,
			append([]any{agentID, path, userID}, tcArgs...)...).Scan(&docID)
	}
	if err != nil {
		return err
	}

	// Delete old chunks
	if _, delErr := s.db.ExecContext(ctx, "DELETE FROM memory_chunks WHERE document_id = ?", docID); delErr != nil {
		return fmt.Errorf("delete old chunks: %w", delErr)
	}

	// Resolve chunk config: per-agent override → global default
	chunkLen, chunkOverlap := s.chunkConfig()
	if rc := store.RunContextFromCtx(ctx); rc != nil && rc.MemoryCfg != nil {
		if rc.MemoryCfg.MaxChunkLen > 0 {
			chunkLen = rc.MemoryCfg.MaxChunkLen
		}
		if rc.MemoryCfg.ChunkOverlap > 0 {
			chunkOverlap = rc.MemoryCfg.ChunkOverlap
		}
	}

	chunks := memory.ChunkText(content, chunkLen, chunkOverlap)
	if len(chunks) == 0 {
		return nil
	}

	tid := tenantIDForInsert(ctx).String()
	var uid *string
	if userID != "" {
		uid = &userID
	}

	for _, tc := range chunks {
		hash := memory.ContentHash(tc.Text)
		chunkID := uuid.Must(uuid.NewV7()).String()
		now := time.Now().UTC()

		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO memory_chunks (id, agent_id, document_id, user_id, path, start_line, end_line, hash, text, tenant_id, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING`,
			chunkID, agentID, docID, uid, path, tc.StartLine, tc.EndLine, hash, tc.Text, tid, now,
		); err != nil {
			slog.Warn("memory: insert chunk failed", "path", path, "error", err)
		}
	}
	return nil
}

func (s *SQLiteMemoryStore) IndexAll(ctx context.Context, agentID, userID string) error {
	docs, err := s.ListDocuments(ctx, agentID, userID)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		s.IndexDocument(ctx, agentID, doc.UserID, doc.Path)
	}
	return nil
}

// ListAllDocumentsGlobal returns all documents across all agents (admin overview).
func (s *SQLiteMemoryStore) ListAllDocumentsGlobal(ctx context.Context) ([]store.DocumentInfo, error) {
	var q string
	var args []any

	if !store.IsCrossTenant(ctx) {
		tid, err := requireTenantID(ctx)
		if err != nil {
			return nil, err
		}
		q = `SELECT agent_id, path, hash, user_id, updated_at
			 FROM memory_documents WHERE tenant_id = ? ORDER BY updated_at DESC`
		args = []any{tid.String()}
	} else {
		q = `SELECT agent_id, path, hash, user_id, updated_at
			 FROM memory_documents ORDER BY updated_at DESC`
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.DocumentInfo
	for rows.Next() {
		var agentID, path, hash string
		var uid *string
		var updatedAt time.Time
		if err := rows.Scan(&agentID, &path, &hash, &uid, &updatedAt); err != nil {
			continue
		}
		info := scanDocumentRow(path, hash, uid, updatedAt)
		info.AgentID = agentID
		result = append(result, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ListAllDocuments returns all documents for an agent across all users.
func (s *SQLiteMemoryStore) ListAllDocuments(ctx context.Context, agentID string) ([]store.DocumentInfo, error) {
	tc, tcArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id, path, hash, user_id, updated_at
		 FROM memory_documents WHERE agent_id = ?`+tc+`
		 ORDER BY updated_at DESC`,
		append([]any{agentID}, tcArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.DocumentInfo
	for rows.Next() {
		var aID, path, hash string
		var uid *string
		var updatedAt time.Time
		if err := rows.Scan(&aID, &path, &hash, &uid, &updatedAt); err != nil {
			continue
		}
		info := scanDocumentRow(path, hash, uid, updatedAt)
		info.AgentID = aID
		result = append(result, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetDocumentDetail returns full document info with chunk count.
// EmbeddedCount is always 0 (no embedding column in SQLite).
func (s *SQLiteMemoryStore) GetDocumentDetail(ctx context.Context, agentID, userID, path string) (*store.DocumentDetail, error) {
	var q string
	var args []any

	if userID == "" {
		tc, tcArgs, err := scopeClauseAlias(ctx, "d")
		if err != nil {
			return nil, err
		}
		q = `SELECT d.path, d.content, d.hash, d.user_id, d.created_at, d.updated_at,
				COUNT(c.id) AS chunk_count
			 FROM memory_documents d
			 LEFT JOIN memory_chunks c ON c.document_id = d.id
			 WHERE d.agent_id = ? AND d.path = ? AND d.user_id IS NULL` + tc + `
			 GROUP BY d.id`
		args = append([]any{agentID, path}, tcArgs...)
	} else {
		tc, tcArgs, err := scopeClauseAlias(ctx, "d")
		if err != nil {
			return nil, err
		}
		q = `SELECT d.path, d.content, d.hash, d.user_id, d.created_at, d.updated_at,
				COUNT(c.id) AS chunk_count
			 FROM memory_documents d
			 LEFT JOIN memory_chunks c ON c.document_id = d.id
			 WHERE d.agent_id = ? AND d.path = ? AND d.user_id = ?` + tc + `
			 GROUP BY d.id`
		args = append([]any{agentID, path, userID}, tcArgs...)
	}

	var detail store.DocumentDetail
	var uid *string
	var createdAt, updatedAt time.Time
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&detail.Path, &detail.Content, &detail.Hash, &uid,
		&createdAt, &updatedAt, &detail.ChunkCount,
	)
	if err != nil {
		return nil, err
	}
	if uid != nil {
		detail.UserID = *uid
	}
	detail.CreatedAt = createdAt.UnixMilli()
	detail.UpdatedAt = updatedAt.UnixMilli()
	// EmbeddedCount always 0 — no embedding column in SQLite
	return &detail, nil
}

// ListChunks returns chunks for a document identified by agent, user, and path.
func (s *SQLiteMemoryStore) ListChunks(ctx context.Context, agentID, userID, path string) ([]store.ChunkInfo, error) {
	var q string
	var args []any

	if userID == "" {
		tc, tcArgs, err := scopeClauseAlias(ctx, "d")
		if err != nil {
			return nil, err
		}
		q = `SELECT c.id, c.start_line, c.end_line, c.text
			 FROM memory_chunks c
			 JOIN memory_documents d ON c.document_id = d.id
			 WHERE d.agent_id = ? AND d.path = ? AND d.user_id IS NULL` + tc + `
			 ORDER BY c.start_line`
		args = append([]any{agentID, path}, tcArgs...)
	} else {
		tc, tcArgs, err := scopeClauseAlias(ctx, "d")
		if err != nil {
			return nil, err
		}
		q = `SELECT c.id, c.start_line, c.end_line, c.text
			 FROM memory_chunks c
			 JOIN memory_documents d ON c.document_id = d.id
			 WHERE d.agent_id = ? AND d.path = ? AND d.user_id = ?` + tc + `
			 ORDER BY c.start_line`
		args = append([]any{agentID, path, userID}, tcArgs...)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ChunkInfo
	for rows.Next() {
		var ci store.ChunkInfo
		if err := rows.Scan(&ci.ID, &ci.StartLine, &ci.EndLine, &ci.TextPreview); err != nil {
			continue
		}
		// HasEmbedding always false — no embedding column in SQLite
		result = append(result, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
