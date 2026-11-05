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

// SQLiteSecretVaultStore implements store.SecretVaultStore backed by SQLite.
// Every query is scoped by the context tenant; the vault has no cross-tenant
// or master fallback.
type SQLiteSecretVaultStore struct {
	db     *sql.DB
	encKey string
}

// NewSQLiteSecretVaultStore returns the store, or nil when no encryption key
// is configured (the vault fails closed).
func NewSQLiteSecretVaultStore(db *sql.DB, encryptionKey string) *SQLiteSecretVaultStore {
	if encryptionKey == "" {
		return nil
	}
	return &SQLiteSecretVaultStore{db: db, encKey: encryptionKey}
}

var _ store.SecretVaultStore = (*SQLiteSecretVaultStore)(nil)

const (
	secretVaultCols = `id, tenant_id, name, kind, allowed_domains, require_approval, hints, created_by, created_at, updated_at,
	        status, created_via, created_by_agent`
	// secretVaultColsE is secretVaultCols qualified with the entries alias e.
	secretVaultColsE = `e.id, e.tenant_id, e.name, e.kind, e.allowed_domains, e.require_approval, e.hints, e.created_by,
	        e.created_at, e.updated_at, e.status, e.created_via, e.created_by_agent`
)

func secretVaultTenant(ctx context.Context) (uuid.UUID, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return uuid.Nil, store.ErrSecretVaultNoTenant
	}
	return tid, nil
}

func isSecretVaultUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

type secretVaultRowScanner interface{ Scan(dest ...any) error }

