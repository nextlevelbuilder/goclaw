package gateway

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/webui"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Server is the main gateway server handling WebSocket and HTTP connections.
// routeRegistrar is implemented by all HTTP API handlers that register routes on a mux.
type routeRegistrar interface {
	RegisterRoutes(mux *http.ServeMux)
}

type Server struct {
	cfg      *config.Config
	eventPub bus.EventPublisher
	agents   *agent.Router
	sessions store.SessionStore
	tools    *tools.Registry
	router   *MethodRouter

	// HTTP API handlers — all implement routeRegistrar.
	// Registered via Set*Handler() setters, routes added in BuildMux() via single loop.
	handlers []routeRegistrar

	// Non-handler dependencies (don't implement RegisterRoutes)
	policyEngine   *permissions.PolicyEngine
	pairingService store.PairingStore
	apiKeyStore    store.APIKeyStore   // for API key auth lookup
	agentStore     store.AgentStore    // for context injection in tools_invoke
	skillStore     store.SkillStore    // for the CRUD MCP server (/api/mcp/) skill tools
	cronStore      store.CronStore     // for the CRUD MCP server (/api/mcp/) cron tools
	msgBus         *bus.MessageBus     // for MCP bridge media delivery
	toolPolicy     *tools.PolicyEngine // for per-agent tool policy enforcement in MCP bridge

	// Additional CRUD MCP server (/api/mcp/) dependencies — all optional, same
	// degrade-gracefully contract as skillStore/cronStore above.
	agentLinkStore   store.AgentLinkStore
	configPermStore  store.ConfigPermissionStore
	bitrixStore      store.BitrixPortalStore
	runTimelineStore store.RunTimelineStore
	teamStore        store.TeamStore
	channelInstStore store.ChannelInstanceStore
	channelMgr       *channels.Manager
	hookStore        hooks.HookStore
	heartbeatStore   store.HeartbeatStore
	providerStore    store.ProviderStore
	execApprovalMgr  *tools.ExecApprovalManager
	quotaChecker     *channels.QuotaChecker
	sqlDB            *sql.DB           // for the CRUD MCP server's quota usage tool (today's trace summary)
	tenantStore      store.TenantStore // for the CRUD MCP server's "X-GoClaw-Tenant-Id" header resolution
	memoryStore      store.MemoryStore
	kgStore          store.KnowledgeGraphStore
	tracingStore     store.TracingStore
	contactStore     store.ContactStore
	pendingMsgStore  store.PendingMessageStore
	activityStore    store.ActivityStore
	systemCfgStore   store.SystemConfigStore
	secureCLIStore   store.SecureCLIStore

	// Phase 3 CRUD MCP server (/api/mcp/) dependencies: chat/LLM/logs/send/voices.
	llmProviders      *providers.Registry
	llmDefaults       mcpbridge.LLMDefaults
	voiceCache        *audio.VoiceCache
	voiceSecretsStore store.ConfigSecretsStore

	upgrader    websocket.Upgrader
	rateLimiter *RateLimiter
	clients     map[string]*Client
	mu          sync.RWMutex

	startedAt     time.Time
	version       string
	db            interface{ PingContext(context.Context) error } // for health check DB ping
	updateChecker *UpdateChecker

	logTee   *LogTee                 // optional; auto-unsubscribes clients on disconnect
	postTurn tools.PostTurnProcessor // optional; for team task dispatch in HTTP API paths

	httpServer *http.Server
	mux        *http.ServeMux

	// publicURLSnapshot remembers the gateway's externally reachable base URL
	// learned from inbound HTTP requests. Reset to a fresh snapshot per Server
	// so test servers don't share state. Read by RPC methods that need to
	// advertise URLs back to external systems (e.g. Bitrix24 install link).
	publicURLSnapshot *PublicURLSnapshot
}

// SetPostTurnProcessor sets the post-turn processor for team task dispatch in HTTP API handlers.
func (s *Server) SetPostTurnProcessor(pt tools.PostTurnProcessor) {
	s.postTurn = pt
}

// SetToolPolicy sets the tool policy engine used to enforce per-agent tool
// access on the MCP bridge server (see internal/mcp/bridge_server.go).
func (s *Server) SetToolPolicy(pe *tools.PolicyEngine) {
	s.toolPolicy = pe
}

// NewServer creates a new gateway server.
func NewServer(cfg *config.Config, eventPub bus.EventPublisher, agents *agent.Router, sess store.SessionStore, toolsReg ...*tools.Registry) *Server {
	s := &Server{
		cfg:               cfg,
		eventPub:          eventPub,
		agents:            agents,
		sessions:          sess,
		clients:           make(map[string]*Client),
		startedAt:         time.Now(),
		publicURLSnapshot: NewPublicURLSnapshot(),
	}

	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.checkOrigin,
	}

	if len(toolsReg) > 0 && toolsReg[0] != nil {
		s.tools = toolsReg[0]
	}

	// Initialize rate limiter.
	// rate_limit_rpm > 0  → enabled at that RPM
	// rate_limit_rpm == 0 → disabled (default, backward compat)
	// rate_limit_rpm < 0  → disabled explicitly
	s.rateLimiter = NewRateLimiter(cfg.Gateway.RateLimitRPM, 5)

	s.router = NewMethodRouter(s)
	return s
}

// RateLimiter returns the server's rate limiter for use by method handlers.
func (s *Server) RateLimiter() *RateLimiter { return s.rateLimiter }

// PublicURLSnapshot returns the snapshot of the gateway's externally reachable
// base URL. Updated by the snapshot middleware on every inbound request.
func (s *Server) PublicURLSnapshot() *PublicURLSnapshot { return s.publicURLSnapshot }

