package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// appendTeamFilter appends the team_id clause to a vault query.
// TeamIDs (personal + listed teams) takes precedence over TeamID (single team / personal-only).
func appendTeamFilter(q string, args []any, p int, teamID *string, teamIDs []string) (string, []any, int) {
	if len(teamIDs) > 0 {
		ph := make([]string, len(teamIDs))
		for i, id := range teamIDs {
			ph[i] = fmt.Sprintf("$%d", p)
			args = append(args, mustParseUUID(id))
			p++
		}
		q += " AND (team_id IS NULL OR team_id IN (" + strings.Join(ph, ",") + "))"
	} else if teamID != nil {
		if *teamID != "" {
			q += fmt.Sprintf(" AND team_id = $%d", p)
			args = append(args, mustParseUUID(*teamID))
			p++
		} else {
			q += " AND team_id IS NULL"
		}
	}
	return q, args, p
}

// PGVaultStore implements store.VaultStore backed by PostgreSQL.
type PGVaultStore struct {
	db          *sql.DB
	embProvider store.EmbeddingProvider
}

// NewPGVaultStore creates a new PG-backed vault store.
func NewPGVaultStore(db *sql.DB) *PGVaultStore {
	return &PGVaultStore{db: db}
}

func (s *PGVaultStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.embProvider = provider
}

func (s *PGVaultStore) Close() error { return nil }

// optAgentUUID converts a nullable *string agent_id to *uuid.UUID for SQL.
func optAgentUUID(agentID *string) *uuid.UUID {
	if agentID == nil || *agentID == "" {
		return nil
	}
	u := mustParseUUID(*agentID)
	return &u
}

// UpsertDocument inserts or updates a vault document.
func (s *PGVaultStore) UpsertDocument(ctx context.Context, doc *store.VaultDocument) error {
	tid := mustParseUUID(doc.TenantID)
	aid := optAgentUUID(doc.AgentID)
	now := time.Now().UTC()

	meta, err := json.Marshal(doc.Metadata)
	if err != nil {
		meta = []byte("{}")
	}

	id := uuid.Must(uuid.NewV7())
	var embStr *string
	if s.embProvider != nil && doc.Summary != "" {
		// Embed title + path + summary for richer vector search.
		embedText := doc.Title + " " + doc.Path
		if doc.Summary != "" {
			embedText += " " + doc.Summary
		}
		vecs, embErr := s.embProvider.Embed(ctx, []string{embedText})
		if embErr == nil && len(vecs) > 0 {
			v := vectorToString(vecs[0])
			embStr = &v
		}
	}

	var teamID *uuid.UUID
	if doc.TeamID != nil && *doc.TeamID != "" {
		t := mustParseUUID(*doc.TeamID)
		teamID = &t
	}

	var actualID uuid.UUID
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO vault_documents
			(id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, embedding, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14)
		ON CONFLICT (tenant_id, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(team_id, '00000000-0000-0000-0000-000000000000'::uuid), scope, path) DO UPDATE SET
			title        = EXCLUDED.title,
			doc_type     = EXCLUDED.doc_type,
			content_hash = EXCLUDED.content_hash,
			summary      = EXCLUDED.summary,
			embedding    = COALESCE(EXCLUDED.embedding, vault_documents.embedding),
			metadata     = EXCLUDED.metadata,
			tenant_id    = EXCLUDED.tenant_id,
			updated_at   = EXCLUDED.updated_at
		RETURNING id`,
		id, tid, aid, teamID, doc.Scope, doc.CustomScope, doc.Path, doc.Title, doc.DocType,
		doc.ContentHash, doc.Summary, embStr, meta, now,
	).Scan(&actualID)
	if err != nil {
		return fmt.Errorf("vault upsert document: %w", err)
	}
	doc.ID = actualID.String()
	return nil
}

// GetDocument retrieves a vault document by tenant, agent, and path.
// Empty agentID means no agent filter (match any agent).
// Team scoping via RunContext: present+TeamID → filter; present+empty → personal; nil → any match.
func (s *PGVaultStore) GetDocument(ctx context.Context, tenantID, agentID, path string) (*store.VaultDocument, error) {
	tid := mustParseUUID(tenantID)

	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents WHERE tenant_id = $1 AND path = $2`
	args := []any{tid, path}
	p := 3

	if agentID != "" {
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, mustParseUUID(agentID))
		p++
	}

	if rc := store.RunContextFromCtx(ctx); rc != nil {
		if rc.TeamID != "" {
			q += fmt.Sprintf(" AND team_id = $%d", p)
			args = append(args, mustParseUUID(rc.TeamID))
		} else {
			q += " AND team_id IS NULL"
		}
	}

	var row vaultDocRow
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&row.ID, &row.TenantID, &row.AgentID, &row.TeamID, &row.Scope, &row.CustomScope,
		&row.Path, &row.Title, &row.DocType, &row.ContentHash, &row.Summary,
		&row.MetaJSON, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	doc := row.toVaultDocument()
	return &doc, nil
}

