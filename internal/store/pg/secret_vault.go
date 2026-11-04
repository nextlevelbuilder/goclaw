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

const secretVaultCols = `id, tenant_id, name, kind, allowed_domains, require_approval, hints, created_by, created_at, updated_at`

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
	var domains []byte
	if err := r.Scan(&e.ID, &e.TenantID, &e.Name, &e.Kind, &domains, &e.RequireApproval,
		&e.Hints, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(domains, &e.AllowedDomains); err != nil {
		return nil, fmt.Errorf("secret vault: decode allowed_domains: %w", err)
	}
	return &e, nil
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

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO secret_vault_entries (id, tenant_id, name, kind, encrypted_payload, allowed_domains,
		 require_approval, hints, created_by, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ID, tid, e.Name, e.Kind, enc, domains, e.RequireApproval, e.Hints, e.CreatedBy, now, now)
	if isUniqueViolation(err) {
		return store.ErrSecretVaultNameTaken
	}
	return err
}

func (s *PGSecretVaultStore) ReplaceValue(ctx context.Context, id uuid.UUID, p store.SecretVaultPayload) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	var kind string
	err = s.db.QueryRowContext(ctx,
		`SELECT kind FROM secret_vault_entries WHERE id = $1 AND tenant_id = $2`, id, tid).Scan(&kind)
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
		`UPDATE secret_vault_entries SET encrypted_payload = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`,
		enc, time.Now().UTC(), id, tid)
	return expectOneRow(res, err)
}

func (s *PGSecretVaultStore) UpdateMeta(ctx context.Context, id uuid.UUID, u store.SecretVaultMetaUpdate) error {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return err
	}
	sets := []string{"updated_at = $1"}
	args := []any{time.Now().UTC()}
	if u.AllowedDomains != nil {
		domains, err := store.NormalizeSecretDomains(*u.AllowedDomains)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(domains)
		args = append(args, raw)
		sets = append(sets, fmt.Sprintf("allowed_domains = $%d", len(args)))
	}
	if u.RequireApproval != nil {
		args = append(args, *u.RequireApproval)
		sets = append(sets, fmt.Sprintf("require_approval = $%d", len(args)))
	}
	if u.Hints != nil {
		if err := store.ValidateSecretHints(*u.Hints); err != nil {
			return err
		}
		args = append(args, *u.Hints)
		sets = append(sets, fmt.Sprintf("hints = $%d", len(args)))
	}
	args = append(args, id, tid)
	q := fmt.Sprintf(`UPDATE secret_vault_entries SET %s WHERE id = $%d AND tenant_id = $%d`,
		strings.Join(sets, ", "), len(args)-1, len(args))
	res, err := s.db.ExecContext(ctx, q, args...)
	return expectOneRow(res, err)
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
		`SELECT e.id, e.tenant_id, e.name, e.kind, e.allowed_domains, e.require_approval, e.hints,
		        e.created_by, e.created_at, e.updated_at
		 FROM secret_vault_entries e
		 JOIN secret_vault_grants g ON g.entry_id = e.id AND g.tenant_id = e.tenant_id
		 WHERE g.agent_id = $1 AND e.tenant_id = $2 ORDER BY e.name`, agentID, tid)
}

func (s *PGSecretVaultStore) ResolveForUse(ctx context.Context, agentID uuid.UUID, name string) (*store.SecretVaultEntry, store.SecretVaultPayload, error) {
	tid, err := secretVaultTenant(ctx)
	if err != nil {
		return nil, store.SecretVaultPayload{}, err
	}
	var e store.SecretVaultEntry
	var domains []byte
	var enc string
	var granted bool
	err = s.db.QueryRowContext(ctx,
		`SELECT e.id, e.tenant_id, e.name, e.kind, e.allowed_domains, e.require_approval, e.hints,
		        e.created_by, e.created_at, e.updated_at, e.encrypted_payload,
		        EXISTS(SELECT 1 FROM secret_vault_grants g
		               WHERE g.entry_id = e.id AND g.agent_id = $3 AND g.tenant_id = e.tenant_id)
		 FROM secret_vault_entries e WHERE e.tenant_id = $1 AND e.name = $2`,
		tid, name, agentID).Scan(&e.ID, &e.TenantID, &e.Name, &e.Kind, &domains, &e.RequireApproval,
		&e.Hints, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &enc, &granted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotFound
	}
	if err != nil {
		return nil, store.SecretVaultPayload{}, err
	}
	if !granted {
		return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotGranted
	}
	if err := json.Unmarshal(domains, &e.AllowedDomains); err != nil {
		return nil, store.SecretVaultPayload{}, fmt.Errorf("secret vault: decode allowed_domains: %w", err)
	}
	p, err := store.DecodeSecretPayload(enc, s.encKey)
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
		`SELECT e.id, e.tenant_id, e.name, e.kind, e.allowed_domains, e.require_approval, e.hints,
		        e.created_by, e.created_at, e.updated_at, e.encrypted_payload
		 FROM secret_vault_entries e
		 JOIN secret_vault_grants g ON g.entry_id = e.id AND g.tenant_id = e.tenant_id
		 WHERE g.agent_id = $1 AND e.tenant_id = $2 ORDER BY e.name`, agentID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.SecretVaultResolved
	for rows.Next() {
		var r store.SecretVaultResolved
		var domains []byte
		var enc string
		e := &r.Entry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Name, &e.Kind, &domains, &e.RequireApproval,
			&e.Hints, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt, &enc); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(domains, &e.AllowedDomains); err != nil {
			return nil, fmt.Errorf("secret vault: decode allowed_domains: %w", err)
		}
		if r.Payload, err = store.DecodeSecretPayload(enc, s.encKey); err != nil {
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
	if a.ID == uuid.Nil {
		a.ID = store.GenNewID()
	}
	a.TenantID = tid
	a.CreatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
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
