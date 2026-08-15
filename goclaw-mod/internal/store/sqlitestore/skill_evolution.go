//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type SQLiteSkillEvolutionStore struct {
	db *sql.DB
}

func NewSQLiteSkillEvolutionStore(db *sql.DB) *SQLiteSkillEvolutionStore {
	return &SQLiteSkillEvolutionStore{db: db}
}

func (s *SQLiteSkillEvolutionStore) resolveSkill(ctx context.Context, skillID uuid.UUID) (string, int, error) {
	tenantID := tenantIDForInsert(ctx)
	var slug, skillTenant string
	var version int
	var isSystem bool
	err := s.db.QueryRowContext(ctx,
		`SELECT slug, version, tenant_id, is_system FROM skills WHERE id = ? AND status != 'deleted'`,
		skillID.String(),
	).Scan(&slug, &version, &skillTenant, &isSystem)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, fmt.Errorf("skill not found")
	}
	if err != nil {
		return "", 0, err
	}
	if !store.IsCrossTenant(ctx) && !isSystem && skillTenant != tenantID.String() {
		return "", 0, fmt.Errorf("skill not found")
	}
	return slug, version, nil
}

func (s *SQLiteSkillEvolutionStore) GetSettings(ctx context.Context, skillID uuid.UUID) (*store.SkillEvolutionSettings, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	var out store.SkillEvolutionSettings
	var tenantStr, skillStr string
	var last nullSqliteTime
	var created, updated sqliteTime
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, skill_id, enabled, mode, last_analyzed_at, created_at, updated_at
		 FROM skill_evolution_settings WHERE tenant_id = ? AND skill_id = ?`,
		tenantID.String(), skillID.String(),
	).Scan(&tenantStr, &skillStr, &out.Enabled, &out.Mode, &last, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return &store.SkillEvolutionSettings{
			TenantID: tenantID,
			SkillID:  skillID,
			Enabled:  false,
			Mode:     store.SkillEvolutionModeSuggestOnly,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	out.TenantID, _ = uuid.Parse(tenantStr)
	out.SkillID, _ = uuid.Parse(skillStr)
	if last.Valid {
		t := last.Time
		out.LastAnalyzedAt = &t
	}
	out.CreatedAt = created.Time
	out.UpdatedAt = updated.Time
	return &out, nil
}

func (s *SQLiteSkillEvolutionStore) UpsertSettings(ctx context.Context, settings store.SkillEvolutionSettings) (*store.SkillEvolutionSettings, error) {
	if _, _, err := s.resolveSkill(ctx, settings.SkillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	mode := strings.TrimSpace(settings.Mode)
	if mode == "" {
		mode = store.SkillEvolutionModeSuggestOnly
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var last any
	if settings.LastAnalyzedAt != nil {
		last = settings.LastAnalyzedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_evolution_settings (tenant_id, skill_id, enabled, mode, last_analyzed_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id, skill_id) DO UPDATE SET
		   enabled = excluded.enabled,
		   mode = excluded.mode,
		   last_analyzed_at = excluded.last_analyzed_at,
		   updated_at = excluded.updated_at`,
		tenantID.String(), settings.SkillID.String(), settings.Enabled, mode, last, now, now,
	)
	if err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, settings.SkillID)
}

