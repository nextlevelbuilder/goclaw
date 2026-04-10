//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteAgentLinkStore implements store.AgentLinkStore backed by SQLite.
type SQLiteAgentLinkStore struct {
	db *sql.DB
}

// NewSQLiteAgentLinkStore creates a new SQLiteAgentLinkStore.
func NewSQLiteAgentLinkStore(db *sql.DB) *SQLiteAgentLinkStore {
	return &SQLiteAgentLinkStore{db: db}
}

const linkSelectCols = `id, source_agent_id, target_agent_id, direction, team_id, description,
	max_concurrent, settings, status, created_by, created_at, updated_at`

const linkSelectColsJoined = `l.id, l.source_agent_id, l.target_agent_id, l.direction, l.team_id, l.description,
	l.max_concurrent, l.settings, l.status, l.created_by, l.created_at, l.updated_at`

func (s *SQLiteAgentLinkStore) CreateLink(ctx context.Context, link *store.AgentLinkData) error {
	if link.ID == uuid.Nil {
		link.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	link.CreatedAt = now
	link.UpdatedAt = now

	settings := link.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_links (id, source_agent_id, target_agent_id, direction, team_id, description,
		 max_concurrent, settings, status, created_by, created_at, updated_at, tenant_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		link.ID, link.SourceAgentID, link.TargetAgentID, link.Direction, link.TeamID, link.Description,
		link.MaxConcurrent, settings, link.Status, link.CreatedBy,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), tenantID,
	)
	return err
}

func (s *SQLiteAgentLinkStore) DeleteLink(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, `DELETE FROM agent_links WHERE id = ?`, id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for delete")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_links WHERE id = ? AND tenant_id = ?`, id, tid)
	return err
}

func (s *SQLiteAgentLinkStore) UpdateLink(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	updates["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "agent_links", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "agent_links", updates, id, tid)
}

func (s *SQLiteAgentLinkStore) GetLink(ctx context.Context, id uuid.UUID) (*store.AgentLinkData, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+linkSelectCols+` FROM agent_links WHERE id = ?`, id)
		return scanLinkRow(row)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("link not found: %w", sql.ErrNoRows)
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+linkSelectCols+` FROM agent_links WHERE id = ? AND tenant_id = ?`, id, tenantID)
	return scanLinkRow(row)
}

func (s *SQLiteAgentLinkStore) ListLinksFrom(ctx context.Context, agentID uuid.UUID) ([]store.AgentLinkData, error) {
	tenantClause, qArgs := linkTenantClause(ctx, agentID, "l.source_agent_id = ?")
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+linkSelectColsJoined+`,
		 sa.agent_key AS source_agent_key,
		 COALESCE(sa.display_name, '') AS source_display_name,
		 COALESCE(sa.emoji, '') AS source_emoji,
		 ta.agent_key AS target_agent_key,
		 COALESCE(ta.display_name, '') AS target_display_name,
		 COALESCE(ta.emoji, '') AS target_emoji,
		 COALESCE(ta.frontmatter, '') AS target_description,
		 COALESCE(tm.name, '') AS team_name,
		 EXISTS(SELECT 1 FROM agent_teams tl WHERE tl.lead_agent_id = l.target_agent_id AND tl.status = 'active') AS target_is_team_lead,
		 COALESCE((SELECT tl.name FROM agent_teams tl WHERE tl.lead_agent_id = l.target_agent_id AND tl.status = 'active' LIMIT 1), '') AS target_team_name
		 FROM agent_links l
		 JOIN agents sa ON sa.id = l.source_agent_id
		 JOIN agents ta ON ta.id = l.target_agent_id
		 LEFT JOIN agent_teams tm ON tm.id = l.team_id
		 WHERE `+tenantClause+`
		 ORDER BY l.created_at`, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinkRowsJoined(rows)
}

