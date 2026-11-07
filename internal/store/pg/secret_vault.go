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

// PGSecretVaultStore implements store.SecretVaultStore backed by Postgres.
// Every query is scoped by the context tenant; the vault has no cross-tenant
// or master fallback.
type PGSecretVaultStore struct {
	db     *sql.DB
	encKey string
}

// NewPGSecretVaultStore returns the store, or nil when no encryption key is
// configured (the vault fails closed).
func NewPGSecretVaultStore(db *sql.DB, encryptionKey string) *PGSecretVaultStore {
	if encryptionKey == "" {
		return nil
	}
	return &PGSecretVaultStore{db: db, encKey: encryptionKey}
}

var _ store.SecretVaultStore = (*PGSecretVaultStore)(nil)

// newPGSecretVaultStoreOrNil avoids storing a typed nil pointer in the
// store.SecretVaultStore interface when the vault is disabled (no key).
func newPGSecretVaultStoreOrNil(db *sql.DB, encryptionKey string) store.SecretVaultStore {
	if s := NewPGSecretVaultStore(db, encryptionKey); s != nil {
		return s
	}
	return nil
}

const (
	secretVaultCols = `id, tenant_id, name, kind, allowed_domains, require_approval, hints, revision, created_by,
	        created_at, updated_at, status, created_via, created_by_agent`
	// secretVaultColsE is secretVaultCols qualified with the entries alias e.
	secretVaultColsE = `e.id, e.tenant_id, e.name, e.kind, e.allowed_domains, e.require_approval, e.hints, e.revision,
	        e.created_by, e.created_at, e.updated_at, e.status, e.created_via, e.created_by_agent`
)

