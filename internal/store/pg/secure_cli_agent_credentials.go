package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const agentCredSelectCols = `c.id, c.binary_id, c.agent_id, c.encrypted_env, c.metadata,
	 c.credential_type, c.host_scope, c.created_by, c.created_at, c.updated_at`

func agentCredentialTenantID(ctx context.Context) (uuid.UUID, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return uuid.Nil, fmt.Errorf("tenant_id required for agent credentials")
	}
	return tid, nil
}

func (s *PGSecureCLIStore) BinaryExists(ctx context.Context, binaryID uuid.UUID) (bool, error) {
	tid, err := agentCredentialTenantID(ctx)
	if err != nil {
		return false, nil
	}
	var exists bool
	err = s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM secure_cli_binaries WHERE id = $1 AND tenant_id = $2)`,
		binaryID, tid,
	).Scan(&exists)
	return exists, err
}

func (s *PGSecureCLIStore) AgentExists(ctx context.Context, agentID uuid.UUID) (bool, error) {
	tid, err := agentCredentialTenantID(ctx)
	if err != nil {
		return false, nil
	}
	var exists bool
	err = s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM agents WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL)`,
		agentID, tid,
	).Scan(&exists)
	return exists, err
}

func (s *PGSecureCLIStore) GetAgentCredentials(ctx context.Context, binaryID uuid.UUID, agentID uuid.UUID) (*store.SecureCLIAgentCredential, error) {
	tid, err := agentCredentialTenantID(ctx)
	if err != nil {
		return nil, err
	}
	var c store.SecureCLIAgentCredential
	var env []byte
	err = s.db.QueryRowContext(ctx,
		`SELECT `+agentCredSelectCols+`, COALESCE(a.agent_key, ''), COALESCE(a.display_name, '')
		 FROM secure_cli_agent_credentials c
		 LEFT JOIN agents a ON a.id = c.agent_id AND a.tenant_id = c.tenant_id
		 WHERE c.binary_id = $1 AND c.agent_id = $2 AND c.tenant_id = $3`,
		binaryID, agentID, tid,
	).Scan(&c.ID, &c.BinaryID, &c.AgentID, &env, &c.Metadata,
		&c.CredentialType, &c.HostScope, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		&c.AgentKey, &c.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.EncryptedEnv = s.decryptAgentCredentialEnv(env)
	return &c, nil
}

func (s *PGSecureCLIStore) SetAgentCredentials(ctx context.Context, binaryID uuid.UUID, agentID uuid.UUID, encryptedEnv []byte, createdBy string) error {
	return s.SetAgentCredentialsTyped(ctx, binaryID, agentID, encryptedEnv, nil, nil, createdBy)
}

func (s *PGSecureCLIStore) SetAgentCredentialsTyped(ctx context.Context, binaryID uuid.UUID, agentID uuid.UUID, encryptedEnv []byte, credentialType, hostScope *string, createdBy string) error {
	tid, err := agentCredentialTenantID(ctx)
	if err != nil {
		return err
	}
	envBytes, err := s.encryptAgentCredentialEnv(encryptedEnv)
	if err != nil {
		return err
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO secure_cli_agent_credentials
		   (binary_id, agent_id, encrypted_env, metadata, tenant_id,
		    credential_type, host_scope, created_by, created_at, updated_at)
		 SELECT b.id, a.id, $3, '{}', $4, $5, $6, $7, $8, $8
		 FROM secure_cli_binaries b
		 JOIN agents a ON a.id = $2 AND a.tenant_id = $4 AND a.deleted_at IS NULL
		 WHERE b.id = $1 AND b.tenant_id = $4
		 ON CONFLICT (binary_id, agent_id, tenant_id) DO UPDATE SET
		   encrypted_env   = EXCLUDED.encrypted_env,
		   credential_type = EXCLUDED.credential_type,
		   host_scope      = EXCLUDED.host_scope,
		   created_by      = EXCLUDED.created_by,
		   updated_at      = EXCLUDED.updated_at`,
		binaryID, agentID, envBytes, tid, credentialType, hostScope, createdBy, now,
	)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PGSecureCLIStore) DeleteAgentCredentials(ctx context.Context, binaryID uuid.UUID, agentID uuid.UUID) error {
	tid, err := agentCredentialTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM secure_cli_agent_credentials WHERE binary_id = $1 AND agent_id = $2 AND tenant_id = $3`,
		binaryID, agentID, tid,
	)
	return err
}

func (s *PGSecureCLIStore) ListAgentCredentials(ctx context.Context, binaryID uuid.UUID) ([]store.SecureCLIAgentCredential, error) {
	tid, err := agentCredentialTenantID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+agentCredSelectCols+`, COALESCE(a.agent_key, ''), COALESCE(a.display_name, '')
		 FROM secure_cli_agent_credentials c
		 LEFT JOIN agents a ON a.id = c.agent_id AND a.tenant_id = c.tenant_id
		 WHERE c.binary_id = $1 AND c.tenant_id = $2
		 ORDER BY c.created_at`, binaryID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SecureCLIAgentCredential
	for rows.Next() {
		var c store.SecureCLIAgentCredential
		var env []byte
		if err := rows.Scan(&c.ID, &c.BinaryID, &c.AgentID, &env, &c.Metadata,
			&c.CredentialType, &c.HostScope, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
			&c.AgentKey, &c.Name); err != nil {
			return nil, err
		}
		c.EncryptedEnv = s.decryptAgentCredentialEnv(env)
		result = append(result, c)
	}
	return result, rows.Err()
}

func (s *PGSecureCLIStore) encryptAgentCredentialEnv(env []byte) ([]byte, error) {
	if len(env) == 0 || s.encKey == "" {
		return env, nil
	}
	encrypted, err := crypto.Encrypt(string(env), s.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt agent credential env: %w", err)
	}
	return []byte(encrypted), nil
}

func (s *PGSecureCLIStore) decryptAgentCredentialEnv(env []byte) []byte {
	if len(env) > 0 && s.encKey != "" {
		if decrypted, err := crypto.Decrypt(string(env), s.encKey); err == nil {
			return []byte(decrypted)
		}
	}
	return env
}