func scanSecretVaultEntry(r secretVaultRowScanner) (*store.SecretVaultEntry, error) {
	var e store.SecretVaultEntry
	if err := scanSecretVaultEntryInto(r, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// scanSecretVaultEntryInto scans secretVaultCols (in order) followed by any
// extra destinations.
func scanSecretVaultEntryInto(r secretVaultRowScanner, e *store.SecretVaultEntry, extra ...any) error {
	var domains string
	created, updated := scanTimePair()
	dest := append([]any{&e.ID, &e.TenantID, &e.Name, &e.Kind, &domains, &e.RequireApproval,
		&e.Hints, &e.CreatedBy, created, updated, &e.Status, &e.CreatedVia, &e.CreatedByAgent}, extra...)
	if err := r.Scan(dest...); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(domains), &e.AllowedDomains); err != nil {
		return fmt.Errorf("secret vault: decode allowed_domains: %w", err)
	}
	e.CreatedAt, e.UpdatedAt = created.Time, updated.Time
	return nil
}

func (s *SQLiteSecretVaultStore) Create(ctx context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	if err := store.ValidateSecretVaultEntry(e); err != nil {
		return err
	}
	if err := store.ValidateSecretVaultPayload(e.Kind, p); err != nil {
		return err
	}
	enc, err := store.EncodeSecretPayload(p, s.encKey)
	if err != nil {
		return err
	}
	domains, err := json.Marshal(e.AllowedDomains)
	if err != nil {
		return err
	}
	if e.ID == uuid.Nil {
		e.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	e.TenantID, e.CreatedAt, e.UpdatedAt = tid, now, now
	ts := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if e.CreatedByAgent != nil {
		if err := s.checkAgentCanCreate(ctx, tx, tid, *e.CreatedByAgent); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO secret_vault_entries (id, tenant_id, name, kind, encrypted_payload, allowed_domains,
		 require_approval, hints, created_by, created_at, updated_at, status, created_via, created_by_agent)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, tid, e.Name, e.Kind, enc, string(domains), e.RequireApproval, e.Hints, e.CreatedBy, ts, ts,
		e.Status, e.CreatedVia, e.CreatedByAgent)
	if isSecretVaultUniqueViolation(err) {
		return store.ErrSecretVaultNameTaken
	}
	if err != nil {
		return err
	}
	if e.CreatedByAgent != nil {
		if err := insertSecretVaultAudit(ctx, tx, tid, &store.SecretVaultAuditEntry{
			EntryID: e.ID, EntryName: e.Name, AgentID: e.CreatedByAgent, UserID: e.CreatedBy,
			Action: store.SecretAuditCreated, Outcome: store.SecretAuditOutcomeAgentPending,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// checkAgentCanCreate verifies the creating agent lives in the tenant and is
// still under its pending cap.
func (s *SQLiteSecretVaultStore) checkAgentCanCreate(ctx context.Context, tx *sql.Tx, tid, agentID uuid.UUID) error {
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM agents WHERE id = ? AND tenant_id = ? AND deleted_at IS NULL)`,
		agentID, tid).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: %s", store.ErrSecretVaultAgentInvalid, agentID)
	}
	n, err := countSecretVaultPending(ctx, tx, tid, agentID)
	if err != nil {
		return err
	}
	if n >= store.SecretVaultMaxPendingPerAgent {
		return store.ErrSecretVaultPendingLimit
	}
	return nil
}

type secretVaultQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func countSecretVaultPending(ctx context.Context, q secretVaultQueryer, tid, agentID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM secret_vault_entries WHERE tenant_id = ? AND created_by_agent = ? AND status = ?`,
		tid, agentID, store.SecretVaultStatusPending).Scan(&n)
	return n, err
}

func (s *SQLiteSecretVaultStore) CountPendingByAgent(ctx context.Context, agentID uuid.UUID) (int, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return 0, err
	}
	return countSecretVaultPending(ctx, s.db, tid, agentID)
}

func (s *SQLiteSecretVaultStore) Approve(ctx context.Context, id uuid.UUID, domains []string) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	norm, err := store.NormalizeSecretDomains(domains)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(norm)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var name, status string
	err = tx.QueryRowContext(ctx,
		`SELECT name, status FROM secret_vault_entries WHERE id = ? AND tenant_id = ?`, id, tid).Scan(&name, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrSecretVaultNotFound
	}
	if err != nil {
		return err
	}
	if status != store.SecretVaultStatusPending {
		return store.ErrSecretVaultNotPending
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE secret_vault_entries SET status = ?, allowed_domains = ?, updated_at = ?
		 WHERE id = ? AND tenant_id = ? AND status = ?`,
		store.SecretVaultStatusActive, string(raw), time.Now().UTC().Format(time.RFC3339Nano),
		id, tid, store.SecretVaultStatusPending)
	if err := secretVaultExpectOneRow(res, err); err != nil {
		if errors.Is(err, store.ErrSecretVaultNotFound) {
			return store.ErrSecretVaultNotPending
		}
		return err
	}
	if err := insertSecretVaultAudit(ctx, tx, tid, &store.SecretVaultAuditEntry{
		EntryID: id, EntryName: name, UserID: store.UserIDFromContext(ctx),
		Action: store.SecretAuditApproved, Outcome: "ok",
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteSecretVaultStore) ReplaceValue(ctx context.Context, id uuid.UUID, p store.SecretVaultPayload) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	var kind string
	err = s.db.QueryRowContext(ctx,
		`SELECT kind FROM secret_vault_entries WHERE id = ? AND tenant_id = ?`, id, tid).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrSecretVaultNotFound
	}
	if err != nil {
		return err
	}
	if err := store.ValidateSecretVaultPayload(kind, p); err != nil {
		return err
	}
	enc, err := store.EncodeSecretPayload(p, s.encKey)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE secret_vault_entries SET encrypted_payload = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		enc, time.Now().UTC().Format(time.RFC3339Nano), id, tid)
	return secretVaultExpectOneRow(res, err)
}

func (s *SQLiteSecretVaultStore) UpdateMeta(ctx context.Context, id uuid.UUID, u store.SecretVaultMetaUpdate) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	sets := []string{"updated_at = ?"}
	args := []any{time.Now().UTC().Format(time.RFC3339Nano)}
	if u.AllowedDomains != nil {
		domains, err := store.NormalizeSecretDomains(*u.AllowedDomains)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(domains)
		sets = append(sets, "allowed_domains = ?")
		args = append(args, string(raw))
	}
	if u.RequireApproval != nil {
		sets = append(sets, "require_approval = ?")
		args = append(args, *u.RequireApproval)
	}
	if u.Hints != nil {
		if err := store.ValidateSecretHints(*u.Hints); err != nil {
			return err
		}
		sets = append(sets, "hints = ?")
		args = append(args, *u.Hints)
	}
	args = append(args, id, tid)
	res, err := s.db.ExecContext(ctx,
		`UPDATE secret_vault_entries SET `+strings.Join(sets, ", ")+` WHERE id = ? AND tenant_id = ?`, args...)
	return secretVaultExpectOneRow(res, err)
}

func secretVaultExpectOneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrSecretVaultNotFound
	}
	return nil
}

func (s *SQLiteSecretVaultStore) Get(ctx context.Context, id uuid.UUID) (*store.SecretVaultEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	e, err := scanSecretVaultEntry(s.db.QueryRowContext(ctx,
		`SELECT `+secretVaultCols+` FROM secret_vault_entries WHERE id = ? AND tenant_id = ?`, id, tid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrSecretVaultNotFound
	}
	return e, err
}

func (s *SQLiteSecretVaultStore) List(ctx context.Context) ([]store.SecretVaultEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	return s.queryEntries(ctx,
		`SELECT `+secretVaultCols+` FROM secret_vault_entries WHERE tenant_id = ? ORDER BY name`, tid)
}

func (s *SQLiteSecretVaultStore) queryEntries(ctx context.Context, q string, args ...any) ([]store.SecretVaultEntry, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SecretVaultEntry
	for rows.Next() {
		e, err := scanSecretVaultEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *SQLiteSecretVaultStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM secret_vault_entries WHERE id = ? AND tenant_id = ?`, id, tid)
	return secretVaultExpectOneRow(res, err)
}

func (s *SQLiteSecretVaultStore) SetGrants(ctx context.Context, entryID uuid.UUID, agentIDs []uuid.UUID) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM secret_vault_entries WHERE id = ? AND tenant_id = ?)`,
		entryID, tid).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return store.ErrSecretVaultNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM secret_vault_grants WHERE entry_id = ? AND tenant_id = ?`, entryID, tid); err != nil {
		return err
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	for _, aid := range agentIDs {
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO secret_vault_grants (entry_id, agent_id, tenant_id, created_at)
			 SELECT ?, a.id, ?, ? FROM agents a WHERE a.id = ? AND a.tenant_id = ? AND a.deleted_at IS NULL`,
			entryID, tid, ts, aid, tid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// Distinguish a duplicate id in the input from a foreign/missing agent.
			var granted bool
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM secret_vault_grants WHERE entry_id = ? AND agent_id = ?)`,
				entryID, aid).Scan(&granted); err != nil {
				return err
			}
			if !granted {
				return fmt.Errorf("%w: %s", store.ErrSecretVaultAgentInvalid, aid)
			}
		}
	}
	return tx.Commit()
}

