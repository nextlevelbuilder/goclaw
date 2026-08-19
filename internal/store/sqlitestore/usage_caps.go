//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteUsageCapStore implements store.UsageCapStore backed by SQLite.
// Mirrors the PG implementation in internal/store/pg (usage_pricing.go +
// usage_caps.go) but uses ? placeholders, TEXT UUIDs, and TEXT RFC3339Nano
// timestamps.
type SQLiteUsageCapStore struct {
	db *sql.DB
}

// scanner is implemented by *sql.Row and *sql.Rows, letting scan helpers read
// either a single row or a streaming row set.
type scanner interface{ Scan(dest ...any) error }

// NewSQLiteUsageCapStore creates a SQLite-backed usage cap + pricing store.
func NewSQLiteUsageCapStore(db *sql.DB) *SQLiteUsageCapStore {
	return &SQLiteUsageCapStore{db: db}
}

// --- Pricing catalog ---

func (s *SQLiteUsageCapStore) UpsertPricingCatalog(ctx context.Context, entries []store.UsagePricingCatalogEntry) (int, error) {
	upserted := 0
	for _, e := range entries {
		if strings.TrimSpace(e.ModelID) == "" {
			continue
		}
		if err := validateUsagePricingFields(e.Pricing); err != nil {
			continue
		}
		if len(e.RawPricing) == 0 {
			e.RawPricing = json.RawMessage(`{}`)
		}
		if len(e.RawModel) == 0 {
			e.RawModel = json.RawMessage(`{}`)
		}
		now := time.Now().UTC()
		// SQLite UPSERT: ON CONFLICT with a UNIQUE column target.
		_, err := s.db.ExecContext(ctx, `
INSERT INTO usage_pricing_catalog (
	id, model_id, canonical_model_id, raw_pricing, raw_model,
	input_price, output_price, cache_read_price, cache_write_price,
	reasoning_price, request_price, image_price, web_search_price,
	synced_at, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(model_id) DO UPDATE SET
	canonical_model_id = excluded.canonical_model_id,
	raw_pricing = excluded.raw_pricing,
	raw_model = excluded.raw_model,
	input_price = excluded.input_price,
	output_price = excluded.output_price,
	cache_read_price = excluded.cache_read_price,
	cache_write_price = excluded.cache_write_price,
	reasoning_price = excluded.reasoning_price,
	request_price = excluded.request_price,
	image_price = excluded.image_price,
	web_search_price = excluded.web_search_price,
	synced_at = excluded.synced_at,
	updated_at = excluded.updated_at`,
			uuid.New().String(), e.ModelID, nullEmpty(e.CanonicalModelID),
			sqliteJSON(e.RawPricing), sqliteJSON(e.RawModel),
			priceVal(e.Pricing.Input), priceVal(e.Pricing.Output),
			priceVal(e.Pricing.CacheRead), priceVal(e.Pricing.CacheWrite),
			priceVal(e.Pricing.Reasoning), priceVal(e.Pricing.Request),
			priceVal(e.Pricing.Image), priceVal(e.Pricing.WebSearch),
			sqliteTimeStr(e.SyncedAt), sqliteTimeStr(now), sqliteTimeStr(now),
		)
		if err != nil {
			return 0, err
		}
		upserted++
	}
	return upserted, nil
}

