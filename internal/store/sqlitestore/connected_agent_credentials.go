package sqlitestore

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteConnectedAgentCredentialStore is a no-op stub. Per-connection connected-
// agent credentials (BYOK) are a cloud/PG-only feature for now; the SQLite store
// also does not round-trip agents.connected_agents (a separate pre-existing
// gap), so desktop/Lite has no connection to attach a credential to. Get returns
// (nil, nil) so callers fall back to the platform credential.
type SQLiteConnectedAgentCredentialStore struct{}

func NewSQLiteConnectedAgentCredentialStore() *SQLiteConnectedAgentCredentialStore {
	return &SQLiteConnectedAgentCredentialStore{}
}

func (s *SQLiteConnectedAgentCredentialStore) Put(ctx context.Context, cred store.ConnectedAgentCredential) error {
	slog.Warn("connected-agent credentials are not supported on the SQLite/desktop edition; ignoring Put",
		"agent_id", cred.AgentID, "connection_id", cred.ConnectionID)
	return nil
}

func (s *SQLiteConnectedAgentCredentialStore) Get(ctx context.Context, agentID uuid.UUID, connectionID string) (*store.ConnectedAgentCredential, error) {
	return nil, nil
}

func (s *SQLiteConnectedAgentCredentialStore) Delete(ctx context.Context, agentID uuid.UUID, connectionID string) error {
	return nil
}

func (s *SQLiteConnectedAgentCredentialStore) DeleteForAgent(ctx context.Context, agentID uuid.UUID) error {
	return nil
}