func (s *SQLiteSecretVaultStore) AddGrant(ctx context.Context, entryID, agentID uuid.UUID) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM secret_vault_entries WHERE id = ? AND tenant_id = ?)`,
		entryID, tid).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return store.ErrSecretVaultNotFound
	}
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO secret_vault_grants (entry_id, agent_id, tenant_id, created_at)
		 SELECT ?, a.id, ?, ? FROM agents a WHERE a.id = ? AND a.tenant_id = ? AND a.deleted_at IS NULL`,
		entryID, tid, time.Now().UTC().Format(time.RFC3339Nano), agentID, tid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Already granted is a no-op; a foreign or missing agent is invalid.
		var granted bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM secret_vault_grants WHERE entry_id = ? AND agent_id = ? AND tenant_id = ?)`,
			entryID, agentID, tid).Scan(&granted); err != nil {
			return err
		}
		if !granted {
			return fmt.Errorf("%w: %s", store.ErrSecretVaultAgentInvalid, agentID)
		}
	}
	return tx.Commit()
}

func (s *SQLiteSecretVaultStore) ListGrants(ctx context.Context, entryID uuid.UUID) ([]uuid.UUID, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id FROM secret_vault_grants WHERE entry_id = ? AND tenant_id = ? ORDER BY created_at, agent_id`,
		entryID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *SQLiteSecretVaultStore) ListGrantedMeta(ctx context.Context, agentID uuid.UUID) ([]store.SecretVaultEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	return s.queryEntries(ctx,
		`SELECT `+secretVaultColsE+`
		 FROM secret_vault_entries e
		 JOIN secret_vault_grants g ON g.entry_id = e.id AND g.tenant_id = e.tenant_id
		 WHERE g.agent_id = ? AND e.tenant_id = ? AND e.status = ? ORDER BY e.name`,
		agentID, tid, store.SecretVaultStatusActive)
}

