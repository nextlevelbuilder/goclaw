package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// compile-time interface assertion
var _ store.WebhookCallStore = (*PGWebhookCallStore)(nil)

// PGWebhookCallStore implements store.WebhookCallStore using PostgreSQL.
type PGWebhookCallStore struct {
	db *sql.DB
}

// NewPGWebhookCallStore creates a new PostgreSQL-backed webhook call store.
func NewPGWebhookCallStore(db *sql.DB) *PGWebhookCallStore {
	return &PGWebhookCallStore{db: db}
}

// webhookCallColumns is the canonical SELECT column list for webhook_calls.
const webhookCallColumns = `id, tenant_id, webhook_id, agent_id, delivery_id,
	idempotency_key, mode, status, callback_url, attempts,
	next_attempt_at, started_at, lease_token, request_payload, response, last_error,
	created_at, completed_at`

// scanWebhookCallRow scans a single webhook_calls row into WebhookCallData.
func scanWebhookCallRow(row interface {
	Scan(dest ...any) error
}) (*store.WebhookCallData, error) {
	var c store.WebhookCallData
	var agentID *uuid.UUID

	err := row.Scan(
		&c.ID, &c.TenantID, &c.WebhookID, &agentID, &c.DeliveryID,
		&c.IdempotencyKey, &c.Mode, &c.Status, &c.CallbackURL, &c.Attempts,
		&c.NextAttemptAt, &c.StartedAt, &c.LeaseToken, &c.RequestPayload, &c.Response, &c.LastError,
		&c.CreatedAt, &c.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	c.AgentID = agentID
	return &c, nil
}

func (s *PGWebhookCallStore) Create(ctx context.Context, call *store.WebhookCallData) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_calls
		 (id, tenant_id, webhook_id, agent_id, delivery_id,
		  idempotency_key, mode, status, callback_url, attempts,
		  next_attempt_at, request_payload, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		call.ID, call.TenantID, call.WebhookID, nilUUID(call.AgentID), call.DeliveryID,
		call.IdempotencyKey, call.Mode, call.Status, call.CallbackURL, call.Attempts,
		call.NextAttemptAt, call.RequestPayload, call.CreatedAt,
	)
	if err != nil {
		// Map partial unique index violation (webhook_id, idempotency_key) → typed sentinel.
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key") {
			if strings.Contains(err.Error(), "uq_webhook_calls_idempotency") || strings.Contains(err.Error(), "idempotency") {
				return store.ErrIdempotencyConflict
			}
		}
		return err
	}
	return nil
}

func (s *PGWebhookCallStore) GetByID(ctx context.Context, id uuid.UUID) (*store.WebhookCallData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+webhookCallColumns+`
		 FROM webhook_calls
		 WHERE id = $1 AND tenant_id = $2`,
		id, tid,
	)
	return scanWebhookCallRow(row)
}

func (s *PGWebhookCallStore) GetByIdempotency(ctx context.Context, webhookID uuid.UUID, key string) (*store.WebhookCallData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+webhookCallColumns+`
		 FROM webhook_calls
		 WHERE webhook_id = $1 AND idempotency_key = $2 AND tenant_id = $3`,
		webhookID, key, tid,
	)
	return scanWebhookCallRow(row)
}

func (s *PGWebhookCallStore) UpdateStatus(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	// webhook_calls has no updated_at column — use BuildMapUpdateWhereTenant without auto-timestamp.
	// We call the lower-level helper directly and build query ourselves to avoid updated_at injection.
	return execMapUpdateWhereTenantNoUpdatedAt(ctx, s.db, "webhook_calls", updates, id, tid)
}

// UpdateStatusCAS applies updates with an optimistic-concurrency guard on lease_token.
// Returns store.ErrLeaseExpired if 0 rows were affected (lease mismatch → row reclaimed).
func (s *PGWebhookCallStore) UpdateStatusCAS(ctx context.Context, id uuid.UUID, lease string, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	return execMapUpdateWhereTenantLease(ctx, s.db, "webhook_calls", updates, id, tid, lease)
}

