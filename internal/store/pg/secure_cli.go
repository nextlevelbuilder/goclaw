package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGSecureCLIStore implements store.SecureCLIStore backed by Postgres.
type PGSecureCLIStore struct {
	db     *sql.DB
	encKey string
}

func NewPGSecureCLIStore(db *sql.DB, encryptionKey string) *PGSecureCLIStore {
	return &PGSecureCLIStore{db: db, encKey: encryptionKey}
}

const secureCLISelectCols = `id, binary_name, binary_path, description, encrypted_env,
 deny_args, deny_verbose, timeout_seconds, tips, agent_id, enabled, created_by, created_at, updated_at`

// secureCLISelectColsAliased is the same as secureCLISelectCols but prefixed with table alias "b."
// Required for LookupByBinary which uses LEFT JOIN (ambiguous column names without prefix).
const secureCLISelectColsAliased = `b.id, b.binary_name, b.binary_path, b.description, b.encrypted_env,
 b.deny_args, b.deny_verbose, b.timeout_seconds, b.tips, b.agent_id, b.enabled, b.created_by, b.created_at, b.updated_at`

func (s *PGSecureCLIStore) Create(ctx context.Context, b *store.SecureCLIBinary) error {
	if err := store.ValidateUserID(b.CreatedBy); err != nil {
		return err
	}
	if b.ID == uuid.Nil {
		b.ID = store.GenNewID()
	}

	// Encrypt env if provided
	var envBytes []byte
	if len(b.EncryptedEnv) > 0 && s.encKey != "" {
		encrypted, err := crypto.Encrypt(string(b.EncryptedEnv), s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt env: %w", err)
		}
		envBytes = []byte(encrypted)
	} else {
		envBytes = b.EncryptedEnv
	}

	now := time.Now()
	b.CreatedAt = now
	b.UpdatedAt = now

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_binaries (id, binary_name, binary_path, description, encrypted_env,
		 deny_args, deny_verbose, timeout_seconds, tips, agent_id, enabled, created_by, created_at, updated_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		b.ID, b.BinaryName, nilStr(derefStr(b.BinaryPath)), b.Description,
		envBytes,
		jsonOrEmptyArray(b.DenyArgs), jsonOrEmptyArray(b.DenyVerbose),
		b.TimeoutSeconds, b.Tips,
		nilUUID(b.AgentID), b.Enabled,
		b.CreatedBy, now, now, tenantID,
	)
	return err
}

