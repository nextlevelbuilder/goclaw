package bitrix24

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// mcpCredsRefreshWindow is the lead time at which we proactively refresh user
// credentials. If the cached BITRIX_EXPIRES_AT is within this window of now,
// the next webhook event triggers an auto-onboard refresh — preventing the
// upcoming tool call from racing a stale token.
//
// Sized 5x typical Bitrix REST round-trip latency. The Bitrix-issued
// access_token TTL is 1h (3600s), so 5 min = 8% of lifetime — refresh load
// stays manageable on busy portals.
const mcpCredsRefreshWindow = 5 * time.Minute

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
	// (nil MCPServerStore, empty mcp_server_name/mcp_base_url, server row
	// not found). Not an error — the channel simply operates without MCP
	// credentials for its users. Agent loop already handles "no creds →
	// skip this server" gracefully.
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
//  3. The mcp_servers row exists (looked up by name).
//
// Bitrix24 OAuth → existing mcp_user_credentials bridge (Bitrix-specific
// glue — see mcp_client.go doc): the MCP server authenticates each
// /api/auto-onboard call via the caller-supplied Bitrix access_token by
// calling Bitrix `profile` and matching the token-owner ID against
// bitrix_user_id — no shared admin secret is required, so multi-tenant
// isolation holds naturally (each portal's users authenticate with their
// own per-portal OAuth tokens).
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
	//
	// PGMCPServerStore.GetServerByName scopes the lookup by tenant_id from
	// context (multi-tenant isolation). Channel.Start receives ctx from the
	// instance loader without that scope set — wrap it explicitly with the
	// channel's own tenant id so the lookup matches the row a tenant admin
	// created via `bitrix-portal create` / dashboard.
	lookupCtx := ctx
	if tid := c.TenantID(); tid != uuid.Nil {
		lookupCtx = store.WithTenantID(ctx, tid)
	}
	server, err := c.mcpStore.GetServerByName(lookupCtx, c.cfg.MCPServerName)
	if err != nil || server == nil {
		slog.Warn("bitrix24 mcp: provisioning disabled — server not found",
			"channel", c.Name(), "mcp_server_name", c.cfg.MCPServerName, "err", err)
		return nil
	}

	c.mcpServerID = server.ID
	c.mcpClient = newMCPClient(c.cfg.MCPBaseURL, 10*time.Second)
	c.mcpDebounce = make(map[mcpDebounceKey]time.Time)

	slog.Info("bitrix24 mcp: provisioning enabled",
		"channel", c.Name(),
		"mcp_server", c.cfg.MCPServerName,
		"mcp_server_id", server.ID)
	return nil
}

