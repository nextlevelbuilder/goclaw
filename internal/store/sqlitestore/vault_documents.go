//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// sqliteAppendTeamFilter appends the team_id clause to a vault query.
func sqliteAppendTeamFilter(q string, args []any, teamID *string, teamIDs []string) (string, []any) {
	if len(teamIDs) > 0 {
		ph := strings.Repeat("?,", len(teamIDs)-1) + "?"
		q += " AND (team_id IS NULL OR team_id IN (" + ph + "))"
		for _, id := range teamIDs {
			args = append(args, id)
		}
	} else if teamID != nil {
		if *teamID != "" {
			q += " AND team_id = ?"
			args = append(args, *teamID)
		} else {
			q += " AND team_id IS NULL"
		}
	}
	return q, args
}

// SQLiteVaultStore implements store.VaultStore backed by SQLite.
type SQLiteVaultStore struct {
	db *sql.DB
}

// NewSQLiteVaultStore creates a new SQLite-backed vault store.
func NewSQLiteVaultStore(db *sql.DB) *SQLiteVaultStore {
	return &SQLiteVaultStore{db: db}
}

func (s *SQLiteVaultStore) SetEmbeddingProvider(_ store.EmbeddingProvider) {} // no-op
func (s *SQLiteVaultStore) Close() error                                   { return nil }

// UpsertDocument inserts or updates a vault document.
// Uses ON CONFLICT DO UPDATE (never INSERT OR REPLACE — preserves FK cascades to vault_links).
func (s *SQLiteVaultStore) UpsertDocument(ctx context.Context, doc *store.VaultDocument) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := uuid.Must(uuid.NewV7()).String()

	meta, err := json.Marshal(doc.Metadata)
	if err != nil {
		meta = []byte("{}")
	}

	// Convert nullable *string AgentID to nil for SQL.
	var agentIDVal any
	if doc.AgentID != nil && *doc.AgentID != "" {
		agentIDVal = *doc.AgentID
	}
	// SQLite has no GENERATED column equivalent via modernc driver; compute
	// the basename app-side. PG auto-populates via GENERATED so this is a
	// no-op on PG callers that share the struct.
	if doc.PathBasename == "" && doc.Path != "" {
		doc.PathBasename = ComputeAttachmentBaseName(doc.Path)
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO vault_documents
			(id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, COALESCE(agent_id,''), COALESCE(team_id,''), scope, path) DO UPDATE SET
			path_basename = excluded.path_basename,
			title         = excluded.title,
			doc_type      = excluded.doc_type,
			content_hash  = excluded.content_hash,
			summary       = excluded.summary,
			metadata      = excluded.metadata,
			tenant_id     = excluded.tenant_id,
			updated_at    = excluded.updated_at
		RETURNING id`,
		id, doc.TenantID, agentIDVal, doc.TeamID, doc.Scope, doc.CustomScope,
		doc.Path, doc.PathBasename, doc.Title, doc.DocType, doc.ContentHash, doc.Summary, string(meta), now, now,
	).Scan(&doc.ID)
	if err != nil {
		return fmt.Errorf("vault upsert document: %w", err)
	}
	return nil
}

// GetDocument retrieves a vault document by tenant, agent, and path.
// Empty agentID means no agent filter.
// Team scoping via RunContext: present+TeamID → filter; present+empty → personal; nil → any match.
func (s *SQLiteVaultStore) GetDocument(ctx context.Context, tenantID, agentID, path string) (*store.VaultDocument, error) {
	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents WHERE tenant_id = ? AND path = ?`
	args := []any{tenantID, path}

	if agentID != "" {
		q += " AND agent_id = ?"
		args = append(args, agentID)
	}

	if rc := store.RunContextFromCtx(ctx); rc != nil {
		if rc.TeamID != "" {
			q += " AND team_id = ?"
			args = append(args, rc.TeamID)
		} else {
			q += " AND team_id IS NULL"
		}
	}

	row := s.db.QueryRowContext(ctx, q, args...)
	return scanVaultDoc(row)
}

