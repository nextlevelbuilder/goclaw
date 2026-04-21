package cmd

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/gateway/methods"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// httpHandlers bundles the results of wireHTTP() for passing to wireHTTPHandlersOnServer.
type httpHandlers struct {
	agents           *httpapi.AgentsHandler
	skills           *httpapi.SkillsHandler
	traces           *httpapi.TracesHandler
	mcp              *httpapi.MCPHandler
	channelInstances *httpapi.ChannelInstancesHandler
	providers        *httpapi.ProvidersHandler
	builtinTools     *httpapi.BuiltinToolsHandler
	pendingMessages  *httpapi.PendingMessagesHandler
	teamEvents       *httpapi.TeamEventsHandler
	secureCLI        *httpapi.SecureCLIHandler
	secureCLIGrant   *httpapi.SecureCLIGrantHandler
	mcpUserCreds     *httpapi.MCPUserCredentialsHandler
}

// wireHTTPHandlersOnServer registers all HTTP handler objects onto the gateway server.
// Called after wireHTTP() and wireExtras() have returned their results.
func (d *gatewayDeps) wireHTTPHandlersOnServer(
	h httpHandlers,
	wakeH *httpapi.WakeHandler,
	mcpPool *mcpbridge.Pool,
	postTurn tools.PostTurnProcessor,
	mediaStore *media.Store,
) {
	if h.providers != nil {
		h.providers.SetAPIBaseFallback(d.cfg.Providers.APIBaseForType)
	}
	if h.agents != nil {
		d.server.SetAgentsHandler(h.agents)
	}
	if h.skills != nil {
		d.server.SetSkillsHandler(h.skills)
	}
	if h.traces != nil {
		d.server.SetTracesHandler(h.traces)
	}
	// External wake/trigger API — wakeH was created by caller before invoking this method.
	d.server.SetWakeHandler(wakeH)
	if h.mcp != nil {
		if mcpPool != nil {
			h.mcp.SetPoolEvictor(mcpPool)
		}
		d.server.SetMCPHandler(h.mcp)
	}
	if h.mcpUserCreds != nil {
		d.server.SetMCPUserCredentialsHandler(h.mcpUserCreds)
	}
	if h.channelInstances != nil {
		d.server.SetChannelInstancesHandler(h.channelInstances)
	}
	if h.providers != nil {
		d.server.SetProvidersHandler(h.providers)
	}
	if h.teamEvents != nil {
		d.server.SetTeamEventsHandler(h.teamEvents)
	}
	if d.pgStores != nil && d.pgStores.Teams != nil {
		d.server.SetTeamAttachmentsHandler(httpapi.NewTeamAttachmentsHandler(d.pgStores.Teams, d.workspace))
		d.server.SetWorkspaceUploadHandler(httpapi.NewWorkspaceUploadHandler(d.pgStores.Teams, d.workspace, d.msgBus))
	}
	if h.builtinTools != nil {
		d.server.SetBuiltinToolsHandler(h.builtinTools)
	}
	if h.pendingMessages != nil {
		if pc := d.cfg.Channels.PendingCompaction; pc != nil {
			h.pendingMessages.SetKeepRecent(pc.KeepRecent)
			h.pendingMessages.SetMaxTokens(pc.MaxTokens)
			h.pendingMessages.SetProviderModel(pc.Provider, pc.Model)
		}
		d.server.SetPendingMessagesHandler(h.pendingMessages)
	}
	if h.secureCLI != nil {
		d.server.SetSecureCLIHandler(h.secureCLI)
	}
	if h.secureCLIGrant != nil {
		d.server.SetSecureCLIGrantHandler(h.secureCLIGrant)
	}

	// Activity audit log API
	if d.pgStores.Activity != nil {
		d.server.SetActivityHandler(httpapi.NewActivityHandler(d.pgStores.Activity))
	}

	// System configs API
	if d.pgStores.SystemConfigs != nil {
		d.server.SetSystemConfigsHandler(httpapi.NewSystemConfigsHandler(d.pgStores.SystemConfigs, d.msgBus))

		// Refresh in-memory config when system_configs change via HTTP API
		d.msgBus.Subscribe(bus.TopicSystemConfigChanged, func(evt bus.Event) {
			// Use tenant context from the request that triggered the change
			ctx := context.Background()
			if reqCtx, ok := evt.Payload.(context.Context); ok {
				ctx = reqCtx
			} else {
				ctx = store.WithTenantID(ctx, store.MasterTenantID)
			}
			if sysConfigs, err := d.pgStores.SystemConfigs.List(ctx); err == nil && len(sysConfigs) > 0 {
				d.cfg.ApplySystemConfigs(sysConfigs)
				// Update PGMemoryStore chunk config so new documents use updated settings
				if mem := d.cfg.Agents.Defaults.Memory; mem != nil {
					if pgMem, ok := d.pgStores.Memory.(*pg.PGMemoryStore); ok {
						pgMem.UpdateChunkConfig(mem.MaxChunkLen, mem.ChunkOverlap)
					}
				}
				// Note: vault enrichment provider is resolved per-tenant at runtime,
				// no hot-reload needed here
				slog.Debug("system_configs refreshed to in-memory config", "keys", len(sysConfigs))
			}
		})
	}

	// Usage analytics API
	if d.pgStores.Snapshots != nil {
		d.server.SetUsageHandler(httpapi.NewUsageHandler(d.pgStores.Snapshots, d.pgStores.DB))
	}

	// Runtime package management (install/uninstall system/pip/npm/github packages)
	initGitHubInstaller()
	d.server.SetPackagesHandler(httpapi.NewPackagesHandler())

	// API documentation (OpenAPI spec + Swagger UI at /docs)
	d.server.SetDocsHandler(httpapi.NewDocsHandler())

	// Edition info (public, no auth — used by desktop UI comparison modal)
	d.server.SetEditionHandler(httpapi.NewEditionHandler())

	if d.pgStores != nil && d.pgStores.APIKeys != nil {
		d.server.SetAPIKeysHandler(httpapi.NewAPIKeysHandler(d.pgStores.APIKeys, d.msgBus))
		d.server.SetAPIKeyStore(d.pgStores.APIKeys)
		httpapi.InitAPIKeyCache(d.pgStores.APIKeys, d.msgBus)
	}

	// K10: single shared webhookLimiter — one per process enforces per-tenant RPM cap across
	// both LLM and message endpoints. Two separate instances would double the effective cap.
	webhookEncKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")

	// K6: refuse to mount any webhook handler when GOCLAW_ENCRYPTION_KEY is unset.
	// crypto.Encrypt("", "") returns plaintext unchanged, so an empty key would silently
	// persist raw secrets to the database — defeating the stated DB-leak protection.
	// Skip-mount approach: process still starts (all other subsystems work), but
	// /v1/webhooks/* returns 404. Set GOCLAW_ENCRYPTION_KEY to re-enable webhooks.
	if webhookEncKey == "" {
		slog.Error("webhook subsystem disabled: GOCLAW_ENCRYPTION_KEY not set. Set the env var to enable /v1/webhooks/* endpoints.")
	} else {
		sharedWebhookLimiter := httpapi.NewWebhookLimiter()

		// Webhook admin CRUD — available in all editions (Standard + Lite).
		// Runtime routes (/v1/webhooks/message, /v1/webhooks/llm) are mounted by phases 05/06.
		if d.pgStores != nil && d.pgStores.Webhooks != nil {
			adminH := httpapi.NewWebhooksAdminHandler(
				d.pgStores.Webhooks,
				d.pgStores.Tenants,
				d.msgBus,
			)
			adminH.SetEncKey(webhookEncKey)
			d.server.SetWebhooksAdminHandler(adminH)
		}

		// Webhook message endpoint — Standard edition only (channels required).
		// Phase 05b: POST /v1/webhooks/message → sync channel send (text + optional media).
		if edition.Current().AllowsChannels() &&
			d.pgStores != nil &&
			d.pgStores.Webhooks != nil &&
			d.pgStores.WebhookCalls != nil &&
			d.pgStores.ChannelInstances != nil &&
			d.channelMgr != nil {
			msgH := httpapi.NewWebhookMessageHandler(
				d.channelMgr,
				d.pgStores.ChannelInstances,
				d.pgStores.WebhookCalls,
				d.pgStores.Webhooks,
				sharedWebhookLimiter, // K10: shared limiter
			)
			msgH.SetEncKey(webhookEncKey) // K6: decrypt secret at HMAC verify time
			d.server.SetWebhookMessageHandler(msgH)
		}

		// Webhook LLM endpoint — all editions (Standard + Lite).
		// Phase 06: POST /v1/webhooks/llm → sync agent run (≤30s) or async enqueue.
		// LocalhostOnly enforcement is handled by WebhookAuthMiddleware at request time.
		// lane=nil → handler self-creates internal default lane (4-slot).
		if d.pgStores != nil &&
			d.pgStores.Webhooks != nil &&
			d.pgStores.WebhookCalls != nil &&
			d.agentRouter != nil {
			llmH := httpapi.NewWebhookLLMHandler(
				d.agentRouter,
				d.pgStores.WebhookCalls,
				d.pgStores.Webhooks,
				sharedWebhookLimiter, // K10: shared limiter
				nil,                  // lane: nil → internal default (4-slot); configurable in future via cfg
			)
			llmH.SetEncKey(webhookEncKey) // K6: decrypt secret at HMAC verify time
			d.server.SetWebhookLLMHandler(llmH)
		}
	}

	// Allow browser-paired users to access HTTP APIs
	if d.pgStores.Pairing != nil {
		httpapi.InitPairingAuth(d.pgStores.Pairing)
	}

	// Memory management API
	if d.pgStores != nil && d.pgStores.Memory != nil {
		d.server.SetMemoryHandler(httpapi.NewMemoryHandler(d.pgStores.Memory))
	}

	// Knowledge graph API
	if d.pgStores != nil && d.pgStores.KnowledgeGraph != nil {
		d.server.SetKnowledgeGraphHandler(httpapi.NewKnowledgeGraphHandler(d.pgStores.KnowledgeGraph, d.providerRegistry))
	}

	// V3: Evolution metrics + suggestions API
	if d.pgStores != nil && d.pgStores.EvolutionMetrics != nil && d.pgStores.EvolutionSuggestions != nil {
		var evoOpts []httpapi.EvolutionHandlerOpt
		if manageStore, ok := d.pgStores.Skills.(store.SkillManageStore); ok && d.skillsLoader != nil {
			evoOpts = append(evoOpts, httpapi.WithSkillCreation(manageStore, d.skillsLoader, d.dataDir))
		}
		if d.pgStores.Agents != nil {
			evoOpts = append(evoOpts, httpapi.WithAgentStore(d.pgStores.Agents))
		}
		if d.pgStores.BuiltinToolTenantCfgs != nil {
			evoOpts = append(evoOpts, httpapi.WithToolTenantCfgs(d.pgStores.BuiltinToolTenantCfgs))
		}
		d.server.SetEvolutionHandler(httpapi.NewEvolutionHandler(d.pgStores.EvolutionMetrics, d.pgStores.EvolutionSuggestions, evoOpts...))
	}

	// V3: Knowledge Vault document API
	if d.pgStores != nil && d.pgStores.Vault != nil {
		vh := httpapi.NewVaultHandler(d.pgStores.Vault, d.pgStores.Teams, d.workspace, d.domainBus, d.pgStores.Agents, d.pgStores.Teams)
		vh.SetEnrichProgress(d.enrichProgress)
		vh.SetEnrichWorker(d.enrichWorker)
		d.server.SetVaultHandler(vh)

		// Lightweight graph visualization endpoints (vault + KG).
		var kgGraph store.KGGraphStore
		if d.pgStores.KnowledgeGraph != nil {
			kgGraph = newKGGraphStore(d.pgStores.DB)
		}
		vgHandler := httpapi.NewVaultGraphHandler(
			newVaultGraphStore(d.pgStores.DB), kgGraph, d.pgStores.Teams,
		)
		d.server.SetVaultGraphHandler(vgHandler)
	}

	// V3: Episodic memory summaries API
	if d.pgStores != nil && d.pgStores.Episodic != nil {
		d.server.SetEpisodicHandler(httpapi.NewEpisodicHandler(d.pgStores.Episodic))
	}

	// V3: Orchestration mode API (read-only)
	if d.pgStores != nil && d.pgStores.Agents != nil {
		d.server.SetOrchestrationHandler(httpapi.NewOrchestrationHandler(d.pgStores.Agents, d.pgStores.Teams, d.pgStores.AgentLinks))
	}

	// V3: Per-agent v3 feature flags API
	if d.pgStores != nil && d.pgStores.Agents != nil {
		d.server.SetV3FlagsHandler(httpapi.NewV3FlagsHandler(d.pgStores.Agents))
	}

	// Workspace file serving endpoint — serves files by absolute path, auth-token protected.
	d.server.SetFilesHandler(httpapi.NewFilesHandler(d.workspace, d.dataDir))

	// Storage file management — browse/delete files under the resolved workspace directory.
	d.server.SetStorageHandler(httpapi.NewStorageHandler(d.workspace))

	// Media upload endpoint — accepts multipart file uploads, returns temp path + MIME type.
	d.server.SetMediaUploadHandler(httpapi.NewMediaUploadHandler())

	// Media serve endpoint — serves persisted media files by ID for WS/web clients.
	if mediaStore != nil {
		d.server.SetMediaServeHandler(httpapi.NewMediaServeHandler(mediaStore))
	}

	// ElevenLabs voice list + refresh endpoints (GET /v1/voices, POST /v1/voices/refresh).
	// VoiceCache is shared between the HTTP handler and the WS voices.list method.
	// TTL 1h + LRU cap 1000 tenants.
	{
		voiceCache := audio.NewVoiceCache(1*time.Hour, 1000)
		var secretStore store.ConfigSecretsStore
		if d.pgStores != nil && d.pgStores.ConfigSecrets != nil {
			secretStore = d.pgStores.ConfigSecrets
		}
		var tenantStore store.TenantStore
		if d.pgStores != nil && d.pgStores.Tenants != nil {
			tenantStore = d.pgStores.Tenants
		}
		voicesH := httpapi.NewVoicesHandler(voiceCache, secretStore, tenantStore)
		d.server.SetVoicesHandler(voicesH)
		// Wire WS method — provider nil means each request resolves key via secretStore at HTTP layer.
		// For WS, use same cache. Provider is resolved via secretStore at WS level in a future phase.
		methods.NewVoicesMethods(voiceCache, nil).Register(d.server.Router())
	}

	// TTS synthesize endpoint — shares audio.Manager with setupTTS.
	if d.audioMgr != nil {
		ttsH := httpapi.NewTTSHandler(d.audioMgr)
		// Reuse the server's rate limiter (per-IP/token; NOT per-user).
		// Server.RateLimiter() is non-nil by construction (server.go:104).
		if rl := d.server.RateLimiter(); rl != nil && rl.Enabled() {
			ttsH.SetRateLimiter(rl.Allow)
		}
		// Wire stores for per-tenant TTS config lookup at synthesis time.
		if d.pgStores.SystemConfigs != nil && d.pgStores.ConfigSecrets != nil {
			ttsH.SetStores(d.pgStores.SystemConfigs, d.pgStores.ConfigSecrets)
			// Wire tenant resolver for channels TTS auto-apply
			d.audioMgr.SetTenantResolver(httpapi.NewTenantTTSResolver(d.pgStores.SystemConfigs, d.pgStores.ConfigSecrets))
		}
		d.server.SetTTSHandler(ttsH)
		d.ttsHandler = ttsH // store for hot-reload
	}

	// Per-tenant TTS config endpoint — allows tenant admins to configure TTS.
	if d.pgStores.SystemConfigs != nil && d.pgStores.ConfigSecrets != nil {
		d.server.SetTTSConfigHandler(httpapi.NewTTSConfigHandler(d.pgStores.SystemConfigs, d.pgStores.ConfigSecrets))
	}

	// Seed + apply builtin tool disables
	if d.pgStores.BuiltinTools != nil {
		seedBuiltinTools(context.Background(), d.pgStores.BuiltinTools)
		migrateBuiltinToolSettings(context.Background(), d.pgStores.BuiltinTools)
		backfillWebFetchSettings(context.Background(), d.pgStores.BuiltinTools)
		applyBuiltinToolDisables(context.Background(), d.pgStores.BuiltinTools, d.toolsReg)
	}
}