func (s *SQLiteUsageCapStore) ListPricingCatalog(ctx context.Context, q store.UsagePricingQuery) ([]store.UsagePricingCatalogEntry, error) {
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var (
		args  []any
		where = "1=1"
	)
	if q.ModelID != "" {
		args = append(args, "%"+q.ModelID+"%")
		where = "model_id LIKE ?"
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, model_id, COALESCE(canonical_model_id,''), raw_pricing, raw_model,
	input_price, output_price, cache_read_price, cache_write_price,
	reasoning_price, request_price, image_price, web_search_price,
	synced_at, created_at, updated_at
FROM usage_pricing_catalog
WHERE `+where+`
ORDER BY model_id
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsagePricingCatalogEntry
	for rows.Next() {
		e, err := scanSQLiteCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteUsageCapStore) PutPricingOverride(ctx context.Context, o *store.UsagePricingOverride) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if o.ProviderID == uuid.Nil || o.TenantID == uuid.Nil || o.ModelID == "" {
		return errors.New("tenant_id, provider_id, and model_id are required")
	}
	if err := validateUsagePricingFields(o.Pricing); err != nil {
		return err
	}
	if err := s.validateUsageCapRefs(ctx, o.TenantID, nil, &o.ProviderID); err != nil {
		return err
	}
	now := time.Now().UTC()
	err := s.db.QueryRowContext(ctx, `
INSERT INTO usage_pricing_overrides (
	id, tenant_id, provider_id, provider_type, model_id,
	input_price, output_price, cache_read_price, cache_write_price,
	reasoning_price, request_price, image_price, web_search_price,
	enabled, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(tenant_id, provider_id, model_id) DO UPDATE SET
	provider_type = excluded.provider_type,
	input_price = excluded.input_price,
	output_price = excluded.output_price,
	cache_read_price = excluded.cache_read_price,
	cache_write_price = excluded.cache_write_price,
	reasoning_price = excluded.reasoning_price,
	request_price = excluded.request_price,
	image_price = excluded.image_price,
	web_search_price = excluded.web_search_price,
	enabled = excluded.enabled,
	updated_at = excluded.updated_at
RETURNING created_at, updated_at`,
		o.ID.String(), o.TenantID.String(), o.ProviderID.String(), o.ProviderType, o.ModelID,
		priceVal(o.Pricing.Input), priceVal(o.Pricing.Output),
		priceVal(o.Pricing.CacheRead), priceVal(o.Pricing.CacheWrite),
		priceVal(o.Pricing.Reasoning), priceVal(o.Pricing.Request),
		priceVal(o.Pricing.Image), priceVal(o.Pricing.WebSearch),
		o.Enabled, sqliteTimeStr(now), sqliteTimeStr(now),
	).Scan(sqliteTimeScan(&o.CreatedAt), sqliteTimeScan(&o.UpdatedAt))
	if err != nil {
		return err
	}
	return nil
}

func (s *SQLiteUsageCapStore) ListPricingOverrides(ctx context.Context, q store.UsagePricingQuery) ([]store.UsagePricingOverride, error) {
	args := []any{q.TenantID.String()}
	where := "tenant_id = ?"
	if q.ProviderID != uuid.Nil {
		args = append(args, q.ProviderID.String())
		where += " AND provider_id = ?"
	}
	rows, err := s.db.QueryContext(ctx, sqliteOverrideSelectSQL+" WHERE "+where+" ORDER BY updated_at DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsagePricingOverride
	for rows.Next() {
		o, err := scanSQLiteOverride(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *SQLiteUsageCapStore) DeletePricingOverride(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM usage_pricing_overrides WHERE tenant_id=? AND id=?`, tenantID.String(), id.String())
	return err
}

func (s *SQLiteUsageCapStore) ResolvePricing(ctx context.Context, tenantID, providerID uuid.UUID, providerName, providerType, modelID string) (*store.ResolvedUsagePricing, error) {
	candidates := usagePricingModelCandidates(providerName, providerType, modelID)
	if tenantID != uuid.Nil && providerID != uuid.Nil {
		for _, candidate := range candidates {
			row := s.db.QueryRowContext(ctx, sqliteOverrideSelectSQL+` WHERE tenant_id=? AND provider_id=? AND model_id=? AND enabled=1`, tenantID.String(), providerID.String(), candidate)
			if o, err := scanSQLiteOverride(row); err == nil {
				return &store.ResolvedUsagePricing{ModelID: o.ModelID, ProviderID: providerID, ProviderType: providerType, Source: "override", Pricing: o.Pricing, OverrideID: o.ID}, nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
	}
	for _, candidate := range candidates {
		row := s.db.QueryRowContext(ctx, `
SELECT id, model_id, COALESCE(canonical_model_id,''), raw_pricing, raw_model,
	input_price, output_price, cache_read_price, cache_write_price,
	reasoning_price, request_price, image_price, web_search_price,
	synced_at, created_at, updated_at
FROM usage_pricing_catalog WHERE model_id=? OR canonical_model_id=? LIMIT 1`, candidate, candidate)
		e, err := scanSQLiteCatalog(row)
		if err == nil {
			return &store.ResolvedUsagePricing{ModelID: e.ModelID, ProviderID: providerID, ProviderType: providerType, Source: "catalog", Pricing: e.Pricing, CatalogSynced: &e.SyncedAt}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return nil, sql.ErrNoRows
}

// --- Usage cap policies ---

func (s *SQLiteUsageCapStore) CreateUsageCapPolicy(ctx context.Context, p *store.UsageCapPolicy) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if err := s.validateUsageCapRefs(ctx, p.TenantID, p.AgentID, p.ProviderID); err != nil {
		return err
	}
	if p.Source == "" {
		p.Source = store.UsageCapSourceManual
	}
	now := time.Now().UTC()
	const q = `
INSERT INTO usage_cap_policies (
	id, tenant_id, agent_id, provider_id, provider_type, model_id, window_key,
	max_tokens, max_cost_micros, warn_at_percent, source, enabled, priority,
	created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`
	res, err := s.db.ExecContext(ctx, q,
		p.ID.String(), p.TenantID.String(), sqliteNilUUIDStr(p.AgentID), sqliteNilUUIDStr(p.ProviderID),
		nullEmpty(p.ProviderType), nullEmpty(p.ModelID), p.Window,
		sqliteIntPtr(p.MaxTokens), sqliteIntPtr(p.MaxCostMicros), warnPtrVal(p.WarnAtPercent),
		p.Source, p.Enabled, p.Priority, sqliteTimeStr(now), sqliteTimeStr(now))
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

func (s *SQLiteUsageCapStore) ListUsageCapPolicies(ctx context.Context, scope store.UsageCapScope, includeDisabled bool) ([]store.UsageCapPolicy, error) {
	args := []any{scope.TenantID.String()}
	conds := []string{"tenant_id = ?"}
	if !includeDisabled {
		conds = append(conds, "enabled = 1")
	}
	if scope.AgentID != uuid.Nil {
		args = append(args, scope.AgentID.String())
		conds = append(conds, "(agent_id IS NULL OR agent_id = ?)")
	} else if !includeDisabled {
		conds = append(conds, "agent_id IS NULL")
	}
	if scope.ProviderID != uuid.Nil {
		args = append(args, scope.ProviderID.String())
		conds = append(conds, "(provider_id IS NULL OR provider_id = ?)")
	} else if !includeDisabled {
		conds = append(conds, "provider_id IS NULL")
	}
	if scope.ProviderType != "" {
		args = append(args, scope.ProviderType)
		conds = append(conds, "(provider_type IS NULL OR provider_type = ?)")
	} else if !includeDisabled {
		conds = append(conds, "provider_type IS NULL")
	}
	if scope.ModelID != "" {
		args = append(args, scope.ModelID)
		conds = append(conds, "(model_id IS NULL OR model_id = ?)")
	} else if !includeDisabled {
		conds = append(conds, "model_id IS NULL")
	}
	rows, err := s.db.QueryContext(ctx, sqlitePolicySelectSQL+" WHERE "+strings.Join(conds, " AND ")+" ORDER BY priority ASC, created_at ASC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsageCapPolicy
	for rows.Next() {
		p, err := scanSQLitePolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLiteUsageCapStore) UpdateUsageCapPolicy(ctx context.Context, tenantID, id uuid.UUID, patch store.UsageCapPolicyPatch) (*store.UsageCapPolicy, error) {
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
UPDATE usage_cap_policies SET agent_id=?, provider_id=?, provider_type=?,
	model_id=?, window_key=?, max_tokens=?, max_cost_micros=?,
	warn_at_percent=?, enabled=?, priority=?, updated_at=?
WHERE tenant_id=? AND id=?`
	res, err := s.db.ExecContext(ctx, q,
		sqliteNilUUIDStr(p.AgentID), sqliteNilUUIDStr(p.ProviderID),
		nullEmpty(p.ProviderType), nullEmpty(p.ModelID), p.Window,
		sqliteIntPtr(p.MaxTokens), sqliteIntPtr(p.MaxCostMicros),
		warnPtrVal(p.WarnAtPercent), p.Enabled, p.Priority,
		sqliteTimeStr(time.Now().UTC()), tenantID.String(), id.String())
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return nil, sql.ErrNoRows
	}
	return s.getPolicy(ctx, tenantID, id)
}

func (s *SQLiteUsageCapStore) DeleteUsageCapPolicy(ctx context.Context, tenantID, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM usage_cap_policies WHERE tenant_id=? AND id=? AND COALESCE(source,'manual') <> ?`,
		tenantID.String(), id.String(), store.UsageCapSourceAgentBudget)
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

// Reserved usage ----------------------------------------------------------

func (s *SQLiteUsageCapStore) ReserveUsage(ctx context.Context, req store.UsageReserveRequest, policies []store.UsageCapPolicy) (*store.UsageReservationResult, error) {
	if len(policies) == 0 {
		return &store.UsageReservationResult{TenantID: req.TenantID, ReservationKey: req.ReservationKey, Skipped: true, Reason: "no_policy"}, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	meta := req.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	for _, p := range policies {
		start, end := usageWindow(time.Now().UTC(), p.Window)
		startStr, endStr := sqliteTimeStr(start), sqliteTimeStr(end)
		// Ensure a counter row exists for (policy_id, window_start).
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO usage_cap_counters (policy_id, window_start, window_end) VALUES (?,?,?)`,
			p.ID.String(), startStr, endStr); err != nil {
			return nil, err
		}
		// Record the reservation first (deduped by (reservation_key, policy_id)).
		// Skipping the counter update when the row already exists makes the whole
		// reserve idempotent for a given key, mirroring the PG ON CONFLICT pattern.
		// id has no DEFAULT on SQLite (PG uses gen_random_uuid()), so supply one
		// explicitly — an INSERT OR IGNORE that violates NOT NULL id would
		// silently no-op and the counter below would never be reserved.
		ins, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO usage_cap_reservations (
	id, reservation_key, policy_id, window_start, reserved_tokens, reserved_cost_micros,
	metadata, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?)`,
			uuid.New().String(), req.ReservationKey, p.ID.String(), startStr, req.EstimatedTokens, req.EstimatedCostMicros,
			string(meta), sqliteTimeStr(time.Now().UTC()), sqliteTimeStr(time.Now().UTC()),
		)
		if err != nil {
			return nil, err
		}
		if n, _ := ins.RowsAffected(); n == 0 {
			// Already reserved for this key/window — nothing more to do.
			continue
		}
		// Reserve against the counter; a low-cap policy may still reject.
		res, err := tx.ExecContext(ctx, `
UPDATE usage_cap_counters SET
	reserved_tokens = reserved_tokens + ?,
	reserved_cost_micros = reserved_cost_micros + ?,
	updated_at = ?
WHERE policy_id=? AND window_start=?
  AND (? IS NULL OR used_tokens + reserved_tokens + ? <= ?)
  AND (? IS NULL OR used_cost_micros + reserved_cost_micros + ? <= ?)`,
			req.EstimatedTokens, req.EstimatedCostMicros, sqliteTimeStr(time.Now().UTC()),
			p.ID.String(), startStr,
			sqliteIntPtr(p.MaxTokens), req.EstimatedTokens, sqliteIntPtr(p.MaxTokens),
			sqliteIntPtr(p.MaxCostMicros), req.EstimatedCostMicros, sqliteIntPtr(p.MaxCostMicros),
		)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			// Counter would exceed the cap.
			return nil, &store.UsageCapExceededError{PolicyID: p.ID, Reason: "cap_exceeded"}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &store.UsageReservationResult{TenantID: req.TenantID, ReservationKey: req.ReservationKey, Policies: policies}, nil
}

func (s *SQLiteUsageCapStore) ReconcileUsage(ctx context.Context, req store.UsageReconcileRequest) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	meta := req.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	now := sqliteTimeStr(time.Now().UTC())
	// Move 'reserved' reservations to their final status, returning the reserved
	// amounts so counters can be adjusted.
	rows, err := tx.QueryContext(ctx, `
UPDATE usage_cap_reservations
SET status=?, actual_tokens=?, actual_cost_micros=?, metadata=?, updated_at=?
WHERE reservation_key=? AND status='reserved'
RETURNING policy_id, window_start, reserved_tokens, reserved_cost_micros`,
		nullStatus(req.Status), req.ActualTokens, req.ActualCostMicros, string(meta), now,
		req.ReservationKey)
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
		var policyIDStr string
		var start sqliteTime
		if err := rows.Scan(&policyIDStr, &start, &r.tokens, &r.cost); err != nil {
			rows.Close()
			return err
		}
		r.policyID = uuid.MustParse(policyIDStr)
		r.start = start.Time
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
	reserved_tokens = MAX(reserved_tokens - ?, 0),
	reserved_cost_micros = MAX(reserved_cost_micros - ?, 0),
	used_tokens = used_tokens + ?,
	used_cost_micros = used_cost_micros + ?,
	updated_at = ?
WHERE policy_id=? AND window_start=?`,
			req.ActualTokens, req.ActualCostMicros, req.ActualTokens, req.ActualCostMicros, sqliteTimeStr(time.Now().UTC()),
			r.policyID.String(), sqliteTimeStr(r.start)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteUsageCapStore) ListUsageCapUtilization(ctx context.Context, tenantID uuid.UUID) ([]store.UsageCapUtilization, error) {
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
FROM usage_cap_counters WHERE policy_id=? AND window_start=?`,
			p.ID.String(), sqliteTimeStr(start)).Scan(&u.UsedTokens, &u.ReservedTokens, &u.UsedCostMicros, &u.ReservedCostMicros)
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

func (s *SQLiteUsageCapStore) ListUsageCapEvents(ctx context.Context, tenantID uuid.UUID, limit int) ([]store.UsageCapEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, tenant_id, policy_id, COALESCE(reservation_key,''), decision, COALESCE(reason,''),
	estimated_tokens, estimated_cost_micros, actual_tokens, actual_cost_micros, metadata, created_at
FROM usage_cap_events WHERE tenant_id=? ORDER BY created_at DESC LIMIT ?`, tenantID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsageCapEvent
	for rows.Next() {
		var e store.UsageCapEvent
		var policyID sql.NullString
		var createdAt sqliteTime
		var metadata string
		if err := rows.Scan(&e.ID, &e.TenantID, &policyID, &e.ReservationKey, &e.Decision, &e.Reason,
			&e.EstimatedTokens, &e.EstimatedCostMicros, &e.ActualTokens, &e.ActualCostMicros,
			&metadata, &createdAt); err != nil {
			return nil, err
		}
		if policyID.Valid && policyID.String != "" {
			id := uuid.MustParse(policyID.String)
			e.PolicyID = &id
		}
		if metadata != "" {
			e.Metadata = json.RawMessage(metadata)
		} else {
			e.Metadata = json.RawMessage(`{}`)
		}
		e.CreatedAt = createdAt.Time
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteUsageCapStore) InsertUsageCapEvent(ctx context.Context, event *store.UsageCapEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if len(event.Metadata) == 0 {
		event.Metadata = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	const q = `
INSERT INTO usage_cap_events (
	id, tenant_id, policy_id, reservation_key, decision, reason,
	estimated_tokens, estimated_cost_micros, actual_tokens, actual_cost_micros, metadata, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`
	if _, err := s.db.ExecContext(ctx, q,
		event.ID.String(), event.TenantID.String(), sqliteNilUUIDStr(event.PolicyID), nullEmpty(event.ReservationKey),
		event.Decision, nullEmpty(event.Reason), event.EstimatedTokens, event.EstimatedCostMicros,
		event.ActualTokens, event.ActualCostMicros, string(event.Metadata), sqliteTimeStr(now)); err != nil {
		return err
	}
	event.CreatedAt = now
	return nil
}

// Budget overview ---------------------------------------------------------

// GetBudgetUsage returns per-policy spend-to-date rows for the tenant's enabled
// policies, mirroring the PG implementation. Warned is best-effort: a metadata
// lookup failure degrades to false (skip once-per-window alert dedup).
func (s *SQLiteUsageCapStore) GetBudgetUsage(ctx context.Context, tenantID uuid.UUID, window store.BudgetUsageWindow) ([]store.BudgetUsageRow, error) {
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
FROM usage_cap_counters WHERE policy_id=? AND window_start=?`, p.ID.String(), sqliteTimeStr(start)).
			Scan(&row.UsedTokens, &row.ReservedTokens, &row.UsedCostMicros, &row.ReservedCostMicros)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		row.PercentUsed = budgetRowPercent(row)
		row.Warned = s.budgetWindowWarned(ctx, tenantID, p.ID, row.WindowStart)
		out = append(out, row)
	}
	return out, nil
}

// BudgetWindowWarned reports whether a warn event already fired for a policy +
// window (stored in event metadata under window_start). Used by ReconcileUsage
// callers so the budget-threshold alert fires exactly once per window.
func (s *SQLiteUsageCapStore) BudgetWindowWarned(ctx context.Context, tenantID, policyID uuid.UUID, windowStart time.Time) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM usage_cap_events
	WHERE tenant_id=? AND policy_id=? AND decision=? AND json_extract(metadata, '$.window_start') = ?
)`, tenantID.String(), policyID.String(), store.UsageCapEventWarn, sqliteTimeStr(windowStart.UTC())).
		Scan(&exists)
	return exists, err
}

func (s *SQLiteUsageCapStore) budgetWindowWarned(ctx context.Context, tenantID uuid.UUID, policyID uuid.UUID, windowStart time.Time) bool {
	exists, err := s.BudgetWindowWarned(ctx, tenantID, policyID, windowStart)
	return err == nil && exists
}

// getPolicy reads a single policy scoped to a tenant.
func (s *SQLiteUsageCapStore) getPolicy(ctx context.Context, tenantID, id uuid.UUID) (*store.UsageCapPolicy, error) {
	row := s.db.QueryRowContext(ctx, sqlitePolicySelectSQL+" WHERE tenant_id=? AND id=?", tenantID.String(), id.String())
	p, err := scanSQLitePolicy(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// validateUsageCapRefs ensures agent/provider refs belong to the given tenant
// (or master tenant, for providers). Mirrors the PG guard.
func (s *SQLiteUsageCapStore) validateUsageCapRefs(ctx context.Context, tenantID uuid.UUID, agentID, providerID *uuid.UUID) error {
	if agentID != nil && *agentID != uuid.Nil {
		var ok bool
		if err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1 FROM agents WHERE id=? AND tenant_id=? AND deleted_at IS NULL
)`, agentID.String(), tenantID.String()).Scan(&ok); err != nil {
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
	SELECT 1 FROM llm_providers WHERE id=? AND tenant_id IN (?, ?)
)`, providerID.String(), tenantID.String(), store.MasterTenantID.String()).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return errors.New("provider_id does not belong to tenant")
		}
	}
	return nil
}

const sqlitePolicySelectSQL = `SELECT id, tenant_id, agent_id, provider_id, COALESCE(provider_type,''), COALESCE(model_id,''),
	window_key, max_tokens, max_cost_micros, warn_at_percent, COALESCE(source,'manual'), enabled, priority, created_at, updated_at FROM usage_cap_policies`

func scanSQLitePolicy(row scanner) (store.UsageCapPolicy, error) {
	var p store.UsageCapPolicy
	var agentID, providerID sql.NullString
	var maxTokens, maxCost sql.NullInt64
	var warnAt sql.NullFloat64
	var createdAt, updatedAt sqliteTime
	err := row.Scan(&p.ID, &p.TenantID, &agentID, &providerID, &p.ProviderType, &p.ModelID,
		&p.Window, &maxTokens, &maxCost, &warnAt, &p.Source, &p.Enabled, &p.Priority,
		&createdAt, &updatedAt)
	if agentID.Valid && agentID.String != "" {
		id := uuid.MustParse(agentID.String)
		p.AgentID = &id
	}
	if providerID.Valid && providerID.String != "" {
		id := uuid.MustParse(providerID.String)
		p.ProviderID = &id
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
	p.CreatedAt = createdAt.Time
	p.UpdatedAt = updatedAt.Time
	return p, err
}

const sqliteOverrideSelectSQL = `SELECT id, tenant_id, provider_id, provider_type, model_id,
	input_price, output_price, cache_read_price, cache_write_price,
	reasoning_price, request_price, image_price, web_search_price,
	enabled, created_at, updated_at FROM usage_pricing_overrides`

func scanSQLiteOverride(row scanner) (store.UsagePricingOverride, error) {
	var o store.UsagePricingOverride
	var prices [8]sql.NullString
	var createdAt, updatedAt sqliteTime
	err := row.Scan(&o.ID, &o.TenantID, &o.ProviderID, &o.ProviderType, &o.ModelID,
		&prices[0], &prices[1], &prices[2], &prices[3], &prices[4], &prices[5], &prices[6], &prices[7],
		&o.Enabled, &createdAt, &updatedAt)
	o.Pricing = pricingFromNulls(prices)
	o.CreatedAt = createdAt.Time
	o.UpdatedAt = updatedAt.Time
	return o, err
}

func scanSQLiteCatalog(row scanner) (store.UsagePricingCatalogEntry, error) {
	var e store.UsagePricingCatalogEntry
	var prices [8]sql.NullString
	var syncedAt, createdAt, updatedAt sqliteTime
	// raw_pricing / raw_model are SQLite TEXT; database/sql cannot scan a
	// string driver value directly into *json.RawMessage, so scan via strings
	// and convert after (non-null '' -> nil keeps struct round-trips clean).
	var rawPricing, rawModel string
	err := row.Scan(&e.ID, &e.ModelID, &e.CanonicalModelID, &rawPricing, &rawModel,
		&prices[0], &prices[1], &prices[2], &prices[3], &prices[4], &prices[5], &prices[6], &prices[7],
		&syncedAt, &createdAt, &updatedAt)
	e.Pricing = pricingFromNulls(prices)
	if rawPricing != "" {
		e.RawPricing = json.RawMessage(rawPricing)
	}
	if e.RawPricing == nil {
		e.RawPricing = json.RawMessage(`{}`)
	}
	if rawModel != "" {
		e.RawModel = json.RawMessage(rawModel)
	}
	if e.RawModel == nil {
		e.RawModel = json.RawMessage(`{}`)
	}
	e.SyncedAt = syncedAt.Time
	e.CreatedAt = createdAt.Time
	e.UpdatedAt = updatedAt.Time
	return e, err
}

// --- Shared helpers (mirrored from pg package) ---

// usagePricingModelCandidates expands an unqualified modelID into OpenRouter-style
// candidates (provider prefix + model prefix) for pricing resolution. Mirrors pg.
func usagePricingModelCandidates(providerName, providerType, modelID string) []string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	out := []string{modelID}
	if strings.Contains(modelID, "/") {
		return out
	}
	for _, prefix := range openRouterProviderPrefixes(providerName, providerType) {
		out = appendUniqueString(out, prefix+"/"+modelID)
	}
	for _, prefix := range openRouterModelPrefixes(modelID) {
		out = appendUniqueString(out, prefix+"/"+modelID)
	}
	return out
}

func openRouterProviderPrefixes(providerName, providerType string) []string {
	switch providerType {
	case store.ProviderAnthropicNative:
		return []string{"anthropic"}
	case store.ProviderGeminiNative, store.ProviderVertex:
		return []string{"google"}
	case store.ProviderOpenAICompat:
		switch normalizeProviderAlias(providerName) {
		case "openai", "azure", "azure-openai", "azure_openai":
			return []string{"openai"}
		case "anthropic":
			return []string{"anthropic"}
		case "gemini", "google", "vertex":
			return []string{"google"}
		}
		return nil
	case store.ProviderOpenRouter:
		return nil
	case store.ProviderGroq:
		return []string{"groq"}
	case store.ProviderDeepSeek:
		return []string{"deepseek"}
	case store.ProviderMistral:
		return []string{"mistralai"}
	case store.ProviderXAI:
		return []string{"x-ai"}
	case store.ProviderMiniMax:
		return []string{"minimax"}
	case store.ProviderCohere:
		return []string{"cohere"}
	case store.ProviderPerplexity:
		return []string{"perplexity"}
	case store.ProviderDashScope, store.ProviderBailian:
		return []string{"qwen"}
	default:
		return nil
	}
}

func openRouterModelPrefixes(modelID string) []string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	switch {
	case strings.HasPrefix(modelID, "gpt-"),
		strings.HasPrefix(modelID, "o1"),
		strings.HasPrefix(modelID, "o3"),
		strings.HasPrefix(modelID, "o4"),
		strings.HasPrefix(modelID, "o5"):
		return []string{"openai"}
	case strings.HasPrefix(modelID, "qwen"):
		return []string{"qwen"}
	case strings.HasPrefix(modelID, "claude-"):
		return []string{"anthropic"}
	case strings.HasPrefix(modelID, "gemini-"):
		return []string{"google"}
	default:
		return nil
	}
}

func normalizeProviderAlias(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func appendUniqueString(values []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return values
	}
	for _, existing := range values {
		if existing == next {
			return values
		}
	}
	return append(values, next)
}

func pricingFromNulls(p [8]sql.NullString) store.UsagePricingFields {
	return store.UsagePricingFields{
		Input: pricePtr(p[0]), Output: pricePtr(p[1]),
		CacheRead: pricePtr(p[2]), CacheWrite: pricePtr(p[3]),
		Reasoning: pricePtr(p[4]), Request: pricePtr(p[5]),
		Image: pricePtr(p[6]), WebSearch: pricePtr(p[7]),
	}
}

func pricePtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func priceVal(v *string) any {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return strings.TrimSpace(*v)
}

func validateUsagePricingFields(fields store.UsagePricingFields) error {
	values := map[string]*string{
		"input":       fields.Input,
		"output":      fields.Output,
		"cache_read":  fields.CacheRead,
		"cache_write": fields.CacheWrite,
		"reasoning":   fields.Reasoning,
		"request":     fields.Request,
		"image":       fields.Image,
		"web_search":  fields.WebSearch,
	}
	for name, raw := range values {
		if raw == nil || strings.TrimSpace(*raw) == "" {
			continue
		}
		rat, ok := new(big.Rat).SetString(strings.TrimSpace(*raw))
		if !ok {
			return fmt.Errorf("invalid %s price", name)
		}
		if rat.Sign() < 0 {
			return fmt.Errorf("%s price must be non-negative", name)
		}
	}
	return nil
}

func nullEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func warnPtrVal(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func sqliteIntPtr(v *int64) any {
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

func sqliteNilUUIDStr(v *uuid.UUID) any {
	if v == nil || *v == uuid.Nil {
		return nil
	}
	return v.String()
}

func sqliteJSON(v json.RawMessage) string {
	if len(v) == 0 {
		return "{}"
	}
	return string(v)
}

func sqliteTimeStr(t time.Time) string {
	if t.IsZero() {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// sqliteTimeScan adapts a *time.Time destination for scanning a sqliteTime.
var sqliteTimeScan = func(dst *time.Time) sql.Scanner { return &sqliteTimeScanDest{dst: dst} }

type sqliteTimeScanDest struct{ dst *time.Time }

func (d *sqliteTimeScanDest) Scan(src any) error {
	var st sqliteTime
	if err := st.Scan(src); err != nil {
		return err
	}
	*d.dst = st.Time
	return nil
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

// usageWindow returns the [start, end) bounds for a usage cap window.
// Hour truncates to the hour; week starts Monday UTC.
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