func (s *SQLiteAgentLinkStore) ListLinksTo(ctx context.Context, agentID uuid.UUID) ([]store.AgentLinkData, error) {
	tenantClause, qArgs := linkTenantClause(ctx, agentID, "l.target_agent_id = ?")
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+linkSelectColsJoined+`,
		 sa.agent_key AS source_agent_key,
		 COALESCE(sa.display_name, '') AS source_display_name,
		 COALESCE(sa.emoji, '') AS source_emoji,
		 ta.agent_key AS target_agent_key,
		 COALESCE(ta.display_name, '') AS target_display_name,
		 COALESCE(ta.emoji, '') AS target_emoji,
		 COALESCE(ta.frontmatter, '') AS target_description,
		 COALESCE(tm.name, '') AS team_name,
		 EXISTS(SELECT 1 FROM agent_teams tl WHERE tl.lead_agent_id = l.target_agent_id AND tl.status = 'active') AS target_is_team_lead,
		 COALESCE((SELECT tl.name FROM agent_teams tl WHERE tl.lead_agent_id = l.target_agent_id AND tl.status = 'active' LIMIT 1), '') AS target_team_name
		 FROM agent_links l
		 JOIN agents sa ON sa.id = l.source_agent_id
		 JOIN agents ta ON ta.id = l.target_agent_id
		 LEFT JOIN agent_teams tm ON tm.id = l.team_id
		 WHERE `+tenantClause+`
		 ORDER BY l.created_at`, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinkRowsJoined(rows)
}

// linkTenantClause builds the WHERE clause using ? placeholders.
// baseCondition must use ? for the agentID parameter (first arg).
func linkTenantClause(ctx context.Context, agentID uuid.UUID, baseCondition string) (string, []any) {
	args := []any{agentID}
	if store.IsCrossTenant(ctx) {
		return baseCondition, args
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return baseCondition + " AND l.tenant_id = ?", append(args, uuid.Nil)
	}
	return baseCondition + " AND l.tenant_id = ?", append(args, tenantID)
}

