//go:build sqlite || sqliteonly

package sqlitestore

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
var _ store.WebhookCallStore = (*SQLiteWebhookCallStore)(nil)

// SQLiteWebhookCallStore implements store.WebhookCallStore backed by SQLite.
type SQLiteWebhookCallStore struct {
	db *sql.DB
}

// NewSQLiteWebhookCallStore creates a new SQLite-backed webhook call store.
func NewSQLiteWebhookCallStore(db *sql.DB) *SQLiteWebhookCallStore {
	return &SQLiteWebhookCallStore{db: db}
}

// sqliteWebhookCallSelectCols is the canonical SELECT column list for webhook_calls in SQLite.
const sqliteWebhookCallSelectCols = `id, tenant_id, webhook_id, agent_id, delivery_id,
	idempotency_key, mode, status, callback_url, attempts,
	next_attempt_at, started_at, lease_token, request_payload, response, last_error,
	created_at, completed_at`

// scanSQLiteWebhookCallRow scans a single webhook_calls row from SQLite into WebhookCallData.
func scanSQLiteWebhookCallRow(row interface {
	Scan(dest ...any) error
}) (*store.WebhookCallData, error) {
	var c store.WebhookCallData
	var agentID *uuid.UUID
	var nextAttemptAt, startedAt, completedAt nullSqliteTime
	createdAt := &sqliteTime{}

	err := row.Scan(
		&c.ID, &c.TenantID, &c.WebhookID, &agentID, &c.DeliveryID,
		&c.IdempotencyKey, &c.Mode, &c.Status, &c.CallbackURL, &c.Attempts,
		&nextAttemptAt, &startedAt, &c.LeaseToken, &c.RequestPayload, &c.Response, &c.LastError,
		createdAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}
	c.AgentID = agentID
	c.CreatedAt = createdAt.Time
	if nextAttemptAt.Valid {
		c.NextAttemptAt = &nextAttemptAt.Time
	}
	if startedAt.Valid {
		c.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		c.CompletedAt = &completedAt.Time
	}
	return &c, nil
}

func (s *SQLiteWebhookCallStore) Create(ctx context.Context, call *store.WebhookCallData) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_calls
		 (id, tenant_id, webhook_id, agent_id, delivery_id,
		  idempotency_key, mode, status, callback_url, attempts,
		  next_attempt_at, request_payload, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		call.ID, call.TenantID, call.WebhookID, nilUUID(call.AgentID), call.DeliveryID,
		call.IdempotencyKey, call.Mode, call.Status, call.CallbackURL, call.Attempts,
		call.NextAttemptAt, call.RequestPayload, call.CreatedAt,
	)
	if err != nil {
		// Map partial unique index violation (webhook_id, idempotency_key) → typed sentinel.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") &&
			strings.Contains(err.Error(), "idempotency") {
			return store.ErrIdempotencyConflict
		}
		return err
	}
	return nil
}

func (s *SQLiteWebhookCallStore) GetByID(ctx context.Context, id uuid.UUID) (*store.WebhookCallData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sqliteWebhookCallSelectCols+`
		 FROM webhook_calls
		 WHERE id = ? AND tenant_id = ?`,
		id, tid,
	)
	return scanSQLiteWebhookCallRow(row)
}

func (s *SQLiteWebhookCallStore) GetByIdempotency(ctx context.Context, webhookID uuid.UUID, key string) (*store.WebhookCallData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sqliteWebhookCallSelectCols+`
		 FROM webhook_calls
		 WHERE webhook_id = ? AND idempotency_key = ? AND tenant_id = ?`,
		webhookID, key, tid,
	)
	return scanSQLiteWebhookCallRow(row)
}

func (s *SQLiteWebhookCallStore) UpdateStatus(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	// webhook_calls has no updated_at column — build UPDATE manually without auto-timestamp.
	return execMapUpdateWhereTenantNoUpdatedAt(ctx, s.db, "webhook_calls", updates, id, tid)
}

// UpdateStatusCAS applies updates with an optimistic-concurrency guard on lease_token.
// Returns store.ErrLeaseExpired if 0 rows were affected (lease mismatch → row reclaimed).
func (s *SQLiteWebhookCallStore) UpdateStatusCAS(ctx context.Context, id uuid.UUID, lease string, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	return execMapUpdateWhereTenantLeaseNoUpdatedAt(ctx, s.db, "webhook_calls", updates, id, tid, lease)
}