func (s *SQLiteSkillEvolutionStore) RecordUsage(ctx context.Context, metric store.SkillUsageMetric) error {
	slug, version, err := s.resolveSkill(ctx, metric.SkillID)
	if err != nil {
		return err
	}
	tenantID := tenantIDForInsert(ctx)
	if metric.ID == uuid.Nil {
		metric.ID = uuid.New()
	}
	if metric.SkillSlug == "" {
		metric.SkillSlug = slug
	}
	if metric.SkillVersion == 0 {
		metric.SkillVersion = version
	}
	if metric.Status == "" {
		metric.Status = store.SkillUsageStatusStarted
	}
	agentID := ""
	if metric.AgentID != uuid.Nil {
		agentID = metric.AgentID.String()
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO skill_usage_metrics
		 (id, tenant_id, skill_id, skill_slug, skill_version, agent_id, user_id, session_key,
		  trace_id, invocation_id, invocation_source, status, failure_reason, tool_calls_count, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		metric.ID.String(), tenantID.String(), metric.SkillID.String(), metric.SkillSlug, metric.SkillVersion,
		agentID, metric.UserID, metric.SessionKey, metric.TraceID, metric.InvocationID, metric.InvocationSource,
		metric.Status, metric.FailureReason, metric.ToolCallsCount, metric.DurationMs,
	)
	return err
}

func (s *SQLiteSkillEvolutionStore) AggregateUsage(ctx context.Context, skillID uuid.UUID, since *time.Time) (*store.SkillUsageStats, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	where := "tenant_id = ? AND skill_id = ?"
	args := []any{tenantID.String(), skillID.String()}
	if since != nil {
		where += " AND created_at >= ?"
		args = append(args, since.UTC().Format(time.RFC3339Nano))
	}
	query := fmt.Sprintf(
		`SELECT COUNT(*),
		        SUM(CASE WHEN status = 'started' THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),
		        SUM(CASE WHEN status = 'abandoned' THEN 1 ELSE 0 END),
		        MAX(created_at)
		 FROM skill_usage_metrics WHERE %s`,
		where,
	)
	var out store.SkillUsageStats
	out.SkillID = skillID
	var last nullSqliteTime
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&out.TotalCalls, &out.Started, &out.Succeeded, &out.Failed, &out.Abandoned, &last); err != nil {
		return nil, err
	}
	if last.Valid {
		t := last.Time
		out.LastUsedAt = &t
	}
	if out.TotalCalls > 0 {
		out.SuccessRate = float64(out.Succeeded) / float64(out.TotalCalls)
		out.FailureRate = float64(out.Failed) / float64(out.TotalCalls)
	}
	reasonQuery := fmt.Sprintf(
		`SELECT failure_reason, COUNT(*), MAX(created_at)
		 FROM skill_usage_metrics
		 WHERE %s AND status = 'failed' AND COALESCE(failure_reason, '') != ''
		 GROUP BY failure_reason ORDER BY COUNT(*) DESC, MAX(created_at) DESC LIMIT 5`,
		where,
	)
	rows, err := s.db.QueryContext(ctx, reasonQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r store.SkillFailureReason
		var lastSeen sqliteTime
		if err := rows.Scan(&r.Reason, &r.Count, &lastSeen); err != nil {
			return nil, err
		}
		r.LastSeen = lastSeen.Time
		out.TopFailureReasons = append(out.TopFailureReasons, r)
	}
	return &out, rows.Err()
}

