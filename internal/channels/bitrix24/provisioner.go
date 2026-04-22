package bitrix24

import (
	"context"
	"encoding/json"
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

// mcpAdminTokenEnvKey is the key looked up inside mcp_servers.env for the
// admin token that authorizes POST /api/auto-onboard. This is the
// *primary* source — per-server, per-tenant, AES-encrypted at rest by
// partner's MCPServerStore. Multiple tenants each register their own MCP
// server row with their own admin token, so tenant isolation holds.
//
// Key is the SAME string the MCP server process receives as an environment
// variable at spawn time (partner's bridge passes srv.Env through to the
// server), so admin only sets the secret in one place.
const mcpAdminTokenEnvKey = "BITRIX_MCP_ADMIN_TOKEN"

// mcpAdminTokenEnv is a legacy *process-level* env var fallback. Predates
// the per-server token (pre-1.2 deployments set this directly on the
// goclaw process). Kept as a fallback so single-tenant self-hosts that
// already deployed Phase C don't have to migrate the token to DB before
// upgrading — they can do it whenever.
//
// DEPRECATED for multi-tenant: this is a single global value, so it can't
// distinguish between tenant A's MCP server and tenant B's. New
// deployments should set BITRIX_MCP_ADMIN_TOKEN inside the target
// mcp_servers row's env JSONB instead.
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
// Four things have to line up before provisioner can run:
//  1. Factory was called with a non-nil MCPServerStore.
//  2. Instance config has both mcp_server_name and mcp_base_url set.
//  3. The mcp_servers row exists (looked up by name).
//  4. An admin token is available — EITHER in that row's env JSONB under
//     key BITRIX_MCP_ADMIN_TOKEN (per-tenant, encrypted; preferred) OR in
//     the process-level env var GOCLAW_BITRIX_MCP_ADMIN_TOKEN (legacy
//     single-tenant fallback).
//
// The per-server token is looked up FIRST because it's the only path that
// preserves multi-tenant isolation — the process env var can't distinguish
// between tenants sharing the same goclaw deployment. Legacy env var
// remains as a migration ramp for existing single-tenant self-hosts.
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

	// Resolve admin token: per-server env first (multi-tenant safe,
	// encrypted in DB by MCPServerStore), legacy process env var as
	// fallback (single-tenant deployments predating this refactor).
	adminToken, tokenSource := resolveMCPAdminToken(server)
	if adminToken == "" {
		// This case IS worth a Warn: admin set mcp_server_name + mcp_base_url
		// so they clearly intended provisioning, but no token was found in
		// either source. Surfacing it in logs helps catch the misconfiguration
		// before users start hitting "no MCP tools" surprises.
		slog.Warn("bitrix24 mcp: provisioning disabled — admin token missing",
			"channel", c.Name(),
			"primary_source", "mcp_servers.env["+mcpAdminTokenEnvKey+"]",
			"fallback_source", "env "+mcpAdminTokenEnv)
		return nil
	}

	c.mcpServerID = server.ID
	c.mcpClient = newMCPClient(c.cfg.MCPBaseURL, adminToken, 10*time.Second)
	c.mcpDebounce = make(map[mcpDebounceKey]time.Time)

	// token_source tells operators whether the deployment is already on the
	// multi-tenant-safe per-server token (tokenSource="mcp_servers.env") or
	// still riding the legacy global env var (tokenSource="env"). The latter
	// is a migration signal, not an error.
	slog.Info("bitrix24 mcp: provisioning enabled",
		"channel", c.Name(),
		"mcp_server", c.cfg.MCPServerName,
		"mcp_server_id", server.ID,
		"token_source", tokenSource)
	return nil
}