// ClaimNext atomically claims the next queued call due for delivery.
// Uses SELECT ... FOR UPDATE SKIP LOCKED to prevent double-claiming under concurrency.
// Sets status='running' and started_at=now. Does NOT touch attempts.
func (s *PGWebhookCallStore) ClaimNext(ctx context.Context, tenantID uuid.UUID, now time.Time) (*store.WebhookCallData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("webhook_calls ClaimNext begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Lock the next eligible row exclusively; skip rows locked by concurrent workers.
	var callID uuid.UUID
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM webhook_calls
		 WHERE tenant_id = $1
		   AND status = 'queued'
		   AND (next_attempt_at IS NULL OR next_attempt_at <= $2)
		 ORDER BY next_attempt_at ASC NULLS FIRST
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`,
		tenantID, now,
	).Scan(&callID)
	if err != nil {
		return nil, err // includes sql.ErrNoRows when queue is empty
	}

	// Mark running, record started_at, and set a fresh lease_token for CAS guards.
	// Attempts untouched — worker increments post-send.
	lease := uuid.New().String()
	row := tx.QueryRowContext(ctx,
		`UPDATE webhook_calls
		 SET status = 'running', started_at = $1, lease_token = $2
		 WHERE id = $3
		 RETURNING `+webhookCallColumns,
		now, lease, callID,
	)
	call, err := scanWebhookCallRow(row)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("webhook_calls ClaimNext commit: %w", err)
	}
	return call, nil
}

func (s *PGWebhookCallStore) List(ctx context.Context, f store.WebhookCallListFilter) ([]store.WebhookCallData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	q := `SELECT ` + webhookCallColumns + ` FROM webhook_calls WHERE tenant_id = $1`
	args := []any{tid}
	n := 2

	if f.WebhookID != nil {
		q += fmt.Sprintf(` AND webhook_id = $%d`, n)
		args = append(args, *f.WebhookID)
		n++
	}
	if f.Status != "" {
		q += fmt.Sprintf(` AND status = $%d`, n)
		args = append(args, f.Status)
		n++
	}
	q += ` ORDER BY created_at DESC`

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q += fmt.Sprintf(` LIMIT $%d OFFSET $%d`, n, n+1)
	args = append(args, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.WebhookCallData
	for rows.Next() {
		c, scanErr := scanWebhookCallRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *PGWebhookCallStore) DeleteOlderThan(ctx context.Context, tenantID uuid.UUID, ts time.Time) (int64, error) {
	var res sql.Result
	var err error
	if tenantID == uuid.Nil {
		// Retention worker: cross-tenant sweep.
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM webhook_calls
			 WHERE status IN ('done','failed','dead') AND created_at < $1`,
			ts,
		)
	} else {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM webhook_calls
			 WHERE tenant_id = $1 AND status IN ('done','failed','dead') AND created_at < $2`,
			tenantID, ts,
		)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReclaimStale resets stale running rows back to queued so the worker can retry them.
// A row is considered stale when started_at < staleThreshold (i.e., the worker that
// claimed it crashed before completing UpdateStatus).
// Cross-tenant: no tenant_id filter — the retention worker sweeps the whole table.
func (s *PGWebhookCallStore) ReclaimStale(ctx context.Context, staleThreshold time.Time) (int64, error) {
	// Clear lease_token so any in-flight UpdateStatusCAS from the crashed worker returns ErrLeaseExpired.
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_calls
		 SET status = 'queued', started_at = NULL, lease_token = NULL
		 WHERE status = 'running' AND started_at < $1`,
		staleThreshold,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// execMapUpdateWhereTenantLease is like execMapUpdateWhereTenantNoUpdatedAt but adds
// AND lease_token = $N to the WHERE clause for optimistic concurrency.
// Returns store.ErrLeaseExpired when RowsAffected() == 0 (lease mismatch).
func execMapUpdateWhereTenantLease(ctx context.Context, db *sql.DB, table string, updates map[string]any, id, tenantID uuid.UUID, lease string) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	n := 1
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, val)
		n++
	}
	args = append(args, id, tenantID, lease)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d AND tenant_id = $%d AND lease_token = $%d",
		table, strings.Join(setClauses, ", "), n, n+1, n+2)
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return store.ErrLeaseExpired
	}
	return nil
}

// execMapUpdateWhereTenantNoUpdatedAt is like execMapUpdateWhereTenant but does NOT
// auto-inject updated_at. Used for webhook_calls which has no updated_at column.
func execMapUpdateWhereTenantNoUpdatedAt(ctx context.Context, db *sql.DB, table string, updates map[string]any, id, tenantID uuid.UUID) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	n := 1
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, n))
		args = append(args, val)
		n++
	}
	args = append(args, id, tenantID)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d AND tenant_id = $%d",
		table, strings.Join(setClauses, ", "), n, n+1)
	_, err := db.ExecContext(ctx, q, args...)
	return err
}
