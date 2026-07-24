package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ConnectedAgentCredential is a per-connection secret for a connected agent —
// a user's own Anthropic API key or Claude subscription OAuth token, attached to
// one connection in agents.connected_agents. Stored encrypted at rest; NEVER
// placed inline in the connected_agents JSON (which is returned to clients
// unmasked). The connection JSON keeps only a CredentialRef pointer + non-secret
// type/status.
type ConnectedAgentCredential struct {
	AgentID      uuid.UUID
	ConnectionID string
	// Type is the credential kind: "api_key" | "oauth".
	Type string
	// Inject tells the runtime how to deliver the secret into the sandbox exec:
	//   "env:VAR"   → set env var VAR to Secret (e.g. env:CLAUDE_CODE_OAUTH_TOKEN)
	//   "file:PATH" → write Secret to PATH under the sandbox home before running
	// Carrying this with the credential lets file-based CLIs (Codex, Gemini) slot
	// in later without a schema change.
	Inject    string
	Secret    string // plaintext in memory only; encrypted at rest
	UpdatedAt time.Time
}

// ConnectedAgentCredentialStore persists encrypted per-connection credentials.
type ConnectedAgentCredentialStore interface {
	// Put upserts the credential, encrypting Secret at rest.
	Put(ctx context.Context, cred ConnectedAgentCredential) error
	// Get returns the credential for (agentID, connectionID) with Secret
	// decrypted, or (nil, nil) when none exists.
	Get(ctx context.Context, agentID uuid.UUID, connectionID string) (*ConnectedAgentCredential, error)
	// Delete removes the credential for (agentID, connectionID); no-op if absent.
	Delete(ctx context.Context, agentID uuid.UUID, connectionID string) error
	// DeleteForAgent removes every credential belonging to an agent.
	DeleteForAgent(ctx context.Context, agentID uuid.UUID) error
}