// checkOrigin validates WebSocket connection origin against the allowed origins whitelist.
// If no origins are configured, all origins are allowed (backward compatibility / dev mode).
// Empty Origin header (non-browser clients like CLI/SDK) is always allowed.
func (s *Server) checkOrigin(r *http.Request) bool {
	allowed := s.cfg.Gateway.AllowedOrigins
	if len(allowed) == 0 {
		return true // no config = allow all (backward compat)
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser clients (CLI, SDK, channels)
	}
	for _, a := range allowed {
		if origin == a || a == "*" {
			return true
		}
	}
	slog.Warn("security.cors_rejected", "origin", origin)
	return false
}

// BuildMux creates and caches the HTTP mux with all routes registered.
// Call this before Start() if you need the mux for additional listeners (e.g. Tailscale).
func (s *Server) BuildMux() *http.ServeMux {
	if s.mux != nil {
		return s.mux
	}

	mux := http.NewServeMux()

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// HTTP API endpoints
	mux.HandleFunc("/health", s.handleHealth)

	// OpenAI-compatible chat completions
	isManaged := s.agentStore != nil
	chatHandler := httpapi.NewChatCompletionsHandler(s.agents, s.sessions, isManaged)
	if s.rateLimiter.Enabled() {
		chatHandler.SetRateLimiter(s.rateLimiter.Allow)
	}
	if s.postTurn != nil {
		chatHandler.SetPostTurnProcessor(s.postTurn)
	}
	mux.Handle("/v1/chat/completions", chatHandler)

	// OpenResponses protocol
	responsesHandler := httpapi.NewResponsesHandler(s.agents, s.sessions)
	if s.postTurn != nil {
		responsesHandler.SetPostTurnProcessor(s.postTurn)
	}
	mux.Handle("/v1/responses", responsesHandler)

	// Direct tool invocation
	if s.tools != nil {
		toolsHandler := httpapi.NewToolsInvokeHandler(s.tools, s.agentStore)
		mux.Handle("/v1/tools/invoke", toolsHandler)
	}

	// Read-only HTTP compatibility for automation clients.
	if s.sessions != nil {
		httpapi.NewSessionsHandler(s.sessions, s.cfg.Gateway.OwnerIDs).RegisterRoutes(mux)
	}

	// Register all HTTP API handlers (agents, skills, teams, storage, etc.)
	for _, h := range s.handlers {
		if h != nil {
			h.RegisterRoutes(mux)
		}
	}

	httpapi.RegisterAPINotFoundRoute(mux)

	// MCP bridge: expose GoClaw tools to Claude CLI via streamable-http.
	// Only listens on localhost (CLI runs on the same machine).
	// Protected by gateway token; disabled when no token is configured to
	// prevent unauthenticated tool invocations if port is exposed.
	if s.tools != nil {
		if s.cfg.Gateway.Token != "" {
			bridgeHandler := mcpbridge.NewBridgeServer(s.tools, "1.0.0", s.msgBus, s.toolPolicy)
			handler := tokenAuthMiddleware(s.cfg.Gateway.Token,
				bridgeContextMiddleware(s.cfg.Gateway.Token, s.agentStore, bridgeHandler))
			mux.Handle("/mcp/bridge", handler)
		} else {
			slog.Warn("security.mcp_bridge_disabled: no gateway token configured, MCP bridge is disabled")
			mux.HandleFunc("/mcp/bridge", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"mcp bridge disabled: set GOCLAW_GATEWAY_TOKEN to enable"}`))
			})
		}
	}

	// CRUD MCP server: exposes goclaw's agents/sessions/skills/cron/config
	// resource management as MCP tools, backed directly by the real stores.
	// Distinct from /mcp/bridge (agent tool bridge for Claude CLI) above —
	// this one is meant for external automation/admin clients. Gated by its
	// own bearer token (gateway.mcp_server_token / GOCLAW_MCP_SERVER_TOKEN),
	// independent from the general gateway token, so it can be rotated or
	// disabled separately. When unset, the server is not constructed and the
	// route is not mounted at all (no 403 handler either) — the endpoint
	// simply does not exist, so it can never leak the fact that a CRUD MCP
	// surface is present on this deployment.
	//
	// Mounted under /api/mcp/ (NOT /mcp/) -- the web UI owns the client-side
	// route "/mcp" (see ui/web/src/lib/routes.ts) for its "manage external
	// MCP servers" page. http.ServeMux resolves the longest matching pattern
	// first, so a subtree registration at "/mcp/" would shadow the SPA
	// catch-all on every full page load/refresh of that route (the browser
	// requests "/mcp" directly from the backend, bypassing React Router
	// entirely). /api/mcp/ lives in a namespace no frontend route will ever
	// occupy, so it can never collide with future client-side routes either.
	if s.cfg.Gateway.MCPServerToken != "" {
		var agentRuntime mcpbridge.AgentRuntimeLookup
		var chatRunner mcpbridge.ChatRunner
		if s.agents != nil {
			agentRuntime = func(ctx context.Context, agentID string) (string, bool, error) {
				loop, err := s.agents.Get(ctx, agentID)
				if err != nil {
					return "", false, fmt.Errorf("get agent %q: %w", agentID, err)
				}
				return loop.ID(), loop.IsRunning(), nil
			}
			chatRunner = &agentChatRunner{agents: s.agents}
		}
		var runtimeLogs mcpbridge.RuntimeLogSnapshotter
		if s.logTee != nil {
			runtimeLogs = s.logTee
		}
		crudHandler := mcpbridge.NewCRUDServer(mcpbridge.CRUDDeps{
			Agents:            s.agentStore,
			AgentRuntime:      agentRuntime,
			Sessions:          s.sessions,
			Skills:            s.skillStore,
			Cron:              s.cronStore,
			Config:            s.cfg,
			AgentLinks:        s.agentLinkStore,
			APIKeys:           s.apiKeyStore,
			ConfigPermissions: s.configPermStore,
			Bitrix:            s.bitrixStore,
			RunTimeline:       s.runTimelineStore,
			Teams:             s.teamStore,
			ChannelInstances:  s.channelInstStore,
			ChannelManager:    s.channelMgr,
			Hooks:             s.hookStore,
			Heartbeats:        s.heartbeatStore,
			Providers:         s.providerStore,
			Pairing:           s.pairingService,
			ExecApproval:      s.execApprovalMgr,
			Quota:             s.quotaChecker,
			DB:                s.sqlDB,
			ChatRunner:        chatRunner,
			LLMProviders:      s.llmProviders,
			LLMDefaults:       s.llmDefaults,
			MessageBus:        s.msgBus,
			RuntimeLogs:       runtimeLogs,
			VoiceCache:        s.voiceCache,
			VoiceSecretsStore: s.voiceSecretsStore,
			Tenants:           s.tenantStore,
			Memory:            s.memoryStore,
			KnowledgeGraph:    s.kgStore,
			Tracing:           s.tracingStore,
			Contacts:          s.contactStore,
			PendingMessages:   s.pendingMsgStore,
			Activity:          s.activityStore,
			SystemConfigs:     s.systemCfgStore,
			SecureCLI:         s.secureCLIStore,
		}, s.version)
		mux.Handle("/api/mcp/", mcpServerTokenAuthMiddleware(s.cfg.Gateway.MCPServerToken, crudHandler))
	} else {
		slog.Info("mcp.crud_disabled: no gateway.mcp_server_token configured, CRUD MCP server is not mounted at /api/mcp/")
	}

	// Embedded web UI (built with -tags embedui). Catch-all after all API routes.
	// When the build does NOT include the embedui tag, webui.Handler() returns nil
	// and there's no handler for "/" — http.ServeMux would then return an opaque
	// 404 for the root URL, confusing operators who open the deployed URL in a
	// browser to check the service. Install a minimal JSON index handler in that
	// case so the root responds with something useful (and any unmatched path
	// still returns 404, just with a JSON body).
	if h := webui.Handler(s.cfg); h != nil {
		mux.Handle("/", h)
		slog.Info("serving embedded web UI")
	} else {
		mux.HandleFunc("/", s.handleIndex)
	}

	s.mux = mux
	return mux
}

// bridgeContextMiddleware extracts X-Agent-ID, X-User-ID, and X-Workspace headers
// from the MCP bridge request and injects them into the context so bridge tools can
// access agent/user scope and resolve workspace-relative paths.
// When a gateway token is configured, the context headers must be accompanied by
// a valid X-Bridge-Sig HMAC to prevent forgery.
func bridgeContextMiddleware(gatewayToken string, agentStore store.AgentStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		agentIDStr := r.Header.Get("X-Agent-ID")
		userID := r.Header.Get("X-User-ID")
		channel := r.Header.Get("X-Channel")
		chatID := r.Header.Get("X-Chat-ID")
		peerKind := r.Header.Get("X-Peer-Kind")
		workspace := r.Header.Get("X-Workspace")
		localKey := r.Header.Get("X-Local-Key")
		sessionKey := r.Header.Get("X-Session-Key")

		if agentIDStr != "" || userID != "" {
			// Reject context headers when no gateway token — prevents unauthenticated impersonation.
			if gatewayToken == "" {
				slog.Warn("security.mcp_bridge: no gateway token, ignoring context headers",
					"agent_id", agentIDStr, "user_id", userID)
				next.ServeHTTP(w, r)
				return
			}

			// Verify HMAC signature over all context fields.
			tenantIDStr := r.Header.Get("X-Tenant-ID")
			sig := r.Header.Get("X-Bridge-Sig")
			ok, tenantVerified := providers.VerifyBridgeContext(gatewayToken, agentIDStr, userID, channel, chatID, peerKind, workspace, tenantIDStr, sig, localKey, sessionKey)
			if !ok {
				slog.Warn("security.mcp_bridge: invalid bridge context signature",
					"agent_id", agentIDStr, "user_id", userID)
				http.Error(w, `{"error":"invalid bridge context signature"}`, http.StatusForbidden)
				return
			}

			if agentIDStr != "" {
				if id, err := uuid.Parse(agentIDStr); err == nil {
					ctx = store.WithAgentID(ctx, id)

					// Inject per-agent shell deny group overrides so the exec tool
					// respects the same policy as the normal agent loop.
					if agentStore != nil {
						ag, err := agentStore.GetByIDUnscoped(ctx, id)
						if err == nil && ag != nil {
							// Propagate the agent key so bridged session tools (sessions_list/
							// history/send) can resolve identity via ToolAgentKeyFromCtx. The MCP
							// bridge otherwise injects only the agent UUID, leaving the key empty
							// -> session tools fail with "agent context required".
							ctx = tools.WithToolAgentKey(ctx, ag.AgentKey)
							// Propagate the agent's per-agent tool policy so the MCP bridge
							// handler can enforce the same policy-filtered allowlist the
							// normal agent loop uses (see bridge_server.go makeToolHandler),
							// instead of exposing all BridgeToolNames unconditionally.
							ctx = tools.WithToolAgentPolicy(ctx, ag.ParseToolsConfig())
							groups := ag.ParseShellDenyGroups()
							if groups != nil {
								ctx = store.WithShellDenyGroups(ctx, groups)
							}
						}
					}
				}
			}
			if userID != "" {
				ctx = store.WithUserID(ctx, userID)
			}
			// Only inject tenant_id when HMAC actually covers it (level 1).
			// Fallback levels (pre-tenantID sessions) must not trust unsigned tenant headers.
			if tenantVerified && tenantIDStr != "" {
				if tid, err := uuid.Parse(tenantIDStr); err == nil {
					ctx = store.WithTenantID(ctx, tid)
				}
			}
		}

		// Inject channel routing context for tools like message, cron, etc.
		if channel != "" {
			ctx = tools.WithToolChannel(ctx, channel)
		}
		if chatID != "" {
			ctx = tools.WithToolChatID(ctx, chatID)
		}
		if peerKind != "" {
			ctx = tools.WithToolPeerKind(ctx, peerKind)
		}
		// Inject workspace so bridge tools (read_image, read_file, etc.) can resolve paths.
		// Only when agent context is present (HMAC-protected) to prevent unauthenticated path injection.
		if workspace != "" && (agentIDStr != "" || userID != "") {
			ctx = tools.WithToolWorkspace(ctx, workspace)
		}
		// Routing context (localKey, sessionKey) is injected unconditionally like channel/chatID.
		// These are used for message routing (forum topics), not security-sensitive operations.
		// Without valid agent context, tool execution will fail anyway.
		if localKey != "" {
			ctx = tools.WithToolLocalKey(ctx, localKey)
		}
		if sessionKey != "" {
			ctx = tools.WithToolSessionKey(ctx, sessionKey)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tokenAuthMiddleware wraps an http.Handler with Bearer token authentication.
func tokenAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		provided := strings.TrimPrefix(auth, "Bearer ")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// mcpServerTokenAuthMiddleware gates the CRUD MCP server (/api/mcp/) with its own
// bearer token, distinct from the general gateway token used elsewhere.
// Auth failures are logged as security events (missing/invalid token), never
// silently dropped, so operators can spot brute-force or misconfiguration.
func mcpServerTokenAuthMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		provided := strings.TrimPrefix(auth, "Bearer ")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			slog.Warn("security.mcp_auth_failed", "path", r.URL.Path, "remote", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start begins listening for WebSocket and HTTP connections.
func (s *Server) Start(ctx context.Context) error {
	mux := s.BuildMux()

	// Wrap with CORS for desktop dev mode (Wails serves frontend on different port).
	var handler http.Handler = mux
	if os.Getenv("GOCLAW_DESKTOP") == "1" {
		handler = desktopCORS(mux)
	}
	// NOTE: The public-URL snapshot is intentionally NOT updated by a global
	// middleware. An unauthenticated probe with a forged Host header could
	// otherwise poison the URL we hand back to clients (which then ends up
	// in OAuth callbacks and would leak tokens to an attacker-controlled
	// host). Instead, the snapshot is updated inside handleConnect AFTER
	// token authentication succeeds (see internal/gateway/router.go), and
	// from /bitrix24/install (already gated by valid OAuth state).

	addr := fmt.Sprintf("%s:%d", s.cfg.Gateway.Host, s.cfg.Gateway.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  3600 * time.Second,  // 1h: allow large uploads, long-running reads
		WriteTimeout: 3600 * time.Second,  // 1h: allow streaming responses, slow clients
		IdleTimeout:  30 * time.Second,    // 30s: close idle connections
	}

	slog.Info("gateway starting", "addr", addr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	if err := s.httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("gateway server: %w", err)
	}
	return nil
}

// handleWebSocket upgrades HTTP to WebSocket and manages the connection.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	client := NewClient(conn, s, clientIP(r))
	// Capture the public URL from the HTTP upgrade request. We DON'T snapshot
	// it server-wide yet — that happens only after the client authenticates
	// in handleConnect. This prevents an unauthenticated probe with a forged
	// Host header from poisoning the gateway-wide public URL.
	client.setUpgradeURL(derivePublicURLFromRequest(r))
	s.registerClient(client)

	defer func() {
		s.unregisterClient(client)
		client.Close()
	}()

	client.Run(r.Context())
}

// handleHealth returns a simple health check response.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","protocol":%d}`, protocol.ProtocolVersion)
}

// handleIndex is the fallback "/" handler when no embedded web UI is present.
// It returns a small JSON service-info document for exact-match "/" requests
// and a JSON 404 for everything else — http.ServeMux routes "/" as a
// catch-all, so unrelated paths fall through here too.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w,
		`{"service":"goclaw","status":"ok","protocol":%d,`+
			`"endpoints":["/health","/v1/chat/completions","/v1/responses","/v1/tools/invoke","/ws"]}`,
		protocol.ProtocolVersion)
}

