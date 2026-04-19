//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteSecureCLIStore implements store.SecureCLIStore backed by SQLite.
// encKey is required for AES-256-GCM encryption of encrypted_env columns.
type SQLiteSecureCLIStore struct {
	db     *sql.DB
	encKey string
}

// NewSQLiteSecureCLIStore creates a new SQLiteSecureCLIStore.
func NewSQLiteSecureCLIStore(db *sql.DB, encKey string) *SQLiteSecureCLIStore {
	return &SQLiteSecureCLIStore{db: db, encKey: encKey}
}

const secureCLISelectCols = `id, binary_name, binary_path, description, encrypted_env,
 deny_args, deny_verbose, timeout_seconds, tips, is_global, enabled, created_by, created_at, updated_at`

const secureCLISelectColsAliased = `b.id, b.binary_name, b.binary_path, b.description, b.encrypted_env,
 b.deny_args, b.deny_verbose, b.timeout_seconds, b.tips, b.is_global, b.enabled, b.created_by, b.created_at, b.updated_at`

func (s *SQLiteSecureCLIStore) Create(ctx context.Context, b *store.SecureCLIBinary) error {
	if err := store.ValidateUserID(b.CreatedBy); err != nil {
		return err
	}
	if b.ID == uuid.Nil {
		b.ID = store.GenNewID()
	}

	// Normalize binary_name to lowercase so IsRegisteredBinary (which lowercases
	// the candidate) can match. Admin entering "Gh" becomes "gh".
	b.BinaryName = strings.ToLower(strings.TrimSpace(b.BinaryName))

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

	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	nowStr := now.Format(time.RFC3339Nano)

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_binaries (id, binary_name, binary_path, description, encrypted_env,
		 deny_args, deny_verbose, timeout_seconds, tips, is_global, enabled, created_by, created_at, updated_at, tenant_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.BinaryName, nilStr(derefStr(b.BinaryPath)), b.Description,
		envBytes,
		jsonOrEmptyArray(b.DenyArgs), jsonOrEmptyArray(b.DenyVerbose),
		b.TimeoutSeconds, b.Tips,
		b.IsGlobal, b.Enabled,
		b.CreatedBy, nowStr, nowStr, tenantID,
	)
	return err
}

func (s *SQLiteSecureCLIStore) Get(ctx context.Context, id uuid.UUID) (*store.SecureCLIBinary, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries WHERE id = ?`, id)
		return s.scanRow(row)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries WHERE id = ? AND tenant_id = ?`, id, tenantID)
	return s.scanRow(row)
}

