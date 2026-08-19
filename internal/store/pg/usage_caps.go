package pg

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

func (s *PGUsageCapStore) CreateUsageCapPolicy(ctx context.Context, p *store.UsageCapPolicy) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if err := s.validateUsageCapRefs(ctx, p.TenantID, p.AgentID, p.ProviderID); err != nil {
		return err
	}
	if p.Source == "" {
		p.Source = store.UsageCapSourceManual
	}
	const q = `
	INSERT INTO usage_cap_policies (
		id, tenant_id, agent_id, provider_id, provider_type, model_id, window_key,
		max_tokens, max_cost_micros, warn_at_percent, source, enabled, priority
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	RETURNING created_at, updated_at`
	return s.db.QueryRowContext(ctx, q,
		p.ID, p.TenantID, uuidPtrVal(p.AgentID), uuidPtrVal(p.ProviderID),
		nullEmpty(p.ProviderType), nullEmpty(p.ModelID), p.Window,
		intPtrVal(p.MaxTokens), intPtrVal(p.MaxCostMicros), warnPtrVal(p.WarnAtPercent),
		p.Source, p.Enabled, p.Priority,
	).Scan(&p.CreatedAt, &p.UpdatedAt)
}

func (s *PGUsageCapStore) ListUsageCapPolicies(ctx context.Context, scope store.UsageCapScope, includeDisabled bool) ([]store.UsageCapPolicy, error) {
	args := []any{scope.TenantID}
	conds := []string{"tenant_id = $1"}
	if !includeDisabled {
		conds = append(conds, "enabled = true")
	}
	if scope.AgentID != uuid.Nil {
		args = append(args, scope.AgentID)
		conds = append(conds, "(agent_id IS NULL OR agent_id = $"+fmt.Sprint(len(args))+")")
	} else if !includeDisabled {
		conds = append(conds, "agent_id IS NULL")
	}
	if scope.ProviderID != uuid.Nil {
		args = append(args, scope.ProviderID)
		conds = append(conds, "(provider_id IS NULL OR provider_id = $"+fmt.Sprint(len(args))+")")
	} else if !includeDisabled {
		conds = append(conds, "provider_id IS NULL")
	}
	if scope.ProviderType != "" {
		args = append(args, scope.ProviderType)
		conds = append(conds, "(provider_type IS NULL OR provider_type = $"+fmt.Sprint(len(args))+")")
	} else if !includeDisabled {
		conds = append(conds, "provider_type IS NULL")
	}
	if scope.ModelID != "" {
		args = append(args, scope.ModelID)
		conds = append(conds, "(model_id IS NULL OR model_id = $"+fmt.Sprint(len(args))+")")
	} else if !includeDisabled {
		conds = append(conds, "model_id IS NULL")
	}
	rows, err := s.db.QueryContext(ctx, policySelectSQL+" WHERE "+strings.Join(conds, " AND ")+" ORDER BY priority ASC, created_at ASC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsageCapPolicy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PGUsageCapStore) UpdateUsageCapPolicy(ctx context.Context, tenantID, id uuid.UUID, patch store.UsageCapPolicyPatch) (*store.UsageCapPolicy, error) {
	p, err := s.getPolicy(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if p.Source == store.UsageCapSourceAgentBudget {
		return nil, fmt.Errorf("%w: agent monthly budget", store.ErrUsageCapPolicyManaged)
	}
	if patch.AgentID != nil {
		p.AgentID = *patch.AgentID
	}
	if patch.ProviderID != nil {
		p.ProviderID = *patch.ProviderID
	}
	if patch.ProviderType != nil {
		p.ProviderType = *patch.ProviderType
	}
	if patch.ModelID != nil {
		p.ModelID = *patch.ModelID
	}
	if patch.Window != nil {
		p.Window = *patch.Window
	}
	if patch.MaxTokens != nil {
		p.MaxTokens = *patch.MaxTokens
	}
	if patch.MaxCostMicros != nil {
		p.MaxCostMicros = *patch.MaxCostMicros
	}
	if patch.Enabled != nil {
		p.Enabled = *patch.Enabled
	}
	if patch.Priority != nil {
		p.Priority = *patch.Priority
	}
	if patch.WarnAtPercent != nil {
		p.WarnAtPercent = *patch.WarnAtPercent
	}
	if err := s.validateUsageCapRefs(ctx, tenantID, p.AgentID, p.ProviderID); err != nil {
		return nil, err
	}
	const q = `
UPDATE usage_cap_policies SET agent_id=$3, provider_id=$4, provider_type=$5,
	model_id=$6, window_key=$7, max_tokens=$8, max_cost_micros=$9,
	warn_at_percent=$10, enabled=$11, priority=$12, updated_at=now()
WHERE tenant_id=$1 AND id=$2
RETURNING updated_at`
	if err := s.db.QueryRowContext(ctx, q, tenantID, id, uuidPtrVal(p.AgentID), uuidPtrVal(p.ProviderID),
		nullEmpty(p.ProviderType), nullEmpty(p.ModelID), p.Window, intPtrVal(p.MaxTokens),
		intPtrVal(p.MaxCostMicros), warnPtrVal(p.WarnAtPercent), p.Enabled, p.Priority).Scan(&p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PGUsageCapStore) DeleteUsageCapPolicy(ctx context.Context, tenantID, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM usage_cap_policies
		 WHERE tenant_id=$1 AND id=$2 AND COALESCE(source,'manual') <> $3`,
		tenantID, id, store.UsageCapSourceAgentBudget)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.getPolicy(ctx, tenantID, id); err == nil {
			return fmt.Errorf("%w: agent monthly budget", store.ErrUsageCapPolicyManaged)
		}
		return sql.ErrNoRows
	}
	return nil
}

func (s *PGUsageCapStore) ReserveUsage(ctx context.Context, req store.UsageReserveRequest, policies []store.UsageCapPolicy) (*store.UsageReservationResult, error) {
	if len(policies) == 0 {
		return &store.UsageReservationResult{TenantID: req.TenantID, ReservationKey: req.ReservationKey, Skipped: true, Reason: "no_policy"}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, p := range policies {
		start, end := usageWindow(time.Now().UTC(), p.Window)
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_cap_counters (policy_id, window_start, window_end) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, p.ID, start, end); err != nil {
			return nil, err
		}
		meta := req.Metadata
		if len(meta) == 0 {
			meta = json.RawMessage(`{}`)
		}
		var inserted bool
		err := tx.QueryRowContext(ctx, `
INSERT INTO usage_cap_reservations (reservation_key, policy_id, window_start, reserved_tokens, reserved_cost_micros, metadata)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (reservation_key, policy_id) DO NOTHING
RETURNING true`,
			req.ReservationKey, p.ID, start, req.EstimatedTokens, req.EstimatedCostMicros, meta).Scan(&inserted)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var ok bool
		err = tx.QueryRowContext(ctx, `
UPDATE usage_cap_counters SET
		reserved_tokens = reserved_tokens + $3,
		reserved_cost_micros = reserved_cost_micros + $4,
		updated_at = now()
WHERE policy_id=$1 AND window_start=$2
	  AND ($5::bigint IS NULL OR used_tokens + reserved_tokens + $3 <= $5)
	  AND ($6::bigint IS NULL OR used_cost_micros + reserved_cost_micros + $4 <= $6)
		RETURNING true`, p.ID, start, req.EstimatedTokens, req.EstimatedCostMicros, intPtrVal(p.MaxTokens), intPtrVal(p.MaxCostMicros)).Scan(&ok)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			return nil, &store.UsageCapExceededError{PolicyID: p.ID, Reason: "cap_exceeded"}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &store.UsageReservationResult{TenantID: req.TenantID, ReservationKey: req.ReservationKey, Policies: policies}, nil
}

func (s *PGUsageCapStore) ReconcileUsage(ctx context.Context, req store.UsageReconcileRequest) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	meta := req.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	rows, err := tx.QueryContext(ctx, `
UPDATE usage_cap_reservations
SET status=$2, actual_tokens=$3, actual_cost_micros=$4, metadata=$5, updated_at=now()
WHERE reservation_key=$1 AND status='reserved'
RETURNING policy_id, window_start, reserved_tokens, reserved_cost_micros`,
		req.ReservationKey, nullStatus(req.Status), req.ActualTokens, req.ActualCostMicros, meta)
	if err != nil {
		return err
	}
	type resv struct {
		policyID uuid.UUID
		start    time.Time
		tokens   int64
		cost     int64
	}
	var reservations []resv
	for rows.Next() {
		var r resv
		if err := rows.Scan(&r.policyID, &r.start, &r.tokens, &r.cost); err != nil {
			rows.Close()
			return err
		}
		reservations = append(reservations, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range reservations {
		// Reconcile settles a reservation: the consumed amount (actual) leaves
		// reserved and lands in used; the unconsumed portion stays reserved as
		// pending usage. Subtracting the full reserved amount would drop the
		// reservation to zero regardless of what was actually consumed.
		if _, err := tx.ExecContext(ctx, `
UPDATE usage_cap_counters SET
	reserved_tokens = GREATEST(reserved_tokens - $3, 0),
	reserved_cost_micros = GREATEST(reserved_cost_micros - $4, 0),
	used_tokens = used_tokens + $5,
	used_cost_micros = used_cost_micros + $6,
	updated_at = now()
WHERE policy_id=$1 AND window_start=$2`, r.policyID, r.start, req.ActualTokens, req.ActualCostMicros, req.ActualTokens, req.ActualCostMicros); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PGUsageCapStore) ListUsageCapUtilization(ctx context.Context, tenantID uuid.UUID) ([]store.UsageCapUtilization, error) {
	policies, err := s.ListUsageCapPolicies(ctx, store.UsageCapScope{TenantID: tenantID}, true)
	if err != nil {
		return nil, err
	}
	out := make([]store.UsageCapUtilization, 0, len(policies))
	for _, p := range policies {
		start, end := usageWindow(time.Now().UTC(), p.Window)
		u := store.UsageCapUtilization{Policy: p, WindowStart: start, WindowEnd: end}
		err := s.db.QueryRowContext(ctx, `
SELECT used_tokens, reserved_tokens, used_cost_micros, reserved_cost_micros
FROM usage_cap_counters WHERE policy_id=$1 AND window_start=$2`, p.ID, start).Scan(&u.UsedTokens, &u.ReservedTokens, &u.UsedCostMicros, &u.ReservedCostMicros)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				out = append(out, u)
				continue
			}
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func (s *PGUsageCapStore) ListUsageCapEvents(ctx context.Context, tenantID uuid.UUID, limit int) ([]store.UsageCapEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, policy_id, COALESCE(reservation_key,''), decision, COALESCE(reason,''),
	estimated_tokens, estimated_cost_micros, actual_tokens, actual_cost_micros, metadata, created_at
FROM usage_cap_events WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsageCapEvent
	for rows.Next() {
		var e store.UsageCapEvent
		var pid uuid.NullUUID
		if err := rows.Scan(&e.ID, &e.TenantID, &pid, &e.ReservationKey, &e.Decision, &e.Reason,
			&e.EstimatedTokens, &e.EstimatedCostMicros, &e.ActualTokens, &e.ActualCostMicros, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		if pid.Valid {
			e.PolicyID = &pid.UUID
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PGUsageCapStore) InsertUsageCapEvent(ctx context.Context, event *store.UsageCapEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if len(event.Metadata) == 0 {
		event.Metadata = json.RawMessage(`{}`)
	}
	return s.db.QueryRowContext(ctx, `
INSERT INTO usage_cap_events (id, tenant_id, policy_id, reservation_key, decision, reason,
	estimated_tokens, estimated_cost_micros, actual_tokens, actual_cost_micros, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING created_at`, event.ID, event.TenantID, uuidPtrVal(event.PolicyID), nullEmpty(event.ReservationKey),
		event.Decision, nullEmpty(event.Reason), event.EstimatedTokens, event.EstimatedCostMicros,
		event.ActualTokens, event.ActualCostMicros, event.Metadata).Scan(&event.CreatedAt)
}

func (s *PGUsageCapStore) getPolicy(ctx context.Context, tenantID, id uuid.UUID) (*store.UsageCapPolicy, error) {
	row := s.db.QueryRowContext(ctx, policySelectSQL+" WHERE tenant_id=$1 AND id=$2", tenantID, id)
	p, err := scanPolicy(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PGUsageCapStore) validateUsageCapRefs(ctx context.Context, tenantID uuid.UUID, agentID, providerID *uuid.UUID) error {
	if agentID != nil && *agentID != uuid.Nil {
		var ok bool
		if err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM agents WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
)`, *agentID, tenantID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errors.New("agent_id does not belong to tenant")
		}
	}
	if providerID != nil && *providerID != uuid.Nil {
		var ok bool
		if err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM llm_providers WHERE id=$1 AND tenant_id IN ($2, $3)
)`, *providerID, tenantID, store.MasterTenantID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errors.New("provider_id does not belong to tenant")
		}
	}
	return nil
}

const policySelectSQL = `SELECT id, tenant_id, agent_id, provider_id, COALESCE(provider_type,''), COALESCE(model_id,''),
	window_key, max_tokens, max_cost_micros, warn_at_percent, COALESCE(source,'manual'), enabled, priority, created_at, updated_at FROM usage_cap_policies`

func scanPolicy(row scanner) (store.UsageCapPolicy, error) {
	var p store.UsageCapPolicy
	var agentID, providerID uuid.NullUUID
	var maxTokens, maxCost sql.NullInt64
	var warnAt sql.NullFloat64
	err := row.Scan(&p.ID, &p.TenantID, &agentID, &providerID, &p.ProviderType, &p.ModelID,
		&p.Window, &maxTokens, &maxCost, &warnAt, &p.Source, &p.Enabled, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
	if agentID.Valid {
		p.AgentID = &agentID.UUID
	}
	if providerID.Valid {
		p.ProviderID = &providerID.UUID
	}
	if maxTokens.Valid {
		p.MaxTokens = &maxTokens.Int64
	}
	if maxCost.Valid {
		p.MaxCostMicros = &maxCost.Int64
	}
	if warnAt.Valid {
		p.WarnAtPercent = &warnAt.Float64
	}
	return p, err
}

// GetBudgetUsage returns per-policy spend-to-date rows for the tenant's
// enabled policies. Window bounds are derived from BudgetUsageWindow when set;
// otherwise each policy's own window_key is used (usageWindow already handles
// default = hour). Percent is 0..1 against the policy's active limit.
func (s *PGUsageCapStore) GetBudgetUsage(ctx context.Context, tenantID uuid.UUID, window store.BudgetUsageWindow) ([]store.BudgetUsageRow, error) {
	policies, err := s.ListUsageCapPolicies(ctx, store.UsageCapScope{TenantID: tenantID}, false)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]store.BudgetUsageRow, 0, len(policies))
	for _, p := range policies {
		var start, end time.Time
		switch {
		case window.Window != "":
			start, end = usageWindow(now, window.Window)
		case !window.Start.IsZero() && !window.End.IsZero():
			start, end = window.Start.UTC(), window.End.UTC()
		default:
			start, end = usageWindow(now, p.Window)
		}
		row := store.BudgetUsageRow{Policy: p, WindowStart: start, WindowEnd: end, WarnAtPercent: p.WarnAtPercent}
		err := s.db.QueryRowContext(ctx, `
SELECT used_tokens, reserved_tokens, used_cost_micros, reserved_cost_micros
FROM usage_cap_counters WHERE policy_id=$1 AND window_start=$2`, p.ID, start).
			Scan(&row.UsedTokens, &row.ReservedTokens, &row.UsedCostMicros, &row.ReservedCostMicros)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		row.PercentUsed = budgetRowPercent(row)
		row.Warned = s.budgetWindowWarned(ctx, tenantID, p.ID, row)
		out = append(out, row)
	}
	return out, nil
}

func (s *PGUsageCapStore) budgetWindowWarned(ctx context.Context, tenantID uuid.UUID, policyID uuid.UUID, row store.BudgetUsageRow) bool {
	if !row.WindowStart.IsZero() {
		var exists bool
		if err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM usage_cap_events
	WHERE tenant_id=$1 AND policy_id=$2 AND decision=$3 AND metadata->>'window_start' = $4
)`, tenantID, policyID, store.UsageCapEventWarn, row.WindowStart.UTC().Format(time.RFC3339)).
			Scan(&exists); err != nil {
			return false
		}
		return exists
	}
	return false
}

