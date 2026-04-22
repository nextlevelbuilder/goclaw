package bitrix24

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mcpProvisionDebounceTTL is how long we suppress repeat auto-onboard calls
// for the same (serverID, userID) pair after a successful OR failed attempt.
// 60s is long enough to swallow Bitrix24 webhook retries (which can spam 3–5
// events in a burst on transient 5xx) but short enough that a recovered MCP
// server is usable again within one minute.
const mcpProvisionDebounceTTL = 60 * time.Second

// mcpAdminTokenEnv is the env var read at Channel.Start() to unlock
// provisioning. Lives in env rather than config because channel_instances.config
// is plaintext JSONB — partner's Phase D RFC will propose a per-tenant secret
// store; until then this keeps the secret out of the DB.
const mcpAdminTokenEnv = "GOCLAW_BITRIX_MCP_ADMIN_TOKEN"

// mcpDebounceKey keys the in-memory rate-limit map. ServerID + UserID is
// sufficient — different Bitrix portals route to different channel instances
// with different debounce maps, so cross-portal collision isn't possible.
type mcpDebounceKey struct {
	serverID uuid.UUID
	userID   string
}

// Sentinel errors. Callers log-and-continue on any of these — none are fatal
// to message processing. Kept as package-level vars (not fmt.Errorf literals)
// so tests can errors.Is() against them without string matching.
var (
	// ErrProvisionSkippedOpenChannel means the channel is a Bitrix24 Open
	// Channel bot (TYPE "O"). Auto-onboard is disabled because transient
	// customers don't have tenant_users rows — shared-credential support
	// is deferred to Phase E.
	ErrProvisionSkippedOpenChannel = errors.New("bitrix24 mcp: provisioning skipped for Open Channel bot")

	// ErrProvisionDisabled means the channel was built without MCP wiring
	// (nil MCPServerStore, empty mcp_server_name/mcp_base_url, missing
	// admin token env). Not an error — the channel simply operates
	// without MCP credentials for its users. Agent loop already handles
	// "no creds → skip this server" gracefully.
	ErrProvisionDisabled = errors.New("bitrix24 mcp: provisioning disabled")

	// ErrProvisionDebounced means an auto-onboard for this (server, user)
	// pair ran within the last mcpProvisionDebounceTTL. Caller should NOT
	// retry; the previous attempt's outcome (success or failure) is still
	// authoritative.
	ErrProvisionDebounced = errors.New("bitrix24 mcp: provisioning debounced")
)

// initMCPProvisioner wires the lazy-provisioning plumbing at Start() time.
// Safe to call even when provisioning is disabled — in that case it just
// returns nil without touching mcpStore.
//
// Three things have to line up before provisioner can run:
//  1. Factory was called with a non-nil MCPServerStore.
//  2. Instance config has both mcp_server_name and mcp_base_url set.
//  3. Env GOCLAW_BITRIX_MCP_ADMIN_TOKEN is non-empty.
//
// Any single missing piece leaves the channel usable but with
// provisioning off — that's the operator's "staged rollout" path: install
// the channel first, layer MCP on later.
//
// Called under startMu (held by Channel.Start()).
func (c *Channel) initMCPProvisioner(ctx context.Context) error {
	// Fast exits for the explicitly-disabled configurations. We don't log
	// at Info level here because operators who never want MCP shouldn't
	// see recurring startup noise; Debug level surfaces it for troubleshooting.
	if c.mcpStore == nil {
		slog.Debug("bitrix24 mcp: provisioning disabled (no MCPServerStore wired at factory)",
			"channel", c.Name())
		return nil
	}
	if strings.TrimSpace(c.cfg.MCPServerName) == "" || strings.TrimSpace(c.cfg.MCPBaseURL) == "" {
		slog.Debug("bitrix24 mcp: provisioning disabled (mcp_server_name or mcp_base_url empty)",
			"channel", c.Name())
		return nil
	}

	adminToken := strings.TrimSpace(os.Getenv(mcpAdminTokenEnv))
	if adminToken == "" {
		// This case IS worth a Warn: admin set mcp_server_name + mcp_base_url
		// so they clearly intended provisioning, but forgot the env var.
		// Surfacing it in logs helps catch the misconfiguration before
		// users start hitting "no MCP tools" surprises.
		slog.Warn("bitrix24 mcp: provisioning disabled — env var missing",
			"channel", c.Name(), "env", mcpAdminTokenEnv)
		return nil
	}

	// Resolve server name → UUID once at startup. If the server name is
	// wrong or the row doesn't exist yet, log and disable provisioning —
	// don't block channel startup. Admin can create the server + reload
	// the channel later.
	server, err := c.mcpStore.GetServerByName(ctx, c.cfg.MCPServerName)
	if err != nil || server == nil {
		slog.Warn("bitrix24 mcp: provisioning disabled — server not found",
			"channel", c.Name(), "mcp_server_name", c.cfg.MCPServerName, "err", err)
		return nil
	}

	c.mcpServerID = server.ID
	c.mcpClient = newMCPClient(c.cfg.MCPBaseURL, adminToken, 10*time.Second)
	c.mcpDebounce = make(map[mcpDebounceKey]time.Time)

	slog.Info("bitrix24 mcp: provisioning enabled",
		"channel", c.Name(), "mcp_server", c.cfg.MCPServerName, "mcp_server_id", server.ID)
	return nil
}

