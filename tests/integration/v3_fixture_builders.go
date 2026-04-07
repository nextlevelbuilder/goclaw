//go:build integration

package integration

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// seedTwoTenants creates 2 independent tenants with agents for isolation testing.
func seedTwoTenants(t *testing.T, db *sql.DB) (tenantA, tenantB, agentA, agentB uuid.UUID) {
	t.Helper()
	tenantA, agentA = seedTenantAgent(t, db)
	tenantB, agentB = seedTenantAgent(t, db)
	return
}

// seedTeam creates an active team with v2 settings (required for recovery queries).
// The ownerAgentID is set as lead. A second agent is created and added as a member.
// Returns teamID and the second agent's ID.
func seedTeam(t *testing.T, db *sql.DB, tenantID, ownerAgentID uuid.UUID) (teamID, memberAgentID uuid.UUID) {
	t.Helper()

	teamID = uuid.New()
	memberAgentID = uuid.New()
	memberKey := "member-" + memberAgentID.String()[:8]

	// Create second agent as team member.
	_, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')
		 ON CONFLICT DO NOTHING`,
		memberAgentID, tenantID, memberKey)
	if err != nil {
		t.Fatalf("seed member agent: %v", err)
	}

	// Create team with v2 settings — CRITICAL for recovery/lifecycle queries.
	_, err = db.Exec(
		`INSERT INTO agent_teams (id, tenant_id, name, lead_agent_id, status, settings, created_by)
		 VALUES ($1, $2, $3, $4, 'active', '{"version": 2}', 'test')`,
		teamID, tenantID, "test-team-"+teamID.String()[:8], ownerAgentID)
	if err != nil {
		t.Fatalf("seed team: %v", err)
	}

	// Add both agents as members.
	for _, m := range []struct {
		agentID uuid.UUID
		role    string
	}{
		{ownerAgentID, "lead"},
		{memberAgentID, "member"},
	} {
		_, err = db.Exec(
			`INSERT INTO agent_team_members (team_id, agent_id, tenant_id, role)
			 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
			teamID, m.agentID, tenantID, m.role)
		if err != nil {
			t.Fatalf("seed team member: %v", err)
		}
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_team_members WHERE team_id = $1", teamID)
		db.Exec("DELETE FROM agent_teams WHERE id = $1", teamID)
		db.Exec("DELETE FROM agents WHERE id = $1", memberAgentID)
	})

	return teamID, memberAgentID
}

// seedSession creates a minimal session record.
func seedSession(t *testing.T, db *sql.DB, tenantID, agentID uuid.UUID) string {
	t.Helper()

	sessionKey := "sess-" + uuid.New().String()[:8]
	_, err := db.Exec(
		`INSERT INTO sessions (session_key, tenant_id, agent_id, user_id, messages, summary)
		 VALUES ($1, $2, $3, 'test-user', '[]', '')`,
		sessionKey, tenantID, agentID)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", sessionKey, tenantID)
	})

	return sessionKey
}

// seedMCPServer creates a minimal MCP server record.
func seedMCPServer(t *testing.T, db *sql.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()

	serverID := uuid.New()
	name := "test-mcp-" + serverID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO mcp_servers (id, tenant_id, name, display_name, transport, enabled, created_by)
		 VALUES ($1, $2, $3, $3, 'stdio', true, 'test-user')`,
		serverID, tenantID, name)
	if err != nil {
		t.Fatalf("seed mcp server: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM mcp_user_credentials WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_access_requests WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_user_grants WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_agent_grants WHERE server_id = $1", serverID)
		db.Exec("DELETE FROM mcp_servers WHERE id = $1", serverID)
	})

	return serverID
}

// seedSecureCLI creates a minimal secure CLI binary record.
// Uses testEncryptionKey for the encrypted_env field.
func seedSecureCLI(t *testing.T, db *sql.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()

	binaryID := uuid.New()
	name := "test-cli-" + binaryID.String()[:8]
	// encrypted_env is BYTEA NOT NULL — use a dummy encrypted value.
	dummyEnv := []byte(`{"TEST_KEY": "test_value"}`)

	_, err := db.Exec(
		`INSERT INTO secure_cli_binaries (id, tenant_id, binary_name, encrypted_env, description, enabled)
		 VALUES ($1, $2, $3, $4, 'test CLI', true)`,
		binaryID, tenantID, name, dummyEnv)
	if err != nil {
		t.Fatalf("seed secure cli: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM secure_cli_user_credentials WHERE binary_id = $1", binaryID)
		db.Exec("DELETE FROM secure_cli_agent_grants WHERE binary_id = $1", binaryID)
		db.Exec("DELETE FROM secure_cli_binaries WHERE id = $1", binaryID)
	})

	return binaryID
}

// seedAPIKey creates a minimal API key record. Returns key ID and the raw key hash.
func seedAPIKey(t *testing.T, db *sql.DB, tenantID uuid.UUID) (uuid.UUID, string) {
	t.Helper()

	keyID := uuid.New()
	rawKey := "gclw_test_" + keyID.String()[:16]
	hash := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(hash[:])

	_, err := db.Exec(
		`INSERT INTO api_keys (id, tenant_id, name, prefix, key_hash, scopes, created_by)
		 VALUES ($1, $2, 'test-key', $3, $4, '{}', 'test-user')`,
		keyID, tenantID, rawKey[:8], keyHash)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM api_keys WHERE id = $1", keyID)
	})

	return keyID, keyHash
}

// seedContact creates a minimal channel contact record.
func seedContact(t *testing.T, db *sql.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()

	contactID := uuid.New()
	senderID := fmt.Sprintf("sender-%s", contactID.String()[:8])
	_, err := db.Exec(
		`INSERT INTO channel_contacts (id, tenant_id, channel_type, sender_id, peer_kind)
		 VALUES ($1, $2, 'telegram', $3, 'private')`,
		contactID, tenantID, senderID)
	if err != nil {
		t.Fatalf("seed contact: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM channel_contacts WHERE id = $1", contactID)
	})

	return contactID
}