func secretVaultTenant(ctx context.Context) (uuid.UUID, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return uuid.Nil, store.ErrSecretVaultNoTenant
	}
	return tid, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key"))
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSecretVaultEntry(r rowScanner) (*store.SecretVaultEntry, error) {
	var e store.SecretVaultEntry
	if err := scanSecretVaultEntryInto(r, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// scanSecretVaultEntryInto scans secretVaultCols (in order) followed by any
// extra destinations.
func scanSecretVaultEntryInto(r rowScanner, e *store.SecretVaultEntry, extra ...any) error {
	var domains []byte
	dest := append([]any{&e.ID, &e.TenantID, &e.Name, &e.Kind, &domains, &e.RequireApproval,
		&e.Hints, &e.Revision, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &e.Status, &e.CreatedVia, &e.CreatedByAgent}, extra...)
	if err := r.Scan(dest...); err != nil {
		return err
	}
	if err := json.Unmarshal(domains, &e.AllowedDomains); err != nil {
		return fmt.Errorf("secret vault: decode allowed_domains: %w", err)
	}
	return nil
}

func (s *PGSecretVaultStore) Create(ctx context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
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
	if e.ID == uuid.Nil {
		e.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	e.TenantID, e.CreatedAt, e.UpdatedAt, e.Revision = tid, now, now, 1
	enc, err := store.EncodeSecretPayloadAAD(p, s.encKey, e)
	if err != nil {
		return err
	}
	domains, err := json.Marshal(e.AllowedDomains)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if e.CreatedByAgent != nil {
		if err := checkSecretVaultAgentCanCreate(ctx, tx, tid, *e.CreatedByAgent); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO secret_vault_entries (id, tenant_id, name, kind, encrypted_payload, allowed_domains,
		 require_approval, hints, revision, created_by, created_at, updated_at, status, created_via, created_by_agent)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		e.ID, tid, e.Name, e.Kind, enc, domains, e.RequireApproval, e.Hints, e.Revision, e.CreatedBy, now, now,
		e.Status, e.CreatedVia, e.CreatedByAgent)
	if isUniqueViolation(err) {
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

// checkSecretVaultAgentCanCreate verifies the creating agent lives in the
// tenant and is still under its pending cap. The agent row is locked so
// concurrent creates by the same agent cannot both pass the cap check.
func checkSecretVaultAgentCanCreate(ctx context.Context, tx *sql.Tx, tid, agentID uuid.UUID) error {
	var one int
	err := tx.QueryRowContext(ctx,
		`SELECT 1 FROM agents WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL FOR UPDATE`,
		agentID, tid).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", store.ErrSecretVaultAgentInvalid, agentID)
	}
	if err != nil {
		return err
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
		`SELECT COUNT(*) FROM secret_vault_entries WHERE tenant_id = $1 AND created_by_agent = $2 AND status = $3`,
		tid, agentID, store.SecretVaultStatusPending).Scan(&n)
	return n, err
}

func (s *PGSecretVaultStore) CountPendingByAgent(ctx context.Context, agentID uuid.UUID) (int, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return 0, err
	}
	return countSecretVaultPending(ctx, s.db, tid, agentID)
}

func (s *PGSecretVaultStore) Approve(ctx context.Context, id uuid.UUID, domains []string) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	norm, err := store.NormalizeSecretDomains(domains)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	e, stored, err := lockSecretVaultEntry(ctx, tx, id, tid)
	if err != nil {
		return err
	}
	if e.Status != store.SecretVaultStatusPending {
		return store.ErrSecretVaultNotPending
	}
	// The confirmed domains are bound into the ciphertext, so the payload is
	// decrypted with the pending binding and sealed again with the new one.
	payload, err := store.DecodeSecretPayload(stored, s.encKey, e)
	if err != nil {
		return err
	}
	e.AllowedDomains = norm
	e.Revision++
	enc, err := store.EncodeSecretPayloadAAD(payload, s.encKey, e)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(norm)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE secret_vault_entries SET status = $1, allowed_domains = $2, encrypted_payload = $3, revision = $4,
		 updated_at = $5 WHERE id = $6 AND tenant_id = $7`,
		store.SecretVaultStatusActive, raw, enc, e.Revision, time.Now().UTC(), id, tid); err != nil {
		return err
	}
	if err := insertSecretVaultAudit(ctx, tx, tid, &store.SecretVaultAuditEntry{
		EntryID: id, EntryName: e.Name, UserID: store.UserIDFromContext(ctx),
		Action: store.SecretAuditApproved, Outcome: "ok",
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// lockSecretVaultEntry reads one entry and its stored ciphertext inside tx and
// locks the row, so a concurrent writer cannot interleave a read-modify-write.
func lockSecretVaultEntry(ctx context.Context, tx *sql.Tx, id, tid uuid.UUID) (*store.SecretVaultEntry, string, error) {
	var e store.SecretVaultEntry
	var enc string
	err := scanSecretVaultEntryInto(tx.QueryRowContext(ctx,
		`SELECT `+secretVaultCols+`, encrypted_payload FROM secret_vault_entries WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		id, tid), &e, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", store.ErrSecretVaultNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return &e, enc, nil
}

func (s *PGSecretVaultStore) ReplaceValue(ctx context.Context, id uuid.UUID, p store.SecretVaultPayload, expectedRev int64) (int64, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	e, _, err := lockSecretVaultEntry(ctx, tx, id, tid)
	if err != nil {
		return 0, err
	}
	if expectedRev != 0 && e.Revision != expectedRev {
		return e.Revision, store.ErrSecretVaultConflict
	}
	if err := store.ValidateSecretVaultPayload(e.Kind, p); err != nil {
		return 0, err
	}
	e.Revision++
	enc, err := store.EncodeSecretPayloadAAD(p, s.encKey, e)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE secret_vault_entries SET encrypted_payload = $1, revision = $2, updated_at = $3 WHERE id = $4 AND tenant_id = $5`,
		enc, e.Revision, time.Now().UTC(), id, tid); err != nil {
		return 0, err
	}
	return e.Revision, tx.Commit()
}

func (s *PGSecretVaultStore) UpdateMeta(ctx context.Context, id uuid.UUID, u store.SecretVaultMetaUpdate, expectedRev int64) (int64, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	e, stored, err := lockSecretVaultEntry(ctx, tx, id, tid)
	if err != nil {
		return 0, err
	}
	if expectedRev != 0 && e.Revision != expectedRev {
		return e.Revision, store.ErrSecretVaultConflict
	}
	// The payload is bound to the metadata being changed, so it is decrypted
	// with the old binding and sealed again with the new one.
	payload, err := store.DecodeSecretPayload(stored, s.encKey, e)
	if err != nil {
		return 0, err
	}
	if u.AllowedDomains != nil {
		domains, err := store.NormalizeSecretDomains(*u.AllowedDomains)
		if err != nil {
			return 0, err
		}
		e.AllowedDomains = domains
	}
	if u.RequireApproval != nil {
		e.RequireApproval = *u.RequireApproval
	}
	if u.Hints != nil {
		if err := store.ValidateSecretHints(*u.Hints); err != nil {
			return 0, err
		}
		e.Hints = *u.Hints
	}
	e.Revision++
	enc, err := store.EncodeSecretPayloadAAD(payload, s.encKey, e)
	if err != nil {
		return 0, err
	}
	raw, err := json.Marshal(e.AllowedDomains)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE secret_vault_entries SET allowed_domains = $1, require_approval = $2, hints = $3,
		 encrypted_payload = $4, revision = $5, updated_at = $6 WHERE id = $7 AND tenant_id = $8`,
		raw, e.RequireApproval, e.Hints, enc, e.Revision, time.Now().UTC(), id, tid); err != nil {
		return 0, err
	}
	return e.Revision, tx.Commit()
}

