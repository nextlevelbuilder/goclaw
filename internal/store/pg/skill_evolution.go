package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type PGSkillEvolutionStore struct {
	db *sql.DB
}

func NewPGSkillEvolutionStore(db *sql.DB) *PGSkillEvolutionStore {
	return &PGSkillEvolutionStore{db: db}
}

func (s *PGSkillEvolutionStore) resolveSkill(ctx context.Context, skillID uuid.UUID) (string, int, error) {
	tenantID := tenantIDForInsert(ctx)
	var slug string
	var version int
	var skillTenant uuid.UUID
	var isSystem bool
	err := s.db.QueryRowContext(ctx,
		`SELECT slug, version, tenant_id, is_system
		 FROM skills
		 WHERE id = $1 AND status != 'deleted'`,
		skillID,
	).Scan(&slug, &version, &skillTenant, &isSystem)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, fmt.Errorf("skill not found")
	}
	if err != nil {
		return "", 0, err
	}
	if !store.IsCrossTenant(ctx) && !isSystem && skillTenant != tenantID {
		return "", 0, fmt.Errorf("skill not found")
	}
	return slug, version, nil
}

func (s *PGSkillEvolutionStore) GetSettings(ctx context.Context, skillID uuid.UUID) (*store.SkillEvolutionSettings, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	var out store.SkillEvolutionSettings
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, skill_id, enabled, mode, last_analyzed_at, created_at, updated_at
		 FROM skill_evolution_settings
		 WHERE tenant_id = $1 AND skill_id = $2`,
		tenantID, skillID,
	).Scan(&out.TenantID, &out.SkillID, &out.Enabled, &out.Mode, &out.LastAnalyzedAt, &out.CreatedAt, &out.UpdatedAt)
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
	return &out, nil
}

func (s *PGSkillEvolutionStore) UpsertSettings(ctx context.Context, settings store.SkillEvolutionSettings) (*store.SkillEvolutionSettings, error) {
	if _, _, err := s.resolveSkill(ctx, settings.SkillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	mode := strings.TrimSpace(settings.Mode)
	if mode == "" {
		mode = store.SkillEvolutionModeSuggestOnly
	}
	var out store.SkillEvolutionSettings
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO skill_evolution_settings (tenant_id, skill_id, enabled, mode, last_analyzed_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (tenant_id, skill_id) DO UPDATE SET
		   enabled = EXCLUDED.enabled,
		   mode = EXCLUDED.mode,
		   last_analyzed_at = EXCLUDED.last_analyzed_at,
		   updated_at = NOW()
		 RETURNING tenant_id, skill_id, enabled, mode, last_analyzed_at, created_at, updated_at`,
		tenantID, settings.SkillID, settings.Enabled, mode, settings.LastAnalyzedAt,
	).Scan(&out.TenantID, &out.SkillID, &out.Enabled, &out.Mode, &out.LastAnalyzedAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *PGSkillEvolutionStore) RecordUsage(ctx context.Context, metric store.SkillUsageMetric) error {
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
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO skill_usage_metrics
		 (id, tenant_id, skill_id, skill_slug, skill_version, agent_id, user_id, session_key,
		  trace_id, invocation_id, invocation_source, status, failure_reason, tool_calls_count, duration_ms)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, $16), $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		metric.ID, tenantID, metric.SkillID, metric.SkillSlug, metric.SkillVersion,
		metric.AgentID, metric.UserID, metric.SessionKey, metric.TraceID, metric.InvocationID,
		metric.InvocationSource, metric.Status, metric.FailureReason, metric.ToolCallsCount, metric.DurationMs, uuid.Nil,
	)
	return err
}