func (s *PGSecureCLIStore) Get(ctx context.Context, id uuid.UUID) (*store.SecureCLIBinary, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries WHERE id = $1`, id)
		return s.scanRow(row)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return s.scanRow(row)
}

func (s *PGSecureCLIStore) scanRow(row *sql.Row) (*store.SecureCLIBinary, error) {
	var b store.SecureCLIBinary
	var binaryPath *string
	var agentID *uuid.UUID
	var denyArgs, denyVerbose *[]byte
	var env []byte

	err := row.Scan(
		&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
		&denyArgs, &denyVerbose,
		&b.TimeoutSeconds, &b.Tips, &agentID,
		&b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	b.BinaryPath = binaryPath
	b.AgentID = agentID
	if denyArgs != nil {
		b.DenyArgs = *denyArgs
	}
	if denyVerbose != nil {
		b.DenyVerbose = *denyVerbose
	}

	// Decrypt env
	if len(env) > 0 && s.encKey != "" {
		decrypted, err := crypto.Decrypt(string(env), s.encKey)
		if err != nil {
			slog.Warn("secure_cli: failed to decrypt env", "binary", b.BinaryName, "error", err)
		} else {
			b.EncryptedEnv = []byte(decrypted)
		}
	} else {
		b.EncryptedEnv = env
	}

	return &b, nil
}

func (s *PGSecureCLIStore) scanRows(rows *sql.Rows) ([]store.SecureCLIBinary, error) {
	defer rows.Close()
	var result []store.SecureCLIBinary
	for rows.Next() {
		var b store.SecureCLIBinary
		var binaryPath *string
		var agentID *uuid.UUID
		var denyArgs, denyVerbose *[]byte
		var env []byte

		if err := rows.Scan(
			&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
			&denyArgs, &denyVerbose,
			&b.TimeoutSeconds, &b.Tips, &agentID,
			&b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			continue
		}

		b.BinaryPath = binaryPath
		b.AgentID = agentID
		if denyArgs != nil {
			b.DenyArgs = *denyArgs
		}
		if denyVerbose != nil {
			b.DenyVerbose = *denyVerbose
		}
		if len(env) > 0 && s.encKey != "" {
			if decrypted, err := crypto.Decrypt(string(env), s.encKey); err == nil {
				b.EncryptedEnv = []byte(decrypted)
			}
		} else {
			b.EncryptedEnv = env
		}

		result = append(result, b)
	}
	return result, nil
}

// secureCLIAllowedFields is the allowlist of columns that can be updated via execMapUpdate.
// Defense-in-depth: prevents column name injection even if caller skips validation.
var secureCLIAllowedFields = map[string]bool{
	"binary_name": true, "binary_path": true, "description": true,
	"encrypted_env": true, "deny_args": true, "deny_verbose": true,
	"timeout_seconds": true, "tips": true, "agent_id": true, "enabled": true,
	"updated_at": true,
}

func (s *PGSecureCLIStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	// Filter unknown fields to prevent column name injection
	for k := range updates {
		if !secureCLIAllowedFields[k] {
			delete(updates, k)
		}
	}

	// Encrypt env if present in updates
	if envVal, ok := updates["encrypted_env"]; ok {
		if envStr, isStr := envVal.(string); isStr && envStr != "" && s.encKey != "" {
			encrypted, err := crypto.Encrypt(envStr, s.encKey)
			if err != nil {
				return fmt.Errorf("encrypt env: %w", err)
			}
			updates["encrypted_env"] = []byte(encrypted)
		}
	}
	updates["updated_at"] = time.Now()
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "secure_cli_binaries", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "secure_cli_binaries", updates, id, tid)
}

func (s *PGSecureCLIStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_binaries WHERE id = $1", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_binaries WHERE id = $1 AND tenant_id = $2", id, tid)
	return err
}

func (s *PGSecureCLIStore) List(ctx context.Context) ([]store.SecureCLIBinary, error) {
	query := `SELECT ` + secureCLISelectCols + ` FROM secure_cli_binaries`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		query += ` WHERE tenant_id = $1`
		qArgs = append(qArgs, tenantID)
	}
	query += ` ORDER BY binary_name, agent_id NULLS LAST`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *PGSecureCLIStore) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIBinary, error) {
	if store.IsCrossTenant(ctx) {
		rows, err := s.db.QueryContext(ctx,
			`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries
			 WHERE (agent_id = $1 OR agent_id IS NULL) AND enabled = true
			 ORDER BY binary_name, agent_id NULLS LAST`, agentID)
		if err != nil {
			return nil, err
		}
		return s.scanRows(rows)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries
		 WHERE (agent_id = $1 OR agent_id IS NULL) AND enabled = true AND tenant_id = $2
		 ORDER BY binary_name, agent_id NULLS LAST`, agentID, tenantID)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