// clientIP extracts the real client IP from the request, checking proxy headers first.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.IndexByte(fwd, ','); i > 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// Router returns the method router for registering additional handlers.
func (s *Server) Router() *MethodRouter { return s.router }

// SetPolicyEngine sets the permission policy engine for RPC method authorization.
func (s *Server) SetPolicyEngine(pe *permissions.PolicyEngine) { s.policyEngine = pe }

// SetPairingService sets the pairing service for channel authentication.
func (s *Server) SetPairingService(ps store.PairingStore) { s.pairingService = ps }

// SetAgentsHandler sets the agent CRUD handler.
func (s *Server) SetAgentsHandler(h *httpapi.AgentsHandler) { s.handlers = append(s.handlers, h) }

// SetSkillsHandler sets the skill management handler.
func (s *Server) SetSkillsHandler(h *httpapi.SkillsHandler) { s.handlers = append(s.handlers, h) }

// SetTracesHandler sets the LLM trace listing handler.
func (s *Server) SetTracesHandler(h *httpapi.TracesHandler) { s.handlers = append(s.handlers, h) }

// SetWakeHandler sets the external wake/trigger handler.
func (s *Server) SetWakeHandler(h *httpapi.WakeHandler) { s.handlers = append(s.handlers, h) }

// SetMCPHandler sets the MCP server management handler.
func (s *Server) SetMCPHandler(h *httpapi.MCPHandler)           { s.handlers = append(s.handlers, h) }
func (s *Server) SetMCPOAuthHandler(h *httpapi.MCPOAuthHandler) { s.handlers = append(s.handlers, h) }
func (s *Server) SetMCPUserCredentialsHandler(h *httpapi.MCPUserCredentialsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetChannelInstancesHandler sets the channel instance CRUD handler.
func (s *Server) SetChannelInstancesHandler(h *httpapi.ChannelInstancesHandler) {
	s.handlers = append(s.handlers, h)
}

// SetProvidersHandler sets the provider CRUD handler.
func (s *Server) SetProvidersHandler(h *httpapi.ProvidersHandler) {
	s.handlers = append(s.handlers, h)
}

// SetTeamEventsHandler sets the team event history handler.
func (s *Server) SetTeamEventsHandler(h *httpapi.TeamEventsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetTeamAttachmentsHandler sets the team attachment download handler.
func (s *Server) SetTeamAttachmentsHandler(h *httpapi.TeamAttachmentsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetWorkspaceUploadHandler sets the team workspace file upload handler.
func (s *Server) SetWorkspaceUploadHandler(h *httpapi.WorkspaceUploadHandler) {
	s.handlers = append(s.handlers, h)
}

// SetPendingMessagesHandler sets the pending messages handler.
func (s *Server) SetPendingMessagesHandler(h *httpapi.PendingMessagesHandler) {
	s.handlers = append(s.handlers, h)
}

// SetBuiltinToolsHandler sets the builtin tool management handler.
func (s *Server) SetBuiltinToolsHandler(h *httpapi.BuiltinToolsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetSecureCLIHandler sets the secure CLI credential CRUD handler.
func (s *Server) SetSecureCLIHandler(h *httpapi.SecureCLIHandler) {
	s.handlers = append(s.handlers, h)
}

// SetSecureCLIGrantHandler sets the per-agent secure CLI grant handler.
func (s *Server) SetSecureCLIGrantHandler(h *httpapi.SecureCLIGrantHandler) {
	s.handlers = append(s.handlers, h)
}

// SetBrowserCookiesHandler sets the selected browser-cookie sync handler.
func (s *Server) SetBrowserCookiesHandler(h *httpapi.BrowserCookiesHandler) {
	s.handlers = append(s.handlers, h)
}

// SetPackagesHandler sets the runtime package management handler.
func (s *Server) SetPackagesHandler(h *httpapi.PackagesHandler) {
	s.handlers = append(s.handlers, h)
}

// SetGatewayUpgradeHandler sets the host-local gateway upgrade trigger handler.
func (s *Server) SetGatewayUpgradeHandler(h *httpapi.GatewayUpgradeHandler) {
	s.handlers = append(s.handlers, h)
}

// SetOAuthHandler sets the OAuth handler (available in all modes).
func (s *Server) SetOAuthHandler(h *httpapi.OAuthHandler) { s.handlers = append(s.handlers, h) }

// SetAPIKeysHandler sets the API key management handler.
func (s *Server) SetAPIKeysHandler(h *httpapi.APIKeysHandler) {
	s.handlers = append(s.handlers, h)
}

// SetWebhooksAdminHandler registers the webhook admin CRUD handler.
func (s *Server) SetWebhooksAdminHandler(h *httpapi.WebhooksAdminHandler) {
	s.handlers = append(s.handlers, h)
}

// SetWebhookMessageHandler registers the POST /v1/webhooks/message runtime handler.
// Only called when edition.Current().AllowsChannels() is true (Standard edition).
func (s *Server) SetWebhookMessageHandler(h *httpapi.WebhookMessageHandler) {
	s.handlers = append(s.handlers, h)
}

// SetWebhookLLMHandler registers the POST /v1/webhooks/llm runtime handler.
// Available in all editions (Standard + Lite). Localhost-only enforcement is
// handled by WebhookAuthMiddleware at request time via webhook.LocalhostOnly.
func (s *Server) SetWebhookLLMHandler(h *httpapi.WebhookLLMHandler) {
	s.handlers = append(s.handlers, h)
}

// SetTenantsHandler sets the tenant management handler.
func (s *Server) SetTenantsHandler(h *httpapi.TenantsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetAPIKeyStore sets the API key store for token-based auth lookup.
func (s *Server) SetAPIKeyStore(st store.APIKeyStore) { s.apiKeyStore = st }

// SetFilesHandler sets the workspace file serving handler.
func (s *Server) SetFilesHandler(h *httpapi.FilesHandler) { s.handlers = append(s.handlers, h) }

// SetStorageHandler sets the storage file management handler.
func (s *Server) SetStorageHandler(h *httpapi.StorageHandler) { s.handlers = append(s.handlers, h) }

// SetMediaUploadHandler sets the media upload handler.
func (s *Server) SetMediaUploadHandler(h *httpapi.MediaUploadHandler) {
	s.handlers = append(s.handlers, h)
}

// SetMediaServeHandler sets the media serve handler.
func (s *Server) SetMediaServeHandler(h *httpapi.MediaServeHandler) {
	s.handlers = append(s.handlers, h)
}

// SetMemoryHandler sets the memory management handler.
func (s *Server) SetMemoryHandler(h *httpapi.MemoryHandler) { s.handlers = append(s.handlers, h) }

// SetKnowledgeGraphHandler sets the knowledge graph handler.
func (s *Server) SetKnowledgeGraphHandler(h *httpapi.KnowledgeGraphHandler) {
	s.handlers = append(s.handlers, h)
}

// SetEvolutionHandler sets the evolution metrics + suggestions handler.
func (s *Server) SetEvolutionHandler(h *httpapi.EvolutionHandler) {
	s.handlers = append(s.handlers, h)
}

// SetVoicesHandler sets the ElevenLabs voices list + refresh handler.
func (s *Server) SetVoicesHandler(h *httpapi.VoicesHandler) { s.handlers = append(s.handlers, h) }

// SetTTSHandler sets the TTS synthesize handler.
func (s *Server) SetTTSHandler(h *httpapi.TTSHandler) { s.handlers = append(s.handlers, h) }

// SetTTSConfigHandler sets the per-tenant TTS config handler.
func (s *Server) SetTTSConfigHandler(h *httpapi.TTSConfigHandler) { s.handlers = append(s.handlers, h) }

// SetVaultHandler sets the Knowledge Vault document handler.
func (s *Server) SetVaultHandler(h *httpapi.VaultHandler) { s.handlers = append(s.handlers, h) }

// SetVaultGraphHandler sets the lightweight graph visualization handler.
func (s *Server) SetVaultGraphHandler(h *httpapi.VaultGraphHandler) {
	s.handlers = append(s.handlers, h)
}

// SetEpisodicHandler sets the episodic memory handler.
func (s *Server) SetEpisodicHandler(h *httpapi.EpisodicHandler) { s.handlers = append(s.handlers, h) }

// SetOrchestrationHandler sets the orchestration mode handler.
func (s *Server) SetOrchestrationHandler(h *httpapi.OrchestrationHandler) {
	s.handlers = append(s.handlers, h)
}

// SetV3FlagsHandler sets the per-agent v3 feature flag handler.
func (s *Server) SetV3FlagsHandler(h *httpapi.V3FlagsHandler) { s.handlers = append(s.handlers, h) }

// SetActivityHandler sets the activity audit log handler.
func (s *Server) SetActivityHandler(h *httpapi.ActivityHandler) {
	s.handlers = append(s.handlers, h)
}

// SetRuntimeLogsHandler sets the runtime log aggregate handler.
func (s *Server) SetRuntimeLogsHandler(h *httpapi.RuntimeLogsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetSystemConfigsHandler sets the system configs handler.
func (s *Server) SetSystemConfigsHandler(h *httpapi.SystemConfigsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetBrandingAssetsHandler sets the public branding asset upload/serve handler.
func (s *Server) SetBrandingAssetsHandler(h *httpapi.BrandingAssetsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetUsageHandler sets the usage analytics handler.
func (s *Server) SetUsageHandler(h *httpapi.UsageHandler) { s.handlers = append(s.handlers, h) }

// SetUsageCapsHandler sets usage cap and model pricing handlers.
func (s *Server) SetUsageCapsHandler(h *httpapi.UsageCapsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetBackupHandler sets the system backup handler.
func (s *Server) SetBackupHandler(h *httpapi.BackupHandler) { s.handlers = append(s.handlers, h) }

// SetRestoreHandler sets the system restore handler.
func (s *Server) SetRestoreHandler(h *httpapi.RestoreHandler) { s.handlers = append(s.handlers, h) }

// SetBackupS3Handler sets the S3 backup integration handler.
func (s *Server) SetBackupS3Handler(h *httpapi.BackupS3Handler) { s.handlers = append(s.handlers, h) }

// SetTenantBackupHandler sets the tenant-scoped backup/restore handler.
func (s *Server) SetTenantBackupHandler(h *httpapi.TenantBackupHandler) {
	s.handlers = append(s.handlers, h)
}

// SetDocsHandler sets the OpenAPI spec + Swagger UI handler.
func (s *Server) SetDocsHandler(h *httpapi.DocsHandler) { s.handlers = append(s.handlers, h) }

// SetEditionHandler sets the edition info handler.
func (s *Server) SetEditionHandler(h *httpapi.EditionHandler) { s.handlers = append(s.handlers, h) }

// SetAgentStore sets the agent store for context injection in tools_invoke.
func (s *Server) SetAgentStore(as store.AgentStore) { s.agentStore = as }

// SetMessageBus sets the message bus for MCP bridge media delivery.
func (s *Server) SetMessageBus(mb *bus.MessageBus) { s.msgBus = mb }

// SetSkillStore sets the skill store, used by the CRUD MCP server (see
// internal/mcp/crud_server.go) to expose skill listing/lookup tools.
func (s *Server) SetSkillStore(ss store.SkillStore) { s.skillStore = ss }

// SetCronStore sets the cron store, used by the CRUD MCP server (see
// internal/mcp/crud_server.go) to expose cron job CRUD tools.
func (s *Server) SetCronStore(cs store.CronStore) { s.cronStore = cs }

// SetAgentLinkStore sets the agent link store, used by the CRUD MCP server
// (see internal/mcp/crud_server.go) to expose agent-link CRUD tools.
func (s *Server) SetAgentLinkStore(als store.AgentLinkStore) { s.agentLinkStore = als }

// SetConfigPermissionStore sets the config permission store, used by the
// CRUD MCP server (see internal/mcp/crud_server.go) to expose config
// permission CRUD tools.
func (s *Server) SetConfigPermissionStore(cps store.ConfigPermissionStore) { s.configPermStore = cps }

// SetBitrixPortalStore sets the Bitrix24 portal store, used by the CRUD MCP
// server (see internal/mcp/crud_server.go) to expose Bitrix24 portal CRUD tools.
func (s *Server) SetBitrixPortalStore(bs store.BitrixPortalStore) { s.bitrixStore = bs }

// SetRunTimelineStore sets the run timeline store, used by the CRUD MCP
// server (see internal/mcp/crud_server.go) to expose the run timeline read tool.
func (s *Server) SetRunTimelineStore(rts store.RunTimelineStore) { s.runTimelineStore = rts }

// SetTeamStore sets the team store, used by the CRUD MCP server (see
// internal/mcp/crud_server.go) to expose teams/tasks/workspace CRUD tools.
func (s *Server) SetTeamStore(ts store.TeamStore) { s.teamStore = ts }

// SetChannelInstanceStore sets the channel instance store, used by the CRUD
// MCP server (see internal/mcp/crud_server.go) to expose channel instance
// CRUD tools.
func (s *Server) SetChannelInstanceStore(cis store.ChannelInstanceStore) { s.channelInstStore = cis }

// SetChannelManager sets the channel runtime manager, used by the CRUD MCP
// server (see internal/mcp/crud_server.go) to expose channels.list/status tools.
func (s *Server) SetChannelManager(cm *channels.Manager) { s.channelMgr = cm }

// SetHookStore sets the hook store, used by the CRUD MCP server (see
// internal/mcp/crud_server.go) to expose hooks CRUD tools.
func (s *Server) SetHookStore(hs hooks.HookStore) { s.hookStore = hs }

// SetHeartbeatStore sets the heartbeat store, used by the CRUD MCP server
// (see internal/mcp/crud_server.go) to expose heartbeat CRUD tools.
func (s *Server) SetHeartbeatStore(hb store.HeartbeatStore) { s.heartbeatStore = hb }

// SetProviderStore sets the provider store, used by the CRUD MCP server
// (see internal/mcp/crud_server.go) to resolve provider names in heartbeat.set.
func (s *Server) SetProviderStore(ps store.ProviderStore) { s.providerStore = ps }

// SetTenantStore sets the tenant store, used by the CRUD MCP server (see
// internal/mcp/crud_server.go) to resolve the optional "X-GoClaw-Tenant-Id"
// request header (UUID or slug) to a concrete tenant for every CRUD MCP call.
func (s *Server) SetTenantStore(ts store.TenantStore) { s.tenantStore = ts }

// SetMemoryStore sets the memory store, used by the CRUD MCP server (see
// internal/mcp/crud_server.go) to expose goclaw_memory_* tools.
func (s *Server) SetMemoryStore(ms store.MemoryStore) { s.memoryStore = ms }

// SetKnowledgeGraphStore sets the knowledge graph store, used by the CRUD
// MCP server (see internal/mcp/crud_server.go) to expose goclaw_kg_* tools.
func (s *Server) SetKnowledgeGraphStore(kg store.KnowledgeGraphStore) { s.kgStore = kg }

// SetTracingStore sets the LLM call tracing store, used by the CRUD MCP
// server (see internal/mcp/crud_server.go) to expose goclaw_traces_* tools.
func (s *Server) SetTracingStore(ts store.TracingStore) { s.tracingStore = ts }

// SetContactStore sets the channel contact store, used by the CRUD MCP
// server (see internal/mcp/crud_server.go) to expose goclaw_contacts_* tools.
func (s *Server) SetContactStore(cs store.ContactStore) { s.contactStore = cs }

// SetPendingMessageStore sets the pending-message store, used by the CRUD
// MCP server (see internal/mcp/crud_server.go) to expose
// goclaw_pending_messages_* tools.
func (s *Server) SetPendingMessageStore(pm store.PendingMessageStore) { s.pendingMsgStore = pm }

// SetActivityStore sets the audit-log store, used by the CRUD MCP server
// (see internal/mcp/crud_server.go) to expose goclaw_activity_list.
func (s *Server) SetActivityStore(as store.ActivityStore) { s.activityStore = as }

// SetSystemConfigStore sets the system config store, used by the CRUD MCP
// server (see internal/mcp/crud_server.go) to expose goclaw_system_config_*
// tools.
func (s *Server) SetSystemConfigStore(sc store.SystemConfigStore) { s.systemCfgStore = sc }

// SetSecureCLIStore sets the secure-CLI binary registry store, used by the
// CRUD MCP server (see internal/mcp/crud_server.go) to expose
// goclaw_secure_cli_binaries_* tools.
func (s *Server) SetSecureCLIStore(sc store.SecureCLIStore) { s.secureCLIStore = sc }

// SetExecApprovalManager sets the exec approval manager, used by the CRUD MCP
// server (see internal/mcp/crud_server.go) to expose exec approval tools.
func (s *Server) SetExecApprovalManager(m *tools.ExecApprovalManager) { s.execApprovalMgr = m }

// SetQuotaChecker sets the channel quota checker, used by the CRUD MCP server
// (see internal/mcp/crud_server.go) to expose the quota usage tool.
func (s *Server) SetQuotaChecker(qc *channels.QuotaChecker) { s.quotaChecker = qc }

// SetSQLDB sets the raw *sql.DB handle, used by the CRUD MCP server (see
// internal/mcp/crud_server.go) to query today's quota trace summary. Distinct
// from SetDB's narrow PingContext-only interface used for health checks.
func (s *Server) SetSQLDB(db *sql.DB) { s.sqlDB = db }

// SetLLMProviders sets the provider registry and background provider/model
// fallback used by the CRUD MCP server (see internal/mcp/crud_server.go) to
// expose goclaw_llm_complete, mirroring internal/gateway/methods/llm.go.
func (s *Server) SetLLMProviders(reg *providers.Registry, defaultProvider, defaultModel string) {
	s.llmProviders = reg
	s.llmDefaults = mcpbridge.LLMDefaults{Provider: defaultProvider, Model: defaultModel}
}

// SetVoiceCache sets the shared TTS voice cache and per-tenant secrets store,
// used by the CRUD MCP server (see internal/mcp/crud_server.go) to expose
// goclaw_voices_{list,refresh}, mirroring internal/http/voices.go.
func (s *Server) SetVoiceCache(cache *audio.VoiceCache, secretStore store.ConfigSecretsStore) {
	s.voiceCache = cache
	s.voiceSecretsStore = secretStore
}

// SetWorkstationsHandler sets the workstations CRUD handler (Standard edition only).
func (s *Server) SetWorkstationsHandler(h *httpapi.WorkstationsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetVersion sets the server version for health responses.
func (s *Server) SetVersion(v string) { s.version = v }

// SetDB sets the database connection for health check pings.
func (s *Server) SetDB(db interface{ PingContext(context.Context) error }) { s.db = db }

// StartedAt returns the server start time.
func (s *Server) StartedAt() time.Time { return s.startedAt }

// Version returns the server version string.
func (s *Server) Version() string { return s.version }

// StartUpdateChecker starts a background goroutine that periodically checks
// GitHub for new releases and caches the result for the health endpoint.
func (s *Server) StartUpdateChecker(ctx context.Context) {
	s.updateChecker = NewUpdateChecker(s.version)
	s.updateChecker.Start(ctx)
}

// ClientList returns a snapshot of all connected clients.
func (s *Server) ClientList() []*Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		list = append(list, c)
	}
	return list
}

// BroadcastEvent sends an event to all connected clients.
func (s *Server) BroadcastEvent(event protocol.EventFrame) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		client.SendEvent(event)
	}
}

// DisconnectByPairing force-closes WebSocket connections authenticated via the
// given pairing senderID and channel. Called after revoking a paired device so
// that the revoked client cannot continue operating with its old role.
func (s *Server) DisconnectByPairing(senderID, channel string) {
	s.mu.RLock()
	var targets []*Client
	for _, c := range s.clients {
		if c.pairedSenderID == senderID && c.pairedChannel == channel {
			targets = append(targets, c)
		}
	}
	s.mu.RUnlock()

	for _, c := range targets {
		slog.Info("disconnecting revoked paired device", "client", c.id, "sender_id", senderID, "channel", channel)
		c.conn.Close()
	}
}

func (s *Server) registerClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.id] = c

	// Subscribe to bus events with per-user/team filtering.
	s.eventPub.Subscribe(c.id, func(event bus.Event) {
		if clientCanReceiveEvent(c, event) {
			c.SendEvent(*protocol.NewEvent(event.Name, event.Payload))
		}
	})

	slog.Info("client connected", "id", c.id)
}

func (s *Server) unregisterClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c.id)
	s.eventPub.Unsubscribe(c.id)
	if s.logTee != nil {
		s.logTee.Unsubscribe(c.id)
	}
	slog.Info("client disconnected", "id", c.id)
}

// SetLogTee attaches a LogTee so that disconnecting clients are auto-unsubscribed.
func (s *Server) SetLogTee(lt *LogTee) {
	s.logTee = lt
}

// StartTestServer creates a listener on :0 (random port) and returns the
// actual address and a start function. Used for integration tests.
func StartTestServer(s *Server, ctx context.Context) (addr string, start func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	isManaged := s.agentStore != nil
	chatHandler := httpapi.NewChatCompletionsHandler(s.agents, s.sessions, isManaged)
	if s.rateLimiter.Enabled() {
		chatHandler.SetRateLimiter(s.rateLimiter.Allow)
	}
	if s.postTurn != nil {
		chatHandler.SetPostTurnProcessor(s.postTurn)
	}
	mux.Handle("/v1/chat/completions", chatHandler)

	responsesHandler := httpapi.NewResponsesHandler(s.agents, s.sessions)
	if s.postTurn != nil {
		responsesHandler.SetPostTurnProcessor(s.postTurn)
	}
	mux.Handle("/v1/responses", responsesHandler)

	if s.tools != nil {
		toolsHandler := httpapi.NewToolsInvokeHandler(s.tools, s.agentStore)
		mux.Handle("/v1/tools/invoke", toolsHandler)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic("listen: " + err.Error())
	}

	s.httpServer = &http.Server{Handler: mux}
	addr = ln.Addr().String()

	start = func() {
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s.httpServer.Shutdown(shutdownCtx)
		}()
		s.httpServer.Serve(ln)
	}

	return addr, start
}

// desktopCORS wraps a handler with permissive CORS headers for desktop dev mode.
// Only active when GOCLAW_DESKTOP=1 (set by desktop app.go).
func desktopCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-GoClaw-Tenant-Id, X-GoClaw-User-Id")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