// GetDocumentByID retrieves a vault document by ID with tenant isolation.
func (s *PGVaultStore) GetDocumentByID(ctx context.Context, tenantID, id string) (*store.VaultDocument, error) {
	uid := mustParseUUID(id)
	tid := mustParseUUID(tenantID)
	var row vaultDocRow
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents WHERE id = $1 AND tenant_id = $2`, uid, tid,
	).Scan(&row.ID, &row.TenantID, &row.AgentID, &row.TeamID, &row.Scope, &row.CustomScope,
		&row.Path, &row.Title, &row.DocType, &row.ContentHash, &row.Summary,
		&row.MetaJSON, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	doc := row.toVaultDocument()
	return &doc, nil
}

// GetDocumentsByIDs returns documents matching the given IDs with tenant isolation.
// Chunks by 500 to stay within PG param limits.
func (s *PGVaultStore) GetDocumentsByIDs(ctx context.Context, tenantID string, docIDs []string) ([]store.VaultDocument, error) {
	if len(docIDs) == 0 {
		return nil, nil
	}
	tid := mustParseUUID(tenantID)
	const chunkSize = 500
	var all []store.VaultDocument
	for start := 0; start < len(docIDs); start += chunkSize {
		end := min(start+chunkSize, len(docIDs))
		var scanned []vaultDocRow
		if err := pkgSqlxDB.SelectContext(ctx, &scanned,
			`SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at
			 FROM vault_documents WHERE id = ANY($1) AND tenant_id = $2`,
			pqStringArray(docIDs[start:end]), tid); err != nil {
			return nil, err
		}
		for i := range scanned {
			all = append(all, scanned[i].toVaultDocument())
		}
	}
	return all, nil
}

// GetDocumentByBasename finds a document by path basename (case-insensitive).
// Uses the stored generated column path_basename + index for fast lookup.
func (s *PGVaultStore) GetDocumentByBasename(ctx context.Context, tenantID, agentID, basename string) (*store.VaultDocument, error) {
	tid := mustParseUUID(tenantID)
	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents
		WHERE tenant_id = $1 AND path_basename = lower($2)`
	args := []any{tid, basename}
	p := 3
	if agentID != "" {
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, mustParseUUID(agentID))
	}
	q += " LIMIT 1"
	var row vaultDocRow
	err := s.db.QueryRowContext(ctx, q, args...).Scan(
		&row.ID, &row.TenantID, &row.AgentID, &row.TeamID, &row.Scope, &row.CustomScope,
		&row.Path, &row.Title, &row.DocType, &row.ContentHash, &row.Summary,
		&row.MetaJSON, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	doc := row.toVaultDocument()
	return &doc, nil
}