func (s *PGSkillEvolutionStore) AggregateUsage(ctx context.Context, skillID uuid.UUID, since *time.Time) (*store.SkillUsageStats, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	where := "tenant_id = $1 AND skill_id = $2"
	args := []any{tenantID, skillID}
	if since != nil {
		where += " AND created_at >= $3"
		args = append(args, *since)
	}
	query := fmt.Sprintf(
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE status = 'started'),
		        COUNT(*) FILTER (WHERE status = 'succeeded'),
		        COUNT(*) FILTER (WHERE status = 'failed'),
		        COUNT(*) FILTER (WHERE status = 'abandoned'),
		        MAX(created_at)
		 FROM skill_usage_metrics WHERE %s`,
		where,
	)
	var out store.SkillUsageStats
	out.SkillID = skillID
	var last sql.NullTime
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&out.TotalCalls, &out.Started, &out.Succeeded, &out.Failed, &out.Abandoned, &last); err != nil {
		return nil, err
	}
	if last.Valid {
		out.LastUsedAt = &last.Time
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
		if err := rows.Scan(&r.Reason, &r.Count, &r.LastSeen); err != nil {
			return nil, err
		}
		out.TopFailureReasons = append(out.TopFailureReasons, r)
	}
	return &out, rows.Err()
}

func (s *PGSkillEvolutionStore) ListUsage(ctx context.Context, skillID uuid.UUID, limit int) ([]store.SkillUsageMetric, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, skill_id, skill_slug, skill_version, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid),
		        COALESCE(user_id,''), COALESCE(session_key,''), COALESCE(trace_id,''), COALESCE(invocation_id,''),
		        invocation_source, status, COALESCE(failure_reason,''), tool_calls_count, duration_ms, created_at
		 FROM skill_usage_metrics
		 WHERE tenant_id = $1 AND skill_id = $2
		 ORDER BY created_at DESC LIMIT $3`,
		tenantID, skillID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SkillUsageMetric
	for rows.Next() {
		var m store.SkillUsageMetric
		if err := rows.Scan(&m.ID, &m.TenantID, &m.SkillID, &m.SkillSlug, &m.SkillVersion, &m.AgentID,
			&m.UserID, &m.SessionKey, &m.TraceID, &m.InvocationID, &m.InvocationSource, &m.Status,
			&m.FailureReason, &m.ToolCallsCount, &m.DurationMs, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PGSkillEvolutionStore) CreateSuggestion(ctx context.Context, sg store.SkillImprovementSuggestion) (*store.SkillImprovementSuggestion, error) {
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
	var out store.SkillImprovementSuggestion
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO skill_improvement_suggestions
		 (id, tenant_id, skill_id, skill_slug, suggestion_type, status, reason, evidence,
		  draft_patch, target_file, created_by_actor_type, created_by_actor_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE($8, '{}'::jsonb), COALESCE($9, '{}'::jsonb), $10, $11, $12)
		 RETURNING id, tenant_id, skill_id, skill_slug, suggestion_type, status, reason, evidence, draft_patch,
		           COALESCE(target_file,''), COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
		           COALESCE(reviewed_by_actor_type,''), COALESCE(reviewed_by_actor_id,''), reviewed_at, applied_version,
		           created_at, updated_at`,
		sg.ID, tenantID, sg.SkillID, sg.SkillSlug, sg.SuggestionType, sg.Status, sg.Reason,
		jsonOrEmpty(sg.Evidence), jsonOrEmpty(sg.DraftPatch), sg.TargetFile, sg.CreatedByActorType, sg.CreatedByActorID,
	).Scan(&out.ID, &out.TenantID, &out.SkillID, &out.SkillSlug, &out.SuggestionType, &out.Status, &out.Reason,
		&out.Evidence, &out.DraftPatch, &out.TargetFile, &out.CreatedByActorType, &out.CreatedByActorID,
		&out.ReviewedByActorType, &out.ReviewedByActorID, &out.ReviewedAt, &out.AppliedVersion,
		&out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *PGSkillEvolutionStore) ListSuggestions(ctx context.Context, skillID uuid.UUID, status string, limit int) ([]store.SkillImprovementSuggestion, error) {
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
	      FROM skill_improvement_suggestions WHERE tenant_id = $1 AND skill_id = $2`
	args := []any{tenantID, skillID}
	if status != "" {
		args = append(args, status)
		q += fmt.Sprintf(" AND status = $%d", len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPGSuggestions(rows)
}

func (s *PGSkillEvolutionStore) GetSuggestion(ctx context.Context, id uuid.UUID) (*store.SkillImprovementSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, skill_id, skill_slug, suggestion_type, status, reason, evidence, draft_patch,
		        COALESCE(target_file,''), COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
		        COALESCE(reviewed_by_actor_type,''), COALESCE(reviewed_by_actor_id,''), reviewed_at, applied_version,
		        created_at, updated_at
		 FROM skill_improvement_suggestions WHERE tenant_id = $1 AND id = $2`,
		tenantID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanPGSuggestions(rows)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	if _, _, err := s.resolveSkill(ctx, items[0].SkillID); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *PGSkillEvolutionStore) UpdateSuggestionStatus(ctx context.Context, id uuid.UUID, status, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE skill_improvement_suggestions
		 SET status = $1, reviewed_by_actor_type = $2, reviewed_by_actor_id = $3, reviewed_at = NOW(), updated_at = NOW()
		 WHERE tenant_id = $4 AND id = $5`,
		status, actorType, actorID, tenantID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("suggestion not found")
	}
	return s.GetSuggestion(ctx, id)
}

func (s *PGSkillEvolutionStore) MarkSuggestionApplied(ctx context.Context, id uuid.UUID, version int, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	tenantID := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE skill_improvement_suggestions
		 SET status = 'applied', applied_version = $1, reviewed_by_actor_type = $2,
		     reviewed_by_actor_id = $3, reviewed_at = COALESCE(reviewed_at, NOW()), updated_at = NOW()
		 WHERE tenant_id = $4 AND id = $5`,
		version, actorType, actorID, tenantID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("suggestion not found")
	}
	return s.GetSuggestion(ctx, id)
}

func (s *PGSkillEvolutionStore) CreateSkillVersion(ctx context.Context, v store.SkillVersion) (*store.SkillVersion, error) {
	if _, _, err := s.resolveSkill(ctx, v.SkillID); err != nil {
		return nil, err
	}
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	tenantID := tenantIDForInsert(ctx)
	var out store.SkillVersion
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO skill_versions
		 (id, tenant_id, skill_id, version, content_hash, changed_files, created_by_actor_type,
		  created_by_actor_id, created_from_suggestion_id)
		 VALUES ($1, $2, $3, $4, $5, COALESCE($6, '[]'::jsonb), $7, $8, $9)
		 RETURNING id, tenant_id, skill_id, version, content_hash, changed_files,
		           COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
		           created_from_suggestion_id, created_at`,
		v.ID, tenantID, v.SkillID, v.Version, v.ContentHash, jsonOrEmptyArray(v.ChangedFiles),
		v.CreatedByActorType, v.CreatedByActorID, v.CreatedFromSuggestionID,
	).Scan(&out.ID, &out.TenantID, &out.SkillID, &out.Version, &out.ContentHash, &out.ChangedFiles,
		&out.CreatedByActorType, &out.CreatedByActorID, &out.CreatedFromSuggestionID, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *PGSkillEvolutionStore) ListSkillVersions(ctx context.Context, skillID uuid.UUID, limit int) ([]store.SkillVersion, error) {
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
		 FROM skill_versions WHERE tenant_id = $1 AND skill_id = $2
		 ORDER BY version DESC LIMIT $3`,
		tenantID, skillID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPGSkillVersions(rows)
}

func (s *PGSkillEvolutionStore) GetSkillVersion(ctx context.Context, skillID uuid.UUID, version int) (*store.SkillVersion, error) {
	if _, _, err := s.resolveSkill(ctx, skillID); err != nil {
		return nil, err
	}
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, skill_id, version, content_hash, changed_files,
		        COALESCE(created_by_actor_type,''), COALESCE(created_by_actor_id,''),
		        created_from_suggestion_id, created_at
		 FROM skill_versions WHERE tenant_id = $1 AND skill_id = $2 AND version = $3`,
		tenantID, skillID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanPGSkillVersions(rows)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func scanPGSuggestions(rows *sql.Rows) ([]store.SkillImprovementSuggestion, error) {
	var out []store.SkillImprovementSuggestion
	for rows.Next() {
		var sg store.SkillImprovementSuggestion
		if err := rows.Scan(&sg.ID, &sg.TenantID, &sg.SkillID, &sg.SkillSlug, &sg.SuggestionType, &sg.Status,
			&sg.Reason, &sg.Evidence, &sg.DraftPatch, &sg.TargetFile, &sg.CreatedByActorType, &sg.CreatedByActorID,
			&sg.ReviewedByActorType, &sg.ReviewedByActorID, &sg.ReviewedAt, &sg.AppliedVersion,
			&sg.CreatedAt, &sg.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

func scanPGSkillVersions(rows *sql.Rows) ([]store.SkillVersion, error) {
	var out []store.SkillVersion
	for rows.Next() {
		var v store.SkillVersion
		if err := rows.Scan(&v.ID, &v.TenantID, &v.SkillID, &v.Version, &v.ContentHash, &v.ChangedFiles,
			&v.CreatedByActorType, &v.CreatedByActorID, &v.CreatedFromSuggestionID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ store.SkillEvolutionStore = (*PGSkillEvolutionStore)(nil)
