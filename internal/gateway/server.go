package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Server is the main gateway server handling WebSocket and HTTP connections.
type Server struct {
	cfg      *config.Config
	eventPub bus.EventPublisher
	agents   *agent.Router
	sessions store.SessionStore
	tools    *tools.Registry
	router   *MethodRouter

	policyEngine   *permissions.PolicyEngine
	pairingService store.PairingStore
	agentsHandler  *httpapi.AgentsHandler // managed mode: agent CRUD API
	skillsHandler  *httpapi.SkillsHandler // managed mode: skill management API
	tracesHandler  *httpapi.TracesHandler // managed mode: LLM trace listing API
	mcpHandler         *httpapi.MCPHandler         // managed mode: MCP server management API
	customToolsHandler      *httpapi.CustomToolsHandler      // managed mode: custom tool CRUD API
	channelInstancesHandler *httpapi.ChannelInstancesHandler // managed mode: channel instance CRUD API
	providersHandler        *httpapi.ProvidersHandler        // managed mode: provider CRUD API
	delegationsHandler      *httpapi.DelegationsHandler      // managed mode: delegation history API
	builtinToolsHandler     *httpapi.BuiltinToolsHandler     // managed mode: builtin tool management API
	agentStore         store.AgentStore             // managed mode: for context injection in tools_invoke

	upgrader    websocket.Upgrader
	rateLimiter *RateLimiter
	clients     map[string]*Client
	mu          sync.RWMutex

	startedAt time.Time
	version   string
	db        interface{ PingContext(context.Context) error } // for health check DB ping

	httpServer *http.Server
	mux        *http.ServeMux
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
	chatHandler := httpapi.NewChatCompletionsHandler(s.agents, s.sessions, s.cfg.Gateway.Token, isManaged)
	if s.rateLimiter.Enabled() {
		chatHandler.SetRateLimiter(s.rateLimiter.Allow)
	}
	mux.Handle("/v1/chat/completions", chatHandler)

	// OpenResponses protocol
	responsesHandler := httpapi.NewResponsesHandler(s.agents, s.sessions, s.cfg.Gateway.Token)
	mux.Handle("/v1/responses", responsesHandler)

	// Direct tool invocation
	if s.tools != nil {
		toolsHandler := httpapi.NewToolsInvokeHandler(s.tools, s.cfg.Gateway.Token, s.agentStore)
		mux.Handle("/v1/tools/invoke", toolsHandler)
	}

	// Managed mode: agent CRUD + shares API
	if s.agentsHandler != nil {
		s.agentsHandler.RegisterRoutes(mux)
	}

	// Managed mode: skill management API
	if s.skillsHandler != nil {
		s.skillsHandler.RegisterRoutes(mux)
	}

	// Managed mode: LLM trace listing API
	if s.tracesHandler != nil {
		s.tracesHandler.RegisterRoutes(mux)
	}

	// Managed mode: MCP server management API
	if s.mcpHandler != nil {
		s.mcpHandler.RegisterRoutes(mux)
	}

	// Managed mode: custom tool CRUD API
	if s.customToolsHandler != nil {
		s.customToolsHandler.RegisterRoutes(mux)
	}

	// Managed mode: channel instance CRUD API
	if s.channelInstancesHandler != nil {
		s.channelInstancesHandler.RegisterRoutes(mux)
	}

	// Managed mode: provider & model CRUD API
	if s.providersHandler != nil {
		s.providersHandler.RegisterRoutes(mux)
	}

	// Managed mode: delegation history API
	if s.delegationsHandler != nil {
		s.delegationsHandler.RegisterRoutes(mux)
	}

	// Managed mode: builtin tool management API
	if s.builtinToolsHandler != nil {
		s.builtinToolsHandler.RegisterRoutes(mux)
	}

	s.mux = mux
	return mux
}

