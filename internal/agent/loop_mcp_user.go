package agent

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"time"

	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func isUnauthorized401(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unauthorized (401)")
}

func hasNonEmpty(m map[string]string, key string) bool {
	if m == nil {
		return false
	}
	return strings.TrimSpace(m[key]) != ""
}

// resolveActorUserID picks the user identifier used for per-user resource
// lookups (MCP credentials, RBAC grants, audit attribution) given the routing
// fields carried on a pipeline.RunInput / agent.RunRequest.
//
// Provisioner contract: per-user MCP credentials are keyed by the real
// external user id (= SenderID for Bitrix24, Telegram, etc.). The agent
// loop must look them up with the same key the provisioner used to store
// them, otherwise rows are missed and MCP tools silently disappear.
//
// The gateway consumer (cmd/gateway_consumer_normal.go) rewrites UserID in
// two scenarios where the original value would break per-actor lookups:
//
//  1. Group chats: UserID → "group:<channel>:<chatID>" composite (or
//     "guild:<guildID>:user:<senderID>" for Discord) so multiple users in
//     the same group share conversation memory and session state.
//  2. DM with merged contact: UserID → tenant_user UUID after sender has
//     been merged via ContactCollector.ResolveTenantUserID. Enables
//     per-user features cross-channel for the same human, but breaks
//     credential lookups keyed by external user id.
//
// Both rewrites are correct for *memory and tenant-user resolution*, but
// wrong for resources scoped per-actor:
//
//   - MCP credentials are minted per-user by channel provisioners and stored
//     with user_id = SenderID. Looking them up by the rewritten UserID always
//     misses the row.
//   - RBAC grants and audit attribution must reflect the real actor, not
//     the rewritten container — otherwise every action in a group or after
//     contact-merge looks identical to the policy engine.
//
// For Bitrix24 channel, where the provisioner always keys by SenderID
// regardless of DM/group/merge state, we MUST always prefer SenderID.
// Without the channelType discriminator, DMs with merged contacts hit the
// "return userID" branch and silently lose MCP creds.
//
// Other channels (Telegram, Slack, Discord, Zalo) currently do not
// provision per-user MCP credentials, so for them the helper retains the
// previous group-rewrite recovery semantics. When those channels later
// add per-user MCP integrations they can register their type here.
//
// Synthetic ticker / notification senders carry empty SenderID. They do
// not own per-user credentials, so the function falls back to UserID and
// the lookup returns nil safely either way.
func resolveActorUserID(userID, senderID, peerKind, channelType string) string {
	// Bitrix24: provisioner always keys MCP credentials by SenderID
	// (raw Bitrix user id). Group rewrite AND DM merged-contact rewrite
	// both override UserID — SenderID is the only stable lookup key.
	if channelType == "bitrix24" && senderID != "" {
		return senderID
	}
	// Other channels: original group-rewrite recovery only. DMs without
	// channel-specific handling retain UserID semantics (assumed to equal
	// SenderID where it matters).
	if peerKind != "group" || senderID == "" {
		return userID
	}
	return senderID
}