func (s *SQLiteSecureCLIStore) scanRow(row *sql.Row) (*store.SecureCLIBinary, error) {
	var b store.SecureCLIBinary
	var binaryPath *string
	var denyArgs, denyVerbose []byte
	var env []byte
	var createdAt, updatedAt sqliteTime

	err := row.Scan(
		&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
		&denyArgs, &denyVerbose,
		&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
		&b.Enabled, &b.CreatedBy, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	b.BinaryPath = binaryPath
	if len(denyArgs) > 0 {
		b.DenyArgs = json.RawMessage(denyArgs)
	}
	if len(denyVerbose) > 0 {
		b.DenyVerbose = json.RawMessage(denyVerbose)
	}
	b.CreatedAt = createdAt.Time
	b.UpdatedAt = updatedAt.Time

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

func (s *SQLiteSecureCLIStore) scanRows(rows *sql.Rows) ([]store.SecureCLIBinary, error) {
	defer rows.Close()
	var result []store.SecureCLIBinary
	for rows.Next() {
		var b store.SecureCLIBinary
		var binaryPath *string
		var denyArgs, denyVerbose []byte
		var env []byte
		var createdAt, updatedAt sqliteTime

		if err := rows.Scan(
			&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
			&denyArgs, &denyVerbose,
			&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
			&b.Enabled, &b.CreatedBy, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan secure_cli_binaries row: %w", err)
		}

		b.BinaryPath = binaryPath
		if len(denyArgs) > 0 {
			b.DenyArgs = json.RawMessage(denyArgs)
		}
		if len(denyVerbose) > 0 {
			b.DenyVerbose = json.RawMessage(denyVerbose)
		}
		b.CreatedAt = createdAt.Time
		b.UpdatedAt = updatedAt.Time

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

var secureCLIAllowedFields = map[string]bool{
	"binary_name": true, "binary_path": true, "description": true,
	"encrypted_env": true, "deny_args": true, "deny_verbose": true,
	"timeout_seconds": true, "tips": true, "is_global": true, "enabled": true,
	"updated_at": true,
}

func (s *SQLiteSecureCLIStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	for k := range updates {
		if !secureCLIAllowedFields[k] {
			delete(updates, k)
		}
	}

	// Normalize binary_name to lowercase if updated (parity with Create).
	if nameVal, ok := updates["binary_name"]; ok {
		if nameStr, isStr := nameVal.(string); isStr {
			updates["binary_name"] = strings.ToLower(strings.TrimSpace(nameStr))
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
	updates["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "secure_cli_binaries", id, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "secure_cli_binaries", updates, id, tid)
}

func (s *SQLiteSecureCLIStore) Delete(ctx context.Context, id uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_binaries WHERE id = ?", id)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_binaries WHERE id = ? AND tenant_id = ?", id, tid)
	return err
}

func (s *SQLiteSecureCLIStore) List(ctx context.Context) ([]store.SecureCLIBinary, error) {
	query := `SELECT ` + secureCLISelectCols + ` FROM secure_cli_binaries`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		query += ` WHERE tenant_id = ?`
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
// LEFT JOINs grant overrides and per-user credentials.
func (s *SQLiteSecureCLIStore) LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID, userID string) (*store.SecureCLIBinary, error) {
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}

	selectCols := secureCLISelectColsAliased
	selectCols += `, g.deny_args AS grant_deny_args, g.deny_verbose AS grant_deny_verbose, g.timeout_seconds AS grant_timeout, g.tips AS grant_tips, g.enabled AS grant_enabled, g.id AS grant_id`

	var args []any

	query := `SELECT ` + selectCols

	// LEFT JOIN agent grant
	if agentID != nil {
		query += `, uc_user.encrypted_env AS user_env FROM secure_cli_binaries b`
		query += ` LEFT JOIN secure_cli_agent_grants g ON g.binary_id = b.id AND g.agent_id = ?`
		args = append(args, *agentID)
	} else {
		query += `, NULL AS user_env FROM secure_cli_binaries b`
		query += ` LEFT JOIN secure_cli_agent_grants g ON 0`
	}

	// LEFT JOIN user credentials
	if userID != "" {
		if isCross {
			query += ` LEFT JOIN secure_cli_user_credentials uc_user ON uc_user.binary_id = b.id AND uc_user.user_id = ?`
			args = append(args, userID)
		} else {
			query += ` LEFT JOIN secure_cli_user_credentials uc_user ON uc_user.binary_id = b.id AND uc_user.user_id = ? AND uc_user.tenant_id = ?`
			args = append(args, userID, tid)
		}
	} else {
		// Rewrite: no user_env JOIN needed, replace alias reference
		// Already handled by NULL above — but need to adjust query structure
		// We need uc_user alias to not appear in FROM if no userID
		// Simplest: LEFT JOIN on impossible condition
		if agentID == nil {
			// already have NULL AS user_env, skip join
		} else {
			query += ` LEFT JOIN secure_cli_user_credentials uc_user ON 0`
		}
	}

	// WHERE
	query += ` WHERE b.binary_name = ? AND b.enabled = 1`
	args = append(args, binaryName)

	if !isCross {
		query += ` AND b.tenant_id = ?`
		args = append(args, tid)
	}

	// Authorization
	if agentID != nil {
		query += ` AND (
			(b.is_global = 1 AND (g.id IS NULL OR g.enabled = 1))
			OR
			(b.is_global = 0 AND g.id IS NOT NULL AND g.enabled = 1)
		)`
	} else {
		query += ` AND b.is_global = 1`
	}

	query += ` LIMIT 1`

	row := s.db.QueryRowContext(ctx, query, args...)
	return s.scanRowWithGrantAndUserEnv(row)
}

func (s *SQLiteSecureCLIStore) scanRowWithGrantAndUserEnv(row *sql.Row) (*store.SecureCLIBinary, error) {
	var b store.SecureCLIBinary
	var binaryPath *string
	var denyArgs, denyVerbose []byte
	var env []byte
	var grantDenyArgs, grantDenyVerbose []byte
	var grantTimeout *int
	var grantTips *string
	var grantEnabled *bool
	var grantID *uuid.UUID
	var userEnv []byte
	var createdAt, updatedAt sqliteTime

	err := row.Scan(
		&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
		&denyArgs, &denyVerbose,
		&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
		&b.Enabled, &b.CreatedBy, &createdAt, &updatedAt,
		&grantDenyArgs, &grantDenyVerbose, &grantTimeout, &grantTips, &grantEnabled, &grantID,
		&userEnv,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	b.BinaryPath = binaryPath
	if len(denyArgs) > 0 {
		b.DenyArgs = json.RawMessage(denyArgs)
	}
	if len(denyVerbose) > 0 {
		b.DenyVerbose = json.RawMessage(denyVerbose)
	}
	b.CreatedAt = createdAt.Time
	b.UpdatedAt = updatedAt.Time

	// Decrypt base env
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
		if len(grantDenyArgs) > 0 {
			raw := json.RawMessage(grantDenyArgs)
			grant.DenyArgs = &raw
		}
		if len(grantDenyVerbose) > 0 {
			raw := json.RawMessage(grantDenyVerbose)
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

// ListEnabled returns all enabled configs.
func (s *SQLiteSecureCLIStore) ListEnabled(ctx context.Context) ([]store.SecureCLIBinary, error) {
	query := `SELECT ` + secureCLISelectCols + ` FROM secure_cli_binaries WHERE enabled = 1`
	var qArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		query += ` AND tenant_id = ?`
		qArgs = append(qArgs, tenantID)
	}
	query += ` ORDER BY binary_name`
	rows, err := s.db.QueryContext(ctx, query, qArgs...)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

// IsRegisteredBinary reports whether a binary requires a grant (is_global=0)
// and is enabled for the current tenant. See interface godoc for rationale.
func (s *SQLiteSecureCLIStore) IsRegisteredBinary(ctx context.Context, binaryName string) (bool, error) {
	name := strings.ToLower(strings.TrimSpace(binaryName))
	if name == "" {
		return false, nil
	}
	query := `SELECT EXISTS(
		SELECT 1 FROM secure_cli_binaries
		WHERE LOWER(binary_name) = ?
		  AND enabled = 1
		  AND is_global = 0`
	args := []any{name}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return false, nil
		}
		query += ` AND tenant_id = ?`
		args = append(args, tid)
	}
	query += `)`
	var exists bool
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// ListForAgent returns all CLIs accessible by an agent (global + granted),
// with grant overrides merged into the returned configs.
func (s *SQLiteSecureCLIStore) ListForAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIBinary, error) {
	tid := store.TenantIDFromContext(ctx)
	isCross := store.IsCrossTenant(ctx)
	if !isCross && tid == uuid.Nil {
		return nil, nil
	}

	selectCols := secureCLISelectColsAliased +
		`, g.deny_args AS grant_deny_args, g.deny_verbose AS grant_deny_verbose,
		   g.timeout_seconds AS grant_timeout, g.tips AS grant_tips, g.id AS grant_id`

	query := `SELECT ` + selectCols + ` FROM secure_cli_binaries b
		LEFT JOIN secure_cli_agent_grants g ON g.binary_id = b.id AND g.agent_id = ?
		WHERE b.enabled = 1
		  AND (
		    b.is_global = 1
		    OR (b.id IN (SELECT binary_id FROM secure_cli_agent_grants WHERE agent_id = ? AND enabled = 1))
		  )`

	args := []any{agentID, agentID}
	if !isCross {
		query += ` AND b.tenant_id = ?`
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
		var denyArgs, denyVerbose []byte
		var env []byte
		var grantDenyArgs, grantDenyVerbose []byte
		var grantTimeout *int
		var grantTips *string
		var grantID *uuid.UUID
		var createdAt, updatedAt sqliteTime

		if err := rows.Scan(
			&b.ID, &b.BinaryName, &binaryPath, &b.Description, &env,
			&denyArgs, &denyVerbose,
			&b.TimeoutSeconds, &b.Tips, &b.IsGlobal,
			&b.Enabled, &b.CreatedBy, &createdAt, &updatedAt,
			&grantDenyArgs, &grantDenyVerbose, &grantTimeout, &grantTips, &grantID,
		); err != nil {
			return nil, fmt.Errorf("scan secure_cli_binaries row: %w", err)
		}

		b.BinaryPath = binaryPath
		if len(denyArgs) > 0 {
			b.DenyArgs = json.RawMessage(denyArgs)
		}
		if len(denyVerbose) > 0 {
			b.DenyVerbose = json.RawMessage(denyVerbose)
		}
		b.CreatedAt = createdAt.Time
		b.UpdatedAt = updatedAt.Time

		if len(env) > 0 && s.encKey != "" {
			if decrypted, err := crypto.Decrypt(string(env), s.encKey); err == nil {
				b.EncryptedEnv = []byte(decrypted)
			}
		} else {
			b.EncryptedEnv = env
		}

		if grantID != nil {
			grant := &store.SecureCLIAgentGrant{}
			if len(grantDenyArgs) > 0 {
				raw := json.RawMessage(grantDenyArgs)
				grant.DenyArgs = &raw
			}
			if len(grantDenyVerbose) > 0 {
				raw := json.RawMessage(grantDenyVerbose)
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
