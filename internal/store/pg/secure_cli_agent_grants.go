package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGSecureCLIAgentGrantStore implements store.SecureCLIAgentGrantStore backed by Postgres.
type PGSecureCLIAgentGrantStore struct {
	db     *sql.DB
	encKey string // AES-256-GCM key for encrypted_env column
}

func NewPGSecureCLIAgentGrantStore(db *sql.DB, encKey string) *PGSecureCLIAgentGrantStore {
	return &PGSecureCLIAgentGrantStore{db: db, encKey: encKey}
}

const grantSelectCols = `id, binary_id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, encrypted_env, created_at, updated_at`

func (s *PGSecureCLIAgentGrantStore) BinaryExists(ctx context.Context, binaryID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM secure_cli_binaries WHERE id = $1`
	args := []any{binaryID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return false, nil
		}
		query += ` AND tenant_id = $2`
		args = append(args, tid)
	}
	query += `)`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	return exists, err
}

func (s *PGSecureCLIAgentGrantStore) AgentExists(ctx context.Context, agentID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM agents WHERE id = $1 AND deleted_at IS NULL`
	args := []any{agentID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return false, nil
		}
		query += ` AND tenant_id = $2`
		args = append(args, tid)
	}
	query += `)`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	return exists, err
}

func (s *PGSecureCLIAgentGrantStore) Create(ctx context.Context, g *store.SecureCLIAgentGrant) error {
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	now := time.Now()
	g.CreatedAt = now
	g.UpdatedAt = now

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_agent_grants
		 (id, binary_id, agent_id, deny_args, deny_verbose, timeout_seconds, tips, enabled, encrypted_env, tenant_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		g.ID, g.BinaryID, g.AgentID,
		nullableJSON(g.DenyArgs), nullableJSON(g.DenyVerbose),
		g.TimeoutSeconds, g.Tips,
		g.Enabled, nilIfEmpty(g.EncryptedEnv), tenantID, now, now,
	)
	return err
}

func (s *PGSecureCLIAgentGrantStore) Get(ctx context.Context, id uuid.UUID) (*store.SecureCLIAgentGrant, error) {
	query := `SELECT ` + grantSelectCols + ` FROM secure_cli_agent_grants WHERE id = $1`
	args := []any{id}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, sql.ErrNoRows
		}
		query += ` AND tenant_id = $2`
		args = append(args, tid)
	}
	row := s.db.QueryRowContext(ctx, query, args...)
	return s.scanRow(row)
}

var grantAllowedFields = map[string]bool{
	"deny_args": true, "deny_verbose": true, "timeout_seconds": true,
	"tips": true, "enabled": true, "updated_at": true,
}

func (s *PGSecureCLIAgentGrantStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	for k := range updates {
		if !grantAllowedFields[k] {
			delete(updates, k)
		}
	}
	updates["updated_at"] = time.Now()

	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "secure_cli_agent_grants", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "secure_cli_agent_grants", updates, id, tid)
}

func (s *PGSecureCLIAgentGrantStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_agent_grants WHERE id = $1", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_agent_grants WHERE id = $1 AND tenant_id = $2", id, tid)
	return err
}

