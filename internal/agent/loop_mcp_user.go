package agent

import (
	"context"
	"log/slog"
	"maps"

	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// resolveActorUserID picks the user identifier used for per-user resource
// lookups (MCP credentials, RBAC grants, audit attribution) given the routing
// fields carried on a pipeline.RunInput / agent.RunRequest.
//
// In DMs, UserID == SenderID — this function returns UserID unchanged.
//
// In group chats the gateway consumer (cmd/gateway_consumer_normal.go) rewrites
// UserID to a group-scope composite key ("group:<channel>:<chatID>" or
// "guild:<guildID>:user:<senderID>" for Discord) so multiple users in the
// same group share conversation memory and session state. That composite is
// correct for *memory*, but wrong for resources scoped per-actor:
//
//   - MCP credentials are minted per-user via the Phase C lazy provisioner
//     (e.g. Bitrix24 channels/bitrix24/provisioner.go) and keyed by the real
//     external user id (= SenderID). Looking them up by the group-composite
//     key always misses the row, so MCP tools silently disappear in group
//     chats. This is the bug this helper fixes.
//   - RBAC grants and audit attribution must reflect the real actor, not the
//     group container — otherwise every member of a chat appears identical
//     to the policy engine.
//
// SenderID is preserved unchanged on every InboundMessage / RunRequest, so
// this helper can always recover actor identity from the routing fields it
// already has. When SenderID is empty (synthetic ticker / notification
// senders) the function falls back to UserID — those events do not own
// per-user credentials and the lookup will return nil safely either way.
//
// Channels other than Bitrix24 currently do not provision per-user MCP
// credentials, so for them this function is a no-op (the lookup returns nil
// regardless of which key is used). When Telegram / Slack / Discord later
// add per-user MCP integrations the same routing already flows through this
// helper — no per-channel branching needed.
func resolveActorUserID(userID, senderID, peerKind string) string {
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
			slog.Warn("mcp.user_pool_acquire_failed", "server", srv.Name, "user", userID, "error", err)
			continue
		}

		// Release immediately — BridgeTools hold client pointer directly.
		// This allows pool idle eviction to work (refCount=0 + lastUsed for TTL).
		// When pool evicts the connection, BridgeTool.Execute detects connected=false.
		l.mcpPool.ReleaseUser(mcpbridge.UserPoolKey(l.tenantID, srv.Name, userID))

		// Create BridgeTools pointing to user's connection and register in the
		// shared tool registry so ExecuteWithContext can resolve them by name.
		reg, _ := l.tools.(*tools.Registry)
		hints := mcpbridge.ParseToolHints(srv.Settings)
		for _, mcpTool := range entry.MCPTools() {
			bt := mcpbridge.NewBridgeTool(srv.Name, mcpTool, entry.ClientPtr(), srv.ToolPrefix, srv.TimeoutSec, entry.Connected(), srv.ID, l.mcpGrantChecker).
				WithHints(hints.Global, hints.HintFor(mcpTool.Name))
			// Register in registry so ExecuteWithContext can find them.
			// Skip if already registered (another user loaded this server with same tool names).
			if reg != nil {
				if _, exists := reg.Get(bt.Name()); !exists {
					reg.Register(bt)
				}
			}
			userTools = append(userTools, bt)
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
