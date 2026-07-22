// Package mcp exposes goclaw's Model Context Protocol bridge/server surface.
// crud_server.go implements a second, distinct MCP server (separate from the
// tool bridge in bridge_server.go) that exposes goclaw's CRUD-style resource
// management surface — agents, sessions, skills, cron, config, agent links,
// API keys, config permissions, Bitrix24 portals, run timelines, teams,
// teams tasks, teams workspace, channels, channel instances, hooks,
// heartbeat, pairing, exec approval, usage, quota, chat/chat-behavior, LLM
// completion, runtime logs, outbound send, and TTS voices — as MCP tools
// backed directly by the real store/subsystem implementations used by the
// gateway's own WebSocket RPC methods.
//
// Tenant scope: the CRUD MCP server is gated by a single shared bearer
// secret (gateway.mcp_server_token) with no per-caller identity, so it is
// treated like the gateway-token/owner path in internal/http/auth.go —
// callers may optionally scope a request to a tenant via the
// "X-GoClaw-Tenant-Id" header (UUID or slug), with no membership check
// (the token itself is the full-trust boundary), falling back to
// store.MasterTenantID when the header is absent or unresolvable. This is
// applied once per request via mcpserver.WithHTTPContextFunc in
// NewCRUDServer, so every tool handler in this package can rely on
// store.TenantIDFromContext(ctx) already carrying a concrete value.
package mcp

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// CRUDDeps bundles the store dependencies the CRUD MCP server needs.
// All fields are optional — tool groups whose backing store is nil are
// simply not registered, so this server degrades gracefully across editions
// (e.g. SQLite/lite builds that omit certain stores).
type CRUDDeps struct {
	Agents            store.AgentStore
	AgentRuntime      AgentRuntimeLookup // enables goclaw_agent_{get,wait}; agent identity/files work without it
	Sessions          store.SessionStore
	Skills            store.SkillStore
	Cron              store.CronStore
	Config            *config.Config
	AgentLinks        store.AgentLinkStore
	APIKeys           store.APIKeyStore
	ConfigPermissions store.ConfigPermissionStore
	Bitrix            store.BitrixPortalStore
	RunTimeline       store.RunTimelineStore

	// Phase 2: subsystem-backed tool families.
	Teams            store.TeamStore
	ChannelInstances store.ChannelInstanceStore
	ChannelManager   *channels.Manager
	Hooks            hooks.HookStore
	Heartbeats       store.HeartbeatStore
	Providers        store.ProviderStore
	Pairing          store.PairingStore
	ExecApproval     *tools.ExecApprovalManager
	Quota            *channels.QuotaChecker
	DB               *sql.DB // for quota.usage today's trace summary

	// Phase 3: live-runtime-backed tool families (chat, LLM, logs, send, voices).
	ChatRunner        ChatRunner               // enables goclaw_chat_{send,abort,session_status}
	LLMProviders      *providers.Registry      // enables goclaw_llm_complete
	LLMDefaults       LLMDefaults              // background provider/model fallback for goclaw_llm_complete
	MessageBus        *bus.MessageBus          // enables goclaw_send
	RuntimeLogs       RuntimeLogSnapshotter    // enables goclaw_logs_tail
	VoiceCache        *audio.VoiceCache        // enables goclaw_voices_{list,refresh}
	VoiceSecretsStore store.ConfigSecretsStore // per-tenant TTS provider API key lookup for goclaw_voices_*

	// Tenants resolves the optional "X-GoClaw-Tenant-Id" request header (UUID
	// or slug) to a concrete tenant for every CRUD MCP call — see
	// resolveMCPTenantID. Every tenant-scoped tool handler in this package
	// relies on store.TenantIDFromContext(ctx) already carrying a resolved
	// value by the time it runs. When nil, all requests are treated as
	// store.MasterTenantID (same fail-safe default resolveMCPTenantID uses
	// when the header is absent/unresolvable).
	Tenants store.TenantStore

	// Phase 4: read-heavy/admin surfaces (memory, knowledge graph, tracing,
	// tenants, providers) closing the CLI-vs-MCP coverage gap.
	Memory          store.MemoryStore
	KnowledgeGraph  store.KnowledgeGraphStore
	Tracing         store.TracingStore
	Contacts        store.ContactStore
	PendingMessages store.PendingMessageStore
	Activity        store.ActivityStore
	SystemConfigs   store.SystemConfigStore
	SecureCLI       store.SecureCLIStore
}