// provisionIfMissing mints per-user MCP credentials on first sight of a user
// IF all prerequisites hold (provisioning enabled + bot is internal + no
// existing creds + not debounced). Best-effort: every failure mode returns
// a typed error but NEVER blocks the caller — handleMessage proceeds to
// HandleMessage regardless, so user messages always get processed.
//
// Called from handleMessage after EnsureContact, before HandleMessage.
func (c *Channel) provisionIfMissing(ctx context.Context, userID string, auth EventAuth) error {
	// Skip #1: Open Channel bot. No per-user credentials for transient
	// customers — see type docstring.
	if c.IsOpenChannelBot() {
		return ErrProvisionSkippedOpenChannel
	}

	// Skip #2: provisioning disabled at startup. Channel operates without
	// MCP — downstream agent loop sees no creds and skips MCP tools.
	if c.mcpStore == nil || c.mcpClient == nil || c.mcpServerID == uuid.Nil {
		return ErrProvisionDisabled
	}

	// Skip #3: already have creds. Provisioner is a LAZY-MINT path, not
	// a refresh path — credential refresh/rotation is a separate problem
	// (Phase E). Cheap check before the debounce so warm users never
	// touch the mutex.
	existing, err := c.mcpStore.GetUserCredentials(ctx, c.mcpServerID, userID)
	if err == nil && existing != nil && existing.APIKey != "" {
		return nil
	}

	// Skip #4: debounce. Bitrix24 retries webhooks aggressively on 5xx,
	// so a failed auto-onboard can trigger 3–5 attempts per second
	// without this guard. TTL = 60s covers the retry burst window and
	// the typical "MCP server blip" recovery time.
	if c.isMCPProvisionDebounced(c.mcpServerID, userID) {
		return ErrProvisionDebounced
	}
	c.markMCPProvisionDebounced(c.mcpServerID, userID)

	// OAuth tokens are plumbed through the webhook event's auth block —
	// MCP server uses them to call Bitrix REST on behalf of this user.
	// Missing tokens will be caught by mcpClient.autoOnboard validation,
	// but surface them here with a clearer error so operators don't have
	// to trace to mcp_client.go.
	if auth.Domain == "" || auth.AccessToken == "" || auth.RefreshToken == "" {
		return fmt.Errorf("bitrix24 mcp: incomplete auth block (domain/access_token/refresh_token required)")
	}

	resp, err := c.mcpClient.autoOnboard(ctx, autoOnboardRequest{
		Domain:       auth.Domain,
		BitrixUserID: userID,
		AccessToken:  auth.AccessToken,
		RefreshToken: auth.RefreshToken,
		ExpiresIn:    auth.ExpiresIn,
		// DisplayName left empty — Bitrix webhook doesn't carry it; MCP
		// server should enrich via user.get if it needs a label.
	})
	if err != nil {
		return fmt.Errorf("bitrix24 mcp: auto-onboard failed: %w", err)
	}

	// Persist OAuth tokens alongside the minted API key so MCP server can
	// re-authenticate on subsequent tool calls without a fresh onboard.
	// Env map keys are plain strings (partner's MCPServerStore encrypts
	// them transparently via encKey on write).
	expiresAt := time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	creds := store.MCPUserCredentials{
		APIKey: resp.APIKey,
		Env: map[string]string{
			"BITRIX_DOMAIN":        auth.Domain,
			"BITRIX_ACCESS_TOKEN":  auth.AccessToken,
			"BITRIX_REFRESH_TOKEN": auth.RefreshToken,
			"BITRIX_EXPIRES_AT":    expiresAt,
		},
	}
	if err := c.mcpStore.SetUserCredentials(ctx, c.mcpServerID, userID, creds); err != nil {
		return fmt.Errorf("bitrix24 mcp: persist credentials: %w", err)
	}

	slog.Info("bitrix24 mcp: provisioned user credentials",
		"channel", c.Name(), "user_id", userID, "mcp_server_id", c.mcpServerID,
		"created", resp.Created)
	return nil
}

// isMCPProvisionDebounced reports whether a provisioning attempt for
// (serverID, userID) ran within the last mcpProvisionDebounceTTL. Also
// opportunistically prunes expired entries so the map doesn't grow
// unbounded across long-lived channels.
func (c *Channel) isMCPProvisionDebounced(serverID uuid.UUID, userID string) bool {
	c.mcpProvMu.Lock()
	defer c.mcpProvMu.Unlock()
	key := mcpDebounceKey{serverID: serverID, userID: userID}
	if ts, ok := c.mcpDebounce[key]; ok {
		if time.Since(ts) < mcpProvisionDebounceTTL {
			return true
		}
		// Expired — delete so the map stays lean. Cheap to do here since
		// we're already holding the lock for the check.
		delete(c.mcpDebounce, key)
	}
	return false
}

func (c *Channel) markMCPProvisionDebounced(serverID uuid.UUID, userID string) {
	c.mcpProvMu.Lock()
	defer c.mcpProvMu.Unlock()
	if c.mcpDebounce == nil {
		// Defensive: initMCPProvisioner allocates this, but if some code
		// path bypassed init (e.g. test that constructs Channel directly
		// and then calls provisionIfMissing with provisioning enabled),
		// a nil map write would panic. Allocate on demand instead.
		c.mcpDebounce = make(map[mcpDebounceKey]time.Time)
	}
	c.mcpDebounce[mcpDebounceKey{serverID: serverID, userID: userID}] = time.Now()
}

// compile-time assertion: sync.Mutex is always zero-initializable; this
// nudge just documents that mcpProvMu doesn't need an explicit constructor.
var _ sync.Mutex = sync.Mutex{}