// getUserMCPTools returns per-user MCP tools for servers requiring user credentials.
// Tools are cached per-user in mcpUserTools sync.Map and registered in the shared
// tool registry so ExecuteWithContext can resolve them. On first call for a user,
// connections are established via pool.AcquireUser() and BridgeTools created.
func (l *Loop) getUserMCPTools(ctx context.Context, userID string) []tools.Tool {
	if len(l.mcpUserCredSrvs) == 0 || l.mcpPool == nil || l.mcpStore == nil || userID == "" {
		if userID == "" && len(l.mcpUserCredSrvs) > 0 {
			slog.Debug("mcp.user_tools_skipped", "reason", "empty_user_id", "servers", len(l.mcpUserCredSrvs))
		}
		return nil
	}

	if cached, ok := l.mcpUserTools.Load(userID); ok {
		cachedTools := cached.([]tools.Tool)
		// Check if any cached tool's connection was evicted by pool.
		// If so, clear cache and re-acquire connections.
		allConnected := true
		for _, t := range cachedTools {
			if bt, ok := t.(interface{ IsConnected() bool }); ok && !bt.IsConnected() {
				allConnected = false
				break
			}
		}
		if allConnected {
			return cachedTools
		}
		l.mcpUserTools.Delete(userID)
		slog.Debug("mcp.user_tools_stale", "user", userID, "reason", "pool_evicted")
	}

	var userTools []tools.Tool
	for _, info := range l.mcpUserCredSrvs {
		srv := info.Server

		// Check if user has credentials for this server
		uc, err := l.mcpStore.GetUserCredentials(ctx, srv.ID, userID)
		if err != nil || uc == nil || (uc.APIKey == "" && len(uc.Headers) == 0 && len(uc.Env) == 0) {
			continue
		}

		// Resolve connection params: server defaults merged with user overrides
		args := mcpbridge.ParseJSONBytesToStringSlice(srv.Args)
		env := mcpbridge.ParseJSONBytesToStringMap(srv.Env)
		if env == nil {
			env = make(map[string]string)
		}
		headers := mcpbridge.ParseJSONBytesToStringMap(srv.Headers)
		if headers == nil {
			headers = make(map[string]string)
		}

		// Inject server-level API key into headers if present
		if srv.APIKey != "" && headers["Authorization"] == "" {
			headers["Authorization"] = "Bearer " + srv.APIKey
		}

		// Merge user credentials (user overrides server defaults)
		if uc.APIKey != "" {
			headers["Authorization"] = "Bearer " + uc.APIKey
		}
		maps.Copy(headers, uc.Headers)
		maps.Copy(env, uc.Env)

		// Acquire user-keyed pool connection
		entry, err := l.mcpPool.AcquireUser(ctx, l.tenantID, srv.Name, userID,
			srv.Transport, srv.Command, args, env, srv.URL, headers, srv.TimeoutSec)
		if err != nil {
			if isUnauthorized401(err) {
				expiresAt := strings.TrimSpace(uc.Env["BITRIX_EXPIRES_AT"])
				expired := false
				if expiresAt != "" {
					if t, parseErr := time.Parse(time.RFC3339, expiresAt); parseErr == nil {
						expired = time.Now().UTC().After(t)
					}
				}
				slog.Warn("mcp.user_401_diagnostics",
					"server", srv.Name,
					"user", userID,
					"has_bitrix_domain", hasNonEmpty(uc.Env, "BITRIX_DOMAIN"),
					"has_access_token", hasNonEmpty(uc.Env, "BITRIX_ACCESS_TOKEN"),
					"has_refresh_token", hasNonEmpty(uc.Env, "BITRIX_REFRESH_TOKEN"),
					"bitrix_expires_at", expiresAt,
					"bitrix_expired", expired,
				)
				_ = l.mcpStore.DeleteUserCredentials(ctx, srv.ID, userID)
				slog.Warn("mcp.user_credentials_purged", "server", srv.Name, "user", userID, "reason", "unauthorized_401")
			}
			slog.Warn("mcp.user_pool_acquire_failed", "server", srv.Name, "user", userID, "error", err)
			continue
		}

		// Release immediately — BridgeTools hold client pointer directly.
		// This allows pool idle eviction to work (refCount=0 + lastUsed for TTL).
		// When pool evicts the connection, BridgeTool.Execute detects connected=false.
		l.mcpPool.ReleaseUser(mcpbridge.UserPoolKey(l.tenantID, srv.Name, userID))

		// Create BridgeTools pointing to user's connection. Per-user tools are
		// cached in mcpUserTools sync.Map (line below) and resolved at execute
		// time by executeToolForActor — they intentionally do NOT register
		// into the shared tool registry because doing so causes a cross-user
		// identity leak: the first user wins and subsequent users get the first
		// user's BridgeTool (with first user's MCP api_key + pool connection).
		// The shared registry holds only shared/non-MCP tools (memory, web,
		// exec, …).
		//
		// Filter tools upfront by the agent's grant (info.ToolAllow / ToolDeny)
		// so the LLM never sees tools it cannot call. Without this, every
		// per-user MCP server exposes its full tool set and the LLM repeatedly
		// triggers the runtime "grant revoked" path (visible in the original
		// agent_brain_external screenshot).
		hints := mcpbridge.ParseToolHints(srv.Settings)
		var filteredOut []string
		for _, mcpTool := range entry.MCPTools() {
			if !mcpbridge.IsToolAllowed(mcpTool.Name, info.ToolAllow, info.ToolDeny) {
				filteredOut = append(filteredOut, mcpTool.Name)
				continue
			}
			bt := mcpbridge.NewBridgeTool(srv.Name, mcpTool, entry.ClientPtr(), srv.ToolPrefix, srv.TimeoutSec, entry.Connected(), srv.ID, l.mcpGrantChecker).
				WithHints(hints.Global, hints.HintFor(mcpTool.Name)).
				WithForceReconnect(entry.RequestForceReconnect())
			userTools = append(userTools, bt)
		}
		if len(filteredOut) > 0 {
			slog.Info("mcp.tools.filtered_at_register",
				"server", srv.Name,
				"server_id", srv.ID,
				"user", userID,
				"path", "user_cred",
				"filtered_count", len(filteredOut),
				"filtered_tools", filteredOut,
				"allow_size", len(info.ToolAllow),
				"deny_size", len(info.ToolDeny),
			)
		}
	}

	if len(userTools) > 0 {
		l.mcpUserTools.Store(userID, userTools)
		// Update "mcp" tool group so policy expansion via alsoAllow includes
		// per-user tools. MergeToolGroup is additive — safe for concurrent users.
		var names []string
		for _, t := range userTools {
			names = append(names, t.Name())
		}
		l.registry.MergeToolGroup("mcp", names)
		slog.Info("mcp.user_tools_loaded", "user", userID, "tools", len(userTools))
	}
	return userTools
}

// executeToolForActor resolves a tool by name with per-user isolation.
//
// For per-user MCP tools (cached in mcpUserTools by actorUserID), we MUST
// resolve from the user's own slice so the BridgeTool used carries that
// user's MCP api_key + pool connection. Resolving via the shared registry
// alone leaks the first user's BridgeTool to every subsequent user.
//
// Fallback to shared registry for non-MCP tools (memory, web, exec, etc.)
// and for cases where actorUserID has no per-user tools (synthetic events,
// non-Bitrix channels without per-user provisioning).
func (l *Loop) executeToolForActor(
	ctx context.Context,
	name string,
	args map[string]any,
	channel, chatID, peerKind, sessionKey, actorUserID string,
) *tools.Result {
	if actorUserID != "" {
		if cached, ok := l.mcpUserTools.Load(actorUserID); ok {
			for _, t := range cached.([]tools.Tool) {
				if t.Name() != name {
					continue
				}
				// Apply ContextualTool / PeerKindAware setters if supported.
				if ct, ok := t.(tools.ContextualTool); ok {
					ct.SetContext(channel, chatID)
				}
				if pa, ok := t.(tools.PeerKindAware); ok {
					pa.SetPeerKind(peerKind)
				}
				return t.Execute(ctx, args)
			}
		}
	}
	return l.tools.ExecuteWithContext(ctx, name, args, channel, chatID, peerKind, sessionKey, nil)
}