// DeleteDocument removes a vault document by tenant, agent, and path.
// Empty agentID means no agent filter.
// Team scoping via RunContext (same rules as GetDocument).
func (s *PGVaultStore) DeleteDocument(ctx context.Context, tenantID, agentID, path string) error {
	tid := mustParseUUID(tenantID)

	q := `DELETE FROM vault_documents WHERE tenant_id = $1 AND path = $2`
	args := []any{tid, path}
	p := 3

	if agentID != "" {
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, mustParseUUID(agentID))
		p++
	}

	if rc := store.RunContextFromCtx(ctx); rc != nil {
		if rc.TeamID != "" {
			q += fmt.Sprintf(" AND team_id = $%d", p)
			args = append(args, mustParseUUID(rc.TeamID))
		} else {
			q += " AND team_id IS NULL"
		}
	}

	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// ListDocuments returns vault documents for a tenant+agent with optional filters.
func (s *PGVaultStore) ListDocuments(ctx context.Context, tenantID, agentID string, opts store.VaultListOptions) ([]store.VaultDocument, error) {
	tid := mustParseUUID(tenantID)

	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents WHERE tenant_id = $1`
	args := []any{tid}
	p := 2

	// Agent filter is optional — omit for cross-agent listing.
	if agentID != "" {
		aid := mustParseUUID(agentID)
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, aid)
		p++
	}

	q, args, p = appendTeamFilter(q, args, p, opts.TeamID, opts.TeamIDs)

	if opts.Scope != "" {
		q += fmt.Sprintf(" AND scope = $%d", p)
		args = append(args, opts.Scope)
		p++
	}
	if len(opts.DocTypes) > 0 {
		q += fmt.Sprintf(" AND doc_type = ANY($%d)", p)
		args = append(args, pqStringArray(opts.DocTypes))
		p++
	}

	q += " ORDER BY updated_at DESC"
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	q += fmt.Sprintf(" LIMIT $%d", p)
	args = append(args, limit)
	p++
	if opts.Offset > 0 {
		q += fmt.Sprintf(" OFFSET $%d", p)
		args = append(args, opts.Offset)
	}

	var scanned []vaultDocRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, q, args...); err != nil {
		return nil, err
	}

	docs := make([]store.VaultDocument, 0, len(scanned))
	for i := range scanned {
		docs = append(docs, scanned[i].toVaultDocument())
	}
	return docs, nil
}

// CountDocuments returns the total number of vault documents matching the given filters.
func (s *PGVaultStore) CountDocuments(ctx context.Context, tenantID, agentID string, opts store.VaultListOptions) (int, error) {
	tid := mustParseUUID(tenantID)

	q := `SELECT COUNT(*) FROM vault_documents WHERE tenant_id = $1`
	args := []any{tid}
	p := 2

	if agentID != "" {
		aid := mustParseUUID(agentID)
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, aid)
		p++
	}
	q, args, p = appendTeamFilter(q, args, p, opts.TeamID, opts.TeamIDs)
	if opts.Scope != "" {
		q += fmt.Sprintf(" AND scope = $%d", p)
		args = append(args, opts.Scope)
		p++
	}
	if len(opts.DocTypes) > 0 {
		q += fmt.Sprintf(" AND doc_type = ANY($%d)", p)
		args = append(args, pqStringArray(opts.DocTypes))
	}

	var count int
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("vault count documents: %w", err)
	}
	return count, nil
}

// UpdateHash updates the content hash for a vault document with tenant isolation.
func (s *PGVaultStore) UpdateHash(ctx context.Context, tenantID, id, newHash string) error {
	uid := mustParseUUID(id)
	tid := mustParseUUID(tenantID)
	_, err := s.db.ExecContext(ctx,
		`UPDATE vault_documents SET content_hash = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`,
		newHash, time.Now().UTC(), uid, tid)
	return err
}

// UpdateSummaryAndReembed updates summary and re-generates embedding from title+path+summary.
// UpdateSummaryAndReembed and FindSimilarDocs moved to vault_documents_enrichment.go.

// Search performs hybrid FTS + vector search on vault_documents.
func (s *PGVaultStore) Search(ctx context.Context, opts store.VaultSearchOptions) ([]store.VaultSearchResult, error) {
	tid := mustParseUUID(opts.TenantID)
	aid := optAgentUUID(&opts.AgentID) // empty string → nil → no agent filter

	// Build team filter for search sub-queries.
	tf := buildSearchTeamFilter(opts.TeamID, opts.TeamIDs)

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	// FTS search
	ftsResults, err := s.ftsSearch(ctx, opts.Query, tid, aid, tf, opts.Scope, opts.DocTypes, maxResults*2)
	if err != nil {
		return nil, err
	}

	// Vector search if provider available
	var vecResults []store.VaultSearchResult
	if s.embProvider != nil {
		vecs, embErr := s.embProvider.Embed(ctx, []string{opts.Query})
		if embErr == nil && len(vecs) > 0 {
			var vecErr error
			vecResults, vecErr = s.vectorSearch(ctx, vecs[0], tid, aid, tf, opts.Scope, opts.DocTypes, maxResults*2)
			if vecErr != nil {
				slog.Debug("vault.vector_search_fallback", "err", vecErr)
				vecResults = nil
			}
		}
	}

	// Merge: FTS weight 0.4, vector weight 0.6
	merged := s.mergeResults(ftsResults, vecResults, 0.4, 0.6, maxResults)

	// Apply min score filter
	if opts.MinScore > 0 {
		var filtered []store.VaultSearchResult
		for _, r := range merged {
			if r.Score >= opts.MinScore {
				filtered = append(filtered, r)
			}
		}
		return filtered, nil
	}
	return merged, nil
}

// searchTeamFilter holds pre-parsed team filter info for search sub-queries.
type searchTeamFilter struct {
	teamIDs []uuid.UUID // personal + these teams (len > 0 = multi-team mode)
	teamID  *uuid.UUID  // single team filter (nil + active = personal-only)
	active  bool        // whether any team filter is applied
}

func buildSearchTeamFilter(teamID *string, teamIDs []string) searchTeamFilter {
	if len(teamIDs) > 0 {
		uuids := make([]uuid.UUID, len(teamIDs))
		for i, id := range teamIDs {
			uuids[i] = mustParseUUID(id)
		}
		return searchTeamFilter{teamIDs: uuids, active: true}
	}
	if teamID != nil {
		if *teamID != "" {
			t := mustParseUUID(*teamID)
			return searchTeamFilter{teamID: &t, active: true}
		}
		return searchTeamFilter{active: true} // personal-only
	}
	return searchTeamFilter{} // no filter
}

// appendSearchTeamClause appends team filter SQL to a search query.
func (tf searchTeamFilter) append(q string, args []any, p int) (string, []any, int) {
	if !tf.active {
		return q, args, p
	}
	if len(tf.teamIDs) > 0 {
		ph := make([]string, len(tf.teamIDs))
		for i, id := range tf.teamIDs {
			ph[i] = fmt.Sprintf("$%d", p)
			args = append(args, id)
			p++
		}
		q += " AND (team_id IS NULL OR team_id IN (" + strings.Join(ph, ",") + "))"
	} else if tf.teamID != nil {
		q += fmt.Sprintf(" AND team_id = $%d", p)
		args = append(args, *tf.teamID)
		p++
	} else {
		q += " AND team_id IS NULL"
	}
	return q, args, p
}

func (s *PGVaultStore) ftsSearch(ctx context.Context, query string, tenantID uuid.UUID, agentID *uuid.UUID, tf searchTeamFilter, scope string, docTypes []string, limit int) ([]store.VaultSearchResult, error) {
	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at,
			ts_rank(tsv, plainto_tsquery('simple', $1)) AS score
		FROM vault_documents
		WHERE tenant_id = $2 AND tsv @@ plainto_tsquery('simple', $1)`
	args := []any{query, tenantID}
	p := 3

	if agentID != nil {
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, *agentID)
		p++
	}

	q, args, p = tf.append(q, args, p)

	if scope != "" {
		q += fmt.Sprintf(" AND scope = $%d", p)
		args = append(args, scope)
		p++
	}
	if len(docTypes) > 0 {
		q += fmt.Sprintf(" AND doc_type = ANY($%d)", p)
		args = append(args, pqStringArray(docTypes))
		p++
	}

	q += fmt.Sprintf(" ORDER BY score DESC LIMIT $%d", p)
	args = append(args, limit)

	var scanned []vaultSearchRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, q, args...); err != nil {
		return nil, err
	}
	return vaultSearchRowsToResults(scanned, "vault"), nil
}