// resolveMCPAdminToken returns the admin token for this MCP server using
// the two-source lookup: server.Env first, then process env var fallback.
// Returns (token, sourceLabel) where sourceLabel is one of:
//   - "mcp_servers.env"   — primary, per-tenant, encrypted in DB
//   - "env"               — legacy process-level global
//   - "" (empty token)    — neither source had a value
//
// Kept separate from initMCPProvisioner so the fallback logic is unit-
// testable without spinning up a full Channel, and so the resolution
// policy is one place to change if/when the env var is fully retired.
func resolveMCPAdminToken(server *store.MCPServerData) (token, source string) {
	// Primary: per-server env JSONB. Parsed lazily — most deployments will
	// have srv.Env empty or non-JSON-object, which we treat as "not set"
	// silently (no log spam). Partner decrypts srv.Env before we see it,
	// so we parse plaintext JSON here.
	if len(server.Env) > 0 {
		var envMap map[string]string
		if err := json.Unmarshal(server.Env, &envMap); err == nil {
			if tok := strings.TrimSpace(envMap[mcpAdminTokenEnvKey]); tok != "" {
				return tok, "mcp_servers.env"
			}
		}
	}
	// Fallback: process env var (legacy single-tenant deployments).
	if tok := strings.TrimSpace(os.Getenv(mcpAdminTokenEnv)); tok != "" {
		return tok, "env"
	}
	return "", ""
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

// mcpUserNotifyDebounceTTL controls how often a single user can receive
// the "MCP is having issues" degradation notice. 5 minutes is a
// deliberate compromise:
//   - long enough that a webhook retry burst (Bitrix fires 3–5 events in
//     seconds on transient 5xx) doesn't spam the user with duplicates;
//   - short enough that if the user sends a brand-new message 10 minutes
//     later and MCP is still broken, they get re-informed rather than
//     silently wondering why tools don't work.
//
// Not configurable intentionally — giving operators a knob here would
// invite "set it to 0 to test" → real user spam. If a specific deployment
// needs a different cadence, propose the change in a PR with rationale.
const mcpUserNotifyDebounceTTL = 5 * time.Minute

// mcpUserNotifyMessage is the user-facing text sent when provisionIfMissing
// hits an unexpected failure. Keep it short and non-alarming — users don't
// benefit from HTTP status codes or "MCP" jargon. The "contact admin" hint
// is concrete enough that an operator seeing the companion slog.Warn can
// match up user report ↔ log entry.
//
// Vietnamese-first because the current deployment is a Vietnamese team;
// i18n through internal/i18n.T(locale, ...) can replace this literal once
// the channel threads a locale through (right now it does not — HandleMessage
// accepts locale on inbound but the channel itself has no way to reply in
// the user's preferred language yet).
const mcpUserNotifyMessage = "⚠️ Hệ thống đang gặp vấn đề với MCP tools nội bộ. " +
	"Một số chức năng có thể không hoạt động như mong đợi. " +
	"Vui lòng liên hệ admin kỹ thuật để xem lại. " +
	"Tôi vẫn có thể trả lời các câu hỏi cơ bản khác."

// notifyUserOfMCPIssueOnce sends a one-shot degradation notice to the
// Bitrix24 user via imbot.message.add when provisioning fails in an
// unexpected way. Debounced per-user with mcpUserNotifyDebounceTTL so
// webhook retry storms or sustained MCP outages don't flood the DM.
//
// Goals (explicit):
//   - User knows "something is wrong, contact admin" rather than silently
//     getting degraded tool-less replies.
//   - Channel health stays Green (per operator preference for silent
//     degradation). This function writes to the user, NOT to health.
//   - Channel logs the detail via slog.Warn at the call site; THIS
//     function only writes to the debug log on Send failure (which is
//     itself best-effort — notification isn't message delivery).
//
// Non-goals:
//   - Retry on Send failure. If the Bitrix24 portal is unreachable, the
//     whole channel is broken; a notification retry loop is pointless.
//   - Differentiate failure kinds in the user message. User doesn't care
//     whether it's HTTP 500 from MCP vs a missing refresh token — they
//     just need to know "talk to admin".
func (c *Channel) notifyUserOfMCPIssueOnce(ctx context.Context, userID, chatID string) {
	// A missing chatID means we lost the reply target — skipping is the
	// right call (no one to notify). Empty userID shouldn't happen at
	// this call site (handle.go gates on evt.Params.FromUserID) but cheap
	// to defend against.
	if strings.TrimSpace(chatID) == "" || strings.TrimSpace(userID) == "" {
		return
	}

	c.notifyMu.Lock()
	if c.notifyDebounce == nil {
		c.notifyDebounce = make(map[string]time.Time)
	}
	if ts, ok := c.notifyDebounce[userID]; ok && time.Since(ts) < mcpUserNotifyDebounceTTL {
		c.notifyMu.Unlock()
		return
	}
	c.notifyDebounce[userID] = time.Now()
	c.notifyMu.Unlock()

	// Reuse sendChunk directly instead of building a bus.OutboundMessage +
	// going through Send. Two reasons:
	//  1. Send() has a running-state check that'd bounce if the channel is
	//     mid-stop; degradation notice is best-effort so skipping in that
	//     state is fine, but building an OutboundMessage just to get
	//     rejected is wasteful. sendChunk re-checks Client()/BotID() for
	//     us anyway.
	//  2. The notice is plain text — no BBCode conversion, no chunking
	//     (well under the 4000-rune limit), no media. Send's pipeline is
	//     overkill.
	if err := c.sendChunk(ctx, chatID, mcpUserNotifyMessage); err != nil {
		slog.Debug("bitrix24 mcp: failed to send user degradation notice",
			"channel", c.Name(), "user", userID, "chat_id", chatID, "err", err)
	}
}

// compile-time assertion: sync.Mutex is always zero-initializable; this
// nudge just documents that mcpProvMu doesn't need an explicit constructor.
var _ sync.Mutex = sync.Mutex{}