// LookupByBinary finds the best credential config for a binary name.
// Agent-specific config takes priority over global (agent_id IS NULL).
// If userID is non-empty, also fetches per-user env overrides via LEFT JOIN (zero extra queries).
func (s *PGSecureCLIStore) LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID, userID string) (*store.SecureCLIBinary, error) {
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}

	// Build query with optional LEFT JOIN for per-user credentials.
	// Use aliased columns (b.) to avoid ambiguous column reference with JOIN.
	selectCols := secureCLISelectColsAliased
	joinClause := ""
	if userID != "" {
		selectCols += ", uc.encrypted_env AS user_env"
		joinClause = " LEFT JOIN secure_cli_user_credentials uc ON uc.binary_id = b.id AND uc.user_id = $%d AND uc.tenant_id = $%d"
	} else {
		selectCols += ", NULL AS user_env"
	}

	var args []any
	var query string

	if agentID != nil {
		if userID != "" {
			if isCross {
				args = []any{binaryName, *agentID, userID, tid}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b` +
					fmt.Sprintf(joinClause, 3, 4) +
					` WHERE b.binary_name = $1 AND (b.agent_id = $2 OR b.agent_id IS NULL) AND b.enabled = true
					 ORDER BY b.agent_id NULLS LAST LIMIT 1`
			} else {
				args = []any{binaryName, *agentID, tid, userID, tid}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b` +
					fmt.Sprintf(joinClause, 4, 5) +
					` WHERE b.binary_name = $1 AND (b.agent_id = $2 OR b.agent_id IS NULL) AND b.enabled = true AND b.tenant_id = $3
					 ORDER BY b.agent_id NULLS LAST LIMIT 1`
			}
		} else {
			if isCross {
				args = []any{binaryName, *agentID}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b
					 WHERE b.binary_name = $1 AND (b.agent_id = $2 OR b.agent_id IS NULL) AND b.enabled = true
					 ORDER BY b.agent_id NULLS LAST LIMIT 1`
			} else {
				args = []any{binaryName, *agentID, tid}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b
					 WHERE b.binary_name = $1 AND (b.agent_id = $2 OR b.agent_id IS NULL) AND b.enabled = true AND b.tenant_id = $3
					 ORDER BY b.agent_id NULLS LAST LIMIT 1`
			}
		}
	} else {
		if userID != "" {
			if isCross {
				args = []any{binaryName, userID, tid}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b` +
					fmt.Sprintf(joinClause, 2, 3) +
					` WHERE b.binary_name = $1 AND b.agent_id IS NULL AND b.enabled = true LIMIT 1`
			} else {
				args = []any{binaryName, tid, userID, tid}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b` +
					fmt.Sprintf(joinClause, 3, 4) +
					` WHERE b.binary_name = $1 AND b.agent_id IS NULL AND b.enabled = true AND b.tenant_id = $2 LIMIT 1`
			}
		} else {
			if isCross {
				args = []any{binaryName}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b
					 WHERE b.binary_name = $1 AND b.agent_id IS NULL AND b.enabled = true LIMIT 1`
			} else {
				args = []any{binaryName, tid}
				query = `SELECT ` + selectCols + ` FROM secure_cli_binaries b
					 WHERE b.binary_name = $1 AND b.agent_id IS NULL AND b.enabled = true AND b.tenant_id = $2 LIMIT 1`
			}
		}
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	b, err := s.scanRowWithUserEnv(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

// scanRowWithUserEnv scans a row that includes the extra user_env column from LEFT JOIN.
func (s *PGSecureCLIStore) scanRowWithUserEnv(row *sql.Row) (*store.SecureCLIBinary, error) {
	var b store.SecureCLIBinary
	var binaryPath *string
	var agentID *uuid.UUID
	var denyArgs, denyVerbose *[]byte
	var env []byte
	var userEnv []byte

	err := row.Scan(
		&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
		&denyArgs, &denyVerbose,
		&b.TimeoutSeconds, &b.Tips, &agentID,
		&b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt,
		&userEnv,
	)
	if err != nil {
		return nil, err
	}

	b.BinaryPath = binaryPath
	b.AgentID = agentID
	if denyArgs != nil {
		b.DenyArgs = *denyArgs
	}
	if denyVerbose != nil {
		b.DenyVerbose = *denyVerbose
	}

	// Decrypt base env
	if len(env) > 0 && s.encKey != "" {
		if decrypted, err := crypto.Decrypt(string(env), s.encKey); err == nil {
			b.EncryptedEnv = []byte(decrypted)
		}
	} else {
		b.EncryptedEnv = env
	}

	// Decrypt per-user env
	if len(userEnv) > 0 && s.encKey != "" {
		if decrypted, err := crypto.Decrypt(string(userEnv), s.encKey); err == nil {
			b.UserEnv = []byte(decrypted)
		}
	}

	return &b, nil
}

func (s *PGSecureCLIStore) ListEnabled(ctx context.Context) ([]store.SecureCLIBinary, error) {
	query := `SELECT ` + secureCLISelectCols + ` FROM secure_cli_binaries WHERE enabled = true`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		query += ` AND tenant_id = $1`
		qArgs = append(qArgs, tenantID)
	}
	query += ` ORDER BY binary_name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}