func (s *PGVaultStore) vectorSearch(ctx context.Context, embedding []float32, tenantID uuid.UUID, agentID *uuid.UUID, tf searchTeamFilter, scope string, docTypes []string, limit int) ([]store.VaultSearchResult, error) {
	vecStr := vectorToString(embedding)
	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at,
			1 - (embedding <=> $1) AS score
		FROM vault_documents
		WHERE tenant_id = $2 AND embedding IS NOT NULL`
	args := []any{vecStr, tenantID}
	p := 3

	if agentID != nil {
		q += fmt.Sprintf(" AND agent_id = $%d", p)
		args = append(args, *agentID)
		p++
	}

	q, args, p = tf.append(q, args, p)

	if scope != "" {
		q += fmt.Sprintf(" AND scope = $%d", p)
		args = append(args, scope)
		p++
	}
	if len(docTypes) > 0 {
		q += fmt.Sprintf(" AND doc_type = ANY($%d)", p)
		args = append(args, pqStringArray(docTypes))
		p++
	}

	q += fmt.Sprintf(" ORDER BY embedding <=> $1 LIMIT $%d", p)
	args = append(args, limit)

	var scanned []vaultSearchRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, q, args...); err != nil {
		return nil, err
	}
	return vaultSearchRowsToResults(scanned, "vault"), nil
}

// vaultSearchRowsToResults converts a slice of vaultSearchRow to store.VaultSearchResult.
func vaultSearchRowsToResults(rows []vaultSearchRow, source string) []store.VaultSearchResult {
	results := make([]store.VaultSearchResult, 0, len(rows))
	for i := range rows {
		results = append(results, rows[i].toVaultSearchResult(source))
	}
	return results
}

// mergeResults combines FTS and vector results with weighted scoring.
func (s *PGVaultStore) mergeResults(fts, vec []store.VaultSearchResult, ftsW, vecW float64, maxResults int) []store.VaultSearchResult {
	seen := make(map[string]*store.VaultSearchResult)

	// Normalize FTS scores
	var maxFTS float64
	for _, r := range fts {
		if r.Score > maxFTS {
			maxFTS = r.Score
		}
	}
	for _, r := range fts {
		norm := r.Score
		if maxFTS > 0 {
			norm = r.Score / maxFTS
		}
		r.Score = norm * ftsW
		seen[r.Document.ID] = &r
	}

	// Normalize vector scores and merge
	var maxVec float64
	for _, r := range vec {
		if r.Score > maxVec {
			maxVec = r.Score
		}
	}
	for _, r := range vec {
		norm := r.Score
		if maxVec > 0 {
			norm = r.Score / maxVec
		}
		if existing, ok := seen[r.Document.ID]; ok {
			existing.Score += norm * vecW
		} else {
			r.Score = norm * vecW
			seen[r.Document.ID] = &r
		}
	}

	// Collect and sort
	results := make([]store.VaultSearchResult, 0, len(seen))
	for _, r := range seen {
		results = append(results, *r)
	}
	// Sort descending by score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