func expectOneRow(res sql.Result, err error) error {
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

func (s *PGSecretVaultStore) Get(ctx context.Context, id uuid.UUID) (*store.SecretVaultEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	e, err := scanSecretVaultEntry(s.db.QueryRowContext(ctx,
		`SELECT `+secretVaultCols+` FROM secret_vault_entries WHERE id = $1 AND tenant_id = $2`, id, tid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrSecretVaultNotFound
	}
	return e, err
}

func (s *PGSecretVaultStore) List(ctx context.Context) ([]store.SecretVaultEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	return s.queryEntries(ctx,
		`SELECT `+secretVaultCols+` FROM secret_vault_entries WHERE tenant_id = $1 ORDER BY name`, tid)
}

func (s *PGSecretVaultStore) queryEntries(ctx context.Context, q string, args ...any) ([]store.SecretVaultEntry, error) {
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

func (s *PGSecretVaultStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM secret_vault_entries WHERE id = $1 AND tenant_id = $2`, id, tid)
	return expectOneRow(res, err)
}

func (s *PGSecretVaultStore) SetGrants(ctx context.Context, entryID uuid.UUID, agentIDs []uuid.UUID) error {
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
		`SELECT EXISTS(SELECT 1 FROM secret_vault_entries WHERE id = $1 AND tenant_id = $2)`,
		entryID, tid).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return store.ErrSecretVaultNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM secret_vault_grants WHERE entry_id = $1 AND tenant_id = $2`, entryID, tid); err != nil {
		return err
	}
	for _, aid := range agentIDs {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO secret_vault_grants (entry_id, agent_id, tenant_id)
			 SELECT $1, a.id, $3 FROM agents a WHERE a.id = $2 AND a.tenant_id = $3 AND a.deleted_at IS NULL
			 ON CONFLICT DO NOTHING`, entryID, aid, tid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// Distinguish a duplicate id in the input from a foreign/missing agent.
			var granted bool
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM secret_vault_grants WHERE entry_id = $1 AND agent_id = $2)`,
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

func (s *PGSecretVaultStore) AddGrant(ctx context.Context, entryID, agentID uuid.UUID) error {
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
		`SELECT EXISTS(SELECT 1 FROM secret_vault_entries WHERE id = $1 AND tenant_id = $2)`,
		entryID, tid).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return store.ErrSecretVaultNotFound
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO secret_vault_grants (entry_id, agent_id, tenant_id)
		 SELECT $1, a.id, $3 FROM agents a WHERE a.id = $2 AND a.tenant_id = $3 AND a.deleted_at IS NULL
		 ON CONFLICT DO NOTHING`, entryID, agentID, tid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Already granted is a no-op; a foreign or missing agent is invalid.
		var granted bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM secret_vault_grants WHERE entry_id = $1 AND agent_id = $2 AND tenant_id = $3)`,
			entryID, agentID, tid).Scan(&granted); err != nil {
			return err
		}
		if !granted {
			return fmt.Errorf("%w: %s", store.ErrSecretVaultAgentInvalid, agentID)
		}
	}
	return tx.Commit()
}

func (s *PGSecretVaultStore) ListGrants(ctx context.Context, entryID uuid.UUID) ([]uuid.UUID, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT agent_id FROM secret_vault_grants WHERE entry_id = $1 AND tenant_id = $2 ORDER BY created_at, agent_id`,
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

func (s *PGSecretVaultStore) ListGrantedMeta(ctx context.Context, agentID uuid.UUID) ([]store.SecretVaultEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	return s.queryEntries(ctx,
		`SELECT `+secretVaultColsE+`
		 FROM secret_vault_entries e
		 JOIN secret_vault_grants g ON g.entry_id = e.id AND g.tenant_id = e.tenant_id
		 WHERE g.agent_id = $1 AND e.tenant_id = $2 AND e.status = $3 ORDER BY e.name`,
		agentID, tid, store.SecretVaultStatusActive)
}

func (s *PGSecretVaultStore) ResolveForUse(ctx context.Context, agentID uuid.UUID, name string) (*store.SecretVaultEntry, store.SecretVaultPayload, error) {
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
		               WHERE g.entry_id = e.id AND g.agent_id = $3 AND g.tenant_id = e.tenant_id)
		 FROM secret_vault_entries e WHERE e.tenant_id = $1 AND e.name = $2`,
		tid, name, agentID), &e, &enc, &granted)
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
	p, err := store.DecodeSecretPayload(enc, s.encKey, &e)
	if err != nil {
		return nil, store.SecretVaultPayload{}, err
	}
	return &e, p, nil
}