func (s *SQLiteSecretVaultStore) ResolveForUse(ctx context.Context, agentID uuid.UUID, name string) (*store.SecretVaultEntry, store.SecretVaultPayload, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, store.SecretVaultPayload{}, err
	}
	var e store.SecretVaultEntry
	var enc string
	var granted bool
	err = scanSecretVaultEntryInto(s.db.QueryRowContext(ctx,
		`SELECT `+secretVaultColsE+`, e.encrypted_payload,
		        EXISTS(SELECT 1 FROM secret_vault_grants g
		               WHERE g.entry_id = e.id AND g.agent_id = ? AND g.tenant_id = e.tenant_id)
		 FROM secret_vault_entries e WHERE e.tenant_id = ? AND e.name = ?`,
		agentID, tid, name), &e, &enc, &granted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotFound
	}
	if err != nil {
		return nil, store.SecretVaultPayload{}, err
	}
	// A pending entry is reported exactly like an ungranted one.
	if !granted || e.Status != store.SecretVaultStatusActive {
		return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotGranted
	}
	p, err := store.DecodeSecretPayload(enc, s.encKey)
	if err != nil {
		return nil, store.SecretVaultPayload{}, err
	}
	return &e, p, nil
}

func (s *SQLiteSecretVaultStore) ListGrantedSecrets(ctx context.Context, agentID uuid.UUID) ([]store.SecretVaultResolved, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secretVaultColsE+`, e.encrypted_payload
		 FROM secret_vault_entries e
		 JOIN secret_vault_grants g ON g.entry_id = e.id AND g.tenant_id = e.tenant_id
		 WHERE g.agent_id = ? AND e.tenant_id = ? AND e.status = ? ORDER BY e.name`,
		agentID, tid, store.SecretVaultStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SecretVaultResolved
	for rows.Next() {
		var r store.SecretVaultResolved
		var enc string
		if err := scanSecretVaultEntryInto(rows, &r.Entry, &enc); err != nil {
			return nil, err
		}
		if r.Payload, err = store.DecodeSecretPayload(enc, s.encKey); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteSecretVaultStore) AppendAudit(ctx context.Context, a *store.SecretVaultAuditEntry) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	return insertSecretVaultAudit(ctx, s.db, tid, a)
}

type secretVaultExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertSecretVaultAudit(ctx context.Context, x secretVaultExecer, tid uuid.UUID, a *store.SecretVaultAuditEntry) error {
	if a.ID == uuid.Nil {
		a.ID = store.GenNewID()
	}
	a.TenantID = tid
	a.CreatedAt = time.Now().UTC()
	_, err := x.ExecContext(ctx,
		`INSERT INTO secret_vault_audit (id, tenant_id, entry_id, entry_name, agent_id, user_id, action, target_host, outcome, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.ID, tid, a.EntryID, a.EntryName, a.AgentID, a.UserID, a.Action, a.TargetHost, a.Outcome,
		a.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteSecretVaultStore) ListAudit(ctx context.Context, entryID uuid.UUID, limit int) ([]store.SecretVaultAuditEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, entry_id, entry_name, agent_id, user_id, action, target_host, outcome, created_at
		 FROM secret_vault_audit WHERE tenant_id = ? AND entry_id = ? ORDER BY created_at DESC LIMIT ?`,
		tid, entryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SecretVaultAuditEntry
	for rows.Next() {
		var a store.SecretVaultAuditEntry
		var agent *uuid.UUID
		created := &sqliteTime{}
		if err := rows.Scan(&a.ID, &a.TenantID, &a.EntryID, &a.EntryName, &agent, &a.UserID,
			&a.Action, &a.TargetHost, &a.Outcome, created); err != nil {
			return nil, err
		}
		a.AgentID, a.CreatedAt = agent, created.Time
		out = append(out, a)
	}
	return out, rows.Err()
}

// newSecretVaultStoreOrNil avoids storing a typed nil pointer in the
// store.SecretVaultStore interface when the vault is disabled (no key).
func newSecretVaultStoreOrNil(db *sql.DB, encryptionKey string) store.SecretVaultStore {
	if s := NewSQLiteSecretVaultStore(db, encryptionKey); s != nil {
		return s
	}
	return nil
}
