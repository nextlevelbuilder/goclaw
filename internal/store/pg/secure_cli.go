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

// PGSecureCLIStore implements store.SecureCLIStore backed by Postgres.
type PGSecureCLIStore struct {
	db     *sql.DB
	encKey string
}

func NewPGSecureCLIStore(db *sql.DB, encryptionKey string) *PGSecureCLIStore {
	return &PGSecureCLIStore{db: db, encKey: encryptionKey}
}

const secureCLISelectCols = `id, binary_name, binary_path, description, encrypted_env,
 deny_args, deny_verbose, timeout_seconds, tips, is_global, enabled, created_by, created_at, updated_at`

// secureCLISelectColsAliased is prefixed with table alias "b."
// Required for LookupByBinary which uses LEFT JOIN (ambiguous column names without prefix).
const secureCLISelectColsAliased = `b.id, b.binary_name, b.binary_path, b.description, b.encrypted_env,
 b.deny_args, b.deny_verbose, b.timeout_seconds, b.tips, b.is_global, b.enabled, b.created_by, b.created_at, b.updated_at`

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
		 deny_args, deny_verbose, timeout_seconds, tips, is_global, enabled, created_by, created_at, updated_at, tenant_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		b.ID, b.BinaryName, nilStr(derefStr(b.BinaryPath)), b.Description,
		envBytes,
		jsonOrEmptyArray(b.DenyArgs), jsonOrEmptyArray(b.DenyVerbose),
		b.TimeoutSeconds, b.Tips,
		b.IsGlobal, b.Enabled,
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
	var denyArgs, denyVerbose *[]byte
	var env []byte

	err := row.Scan(
		&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
		&denyArgs, &denyVerbose,
		&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
		&b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	b.BinaryPath = binaryPath
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
		var denyArgs, denyVerbose *[]byte
		var env []byte

		if err := rows.Scan(
			&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
			&denyArgs, &denyVerbose,
			&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
			&b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt,
		); err != nil {
			continue
		}

		b.BinaryPath = binaryPath
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
var secureCLIAllowedFields = map[string]bool{
	"binary_name": true, "binary_path": true, "description": true,
	"encrypted_env": true, "deny_args": true, "deny_verbose": true,
	"timeout_seconds": true, "tips": true, "is_global": true, "enabled": true,
	"updated_at": true,
}

func (s *PGSecureCLIStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
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
	query += ` ORDER BY binary_name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

// LookupByBinary finds the credential config for a binary name.
// Checks agent grant authorization and merges overrides if agentID is provided.
// Also fetches per-user env overrides via LEFT JOIN when userID is non-empty.
func (s *PGSecureCLIStore) LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID, userID string) (*store.SecureCLIBinary, error) {
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}

	// Build SELECT columns with optional LEFT JOINs for grant overrides and user env
	selectCols := secureCLISelectColsAliased
	grantCols := ", g.deny_args AS grant_deny_args, g.deny_verbose AS grant_deny_verbose, g.timeout_seconds AS grant_timeout, g.tips AS grant_tips, g.enabled AS grant_enabled, g.id AS grant_id"
	selectCols += grantCols

	var joinClause string
	if userID != "" {
		selectCols += ", uc.encrypted_env AS user_env"
	} else {
		selectCols += ", NULL AS user_env"
	}

	var args []any
	argIdx := 1

	// Base query
	query := `SELECT ` + selectCols + ` FROM secure_cli_binaries b`

	// LEFT JOIN agent grant
	if agentID != nil {
		query += fmt.Sprintf(` LEFT JOIN secure_cli_agent_grants g ON g.binary_id = b.id AND g.agent_id = $%d`, argIdx)
		args = append(args, *agentID)
		argIdx++
	} else {
		query += ` LEFT JOIN secure_cli_agent_grants g ON FALSE` // never match
	}

	// LEFT JOIN user credentials
	if userID != "" {
		joinClause = fmt.Sprintf(` LEFT JOIN secure_cli_user_credentials uc ON uc.binary_id = b.id AND uc.user_id = $%d`, argIdx)
		args = append(args, userID)
		argIdx++
		if !isCross {
			joinClause += fmt.Sprintf(` AND uc.tenant_id = $%d`, argIdx)
			args = append(args, tid)
			argIdx++
		}
		query += joinClause
	}

	// WHERE clause
	query += fmt.Sprintf(` WHERE b.binary_name = $%d AND b.enabled = true`, argIdx)
	args = append(args, binaryName)
	argIdx++

	if !isCross {
		query += fmt.Sprintf(` AND b.tenant_id = $%d`, argIdx)
		args = append(args, tid)
		argIdx++
	}

	// Authorization: global (no grant needed OR has enabled grant) OR non-global (must have enabled grant)
	if agentID != nil {
		query += ` AND (
			(b.is_global = true AND (g.id IS NULL OR g.enabled = true))
			OR
			(b.is_global = false AND g.id IS NOT NULL AND g.enabled = true)
		)`
	} else {
		// No agent context — only return global binaries
		query += ` AND b.is_global = true`
	}

	query += ` LIMIT 1`

	row := s.db.QueryRowContext(ctx, query, args...)
	return s.scanRowWithGrantAndUserEnv(row)
}

// scanRowWithGrantAndUserEnv scans a row that includes grant override columns and user_env.
func (s *PGSecureCLIStore) scanRowWithGrantAndUserEnv(row *sql.Row) (*store.SecureCLIBinary, error) {
	var b store.SecureCLIBinary
	var binaryPath *string
	var denyArgs, denyVerbose *[]byte
	var env []byte
	// Grant override columns (nullable)
	var grantDenyArgs, grantDenyVerbose *[]byte
	var grantTimeout *int
	var grantTips *string
	var grantEnabled *bool
	var grantID *uuid.UUID
	var userEnv []byte

	err := row.Scan(
		&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
		&denyArgs, &denyVerbose,
		&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
		&b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt,
		// Grant columns
		&grantDenyArgs, &grantDenyVerbose, &grantTimeout, &grantTips, &grantEnabled, &grantID,
		// User env
		&userEnv,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	b.BinaryPath = binaryPath
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

	// Apply grant overrides (if grant exists)
	if grantID != nil {
		grant := &store.SecureCLIAgentGrant{}
		if grantDenyArgs != nil {
			raw := json.RawMessage(*grantDenyArgs)
			grant.DenyArgs = &raw
		}
		if grantDenyVerbose != nil {
			raw := json.RawMessage(*grantDenyVerbose)
			grant.DenyVerbose = &raw
		}
		grant.TimeoutSeconds = grantTimeout
		grant.Tips = grantTips
		b.MergeGrantOverrides(grant)
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

// ListForAgent returns all CLIs accessible by an agent (global + granted),
// with grant overrides merged into the returned configs.
func (s *PGSecureCLIStore) ListForAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIBinary, error) {
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}

	selectCols := secureCLISelectColsAliased +
		`, g.deny_args AS grant_deny_args, g.deny_verbose AS grant_deny_verbose,
		   g.timeout_seconds AS grant_timeout, g.tips AS grant_tips, g.id AS grant_id`

	query := `SELECT ` + selectCols + ` FROM secure_cli_binaries b
		LEFT JOIN secure_cli_agent_grants g ON g.binary_id = b.id AND g.agent_id = $1
		WHERE b.enabled = true
		  AND (
		    (b.is_global = true AND (g.id IS NULL OR g.enabled = true))
		    OR
		    (b.is_global = false AND g.id IS NOT NULL AND g.enabled = true)
		  )`

	args := []any{agentID}
	if !isCross {
		query += ` AND b.tenant_id = $2`
		args = append(args, tid)
	}
	query += ` ORDER BY b.binary_name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SecureCLIBinary
	for rows.Next() {
		var b store.SecureCLIBinary
		var binaryPath *string
		var denyArgs, denyVerbose *[]byte
		var env []byte
		var grantDenyArgs, grantDenyVerbose *[]byte
		var grantTimeout *int
		var grantTips *string
		var grantID *uuid.UUID

		if err := rows.Scan(
			&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
			&denyArgs, &denyVerbose,
			&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
			&b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt,
			&grantDenyArgs, &grantDenyVerbose, &grantTimeout, &grantTips, &grantID,
		); err != nil {
			continue
		}

		b.BinaryPath = binaryPath
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

		// Apply grant overrides
		if grantID != nil {
			grant := &store.SecureCLIAgentGrant{}
			if grantDenyArgs != nil {
				raw := json.RawMessage(*grantDenyArgs)
				grant.DenyArgs = &raw
			}
			if grantDenyVerbose != nil {
				raw := json.RawMessage(*grantDenyVerbose)
				grant.DenyVerbose = &raw
			}
			grant.TimeoutSeconds = grantTimeout
			grant.Tips = grantTips
			b.MergeGrantOverrides(grant)
		}

		result = append(result, b)
	}
	return result, nil
}