// provisionIfMissing mints per-user MCP credentials on first sight of a user
// IF all prerequisites hold (provisioning enabled + bot is internal + no
// existing creds + not debounced). Best-effort: every failure mode returns
// a typed error but NEVER blocks the caller — handleMessage proceeds to
// HandleMessage regardless, so user messages always get processed.
//
// Called from handleMessage after EnsureContact, before HandleMessage.
func (c *Channel) provisionIfMissing(ctx context.Context, userID string, fromConnector bool, auth EventAuth) error {
	// Skip #1: Open Channel message from an external connector customer.
	// Transient customers reach the bot via a connector (Zalo/FB/etc.) and
	// report IS_CONNECTOR=Y — they are not Bitrix users and have no per-user
	// OAuth, so skip them. Internal staff who join an Open Channel session
	// report IS_CONNECTOR=N and DO have OAuth, so they must be provisioned.
	if c.IsOpenChannelBot() && fromConnector {
		slog.Debug("bitrix24 mcp: provision skip open channel connector", "channel", c.Name(), "user_id", userID)
		return ErrProvisionSkippedOpenChannel
	}

	// Skip #2: provisioning disabled at startup. Channel operates without
	// MCP — downstream agent loop sees no creds and skips MCP tools.
	if c.mcpStore == nil || c.mcpClient == nil || c.mcpServerID == uuid.Nil {
		slog.Debug("bitrix24 mcp: provision skip disabled", "channel", c.Name(), "user_id", userID)
		return ErrProvisionDisabled
	}

	// Skip #3: already have creds AND token is far from expiry. Provisioner
	// is primarily a LAZY-MINT path, but it also refreshes opportunistically:
	//
	//   - Token expired → must refresh (loop-side 401 purge would otherwise
	//     leave the user stranded until next event)
	//   - Token expiring within mcpCredsRefreshWindow → refresh proactively
	//     so the upcoming tool call doesn't hit a freshly stale token.
	//   - Token warm (> refresh window remaining) → skip; reuse cached creds
	//     to avoid hammering mcp-bx-syn.
	//
	// The refresh window must be > the longest expected tool-call latency so
	// proactive refresh lands before the call. 5 min is conservative given
	// typical Bitrix REST round-trips (sub-second to a few seconds).
	existing, err := c.mcpStore.GetUserCredentials(ctx, c.mcpServerID, userID)
	if err == nil && existing != nil && existing.APIKey != "" {
		expiresAtRaw := strings.TrimSpace(existing.Env["BITRIX_EXPIRES_AT"])
		if expiresAtRaw == "" {
			// Legacy creds without expiry metadata are STALE-unknown. mcp-bx-syn
			// will reject when its stored access_token expires (1h TTL) → loop-side
			// 401 purge fires, breaking the in-flight conversation. Refresh once
			// to write BITRIX_EXPIRES_AT so subsequent events follow the warm-skip
			// path. The "1 HTTP per first-event-after-deploy" cost self-heals
			// after one refresh writes the meta column.
			slog.Info("bitrix24 mcp: refreshing legacy credentials (no expiry meta)",
				"channel", c.Name(), "user_id", userID)
			// fall through to debounce + refresh below
		} else {
			if expiresAt, parseErr := time.Parse(time.RFC3339, expiresAtRaw); parseErr == nil {
				now := time.Now().UTC()
				timeLeft := expiresAt.Sub(now)
				if timeLeft > mcpCredsRefreshWindow {
					slog.Debug("bitrix24 mcp: provision skip warm credentials",
						"channel", c.Name(), "user_id", userID, "expires_at", expiresAtRaw,
						"time_left", timeLeft.String())
					return nil
				}
				if timeLeft > 0 {
					slog.Info("bitrix24 mcp: refreshing near-expiry user credentials",
						"channel", c.Name(),
						"user_id", userID,
						"expires_at", expiresAtRaw,
						"time_left", timeLeft.String())
				} else {
					slog.Info("bitrix24 mcp: refreshing expired user credentials",
						"channel", c.Name(),
						"user_id", userID,
						"expired_at", expiresAtRaw)
				}
			}
		}
	}

	// Skip #4: debounce. Bitrix24 retries webhooks aggressively on 5xx,
	// so a failed auto-onboard can trigger 3–5 attempts per second
	// without this guard. TTL = 60s covers the retry burst window and
	// the typical "MCP server blip" recovery time.
	// Atomic check-and-set: an aggressive Bitrix retry burst can deliver
	// several events for the same user near-simultaneously. A separate
	// check-then-mark leaves a TOCTOU gap where two of them both pass the gate
	// and both run a self-refresh, double-rotating the refresh_token (the
	// second then fails on the now-consumed token). Acquire under one lock.
	if !c.tryAcquireMCPProvision(c.mcpServerID, userID) {
		slog.Warn("bitrix24 mcp: provision debounced", "channel", c.Name(), "user_id", userID)
		return ErrProvisionDebounced
	}

	// OAuth tokens are plumbed through the webhook event's auth block —
	// MCP server uses them to call Bitrix REST on behalf of this user.
	// Missing tokens will be caught by mcpClient.autoOnboard validation,
	// but surface them here with a clearer error so operators don't have
	// to trace to mcp_client.go.
	if auth.Domain == "" || auth.AccessToken == "" || auth.RefreshToken == "" {
		// Direct/group chatbot events don't carry OAuth tokens in the top-level
		// auth[] block — Bitrix only ships them on Open Channel events. If this
		// user already has stored credentials, refresh the USER-context token
		// from the stored refresh_token instead of failing, so per-user MCP
		// stays alive without depending on the event carrying tokens.
		if err := c.selfRefreshUserCreds(ctx, userID, existing); err != nil {
			return err
		}
		return nil
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
		slog.Warn("bitrix24 mcp: auto-onboard failed", "channel", c.Name(), "user_id", userID, "err", err)
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

// selfRefreshUserCreds renews a user's per-user Bitrix MCP credentials WITHOUT
// relying on an inbound event carrying OAuth tokens. It refreshes the stored
// refresh_token via OAuth (grant_type=refresh_token, using the portal's
// client_id/secret), re-onboards the MCP server with the rotated tokens, and
// persists them. Bitrix rotates the refresh_token on every call, so persisting
// the new pair is mandatory — otherwise the next refresh would use a dead token.
//
// Returns an error (surfaced to the caller as a degradation notice) when there
// are no stored credentials to refresh, the stored refresh_token is dead
// (invalid_grant → user must re-authorize), or onboarding/persisting fails.
func (c *Channel) selfRefreshUserCreds(ctx context.Context, userID string, existing *store.MCPUserCredentials) error {
	if existing == nil || existing.Env == nil {
		return fmt.Errorf("bitrix24 mcp: incomplete auth block and no stored credentials to refresh")
	}
	refreshToken := strings.TrimSpace(existing.Env["BITRIX_REFRESH_TOKEN"])
	domain := strings.TrimSpace(existing.Env["BITRIX_DOMAIN"])
	if refreshToken == "" || domain == "" {
		return fmt.Errorf("bitrix24 mcp: incomplete auth block and stored credentials missing domain/refresh_token")
	}
	if c.portal == nil {
		return fmt.Errorf("bitrix24 mcp: portal unavailable for self-refresh")
	}

	tr, err := c.portal.RefreshUserToken(ctx, refreshToken)
	if err != nil {
		slog.Warn("bitrix24 mcp: self-refresh oauth failed", "channel", c.Name(), "user_id", userID, "err", err)
		return fmt.Errorf("bitrix24 mcp: self-refresh oauth failed: %w", err)
	}

	resp, err := c.mcpClient.autoOnboard(ctx, autoOnboardRequest{
		Domain:       domain,
		BitrixUserID: userID,
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresIn:    int(tr.ExpiresIn),
	})
	if err != nil {
		slog.Warn("bitrix24 mcp: self-refresh re-onboard failed", "channel", c.Name(), "user_id", userID, "err", err)
		return fmt.Errorf("bitrix24 mcp: self-refresh re-onboard failed: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	creds := store.MCPUserCredentials{
		APIKey: resp.APIKey,
		Env: map[string]string{
			"BITRIX_DOMAIN":        domain,
			"BITRIX_ACCESS_TOKEN":  tr.AccessToken,
			"BITRIX_REFRESH_TOKEN": tr.RefreshToken, // rotated — persist the new one
			"BITRIX_EXPIRES_AT":    expiresAt,
		},
	}
	// Decouple persist from the caller's context: once Bitrix rotated the
	// refresh_token, a canceled request ctx must NOT drop the new token to the
	// store — the old token is already dead and would strand the user until
	// re-authorization. Mirrors Portal.refreshLocked's WithoutCancel rationale.
	if err := c.mcpStore.SetUserCredentials(context.WithoutCancel(ctx), c.mcpServerID, userID, creds); err != nil {
		return fmt.Errorf("bitrix24 mcp: persist self-refreshed credentials: %w", err)
	}

	slog.Info("bitrix24 mcp: self-refreshed user credentials",
		"channel", c.Name(), "user_id", userID, "mcp_server_id", c.mcpServerID, "created", resp.Created)
	return nil
}

// tryAcquireMCPProvision atomically checks the debounce window for
// (serverID, userID) and, if clear, records a fresh attempt — all under a
// single lock so concurrent webhook-retry events can't both pass the gate
// (which would double-rotate the refresh_token in selfRefreshUserCreds).
// Returns true when the caller acquired the slot (NOT debounced), false
// otherwise. Prunes the entry on the way through to keep the map lean.
func (c *Channel) tryAcquireMCPProvision(serverID uuid.UUID, userID string) bool {
	c.mcpProvMu.Lock()
	defer c.mcpProvMu.Unlock()
	if c.mcpDebounce == nil {
		// Defensive: initMCPProvisioner allocates this, but if some code path
		// bypassed init (e.g. a test that constructs Channel directly), a nil
		// map write would panic. Allocate on demand instead.
		c.mcpDebounce = make(map[mcpDebounceKey]time.Time)
	}
	key := mcpDebounceKey{serverID: serverID, userID: userID}
	if ts, ok := c.mcpDebounce[key]; ok && time.Since(ts) < mcpProvisionDebounceTTL {
		return false // still within the debounce window
	}
	c.mcpDebounce[key] = time.Now()
	return true
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
	// Provisioner notice is a one-off staff alert sent into the user's
	// chat with the bot — always public (default v2 path), no replyId
	// because there's no inbound message to link back to.
	if err := c.sendChunk(ctx, chatID, mcpUserNotifyMessage, sendOptions{visibility: VisibilityPublic}); err != nil {
		slog.Debug("bitrix24 mcp: failed to send user degradation notice",
			"channel", c.Name(), "user", userID, "chat_id", chatID, "err", err)
	}
}

// compile-time assertion: sync.Mutex is always zero-initializable; this
// nudge just documents that mcpProvMu doesn't need an explicit constructor.
var _ sync.Mutex = sync.Mutex{}
