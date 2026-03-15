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

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_binaries (id, binary_name, binary_path, description, encrypted_env,
		 deny_args, deny_verbose, timeout_seconds, tips, agent_id, enabled, created_by, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		b.ID, b.BinaryName, nilStr(derefStr(b.BinaryPath)), b.Description,
		envBytes,
		jsonOrEmptyArray(b.DenyArgs), jsonOrEmptyArray(b.DenyVerbose),
		b.TimeoutSeconds, b.Tips,
		nilUUID(b.AgentID), b.Enabled,
		b.CreatedBy, now, now,
	)
	return err
}

func (s *PGSecureCLIStore) Get(ctx context.Context, id uuid.UUID) (*store.SecureCLIBinary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries WHERE id = $1`, id)
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
	return execMapUpdate(ctx, s.db, "secure_cli_binaries", id, updates)
}

func (s *PGSecureCLIStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM secure_cli_binaries WHERE id = $1", id)
	return err
}

func (s *PGSecureCLIStore) List(ctx context.Context) ([]store.SecureCLIBinary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries ORDER BY binary_name, agent_id NULLS LAST`)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *PGSecureCLIStore) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIBinary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries
		 WHERE (agent_id = $1 OR agent_id IS NULL) AND enabled = true
		 ORDER BY binary_name, agent_id NULLS LAST`, agentID)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

// LookupByBinary finds the best credential config for a binary name.
// Agent-specific config takes priority over global (agent_id IS NULL).
func (s *PGSecureCLIStore) LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID) (*store.SecureCLIBinary, error) {
	var row *sql.Row
	if agentID != nil {
		row = s.db.QueryRowContext(ctx,
			`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries
			 WHERE binary_name = $1 AND (agent_id = $2 OR agent_id IS NULL) AND enabled = true
			 ORDER BY agent_id NULLS LAST LIMIT 1`, binaryName, *agentID)
	} else {
		row = s.db.QueryRowContext(ctx,
			`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries
			 WHERE binary_name = $1 AND agent_id IS NULL AND enabled = true
			 LIMIT 1`, binaryName)
	}
	b, err := s.scanRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func (s *PGSecureCLIStore) ListEnabled(ctx context.Context) ([]store.SecureCLIBinary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+secureCLISelectCols+` FROM secure_cli_binaries
		 WHERE enabled = true ORDER BY binary_name`)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}
