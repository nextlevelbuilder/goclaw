package gateway

import (
	"context"
	"crypto/subtle"
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
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/webui"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
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
	apiKeyStore    store.APIKeyStore  // for API key auth lookup
	agentStore     store.AgentStore   // for context injection in tools_invoke
	msgBus           *bus.MessageBus        // for MCP bridge media delivery
	builtinToolStore store.BuiltinToolStore // for injecting tool settings into MCP bridge context
	bridgeTraceReg   *mcpbridge.BridgeTraceRegistry // for passing trace context from agent loop to bridge

	upgrader    websocket.Upgrader
	rateLimiter *RateLimiter
	clients     map[string]*Client
	mu          sync.RWMutex

	startedAt      time.Time
	version        string
	db             interface{ PingContext(context.Context) error } // for health check DB ping
	updateChecker  *UpdateChecker

	logTee   *LogTee                  // optional; auto-unsubscribes clients on disconnect
	postTurn tools.PostTurnProcessor // optional; for team task dispatch in HTTP API paths

	httpServer *http.Server
	mux        *http.ServeMux
}

// SetPostTurnProcessor sets the post-turn processor for team task dispatch in HTTP API handlers.
func (s *Server) SetPostTurnProcessor(pt tools.PostTurnProcessor) {
	s.postTurn = pt
}