// NewCRUDServer builds a StreamableHTTPServer exposing goclaw's CRUD
// resources (agents, sessions, skills, cron, config, agent links, API keys,
// config permissions, Bitrix24 portals, run timelines) as MCP tools. Callers
// are expected to gate access to the returned handler with a bearer-token
// middleware (see gateway.tokenAuthMiddleware / Server.BuildMux) before
// mounting it — this server performs no authentication of its own.
func NewCRUDServer(deps CRUDDeps, version string) *mcpserver.StreamableHTTPServer {
	srv := mcpserver.NewMCPServer("goclaw-crud", version,
		mcpserver.WithToolCapabilities(false),
	)

	var registered int
	if deps.Agents != nil {
		registerAgentCRUDTools(srv, deps.Agents)
		registered += 5
		registerAgentSkillPinCRUDTools(srv, deps.Agents)
		registered += 2
	}
	if deps.Agents != nil && deps.AgentRuntime != nil {
		registerAgentRuntimeCRUDTools(srv, deps.Agents, deps.AgentRuntime)
		registered += 5
	}
	if deps.Sessions != nil {
		registerSessionCRUDTools(srv, deps.Sessions)
		registered += 7
	}
	if deps.Skills != nil {
		registerSkillCRUDTools(srv, deps.Skills)
		registered += 2
		if manage, ok := deps.Skills.(store.SkillManageStore); ok {
			registerSkillUpdateCRUDTool(srv, deps.Skills, manage)
			registered++
			registerSkillGrantCRUDTools(srv, deps.Skills, manage)
			registered += 2
			if deps.Config != nil {
				registerSkillWriteFileCRUDTool(srv, deps.Skills, manage, deps.Config)
				registered++
				registerSkillCreateCRUDTool(srv, manage, deps.Config)
				registered++
			}
		}
	}
	if deps.Cron != nil {
		registerCronCRUDTools(srv, deps.Cron)
		registered += 8
	}
	if deps.Config != nil {
		registerConfigCRUDTools(srv, deps.Config)
		registered++
	}
	if deps.AgentLinks != nil {
		registerAgentLinkCRUDTools(srv, deps.AgentLinks)
		registered += 4
	}
	if deps.APIKeys != nil {
		registerAPIKeyCRUDTools(srv, deps.APIKeys)
		registered += 3
	}
	if deps.ConfigPermissions != nil {
		registerConfigPermissionCRUDTools(srv, deps.ConfigPermissions)
		registered += 4
	}
	if deps.Bitrix != nil {
		registerBitrixCRUDTools(srv, deps.Bitrix)
		registered += 3
	}
	if deps.RunTimeline != nil {
		registerRunTimelineCRUDTools(srv, deps.RunTimeline)
		registered++
	}
	if deps.Teams != nil {
		registerTeamsCRUDTools(srv, deps.Teams, deps.Agents)
		registered += 10
		registerTeamsTasksCRUDTools(srv, deps.Teams, deps.Agents)
		registered += 13
		if deps.Config != nil {
			registerTeamsWorkspaceCRUDTools(srv, deps.Teams, deps.Config)
			registered += 3
		}
	}
	if deps.ChannelManager != nil {
		registerChannelsCRUDTools(srv, deps.ChannelManager)
		registered += 3
	}
	if deps.ChannelInstances != nil {
		registerChannelInstancesCRUDTools(srv, deps.ChannelInstances, deps.Agents)
		registered += 5
	}
	if deps.Hooks != nil {
		registerHooksCRUDTools(srv, deps.Hooks)
		registered += 7
	}
	if deps.Heartbeats != nil {
		registerHeartbeatCRUDTools(srv, deps.Heartbeats, deps.Agents, deps.Providers)
		registered += 8
	}
	if deps.Pairing != nil {
		registerPairingCRUDTools(srv, deps.Pairing)
		registered += 6
	}
	if deps.ExecApproval != nil {
		registerExecApprovalCRUDTools(srv, deps.ExecApproval)
		registered += 3
	}
	if deps.Sessions != nil {
		registerUsageCRUDTools(srv, deps.Sessions)
		registered += 2
	}
	registerQuotaCRUDTools(srv, deps.Quota, deps.DB)
	registered++

	// Phase 3: live-runtime-backed tool families.
	if deps.ChatRunner != nil && deps.Sessions != nil {
		registerChatCRUDTools(srv, deps.ChatRunner, deps.Sessions)
		registered += 5
	}
	if deps.Config != nil {
		registerChatBehaviorCRUDTool(srv, deps.Config, deps.ChannelManager)
		registered++
	}
	if deps.LLMProviders != nil {
		registerLLMCRUDTool(srv, deps.LLMProviders, deps.LLMDefaults)
		registered++
	}
	if deps.RuntimeLogs != nil {
		registerLogsCRUDTool(srv, deps.RuntimeLogs)
		registered++
	}
	if deps.MessageBus != nil {
		registerSendCRUDTool(srv, deps.MessageBus)
		registered++
	}
	if deps.VoiceCache != nil {
		registerVoicesCRUDTools(srv, deps.VoiceCache, deps.VoiceSecretsStore)
		registered += 2
	}

	// Phase 4: read-heavy/admin surfaces closing CLI-vs-MCP coverage gaps.
	if deps.Memory != nil {
		registerMemoryCRUDTools(srv, deps.Memory)
		registered += 5
	}
	if deps.KnowledgeGraph != nil {
		registerKnowledgeGraphCRUDTools(srv, deps.KnowledgeGraph)
		registered += 14
	}
	if deps.Tenants != nil {
		registerTenantsCRUDTools(srv, deps.Tenants)
		registered += 6
	}
	if deps.Providers != nil {
		registerProvidersCRUDTools(srv, deps.Providers)
		registered += 5
	}
	if deps.Tracing != nil {
		registerTracesCRUDTools(srv, deps.Tracing)
		registered += 2
	}
	if deps.Contacts != nil {
		registerContactsCRUDTools(srv, deps.Contacts)
		registered += 4
	}
	if deps.PendingMessages != nil {
		registerPendingMessagesCRUDTools(srv, deps.PendingMessages)
		registered += 3
	}
	if deps.Activity != nil {
		registerActivityCRUDTools(srv, deps.Activity)
		registered++
	}
	if deps.SystemConfigs != nil {
		registerSystemConfigCRUDTools(srv, deps.SystemConfigs)
		registered += 4
	}
	if deps.Config != nil {
		registerStorageCRUDTools(srv, deps.Config)
		registered += 4
	}
	if deps.Agents != nil {
		registerAgentExportCRUDTools(srv, deps.Agents)
		registered += 2
	}
	if deps.SecureCLI != nil {
		registerSecureCLICRUDTools(srv, deps.SecureCLI)
		registered += 5
	}
	registerHealthCRUDTool(srv, deps.DB, version)
	registered++

	slog.Info("mcp.crud: tools registered", "count", registered)

	tenants := deps.Tenants
	return mcpserver.NewStreamableHTTPServer(srv,
		mcpserver.WithStateLess(true),
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			tenantID := resolveMCPTenantID(ctx, tenants, r.Header.Get(mcpTenantHeader))
			return store.WithTenantID(ctx, tenantID)
		}),
	)
}