// GetDocumentByID retrieves a vault document by ID with tenant isolation.
func (s *SQLiteVaultStore) GetDocumentByID(ctx context.Context, tenantID, id string) (*store.VaultDocument, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents WHERE id = ? AND tenant_id = ?`, id, tenantID)
	return scanVaultDoc(row)
}

// GetDocumentsByIDs returns documents matching the given IDs with tenant isolation.
func (s *SQLiteVaultStore) GetDocumentsByIDs(ctx context.Context, tenantID string, docIDs []string) ([]store.VaultDocument, error) {
	if len(docIDs) == 0 {
		return nil, nil
	}
	const chunkSize = 500
	var all []store.VaultDocument
	for start := 0; start < len(docIDs); start += chunkSize {
		end := min(start+chunkSize, len(docIDs))
		chunk := docIDs[start:end]
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id
		}
		args = append(args, tenantID)
		q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at
			FROM vault_documents WHERE id IN (` + strings.Join(ph, ",") + `) AND tenant_id = ?`
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			doc, scanErr := scanVaultDocRow(rows)
			if scanErr != nil {
				rows.Close()
				return nil, scanErr
			}
			all = append(all, *doc)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// GetDocumentByBasename finds a document by path basename (case-insensitive).
func (s *SQLiteVaultStore) GetDocumentByBasename(ctx context.Context, tenantID, agentID, basename string) (*store.VaultDocument, error) {
	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents
		WHERE tenant_id = ? AND lower(replace(path, rtrim(path, replace(path, '/', '')), '')) = lower(?)`
	args := []any{tenantID, basename}
	if agentID != "" {
		q += " AND agent_id = ?"
		args = append(args, agentID)
	}
	q += " LIMIT 1"
	row := s.db.QueryRowContext(ctx, q, args...)
	return scanVaultDoc(row)
}

// DeleteDocument removes a vault document (FK cascades delete vault_links).
// Empty agentID means no agent filter.
// Team scoping via RunContext (same rules as GetDocument).
func (s *SQLiteVaultStore) DeleteDocument(ctx context.Context, tenantID, agentID, path string) error {
	q := `DELETE FROM vault_documents WHERE tenant_id = ? AND path = ?`
	args := []any{tenantID, path}

	if agentID != "" {
		q += " AND agent_id = ?"
		args = append(args, agentID)
	}

	if rc := store.RunContextFromCtx(ctx); rc != nil {
		if rc.TeamID != "" {
			q += " AND team_id = ?"
			args = append(args, rc.TeamID)
		} else {
			q += " AND team_id IS NULL"
		}
	}

	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// ListDocuments returns vault documents with optional scope/type filters.
func (s *SQLiteVaultStore) ListDocuments(ctx context.Context, tenantID, agentID string, opts store.VaultListOptions) ([]store.VaultDocument, error) {
	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents WHERE tenant_id = ?`
	args := []any{tenantID}

	if agentID != "" {
		q += " AND agent_id = ?"
		args = append(args, agentID)
	}
	q, args = sqliteAppendTeamFilter(q, args, opts.TeamID, opts.TeamIDs)
	if opts.Scope != "" {
		q += " AND scope = ?"
		args = append(args, opts.Scope)
	}
	if len(opts.DocTypes) > 0 {
		placeholders := strings.Repeat("?,", len(opts.DocTypes)-1) + "?"
		q += " AND doc_type IN (" + placeholders + ")"
		for _, dt := range opts.DocTypes {
			args = append(args, dt)
		}
	}

	q += " ORDER BY updated_at DESC"
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	q += " LIMIT ?"
	args = append(args, limit)
	if opts.Offset > 0 {
		q += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []store.VaultDocument
	for rows.Next() {
		doc, scanErr := scanVaultDocRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		docs = append(docs, *doc)
	}
	return docs, rows.Err()
}

// CountDocuments returns the total number of vault documents matching the given filters.
func (s *SQLiteVaultStore) CountDocuments(ctx context.Context, tenantID, agentID string, opts store.VaultListOptions) (int, error) {
	q := `SELECT COUNT(*) FROM vault_documents WHERE tenant_id = ?`
	args := []any{tenantID}

	if agentID != "" {
		q += " AND agent_id = ?"
		args = append(args, agentID)
	}
	q, args = sqliteAppendTeamFilter(q, args, opts.TeamID, opts.TeamIDs)
	if opts.Scope != "" {
		q += " AND scope = ?"
		args = append(args, opts.Scope)
	}
	if len(opts.DocTypes) > 0 {
		placeholders := strings.Repeat("?,", len(opts.DocTypes)-1) + "?"
		q += " AND doc_type IN (" + placeholders + ")"
		for _, dt := range opts.DocTypes {
			args = append(args, dt)
		}
	}

	var count int
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// UpdateHash updates the content hash for a vault document.
func (s *SQLiteVaultStore) UpdateHash(ctx context.Context, tenantID, id, newHash string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`UPDATE vault_documents SET content_hash = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		newHash, now, id, tenantID)
	return err
}

// ListUnenrichedDocs returns documents with empty summary for re-enrichment.
// limit=0 means no limit.
func (s *SQLiteVaultStore) ListUnenrichedDocs(ctx context.Context, tenantID string, limit int) ([]store.VaultDocument, error) {
	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents
		WHERE tenant_id = ? AND (summary IS NULL OR summary = '')
		ORDER BY created_at ASC`
	args := []any{tenantID}

	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []store.VaultDocument
	for rows.Next() {
		doc, scanErr := scanVaultDocRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		docs = append(docs, *doc)
	}
	return docs, rows.Err()
}

// UpdateSummaryAndReembed updates summary (no embedding in SQLite).
func (s *SQLiteVaultStore) UpdateSummaryAndReembed(ctx context.Context, tenantID, docID, summary string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`UPDATE vault_documents SET summary = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		summary, now, docID, tenantID)
	return err
}

// FindSimilarDocs is a no-op in SQLite (no vector support).
func (s *SQLiteVaultStore) FindSimilarDocs(ctx context.Context, tenantID, agentID, docID string, limit int) ([]store.VaultSearchResult, error) {
	return nil, nil
}

// Search performs LIKE-based search on vault documents (no FTS/vector in lite).
func (s *SQLiteVaultStore) Search(ctx context.Context, opts store.VaultSearchOptions) ([]store.VaultSearchResult, error) {
	query := opts.Query
	if len(query) > 500 {
		query = query[:500] // F10: query length cap
	}
	if query == "" {
		return nil, nil
	}

	pattern := "%" + escapeLike(query) + "%"
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	q := `SELECT id, tenant_id, agent_id, team_id, scope, custom_scope, path, path_basename, title, doc_type, content_hash, summary, metadata, created_at, updated_at
		FROM vault_documents
		WHERE tenant_id = ?
		  AND (title LIKE ? ESCAPE '\' OR path LIKE ? ESCAPE '\')`
	args := []any{opts.TenantID, pattern, pattern}

	if opts.AgentID != "" {
		q += " AND agent_id = ?"
		args = append(args, opts.AgentID)
	}

	q, args = sqliteAppendTeamFilter(q, args, opts.TeamID, opts.TeamIDs)

	q += " ORDER BY updated_at DESC LIMIT ?"
	args = append(args, maxResults*2)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lowerQuery := strings.ToLower(query)
	var results []store.VaultSearchResult
	for rows.Next() {
		doc, scanErr := scanVaultDocRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		// Post-query scoring
		score := 1.0
		if strings.Contains(strings.ToLower(doc.Title), lowerQuery) {
			score += 0.3
		}
		if strings.Contains(strings.ToLower(doc.Path), lowerQuery) {
			score += 0.1
		}
		if opts.MinScore > 0 && score < opts.MinScore {
			continue
		}
		results = append(results, store.VaultSearchResult{
			Document: *doc,
			Score:    score,
			Source:   "vault",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// ListTreeEntries returns immediate children (files + virtual folders) under the given path prefix.
func (s *SQLiteVaultStore) ListTreeEntries(ctx context.Context, tenantID string, opts store.VaultTreeOptions) ([]store.VaultTreeEntry, error) {
	prefix := opts.Path
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	fileQ := `SELECT id, path, title, doc_type, scope, updated_at FROM vault_documents WHERE tenant_id = ?`
	fileArgs := []any{tenantID}
	if prefix == "" {
		fileQ += " AND path NOT LIKE '%/%'"
	} else {
		fileQ += " AND path LIKE ? AND path NOT LIKE ?"
		fileArgs = append(fileArgs, prefix+"%", prefix+"%/%")
	}
	fileQ, fileArgs = sqliteAppendTreeFilters(fileQ, fileArgs, opts)
	fileQ += " ORDER BY path"

	deepQ := `SELECT DISTINCT path FROM vault_documents WHERE tenant_id = ?`
	deepArgs := []any{tenantID}
	if prefix == "" {
		deepQ += " AND path LIKE '%/%'"
	} else {
		deepQ += " AND path LIKE ?"
		deepArgs = append(deepArgs, prefix+"%/%")
	}
	deepQ, deepArgs = sqliteAppendTreeFilters(deepQ, deepArgs, opts)
	deepQ += " LIMIT 50000"

	fileRows, err := s.db.QueryContext(ctx, fileQ, fileArgs...)
	if err != nil {
		return nil, fmt.Errorf("vault tree files: %w", err)
	}
	defer fileRows.Close()
	var entries []store.VaultTreeEntry
	for fileRows.Next() {
		var id, path, title, docType, scope string
		var ua sqliteTime
		if err := fileRows.Scan(&id, &path, &title, &docType, &scope, &ua); err != nil {
			return nil, fmt.Errorf("vault tree scan: %w", err)
		}
		name := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			name = path[idx+1:]
		}
		t := ua.Time
		entries = append(entries, store.VaultTreeEntry{
			Name: name, Path: path, DocID: id, DocType: docType, Scope: scope, Title: title, UpdatedAt: &t,
		})
	}
	if err := fileRows.Err(); err != nil {
		return nil, fmt.Errorf("vault tree files: %w", err)
	}

	deepRows, err := s.db.QueryContext(ctx, deepQ, deepArgs...)
	if err != nil {
		return nil, fmt.Errorf("vault tree deep: %w", err)
	}
	defer deepRows.Close()
	seen := make(map[string]bool)
	for deepRows.Next() {
		var p string
		if err := deepRows.Scan(&p); err != nil {
			return nil, fmt.Errorf("vault tree deep scan: %w", err)
		}
		rest := strings.TrimPrefix(p, prefix)
		if idx := strings.Index(rest, "/"); idx > 0 {
			seg := rest[:idx]
			if !seen[seg] {
				seen[seg] = true
				entries = append(entries, store.VaultTreeEntry{
					Name: seg, Path: prefix + seg, IsDir: true, HasChildren: true,
				})
			}
		}
	}
	if err := deepRows.Err(); err != nil {
		return nil, fmt.Errorf("vault tree deep: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func sqliteAppendTreeFilters(q string, args []any, opts store.VaultTreeOptions) (string, []any) {
	if opts.AgentID != "" {
		q += " AND agent_id = ?"
		args = append(args, opts.AgentID)
	}
	q, args = sqliteAppendTeamFilter(q, args, opts.TeamID, opts.TeamIDs)
	if opts.Scope != "" {
		q += " AND scope = ?"
		args = append(args, opts.Scope)
	}
	if len(opts.DocTypes) > 0 {
		ph := strings.Repeat("?,", len(opts.DocTypes)-1) + "?"
		q += " AND doc_type IN (" + ph + ")"
		for _, dt := range opts.DocTypes {
			args = append(args, dt)
		}
	}
	return q, args
}

// --- scan helpers ---

func scanVaultDoc(row *sql.Row) (*store.VaultDocument, error) {
	var doc store.VaultDocument
	var meta []byte
	var agentID *string
	ca, ua := &sqliteTime{}, &sqliteTime{}
	err := row.Scan(&doc.ID, &doc.TenantID, &agentID, &doc.TeamID, &doc.Scope, &doc.CustomScope,
		&doc.Path, &doc.PathBasename, &doc.Title, &doc.DocType, &doc.ContentHash, &doc.Summary, &meta, ca, ua)
	if err != nil {
		return nil, err
	}
	doc.AgentID = agentID
	doc.CreatedAt = ca.Time
	doc.UpdatedAt = ua.Time
	if len(meta) > 2 {
		_ = json.Unmarshal(meta, &doc.Metadata)
	}
	return &doc, nil
}

func scanVaultDocRow(rows *sql.Rows) (*store.VaultDocument, error) {
	var doc store.VaultDocument
	var meta []byte
	var agentID *string
	ca, ua := &sqliteTime{}, &sqliteTime{}
	err := rows.Scan(&doc.ID, &doc.TenantID, &agentID, &doc.TeamID, &doc.Scope, &doc.CustomScope,
		&doc.Path, &doc.PathBasename, &doc.Title, &doc.DocType, &doc.ContentHash, &doc.Summary, &meta, ca, ua)
	if err != nil {
		return nil, err
	}
	doc.AgentID = agentID
	doc.CreatedAt = ca.Time
	doc.UpdatedAt = ua.Time
	if len(meta) > 2 {
		_ = json.Unmarshal(meta, &doc.Metadata)
	}
	return &doc, nil
}

// Interface compliance check.
var _ store.VaultStore = (*SQLiteVaultStore)(nil)
