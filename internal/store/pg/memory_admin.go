package pg

import (
	"context"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListAllDocumentsGlobal returns all documents across all agents (for admin overview).
func (s *PGMemoryStore) ListAllDocumentsGlobal(ctx context.Context) ([]store.DocumentInfo, error) {
	var whereClause string
	var args []any
	if !store.IsCrossTenant(ctx) {
		tid, err := requireTenantID(ctx)
		if err != nil {
			return nil, err
		}
		whereClause = "WHERE tenant_id = $1"
		args = []any{tid}
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id, path, hash, user_id, updated_at
		 FROM memory_documents `+whereClause+`
		 ORDER BY updated_at DESC`, args...)
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
		info := store.DocumentInfo{
			AgentID:   agentID,
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

// ListAllDocuments returns all documents for an agent across all users (global + personal).
func (s *PGMemoryStore) ListAllDocuments(ctx context.Context, agentID string) ([]store.DocumentInfo, error) {
	aid := mustParseUUID(agentID)
	tc, tcArgs, err := tenantClauseN(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id, path, hash, user_id, updated_at
		 FROM memory_documents WHERE agent_id = $1`+tc+`
		 ORDER BY updated_at DESC`, append([]any{aid}, tcArgs...)...)
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
		info := store.DocumentInfo{
			AgentID:   aID,
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

// GetDocumentDetail returns full document info with chunk and embedding counts.
func (s *PGMemoryStore) GetDocumentDetail(ctx context.Context, agentID, userID, path string) (*store.DocumentDetail, error) {
	aid := mustParseUUID(agentID)

	var q string
	var args []any
	if userID == "" {
		tc, tcArgs, err := tenantClauseN(ctx, 3)
		if err != nil {
			return nil, err
		}
		q = `SELECT d.path, d.content, d.hash, d.user_id, d.created_at, d.updated_at,
				COUNT(c.id) AS chunk_count,
				COUNT(c.embedding) AS embedded_count
			 FROM memory_documents d
			 LEFT JOIN memory_chunks c ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id IS NULL` + tc + `
			 GROUP BY d.id`
		args = append([]any{aid, path}, tcArgs...)
	} else {
		tc, tcArgs, err := tenantClauseN(ctx, 4)
		if err != nil {
			return nil, err
		}
		q = `SELECT d.path, d.content, d.hash, d.user_id, d.created_at, d.updated_at,
				COUNT(c.id) AS chunk_count,
				COUNT(c.embedding) AS embedded_count
			 FROM memory_documents d
			 LEFT JOIN memory_chunks c ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id = $3` + tc + `
			 GROUP BY d.id`
		args = append([]any{aid, path, userID}, tcArgs...)
	}

	var detail store.DocumentDetail
	var uid *string
	var createdAt, updatedAt time.Time
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&detail.Path, &detail.Content, &detail.Hash, &uid,
		&createdAt, &updatedAt,
		&detail.ChunkCount, &detail.EmbeddedCount,
	)
	if err != nil {
		return nil, err
	}
	if uid != nil {
		detail.UserID = *uid
	}
	detail.CreatedAt = createdAt.UnixMilli()
	detail.UpdatedAt = updatedAt.UnixMilli()
	return &detail, nil
}

// ListChunks returns chunks for a document identified by agent, user, and path.
func (s *PGMemoryStore) ListChunks(ctx context.Context, agentID, userID, path string) ([]store.ChunkInfo, error) {
	aid := mustParseUUID(agentID)

	var q string
	var args []any
	if userID == "" {
		tc, tcArgs, err := tenantClauseN(ctx, 3)
		if err != nil {
			return nil, err
		}
		q = `SELECT c.id, c.start_line, c.end_line,
				c.text AS text_preview,
				(c.embedding IS NOT NULL) AS has_embedding
			 FROM memory_chunks c
			 JOIN memory_documents d ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id IS NULL` + tc + `
			 ORDER BY c.start_line`
		args = append([]any{aid, path}, tcArgs...)
	} else {
		tc, tcArgs, err := tenantClauseN(ctx, 4)
		if err != nil {
			return nil, err
		}
		q = `SELECT c.id, c.start_line, c.end_line,
				c.text AS text_preview,
				(c.embedding IS NOT NULL) AS has_embedding
			 FROM memory_chunks c
			 JOIN memory_documents d ON c.document_id = d.id
			 WHERE d.agent_id = $1 AND d.path = $2 AND d.user_id = $3` + tc + `
			 ORDER BY c.start_line`
		args = append([]any{aid, path, userID}, tcArgs...)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ChunkInfo
	for rows.Next() {
		var ci store.ChunkInfo
		if err := rows.Scan(&ci.ID, &ci.StartLine, &ci.EndLine, &ci.TextPreview, &ci.HasEmbedding); err != nil {
			continue
		}
		result = append(result, ci)
	}
	return result, nil
}
