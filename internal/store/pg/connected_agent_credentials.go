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

// PGConnectedAgentCredentialStore implements store.ConnectedAgentCredentialStore
// backed by Postgres. Mirrors the encrypt-on-write / decrypt-on-read pattern of
// config_secrets and llm_providers.
type PGConnectedAgentCredentialStore struct {
	db     *sql.DB
	encKey string
}

func NewPGConnectedAgentCredentialStore(db *sql.DB, encryptionKey string) *PGConnectedAgentCredentialStore {
	return &PGConnectedAgentCredentialStore{db: db, encKey: encryptionKey}
}

func (s *PGConnectedAgentCredentialStore) Put(ctx context.Context, cred store.ConnectedAgentCredential) error {
	stored := cred.Secret
	if s.encKey != "" {
		enc, err := crypto.Encrypt(cred.Secret, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt connected-agent credential: %w", err)
		}
		stored = enc
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO connected_agent_credentials
		   (agent_id, connection_id, cred_type, inject, secret_enc, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (agent_id, connection_id)
		 DO UPDATE SET cred_type = $3, inject = $4, secret_enc = $5, updated_at = $6`,
		cred.AgentID, cred.ConnectionID, cred.Type, cred.Inject, stored, time.Now(),
	)
	return err
}

func (s *PGConnectedAgentCredentialStore) Get(ctx context.Context, agentID uuid.UUID, connectionID string) (*store.ConnectedAgentCredential, error) {
	var (
		credType, inject, secret string
		updatedAt                time.Time
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT cred_type, inject, secret_enc, updated_at
		   FROM connected_agent_credentials WHERE agent_id = $1 AND connection_id = $2`,
		agentID, connectionID).Scan(&credType, &inject, &secret, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if secret != "" && s.encKey != "" {
		dec, err := crypto.Decrypt(secret, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt connected-agent credential: %w", err)
		}
		secret = dec
	}
	return &store.ConnectedAgentCredential{
		AgentID:      agentID,
		ConnectionID: connectionID,
		Type:         credType,
		Inject:       inject,
		Secret:       secret,
		UpdatedAt:    updatedAt,
	}, nil
}

func (s *PGConnectedAgentCredentialStore) Delete(ctx context.Context, agentID uuid.UUID, connectionID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM connected_agent_credentials WHERE agent_id = $1 AND connection_id = $2`,
		agentID, connectionID)
	return err
}

func (s *PGConnectedAgentCredentialStore) DeleteForAgent(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM connected_agent_credentials WHERE agent_id = $1`, agentID)
	return err
}