// BudgetWindowWarned reports whether a warn event already fired for the given
// policy + window (stored in event metadata under window_start). Reconcile uses
// it to fire the budget-threshold alert exactly once per window.
func (s *PGUsageCapStore) BudgetWindowWarned(ctx context.Context, tenantID uuid.UUID, policyID uuid.UUID, windowStart time.Time) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM usage_cap_events
	WHERE tenant_id=$1 AND policy_id=$2 AND decision=$3 AND metadata->>'window_start' = $4
)`, tenantID, policyID, store.UsageCapEventWarn, windowStart.UTC().Format(time.RFC3339)).
		Scan(&exists)
	return exists, err
}

// budgetRowPercent computes 0..1 utilization against the policy's active limit.
// Cost limit wins when both are set (money is the classic budget guardrail).
func budgetRowPercent(row store.BudgetUsageRow) float64 {
	limitCost := row.Policy.MaxCostMicros
	if limitCost != nil {
		if *limitCost <= 0 {
			return 0
		}
		used := float64(row.UsedCostMicros + row.ReservedCostMicros)
		percent := used / float64(*limitCost)
		if percent > 1 {
			return 1
		}
		return percent
	}
	limitTokens := row.Policy.MaxTokens
	if limitTokens != nil {
		if *limitTokens <= 0 {
			return 0
		}
		used := float64(row.UsedTokens + row.ReservedTokens)
		percent := used / float64(*limitTokens)
		if percent > 1 {
			return 1
		}
		return percent
	}
	return 0
}

func warnPtrVal(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func usageWindow(now time.Time, window string) (time.Time, time.Time) {
	now = now.UTC()
	switch window {
	case store.UsageCapWindowDay:
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1)
	case store.UsageCapWindowWeek:
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(weekday - 1))
		return start, start.AddDate(0, 0, 7)
	case store.UsageCapWindowMonth:
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	default:
		start := now.Truncate(time.Hour)
		return start, start.Add(time.Hour)
	}
}

func uuidPtrVal(v *uuid.UUID) any {
	if v == nil || *v == uuid.Nil {
		return nil
	}
	return *v
}

func intPtrVal(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullStatus(s string) string {
	if strings.TrimSpace(s) == "" {
		return "reconciled"
	}
	return s
}