// Start begins listening for WebSocket and HTTP connections.
func (s *Server) Start(ctx context.Context) error {
	mux := s.BuildMux()

	addr := fmt.Sprintf("%s:%d", s.cfg.Gateway.Host, s.cfg.Gateway.Port)
	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	slog.Info("gateway starting", "addr", addr)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
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

// SetAgentsHandler sets the managed-mode agent CRUD handler.
func (s *Server) SetAgentsHandler(h *httpapi.AgentsHandler) { s.agentsHandler = h }

// SetSkillsHandler sets the managed-mode skill management handler.
func (s *Server) SetSkillsHandler(h *httpapi.SkillsHandler) { s.skillsHandler = h }

// SetTracesHandler sets the managed-mode LLM trace listing handler.
func (s *Server) SetTracesHandler(h *httpapi.TracesHandler) { s.tracesHandler = h }

// SetMCPHandler sets the managed-mode MCP server management handler.
func (s *Server) SetMCPHandler(h *httpapi.MCPHandler) { s.mcpHandler = h }

// SetCustomToolsHandler sets the managed-mode custom tool CRUD handler.
func (s *Server) SetCustomToolsHandler(h *httpapi.CustomToolsHandler) { s.customToolsHandler = h }

// SetChannelInstancesHandler sets the managed-mode channel instance CRUD handler.
func (s *Server) SetChannelInstancesHandler(h *httpapi.ChannelInstancesHandler) {
	s.channelInstancesHandler = h
}

// SetProvidersHandler sets the managed-mode provider CRUD handler.
func (s *Server) SetProvidersHandler(h *httpapi.ProvidersHandler) { s.providersHandler = h }

// SetDelegationsHandler sets the managed-mode delegation history handler.
func (s *Server) SetDelegationsHandler(h *httpapi.DelegationsHandler) { s.delegationsHandler = h }

// SetBuiltinToolsHandler sets the managed-mode builtin tool management handler.
func (s *Server) SetBuiltinToolsHandler(h *httpapi.BuiltinToolsHandler) {
	s.builtinToolsHandler = h
}

// SetAgentStore sets the agent store for context injection in tools_invoke.
func (s *Server) SetAgentStore(as store.AgentStore) { s.agentStore = as }

// SetVersion sets the server version for health responses.
func (s *Server) SetVersion(v string) { s.version = v }

// SetDB sets the database connection for health check pings.
func (s *Server) SetDB(db interface{ PingContext(context.Context) error }) { s.db = db }

// StartedAt returns the server start time.
func (s *Server) StartedAt() time.Time { return s.startedAt }

// Version returns the server version string.
func (s *Server) Version() string { return s.version }

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

func (s *Server) registerClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c.id] = c

	// Subscribe to bus events for this client (skip internal cache events)
	s.eventPub.Subscribe(c.id, func(event bus.Event) {
		if strings.HasPrefix(event.Name, "cache.") {
			return // internal event, don't forward to WS clients
		}
		c.SendEvent(*protocol.NewEvent(event.Name, event.Payload))
	})

	slog.Info("client connected", "id", c.id)
}

func (s *Server) unregisterClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c.id)
	s.eventPub.Unsubscribe(c.id)
	slog.Info("client disconnected", "id", c.id)
}

// StartTestServer creates a listener on :0 (random port) and returns the
// actual address and a start function. Used for integration tests.
func StartTestServer(s *Server, ctx context.Context) (addr string, start func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	isManaged := s.agentStore != nil
	chatHandler := httpapi.NewChatCompletionsHandler(s.agents, s.sessions, s.cfg.Gateway.Token, isManaged)
	if s.rateLimiter.Enabled() {
		chatHandler.SetRateLimiter(s.rateLimiter.Allow)
	}
	mux.Handle("/v1/chat/completions", chatHandler)

	responsesHandler := httpapi.NewResponsesHandler(s.agents, s.sessions, s.cfg.Gateway.Token)
	mux.Handle("/v1/responses", responsesHandler)

	if s.tools != nil {
		toolsHandler := httpapi.NewToolsInvokeHandler(s.tools, s.cfg.Gateway.Token, s.agentStore)
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