func (s *PGSecretVaultStore) ListGrantedSecrets(ctx context.Context, agentID uuid.UUID) ([]store.SecretVaultResolved, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secretVaultColsE+`, e.encrypted_payload
		 FROM secret_vault_entries e
		 JOIN secret_vault_grants g ON g.entry_id = e.id AND g.tenant_id = e.tenant_id
		 WHERE g.agent_id = $1 AND e.tenant_id = $2 AND e.status = $3 ORDER BY e.name`,
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
		if r.Payload, err = store.DecodeSecretPayload(enc, s.encKey, &r.Entry); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PGSecretVaultStore) AppendAudit(ctx context.Context, a *store.SecretVaultAuditEntry) error {
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
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		a.ID, tid, a.EntryID, a.EntryName, a.AgentID, a.UserID, a.Action, a.TargetHost, a.Outcome, a.CreatedAt)
	return err
}

func (s *PGSecretVaultStore) ListAudit(ctx context.Context, entryID uuid.UUID, limit int) ([]store.SecretVaultAuditEntry, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, entry_id, entry_name, agent_id, user_id, action, target_host, outcome, created_at
		 FROM secret_vault_audit WHERE tenant_id = $1 AND entry_id = $2 ORDER BY created_at DESC LIMIT $3`,
		tid, entryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SecretVaultAuditEntry
	for rows.Next() {
		var a store.SecretVaultAuditEntry
		if err := rows.Scan(&a.ID, &a.TenantID, &a.EntryID, &a.EntryName, &a.AgentID, &a.UserID,
			&a.Action, &a.TargetHost, &a.Outcome, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *PGSecretVaultStore) RewrapSecrets(ctx context.Context, dryRun bool) (store.SecretRewrapReport, error) {
	var rep store.SecretRewrapReport
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return rep, err
	}
	type row struct {
		e   *store.SecretVaultEntry
		enc string
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secretVaultCols+`, encrypted_payload FROM secret_vault_entries WHERE tenant_id = $1 ORDER BY name`, tid)
	if err != nil {
		return rep, err
	}
	var all []row
	for rows.Next() {
		var e store.SecretVaultEntry
		var enc string
		if err := scanSecretVaultEntryInto(rows, &e, &enc); err != nil {
			rows.Close()
			return rep, err
		}
		all = append(all, row{&e, enc})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return rep, err
	}
	rows.Close()

	for _, r := range all {
		rep.Total++
		newEnc, outcome := store.RewrapSecretCiphertext(r.enc, s.encKey, r.e)
		switch outcome {
		case store.SecretRewrapCurrent:
			rep.Current++
		case store.SecretRewrapUnreadable:
			rep.Unreadable = append(rep.Unreadable, r.e.ID)
		case store.SecretRewrapUpgrade:
			if dryRun {
				rep.Rewrapped++
				continue
			}
			// Guarded on the ciphertext just read: a concurrent writer wins and
			// the row is picked up by the next run.
			res, err := s.db.ExecContext(ctx,
				`UPDATE secret_vault_entries SET encrypted_payload = $1
				 WHERE id = $2 AND tenant_id = $3 AND encrypted_payload = $4 AND revision = $5`,
				newEnc, r.e.ID, tid, r.enc, r.e.Revision)
			if err != nil {
				return rep, err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				rep.Rewrapped++
			}
		}
	}
	return rep, nil
}