// ClaimNext atomically claims the next queued call due for processing.
// SQLite has no FOR UPDATE SKIP LOCKED, so we use BEGIN IMMEDIATE to serialize
// writers (single-writer acceptable in Lite edition).
// Sets status='running' and started_at=now. Does NOT increment attempts.
func (s *SQLiteWebhookCallStore) ClaimNext(ctx context.Context, tenantID uuid.UUID, now time.Time) (*store.WebhookCallData, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("webhook_calls ClaimNext begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Find the next eligible queued call.
	var callID uuid.UUID
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM webhook_calls
		 WHERE tenant_id = ?
		   AND status = 'queued'
		   AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
		 ORDER BY next_attempt_at ASC
		 LIMIT 1`,
		tenantID, now,
	).Scan(&callID)
	if err != nil {
		return nil, err // includes sql.ErrNoRows when queue empty
	}

	// Mark running, record started_at, and set a fresh lease_token for CAS guards.
	// Attempts untouched — worker increments post-send.
	lease := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`UPDATE webhook_calls SET status = 'running', started_at = ?, lease_token = ? WHERE id = ?`,
		now, lease, callID,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook_calls ClaimNext update: %w", err)
	}

	// Re-fetch the updated row inside the same transaction.
	row := tx.QueryRowContext(ctx,
		`SELECT `+sqliteWebhookCallSelectCols+` FROM webhook_calls WHERE id = ?`,
		callID,
	)
	var call *store.WebhookCallData
	call, err = scanSQLiteWebhookCallRow(row)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("webhook_calls ClaimNext commit: %w", err)
	}
	return call, nil
}

func (s *SQLiteWebhookCallStore) List(ctx context.Context, f store.WebhookCallListFilter) ([]store.WebhookCallData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	q := `SELECT ` + sqliteWebhookCallSelectCols + ` FROM webhook_calls WHERE tenant_id = ?`
	args := []any{tid}

	if f.WebhookID != nil {
		q += ` AND webhook_id = ?`
		args = append(args, *f.WebhookID)
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	q += ` ORDER BY created_at DESC`

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q += ` LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.WebhookCallData
	for rows.Next() {
		c, scanErr := scanSQLiteWebhookCallRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *SQLiteWebhookCallStore) DeleteOlderThan(ctx context.Context, tenantID uuid.UUID, ts time.Time) (int64, error) {
	var res sql.Result
	var err error
	if tenantID == uuid.Nil {
		// Retention worker: cross-tenant sweep.
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM webhook_calls
			 WHERE status IN ('done','failed','dead') AND created_at < ?`,
			ts,
		)
	} else {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM webhook_calls
			 WHERE tenant_id = ? AND status IN ('done','failed','dead') AND created_at < ?`,
			tenantID, ts,
		)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReclaimStale resets stale running rows back to queued so the worker can retry them.
// Clears lease_token so any in-flight UpdateStatusCAS from the crashed goroutine returns ErrLeaseExpired.
// SQLite stores timestamps as ISO-8601 strings; comparison uses standard string ordering.
func (s *SQLiteWebhookCallStore) ReclaimStale(ctx context.Context, staleThreshold time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_calls
		 SET status = 'queued', started_at = NULL, lease_token = NULL
		 WHERE status = 'running' AND started_at < ?`,
		staleThreshold,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// execMapUpdateWhereTenantLeaseNoUpdatedAt is like execMapUpdateWhereTenantNoUpdatedAt but adds
// AND lease_token = ? to the WHERE clause for optimistic concurrency.
// Returns store.ErrLeaseExpired when RowsAffected() == 0 (lease mismatch).
func execMapUpdateWhereTenantLeaseNoUpdatedAt(ctx context.Context, db *sql.DB, table string, updates map[string]any, id, tenantID uuid.UUID, lease string) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, col+" = ?")
		args = append(args, sqliteVal(val))
	}
	args = append(args, id, tenantID, lease)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = ? AND tenant_id = ? AND lease_token = ?",
		table, strings.Join(setClauses, ", "))
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

// execMapUpdateWhereTenantNoUpdatedAt builds and runs a dynamic UPDATE with id+tenant_id
// in WHERE, without auto-injecting updated_at (for tables without that column).
func execMapUpdateWhereTenantNoUpdatedAt(ctx context.Context, db *sql.DB, table string, updates map[string]any, id, tenantID uuid.UUID) error {
	if len(updates) == 0 {
		return nil
	}
	var setClauses []string
	var args []any
	for col, val := range updates {
		if !validColumnName.MatchString(col) {
			return fmt.Errorf("invalid column name: %q", col)
		}
		setClauses = append(setClauses, col+" = ?")
		args = append(args, sqliteVal(val))
	}
	args = append(args, id, tenantID)
	q := fmt.Sprintf("UPDATE %s SET %s WHERE id = ? AND tenant_id = ?",
		table, strings.Join(setClauses, ", "))
	_, err := db.ExecContext(ctx, q, args...)
	return err
}