func (s *SQLiteAgentLinkStore) CanDelegate(ctx context.Context, fromAgentID, toAgentID uuid.UUID) (bool, error) {
	var exists bool
	tenantFilter := ""
	// args for 4 id placeholders in the query
	args := []any{fromAgentID, toAgentID, toAgentID, fromAgentID}
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return false, nil
		}
		tenantFilter = " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM agent_links WHERE status = 'active'
			AND (
				(source_agent_id = ? AND target_agent_id = ? AND direction IN ('outbound', 'bidirectional'))
				OR
				(source_agent_id = ? AND target_agent_id = ? AND direction IN ('inbound', 'bidirectional'))
			)`+tenantFilter+`
		)`, args...).Scan(&exists)
	return exists, err
}

func (s *SQLiteAgentLinkStore) GetLinkBetween(ctx context.Context, fromAgentID, toAgentID uuid.UUID) (*store.AgentLinkData, error) {
	tenantFilter := ""
	args := []any{fromAgentID, toAgentID, toAgentID, fromAgentID}
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		tenantFilter = " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+linkSelectCols+`
		 FROM agent_links WHERE status = 'active'
		 AND (
			(source_agent_id = ? AND target_agent_id = ? AND direction IN ('outbound', 'bidirectional'))
			OR
			(source_agent_id = ? AND target_agent_id = ? AND direction IN ('inbound', 'bidirectional'))
		 )`+tenantFilter+` LIMIT 1`, args...)
	d, err := scanLinkRow(row)
	if err != nil {
		return nil, nil
	}
	return d, nil
}

func (s *SQLiteAgentLinkStore) DelegateTargets(ctx context.Context, fromAgentID uuid.UUID) ([]store.AgentLinkData, error) {
	if !store.IsCrossTenant(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid == uuid.Nil {
			return nil, nil
		}
	}

	// delegateTenantFilter inlines tenant UUID as string literal — no extra ? needed.
	// buildDelegateArgs repeats fromAgentID for all ? placeholders in SELECT + WHERE.
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+linkSelectColsJoined+`,
		 CASE WHEN l.source_agent_id = ? THEN sa.agent_key ELSE ta.agent_key END AS source_agent_key,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(sa.display_name, '') ELSE COALESCE(ta.display_name, '') END AS source_display_name,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(sa.emoji, '') ELSE COALESCE(ta.emoji, '') END AS source_emoji,
		 CASE WHEN l.source_agent_id = ? THEN ta.agent_key ELSE sa.agent_key END AS target_agent_key,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.display_name, '') ELSE COALESCE(sa.display_name, '') END AS target_display_name,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.emoji, '') ELSE COALESCE(sa.emoji, '') END AS target_emoji,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.frontmatter, '') ELSE COALESCE(sa.frontmatter, '') END AS target_description,
		 COALESCE(tm.name, '') AS team_name,
		 EXISTS(
			SELECT 1 FROM agent_teams tl
			WHERE tl.lead_agent_id = CASE WHEN l.source_agent_id = ? THEN l.target_agent_id ELSE l.source_agent_id END
			  AND tl.status = 'active'
		 ) AS target_is_team_lead,
		 COALESCE((
			SELECT tl.name FROM agent_teams tl
			WHERE tl.lead_agent_id = CASE WHEN l.source_agent_id = ? THEN l.target_agent_id ELSE l.source_agent_id END
			  AND tl.status = 'active'
			LIMIT 1
		 ), '') AS target_team_name
		 FROM agent_links l
		 JOIN agents sa ON sa.id = l.source_agent_id
		 JOIN agents ta ON ta.id = l.target_agent_id
		 LEFT JOIN agent_teams tm ON tm.id = l.team_id
		 WHERE l.status = 'active'
		   AND CASE WHEN l.source_agent_id = ? THEN ta.status ELSE sa.status END = 'active'
		   AND (
			(l.source_agent_id = ? AND l.direction IN ('outbound', 'bidirectional'))
			OR
			(l.target_agent_id = ? AND l.direction IN ('inbound', 'bidirectional'))
		 )`+delegateTenantFilter(ctx)+`
		 ORDER BY CASE WHEN l.source_agent_id = ? THEN ta.agent_key ELSE sa.agent_key END`,
		buildDelegateArgs(ctx, fromAgentID)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinkRowsJoined(rows)
}

// buildDelegateArgs builds args for DelegateTargets: fromAgentID repeated 13 times.
// delegateTenantFilter inlines the tenant UUID as a string literal (no ? placeholder).
func buildDelegateArgs(_ context.Context, fromAgentID uuid.UUID) []any {
	// 13 occurrences of fromAgentID in the query:
	// 7 in SELECT CASE expressions + 2 in EXISTS/COALESCE subqueries + 3 in WHERE + 1 in ORDER BY
	args := make([]any, 13)
	for i := range args {
		args[i] = fromAgentID
	}
	return args
}

// delegateTenantFilter returns a tenant filter clause without a parameter (uses inline UUID string).
func delegateTenantFilter(ctx context.Context) string {
	if store.IsCrossTenant(ctx) {
		return ""
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return " AND l.tenant_id = '00000000-0000-0000-0000-000000000000'"
	}
	return fmt.Sprintf(" AND l.tenant_id = '%s'", tid.String())
}

func (s *SQLiteAgentLinkStore) SearchDelegateTargets(ctx context.Context, fromAgentID uuid.UUID, query string, limit int) ([]store.AgentLinkData, error) {
	if limit <= 0 {
		limit = 5
	}
	if len(query) > 500 {
		query = query[:500] // F10: cap query length
	}
	likePattern := "%" + escapeLike(strings.ToLower(query)) + "%"
	tenantFilter := delegateTenantFilter(ctx)
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, nil
		}
	}

	// Build args: fromAgentID repeated for CASE expressions + LIKE patterns + limit
	args := buildSearchDelegateArgs(fromAgentID, likePattern, limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+linkSelectColsJoined+`,
		 CASE WHEN l.source_agent_id = ? THEN sa.agent_key ELSE ta.agent_key END AS source_agent_key,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(sa.display_name, '') ELSE COALESCE(ta.display_name, '') END AS source_display_name,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(sa.emoji, '') ELSE COALESCE(ta.emoji, '') END AS source_emoji,
		 CASE WHEN l.source_agent_id = ? THEN ta.agent_key ELSE sa.agent_key END AS target_agent_key,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.display_name, '') ELSE COALESCE(sa.display_name, '') END AS target_display_name,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.emoji, '') ELSE COALESCE(sa.emoji, '') END AS target_emoji,
		 CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.frontmatter, '') ELSE COALESCE(sa.frontmatter, '') END AS target_description,
		 COALESCE(tm.name, '') AS team_name,
		 EXISTS(SELECT 1 FROM agent_teams tl WHERE tl.lead_agent_id = CASE WHEN l.source_agent_id = ? THEN l.target_agent_id ELSE l.source_agent_id END AND tl.status = 'active') AS target_is_team_lead,
		 COALESCE((SELECT tl.name FROM agent_teams tl WHERE tl.lead_agent_id = CASE WHEN l.source_agent_id = ? THEN l.target_agent_id ELSE l.source_agent_id END AND tl.status = 'active' LIMIT 1), '') AS target_team_name
		 FROM agent_links l
		 JOIN agents sa ON sa.id = l.source_agent_id
		 JOIN agents ta ON ta.id = l.target_agent_id
		 LEFT JOIN agent_teams tm ON tm.id = l.team_id
		 WHERE l.status = 'active'`+tenantFilter+`
		   AND CASE WHEN l.source_agent_id = ? THEN ta.status ELSE sa.status END = 'active'
		   AND (
		     (l.source_agent_id = ? AND l.direction IN ('outbound', 'bidirectional'))
		     OR
		     (l.target_agent_id = ? AND l.direction IN ('inbound', 'bidirectional'))
		   )
		   AND (
		     LOWER(CASE WHEN l.source_agent_id = ? THEN ta.agent_key ELSE sa.agent_key END) LIKE ? ESCAPE '\'
		     OR LOWER(CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.display_name,'') ELSE COALESCE(sa.display_name,'') END) LIKE ? ESCAPE '\'
		     OR LOWER(CASE WHEN l.source_agent_id = ? THEN COALESCE(ta.frontmatter,'') ELSE COALESCE(sa.frontmatter,'') END) LIKE ? ESCAPE '\'
		   )
		 ORDER BY CASE WHEN l.source_agent_id = ? THEN ta.agent_key ELSE sa.agent_key END
		 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinkRowsJoined(rows)
}