func (s *SQLiteSkillEvolutionStore) ListUsage(ctx context.Context, skillID uuid.UUID, limit int) ([]store.SkillUsageMetric, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, skill_id, skill_slug, skill_version, COALESCE(agent_id, ''),
		        COALESCE(user_id,''), COALESCE(session_key,''), COALESCE(trace_id,''), COALESCE(invocation_id,''),
		        invocation_source, status, COALESCE(failure_reason,''), tool_calls_count, duration_ms, created_at
		 FROM skill_usage_metrics
		 WHERE tenant_id = ? AND skill_id = ?
		 ORDER BY created_at DESC LIMIT ?`,
		tenantID.String(), skillID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SkillUsageMetric
	for rows.Next() {
		var m store.SkillUsageMetric
		var idStr, tenantStr, skillStr, agentStr string
		var created sqliteTime
		if err := rows.Scan(&idStr, &tenantStr, &skillStr, &m.SkillSlug, &m.SkillVersion, &agentStr,
			&m.UserID, &m.SessionKey, &m.TraceID, &m.InvocationID, &m.InvocationSource, &m.Status,
			&m.FailureReason, &m.ToolCallsCount, &m.DurationMs, &created); err != nil {
			return nil, err
		}
		m.ID, _ = uuid.Parse(idStr)
		m.TenantID, _ = uuid.Parse(tenantStr)
		m.SkillID, _ = uuid.Parse(skillStr)
		if agentStr != "" {
			m.AgentID, _ = uuid.Parse(agentStr)
		}
		m.CreatedAt = created.Time
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *SQLiteSkillEvolutionStore) CreateSuggestion(ctx context.Context, sg store.SkillImprovementSuggestion) (*store.SkillImprovementSuggestion, error) {
	slug, _, err := s.resolveSkill(ctx, sg.SkillID)
	if err != nil {
		return nil, err
	}
	if sg.ID == uuid.Nil {
		sg.ID = uuid.New()
	}
	if sg.SkillSlug == "" {
		sg.SkillSlug = slug
	}
	if sg.Status == "" {
		sg.Status = store.SkillSuggestionStatusPending
	}
	tenantID := tenantIDForInsert(ctx)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO skill_improvement_suggestions
		 (id, tenant_id, skill_id, skill_slug, suggestion_type, status, reason, evidence,
		  draft_patch, target_file, created_by_actor_type, created_by_actor_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sg.ID.String(), tenantID.String(), sg.SkillID.String(), sg.SkillSlug, sg.SuggestionType,
		sg.Status, sg.Reason, string(jsonOrDefault(sg.Evidence, "{}")), string(jsonOrDefault(sg.DraftPatch, "{}")),
		sg.TargetFile, sg.CreatedByActorType, sg.CreatedByActorID, now, now,
	)
	if err != nil {
		return nil, err
	}
	return s.GetSuggestion(ctx, sg.ID)
}

func (s *SQLiteSkillEvolutionStore) ListSuggestions(ctx context.Context, skillID uuid.UUID, status string, limit int) ([]store.SkillImprovementSuggestion, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	tenantID := tenantIDForInsert(ctx)
	q := `SELECT id, tenant_id, skill_id, skill_slug, suggestion_type, status, reason, evidence, draft_patch,
	             COALESCE(target_file,''), COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
	             COALESCE(reviewed_by_actor_type,''), COALESCE(reviewed_by_actor_id,''), reviewed_at, applied_version,
	             created_at, updated_at
	      FROM skill_improvement_suggestions WHERE tenant_id = ? AND skill_id = ?`
	args := []any{tenantID.String(), skillID.String()}
	if status != "" {
		q += " AND status = ?"
		args = append(args, status)
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSQLiteSuggestions(rows)
}

func (s *SQLiteSkillEvolutionStore) GetSuggestion(ctx context.Context, id uuid.UUID) (*store.SkillImprovementSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, skill_id, skill_slug, suggestion_type, status, reason, evidence, draft_patch,
		        COALESCE(target_file,''), COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
		        COALESCE(reviewed_by_actor_type,''), COALESCE(reviewed_by_actor_id,''), reviewed_at, applied_version,
		        created_at, updated_at
		 FROM skill_improvement_suggestions WHERE tenant_id = ? AND id = ?`,
		tenantID.String(), id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanSQLiteSuggestions(rows)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	if _, _, err := s.resolveSkill(ctx, items[0].SkillID); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *SQLiteSkillEvolutionStore) UpdateSuggestionStatus(ctx context.Context, id uuid.UUID, status, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`UPDATE skill_improvement_suggestions
		 SET status = ?, reviewed_by_actor_type = ?, reviewed_by_actor_id = ?, reviewed_at = ?, updated_at = ?
		 WHERE tenant_id = ? AND id = ?`,
		status, actorType, actorID, now, now, tenantID.String(), id.String())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("suggestion not found")
	}
	return s.GetSuggestion(ctx, id)
}

func (s *SQLiteSkillEvolutionStore) MarkSuggestionApplied(ctx context.Context, id uuid.UUID, version int, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`UPDATE skill_improvement_suggestions
		 SET status = 'applied', applied_version = ?, reviewed_by_actor_type = ?,
		     reviewed_by_actor_id = ?, reviewed_at = COALESCE(reviewed_at, ?), updated_at = ?
		 WHERE tenant_id = ? AND id = ?`,
		version, actorType, actorID, now, now, tenantID.String(), id.String())
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("suggestion not found")
	}
	return s.GetSuggestion(ctx, id)
}

