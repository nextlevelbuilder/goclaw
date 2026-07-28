package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var _ store.TeamWorkClassificationAuditStore = (*PGTeamStore)(nil)

// RecordTeamWorkClassificationAudit inserts one append-only Team Work
// classification audit row, tenant-scoped from context. It populates audit.ID
// and audit.CreatedAt so the caller can link a resulting workflow to it.
func (s *PGTeamStore) RecordTeamWorkClassificationAudit(ctx context.Context, audit *store.TeamWorkClassificationAudit) error {
	if audit == nil {
		return fmt.Errorf("audit is required")
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	if audit.TenantID != uuid.Nil && audit.TenantID != tid {
		return fmt.Errorf("audit tenant mismatch")
	}
	audit.TenantID = tid
	if audit.ID == uuid.Nil {
		audit.ID = store.GenNewID()
	}
	if audit.CreatedAt.IsZero() {
		audit.CreatedAt = time.Now()
	}
	if audit.AuditSchemaVersion == 0 {
		audit.AuditSchemaVersion = store.TeamWorkClassificationAuditSchemaVersion
	}
	traits := jsonOrDefault(audit.Traits, "[]")
	stageStatuses := jsonOrDefault(audit.StageStatuses, "{}")

	_, err := s.db.ExecContext(ctx, `INSERT INTO team_work_classification_audits (
		id, tenant_id, ingress, run_id, session_key, agent_id,
		original_hash, resolved_hash, verified_shape, traits, requested_mode, effective_mode, independent_review,
		selected_owner_agent_id, coordinator_agent_id, plan_hash, stage_statuses, degraded_stage, degraded_reason,
		classifier_provider, classifier_model, schema_version, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		audit.ID, audit.TenantID, audit.Ingress, audit.RunID, audit.SessionKey, audit.AgentID,
		audit.OriginalHash, audit.ResolvedHash, audit.VerifiedShape, traits, audit.RequestedMode, audit.EffectiveMode, audit.IndependentReview,
		audit.SelectedOwnerAgentID, audit.CoordinatorAgentID, audit.PlanHash, stageStatuses, audit.DegradedStage, audit.DegradedReason,
		audit.ClassifierProvider, audit.ClassifierModel, audit.AuditSchemaVersion, audit.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert team work classification audit: %w", err)
	}
	return nil
}

// jsonOrDefault returns the raw JSON if non-empty, else the given default. It
// keeps NOT NULL JSON columns valid when a caller leaves the field unset.
func jsonOrDefault(raw json.RawMessage, def string) []byte {
	if len(raw) == 0 {
		return []byte(def)
	}
	return []byte(raw)
}