// NewServer creates a new gateway server.
func NewServer(cfg *config.Config, eventPub bus.EventPublisher, agents *agent.Router, sess store.SessionStore, toolsReg ...*tools.Registry) *Server {
	s := &Server{
		cfg:       cfg,
		eventPub:  eventPub,
		agents:    agents,
		sessions:  sess,
		clients:   make(map[string]*Client),
		startedAt: time.Now(),
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

	// Register all HTTP API handlers (agents, skills, teams, storage, etc.)
	for _, h := range s.handlers {
		if h != nil {
			h.RegisterRoutes(mux)
		}
	}

	// MCP bridge: expose GoClaw tools to Claude CLI via streamable-http.
	// Only listens on localhost (CLI runs on the same machine).
	// Protected by gateway token; disabled when no token is configured to
	// prevent unauthenticated tool invocations if port is exposed.
	if s.tools != nil {
		if s.cfg.Gateway.Token != "" {
			bridgeHandler := mcpbridge.NewBridgeServer(s.tools, "1.0.0", s.msgBus)
			handler := tokenAuthMiddleware(s.cfg.Gateway.Token,
				bridgeContextMiddleware(s.cfg.Gateway.Token, s.agentStore, s.builtinToolStore, s.bridgeTraceReg, bridgeHandler))
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

	// Embedded web UI (built with -tags embedui). Catch-all after all API routes.
	if h := webui.Handler(); h != nil {
		mux.Handle("/", h)
		slog.Info("serving embedded web UI")
	}

	s.mux = mux
	return mux
}

// bridgeContextMiddleware extracts X-Agent-ID, X-User-ID, and X-Workspace headers
// from the MCP bridge request and injects them into the context so bridge tools can
// access agent/user scope and resolve workspace-relative paths.
// When a gateway token is configured, the context headers must be accompanied by
// a valid X-Bridge-Sig HMAC to prevent forgery.
// agentStore is used to lookup the agent key from agent UUID for session key rebuilding.
// bts (optional) loads builtin tool settings so media tools (read_image, etc.)
// can resolve their provider chains when called via the bridge.
// traceReg (optional) looks up trace context from the agent loop so bridge tool
// calls appear as child spans of the LLM call in the trace tree.
func bridgeContextMiddleware(gatewayToken string, agentStore store.AgentStore, bts store.BuiltinToolStore, traceReg *mcpbridge.BridgeTraceRegistry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		agentIDStr := r.Header.Get("X-Agent-ID")
		userID := r.Header.Get("X-User-ID")
		channel := r.Header.Get("X-Channel")
		chatID := r.Header.Get("X-Chat-ID")
		peerKind := r.Header.Get("X-Peer-Kind")
		workspace := r.Header.Get("X-Workspace")

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
			sessionKey := r.Header.Get("X-Session-Key")
			sig := r.Header.Get("X-Bridge-Sig")
			ok, tenantVerified := providers.VerifyBridgeContext(gatewayToken, agentIDStr, userID, channel, chatID, peerKind, workspace, tenantIDStr, sig, sessionKey)
			if !ok {
				slog.Warn("security.mcp_bridge: invalid bridge context signature",
					"agent_id", agentIDStr, "user_id", userID)
				http.Error(w, `{"error":"invalid bridge context signature"}`, http.StatusForbidden)
				return
			}

			// Inject tenant_id first — needed by agentStore.GetByID which is tenant-scoped.
			if tenantVerified && tenantIDStr != "" {
				if tid, err := uuid.Parse(tenantIDStr); err == nil {
					ctx = store.WithTenantID(ctx, tid)
				}
			}
			if agentIDStr != "" {
				if id, err := uuid.Parse(agentIDStr); err == nil {
					ctx = store.WithAgentID(ctx, id)

					// Lookup agent from DB to inject key + per-agent config flags.
					if agentStore != nil {
						if ag, err := agentStore.GetByID(ctx, id); err == nil && ag != nil {
							ctx = store.WithAgentKey(ctx, ag.AgentKey)
							if ag.ParseBrowserUseProxy() {
								ctx = tools.WithBrowserUseProxy(ctx, true)
							}
						}
					}
				}
			}
			// Inject session key from HMAC-verified header.
			if sessionKey != "" {
				ctx = tools.WithToolSessionKey(ctx, sessionKey)
			}
			if userID != "" {
				ctx = store.WithUserID(ctx, userID)
			}
		}

		// Inject agent key so session tools can build session key prefixes.
		// X-Agent-Key is not HMAC-signed (agent UUID already is), but only
		// injected when HMAC-verified context is present.
		if agentKey := r.Header.Get("X-Agent-Key"); agentKey != "" && (agentIDStr != "" || userID != "") {
			ctx = tools.WithToolAgentKey(ctx, agentKey)
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

		// Inject builtin tool settings so media tools (read_image, tts, etc.)
		// can resolve their provider chains via ResolveMediaProviderChain.
		if bts != nil {
			if allTools, err := bts.List(ctx); err == nil {
				settings := make(tools.BuiltinToolSettings, len(allTools))
				for _, t := range allTools {
					if len(t.Settings) > 0 && string(t.Settings) != "{}" {
						settings[t.Name] = []byte(t.Settings)
					}
				}
				if len(settings) > 0 {
					ctx = tools.WithBuiltinToolSettings(ctx, settings)
				}
			}
		}

		// Inject trace context from agent loop so bridge tool spans appear in the trace tree.
		if traceReg != nil && agentIDStr != "" {
			if agentUUID, err := uuid.Parse(agentIDStr); err == nil {
				traceKey := mcpbridge.BridgeTraceKey(agentUUID, channel, peerKind, chatID)
				if tc, ok := traceReg.Lookup(traceKey); ok {
					ctx = tracing.WithTraceID(ctx, tc.TraceID)
					ctx = tracing.WithParentSpanID(ctx, tc.ParentSpanID)
					ctx = tracing.WithCollector(ctx, tc.Collector)
					ctx = mcpbridge.WithBridgeAgentID(ctx, tc.AgentID)
					if tc.TenantID != uuid.Nil {
						ctx = store.WithTenantID(ctx, tc.TenantID)
					}
				}
			}
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

// Start begins listening for WebSocket and HTTP connections.
func (s *Server) Start(ctx context.Context) error {
	mux := s.BuildMux()

	// Wrap with CORS for desktop dev mode (Wails serves frontend on different port).
	var handler http.Handler = mux
	if os.Getenv("GOCLAW_DESKTOP") == "1" {
		handler = desktopCORS(mux)
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.Gateway.Host, s.cfg.Gateway.Port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: handler,
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
func (s *Server) SetMCPHandler(h *httpapi.MCPHandler) { s.handlers = append(s.handlers, h) }
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

// SetPackagesHandler sets the runtime package management handler.
func (s *Server) SetPackagesHandler(h *httpapi.PackagesHandler) {
	s.handlers = append(s.handlers, h)
}

// SetOAuthHandler sets the OAuth handler (available in all modes).
func (s *Server) SetOAuthHandler(h *httpapi.OAuthHandler) { s.handlers = append(s.handlers, h) }

// SetAPIKeysHandler sets the API key management handler.
func (s *Server) SetAPIKeysHandler(h *httpapi.APIKeysHandler) {
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

// SetActivityHandler sets the activity audit log handler.
func (s *Server) SetActivityHandler(h *httpapi.ActivityHandler) {
	s.handlers = append(s.handlers, h)
}

// SetSystemConfigsHandler sets the system configs handler.
func (s *Server) SetSystemConfigsHandler(h *httpapi.SystemConfigsHandler) {
	s.handlers = append(s.handlers, h)
}

// SetUsageHandler sets the usage analytics handler.
func (s *Server) SetUsageHandler(h *httpapi.UsageHandler) { s.handlers = append(s.handlers, h) }

// SetDocsHandler sets the OpenAPI spec + Swagger UI handler.
func (s *Server) SetDocsHandler(h *httpapi.DocsHandler) { s.handlers = append(s.handlers, h) }

// SetEditionHandler sets the edition info handler.
func (s *Server) SetEditionHandler(h *httpapi.EditionHandler) { s.handlers = append(s.handlers, h) }

// SetBrowserLiveHandler sets the browser live view handler.
func (s *Server) SetBrowserLiveHandler(h *httpapi.BrowserLiveHandler) {
	s.handlers = append(s.handlers, h)
}

// SetBrowserProxiesHandler sets the browser proxy pool management handler.
func (s *Server) SetBrowserProxiesHandler(h *httpapi.BrowserProxiesHandler) {
	s.handlers = append(s.handlers, h)
}

// SetAgentStore sets the agent store for context injection in tools_invoke.
func (s *Server) SetAgentStore(as store.AgentStore) { s.agentStore = as }

// SetMessageBus sets the message bus for MCP bridge media delivery.
func (s *Server) SetMessageBus(mb *bus.MessageBus) { s.msgBus = mb }

// SetBuiltinToolStore sets the store for loading builtin tool settings into MCP bridge context.
func (s *Server) SetBuiltinToolStore(bts store.BuiltinToolStore) { s.builtinToolStore = bts }

// SetBridgeTraceRegistry sets the trace registry for passing trace context to bridge tool handlers.
func (s *Server) SetBridgeTraceRegistry(r *mcpbridge.BridgeTraceRegistry) { s.bridgeTraceReg = r }

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