func (s *PGSecureCLIAgentGrantStore) ListByBinary(ctx context.Context, binaryID uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	query := `SELECT ` + grantSelectCols + ` FROM secure_cli_agent_grants WHERE binary_id = $1`
	args := []any{binaryID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, nil
		}
		query += ` AND tenant_id = $2`
		args = append(args, tid)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *PGSecureCLIAgentGrantStore) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	query := `SELECT ` + grantSelectCols + ` FROM secure_cli_agent_grants WHERE agent_id = $1`
	args := []any{agentID}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, nil
		}
		query += ` AND tenant_id = $2`
		args = append(args, tid)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *PGSecureCLIAgentGrantStore) scanRow(row *sql.Row) (*store.SecureCLIAgentGrant, error) {
	var g store.SecureCLIAgentGrant
	var denyArgs, denyVerbose *[]byte
	var timeout *int
	var tips *string
	var encEnv []byte

	err := row.Scan(
		&g.ID, &g.BinaryID, &g.AgentID,
		&denyArgs, &denyVerbose, &timeout, &tips,
		&g.Enabled, &encEnv, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	s.applyNullable(&g, denyArgs, denyVerbose, timeout, tips)
	if err := s.decryptEnv(&g, encEnv); err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *PGSecureCLIAgentGrantStore) scanRows(rows *sql.Rows) ([]store.SecureCLIAgentGrant, error) {
	defer rows.Close()
	var result []store.SecureCLIAgentGrant
	for rows.Next() {
		var g store.SecureCLIAgentGrant
		var denyArgs, denyVerbose *[]byte
		var timeout *int
		var tips *string

		var encEnv []byte
		if err := rows.Scan(
			&g.ID, &g.BinaryID, &g.AgentID,
			&denyArgs, &denyVerbose, &timeout, &tips,
			&g.Enabled, &encEnv, &g.CreatedAt, &g.UpdatedAt,
		); err != nil {
			continue
		}
		s.applyNullable(&g, denyArgs, denyVerbose, timeout, tips)
		// Finding #4: Log decrypt failures instead of silently masking them.
		// A corrupted row appears with EncryptedEnv==nil (env_set: false), which
		// could hide a key-rotation incident or DB tamper. Surface it via Error log
		// so ops can detect it. The row is still included in the result so list
		// doesn't break, but the decrypt failure is visible.
		if err := s.decryptEnv(&g, encEnv); err != nil {
			slog.Error("security.grant.decrypt_failed",
				"grant_id", g.ID,
				"binary_id", g.BinaryID,
				"err", err,
			)
			// EncryptedEnv stays nil — populateGrantEnvFields will set env_set=false,
			// which is misleading but acceptable in list view. Callers should inspect
			// logs when admin sees env_set=false on a grant they know has env set.
		}
		result = append(result, g)
	}
	return result, nil
}

// applyNullable converts scanned nullable values to pointer fields on the grant struct.
func (s *PGSecureCLIAgentGrantStore) applyNullable(g *store.SecureCLIAgentGrant, denyArgs, denyVerbose *[]byte, timeout *int, tips *string) {
	if denyArgs != nil {
		raw := json.RawMessage(*denyArgs)
		g.DenyArgs = &raw
	}
	if denyVerbose != nil {
		raw := json.RawMessage(*denyVerbose)
		g.DenyVerbose = &raw
	}
	g.TimeoutSeconds = timeout
	g.Tips = tips
}

// decryptEnv decrypts stored encrypted_env bytes into g.EncryptedEnv.
// Returns error if encKey is set but decryption fails (fail-closed).
func (s *PGSecureCLIAgentGrantStore) decryptEnv(g *store.SecureCLIAgentGrant, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if s.encKey == "" {
		return fmt.Errorf("encryption key missing: cannot decrypt grant env")
	}
	decrypted, err := crypto.Decrypt(string(raw), s.encKey)
	if err != nil {
		return fmt.Errorf("decrypt grant env: %w", err)
	}
	g.EncryptedEnv = []byte(decrypted)
	return nil
}

// UpdateGrantEnv encrypts plaintextEnv and persists it on the grant row.
// Pass nil to clear the env override. Fails closed if encKey is missing and plaintextEnv is non-empty.
func (s *PGSecureCLIAgentGrantStore) UpdateGrantEnv(ctx context.Context, grantID uuid.UUID, plaintextEnv []byte) error {
	var envBytes []byte
	if len(plaintextEnv) > 0 {
		if s.encKey == "" {
			return fmt.Errorf("encryption key missing: cannot persist grant env")
		}
		enc, err := crypto.Encrypt(string(plaintextEnv), s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt grant env: %w", err)
		}
		envBytes = []byte(enc)
	}
	now := time.Now()
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx,
			`UPDATE secure_cli_agent_grants SET encrypted_env = $1, updated_at = $2 WHERE id = $3`,
			nilIfEmpty(envBytes), now, grantID,
		)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE secure_cli_agent_grants SET encrypted_env = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`,
		nilIfEmpty(envBytes), now, grantID, tid,
	)
	return err
}

// nullableJSON returns nil if the pointer is nil, otherwise the raw bytes for the DB driver.
func nullableJSON(v *json.RawMessage) any {
	if v == nil {
		return nil
	}
	return []byte(*v)
}

// nilIfEmpty returns nil if the slice is empty, otherwise the slice (for nullable BYTEA columns).
func nilIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
