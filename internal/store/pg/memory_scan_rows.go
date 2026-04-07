package pg

import (
	"time"

	"github.com/lib/pq"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// documentInfoRow is an sqlx scan struct for memory_documents SELECT queries.
// Handles TIMESTAMPTZ→int64 (UnixMilli) and nullable user_id conversion.
type documentInfoRow struct {
	AgentID   string    `db:"agent_id"`
	Path      string    `db:"path"`
	Hash      string    `db:"hash"`
	UserID    *string   `db:"user_id"`
	UpdatedAt time.Time `db:"updated_at"`
}

func (r *documentInfoRow) toDocumentInfo() store.DocumentInfo {
	info := store.DocumentInfo{
		AgentID:   r.AgentID,
		Path:      r.Path,
		Hash:      r.Hash,
		UpdatedAt: r.UpdatedAt.UnixMilli(),
	}
	if r.UserID != nil {
		info.UserID = *r.UserID
	}
	return info
}

// documentDetailRow is an sqlx scan struct for the GetDocumentDetail query.
type documentDetailRow struct {
	Path          string    `db:"path"`
	Content       string    `db:"content"`
	Hash          string    `db:"hash"`
	UserID        *string   `db:"user_id"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
	ChunkCount    int       `db:"chunk_count"`
	EmbeddedCount int       `db:"embedded_count"`
}

func (r *documentDetailRow) toDocumentDetail() store.DocumentDetail {
	d := store.DocumentDetail{
		Path:          r.Path,
		Content:       r.Content,
		Hash:          r.Hash,
		ChunkCount:    r.ChunkCount,
		EmbeddedCount: r.EmbeddedCount,
		CreatedAt:     r.CreatedAt.UnixMilli(),
		UpdatedAt:     r.UpdatedAt.UnixMilli(),
	}
	if r.UserID != nil {
		d.UserID = *r.UserID
	}
	return d
}

// chunkInfoRow is an sqlx scan struct for memory_chunks SELECT queries.
// All fields are directly compatible with store.ChunkInfo.
type chunkInfoRow struct {
	ID           string `db:"id"`
	StartLine    int    `db:"start_line"`
	EndLine      int    `db:"end_line"`
	TextPreview  string `db:"text_preview"`
	HasEmbedding bool   `db:"has_embedding"`
}

func (r *chunkInfoRow) toChunkInfo() store.ChunkInfo {
	return store.ChunkInfo{
		ID:           r.ID,
		StartLine:    r.StartLine,
		EndLine:      r.EndLine,
		TextPreview:  r.TextPreview,
		HasEmbedding: r.HasEmbedding,
	}
}

// scoredChunkRow is an sqlx scan struct for ftsSearch/vectorSearch queries in memory_search.go.
type scoredChunkRow struct {
	Path      string  `db:"path"`
	StartLine int     `db:"start_line"`
	EndLine   int     `db:"end_line"`
	Text      string  `db:"text"`
	UserID    *string `db:"user_id"`
	Score     float64 `db:"score"`
}

func (r *scoredChunkRow) toScoredChunk() scoredChunk {
	return scoredChunk{
		Path:      r.Path,
		StartLine: r.StartLine,
		EndLine:   r.EndLine,
		Text:      r.Text,
		Score:     r.Score,
		UserID:    r.UserID,
	}
}

// episodicSummaryRow is an sqlx scan struct for episodic_summaries SELECT queries.
// Handles TEXT[] key_topics via pq.StringArray.
type episodicSummaryRow struct {
	ID         string          `db:"id"`
	TenantID   string          `db:"tenant_id"`
	AgentID    string          `db:"agent_id"`
	UserID     string          `db:"user_id"`
	SessionKey string          `db:"session_key"`
	Summary    string          `db:"summary"`
	KeyTopics  pq.StringArray `db:"key_topics"`
	TurnCount  int             `db:"turn_count"`
	TokenCount int             `db:"token_count"`
	L0Abstract string          `db:"l0_abstract"`
	SourceID   string          `db:"source_id"`
	SourceType string          `db:"source_type"`
	CreatedAt  time.Time       `db:"created_at"`
	ExpiresAt  *time.Time      `db:"expires_at"`
}

func (r *episodicSummaryRow) toEpisodicSummary() store.EpisodicSummary {
	ep := store.EpisodicSummary{
		UserID:     r.UserID,
		SessionKey: r.SessionKey,
		Summary:    r.Summary,
		TurnCount:  r.TurnCount,
		TokenCount: r.TokenCount,
		L0Abstract: r.L0Abstract,
		SourceID:   r.SourceID,
		SourceType: r.SourceType,
		CreatedAt:  r.CreatedAt,
		ExpiresAt:  r.ExpiresAt,
	}
	_ = ep.ID.Scan(r.ID)
	_ = ep.TenantID.Scan(r.TenantID)
	_ = ep.AgentID.Scan(r.AgentID)
	ep.KeyTopics = []string(r.KeyTopics)
	return ep
}

// episodicScoredRow is an sqlx scan struct for ftsSearch/vectorSearch in episodic_search.go.
type episodicScoredRow struct {
	ID         string    `db:"id"`
	SessionKey string    `db:"session_key"`
	L0Abstract string    `db:"l0_abstract"`
	Score      float64   `db:"score"`
	CreatedAt  time.Time `db:"created_at"`
}

func (r *episodicScoredRow) toEpisodicScored() episodicScored {
	return episodicScored{
		id:         r.ID,
		sessionKey: r.SessionKey,
		l0:         r.L0Abstract,
		score:      r.Score,
		createdAt:  r.CreatedAt,
	}
}