// buildSearchDelegateArgs builds positional ? args for SearchDelegateTargets.
func buildSearchDelegateArgs(fromAgentID uuid.UUID, likePattern string, limit int) []any {
	id := fromAgentID
	args := []any{
		// 9 for joined SELECT columns
		id, id, id, id, id, id, id, id, id,
		// 3 for WHERE conditions
		id, id, id,
		// 3 pairs of (id, likePattern) for LIKE conditions
		id, likePattern, id, likePattern, id, likePattern,
		// ORDER BY + LIMIT
		id, limit,
	}
	return args
}

// SearchDelegateTargetsByEmbedding returns empty slice — vector search not available in SQLite.
func (s *SQLiteAgentLinkStore) SearchDelegateTargetsByEmbedding(_ context.Context, _ uuid.UUID, _ []float32, _ int) ([]store.AgentLinkData, error) {
	return []store.AgentLinkData{}, nil
}

func (s *SQLiteAgentLinkStore) DeleteTeamLinksForAgent(ctx context.Context, teamID, agentID uuid.UUID) error {
	args := []any{teamID, agentID, agentID}
	tenantFilter := ""
	if !store.IsCrossTenant(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
			tenantFilter = " AND tenant_id = ?"
			args = append(args, tid)
		}
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_links WHERE team_id = ? AND (source_agent_id = ? OR target_agent_id = ?)`+tenantFilter,
		args...,
	)
	return err
}

// --- scan helpers ---

func scanLinkRow(row *sql.Row) (*store.AgentLinkData, error) {
	var d store.AgentLinkData
	var desc sql.NullString
	var createdAt, updatedAt sqliteTime
	err := row.Scan(
		&d.ID, &d.SourceAgentID, &d.TargetAgentID, &d.Direction, &d.TeamID, &desc,
		&d.MaxConcurrent, &d.Settings, &d.Status, &d.CreatedBy, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("link not found: %w", err)
	}
	if desc.Valid {
		d.Description = desc.String
	}
	d.CreatedAt = createdAt.Time
	d.UpdatedAt = updatedAt.Time
	return &d, nil
}

func scanLinkRowsJoined(rows *sql.Rows) ([]store.AgentLinkData, error) {
	var links []store.AgentLinkData
	for rows.Next() {
		var d store.AgentLinkData
		var desc sql.NullString
		var createdAt, updatedAt sqliteTime
		if err := rows.Scan(
			&d.ID, &d.SourceAgentID, &d.TargetAgentID, &d.Direction, &d.TeamID, &desc,
			&d.MaxConcurrent, &d.Settings, &d.Status, &d.CreatedBy, &createdAt, &updatedAt,
			&d.SourceAgentKey, &d.SourceDisplayName, &d.SourceEmoji,
			&d.TargetAgentKey, &d.TargetDisplayName, &d.TargetEmoji, &d.TargetDescription,
			&d.TeamName, &d.TargetIsTeamLead, &d.TargetTeamName,
		); err != nil {
			return nil, err
		}
		if desc.Valid {
			d.Description = desc.String
		}
		d.CreatedAt = createdAt.Time
		d.UpdatedAt = updatedAt.Time
		links = append(links, d)
	}
	return links, rows.Err()
}