func (s *SQLiteSkillEvolutionStore) CreateSkillVersion(ctx context.Context, v store.SkillVersion) (*store.SkillVersion, error) {
	if _, _, err := s.resolveSkill(ctx, v.SkillID); err != nil {
		return nil, err
	}
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	tenantID := tenantIDForInsert(ctx)
	suggestionID := ""
	if v.CreatedFromSuggestionID != nil {
		suggestionID = v.CreatedFromSuggestionID.String()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_versions
		 (id, tenant_id, skill_id, version, content_hash, changed_files, created_by_actor_type,
		  created_by_actor_id, created_from_suggestion_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)`,
		v.ID.String(), tenantID.String(), v.SkillID.String(), v.Version, v.ContentHash,
		string(jsonOrDefault(v.ChangedFiles, "[]")), v.CreatedByActorType, v.CreatedByActorID, suggestionID, now,
	)
	if err != nil {
		return nil, err
	}
	return s.GetSkillVersion(ctx, v.SkillID, v.Version)
}

func (s *SQLiteSkillEvolutionStore) ListSkillVersions(ctx context.Context, skillID uuid.UUID, limit int) ([]store.SkillVersion, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, skill_id, version, content_hash, changed_files,
		        COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
		        created_from_suggestion_id, created_at
		 FROM skill_versions WHERE tenant_id = ? AND skill_id = ?
		 ORDER BY version DESC LIMIT ?`,
		tenantID.String(), skillID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSQLiteSkillVersions(rows)
}

func (s *SQLiteSkillEvolutionStore) GetSkillVersion(ctx context.Context, skillID uuid.UUID, version int) (*store.SkillVersion, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, skill_id, version, content_hash, changed_files,
		        COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
		        created_from_suggestion_id, created_at
		 FROM skill_versions WHERE tenant_id = ? AND skill_id = ? AND version = ?`,
		tenantID.String(), skillID.String(), version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanSQLiteSkillVersions(rows)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func scanSQLiteSuggestions(rows *sql.Rows) ([]store.SkillImprovementSuggestion, error) {
	var out []store.SkillImprovementSuggestion
	for rows.Next() {
		var sg store.SkillImprovementSuggestion
		var idStr, tenantStr, skillStr string
		var evidence, draft []byte
		var reviewedAt nullSqliteTime
		var applied sql.NullInt64
		var created, updated sqliteTime
		if err := rows.Scan(&idStr, &tenantStr, &skillStr, &sg.SkillSlug, &sg.SuggestionType, &sg.Status,
			&sg.Reason, &evidence, &draft, &sg.TargetFile, &sg.CreatedByActorType, &sg.CreatedByActorID,
			&sg.ReviewedByActorType, &sg.ReviewedByActorID, &reviewedAt, &applied,
			&created, &updated); err != nil {
			return nil, err
		}
		sg.ID, _ = uuid.Parse(idStr)
		sg.TenantID, _ = uuid.Parse(tenantStr)
		sg.SkillID, _ = uuid.Parse(skillStr)
		sg.Evidence = evidence
		sg.DraftPatch = draft
		if reviewedAt.Valid {
			t := reviewedAt.Time
			sg.ReviewedAt = &t
		}
		if applied.Valid {
			v := int(applied.Int64)
			sg.AppliedVersion = &v
		}
		sg.CreatedAt = created.Time
		sg.UpdatedAt = updated.Time
		out = append(out, sg)
	}
	return out, rows.Err()
}

func scanSQLiteSkillVersions(rows *sql.Rows) ([]store.SkillVersion, error) {
	var out []store.SkillVersion
	for rows.Next() {
		var v store.SkillVersion
		var idStr, tenantStr, skillStr string
		var changed []byte
		var suggestion sql.NullString
		var created sqliteTime
		if err := rows.Scan(&idStr, &tenantStr, &skillStr, &v.Version, &v.ContentHash, &changed,
			&v.CreatedByActorType, &v.CreatedByActorID, &suggestion, &created); err != nil {
			return nil, err
		}
		v.ID, _ = uuid.Parse(idStr)
		v.TenantID, _ = uuid.Parse(tenantStr)
		v.SkillID, _ = uuid.Parse(skillStr)
		v.ChangedFiles = changed
		if suggestion.Valid && suggestion.String != "" {
			parsed, _ := uuid.Parse(suggestion.String)
			v.CreatedFromSuggestionID = &parsed
		}
		v.CreatedAt = created.Time
		out = append(out, v)
	}
	return out, rows.Err()
}

func jsonOrDefault(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

var _ store.SkillEvolutionStore = (*SQLiteSkillEvolutionStore)(nil)
