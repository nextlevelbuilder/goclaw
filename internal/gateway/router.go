package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"time"

	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// MethodHandler processes a single RPC method request.
type MethodHandler func(ctx context.Context, client *Client, req *protocol.RequestFrame)

// MethodRouter maps method names to handlers.
type MethodRouter struct {
	handlers map[string]MethodHandler
	server   *Server
}

func NewMethodRouter(server *Server) *MethodRouter {
	r := &MethodRouter{
		handlers: make(map[string]MethodHandler),
		server:   server,
	}
	r.registerDefaults()
	return r
}

// Register adds a method handler.
func (r *MethodRouter) Register(method string, handler MethodHandler) {
	r.handlers[method] = handler
}

// Handle dispatches a request to the appropriate handler.
func (r *MethodRouter) Handle(ctx context.Context, client *Client, req *protocol.RequestFrame) {
	handler, ok := r.handlers[req.Method]
	if !ok {
		slog.Warn("unknown method", "method", req.Method, "client", client.id)
		locale := i18n.Normalize(client.locale)
		client.SendResponse(protocol.NewErrorResponse(
			req.ID,
			protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgUnknownMethod, req.Method),
		))
		return
	}

	// Permission check: skip for connect, health, and browser pairing status (used by unauthenticated clients)
	if req.Method != protocol.MethodConnect && req.Method != protocol.MethodHealth && req.Method != protocol.MethodBrowserPairingStatus {
		if pe := r.server.policyEngine; pe != nil {
			if !pe.CanAccess(client.role, req.Method) {
				slog.Warn("permission denied", "method", req.Method, "role", client.role, "client", client.id)
				locale := i18n.Normalize(client.locale)
				client.SendResponse(protocol.NewErrorResponse(
					req.ID,
					protocol.ErrUnauthorized,
					i18n.T(locale, i18n.MsgPermissionDenied, req.Method),
				))
				return
			}
		}
	}

	// Inject locale into context for i18n support
	ctx = store.WithLocale(ctx, i18n.Normalize(client.locale))

	slog.Debug("handling method", "method", req.Method, "client", client.id, "req_id", req.ID)
	handler(ctx, client, req)
}

// registerDefaults registers built-in Phase 1 method handlers.
func (r *MethodRouter) registerDefaults() {
	// System
	r.Register(protocol.MethodConnect, r.handleConnect)
	r.Register(protocol.MethodHealth, r.handleHealth)
	r.Register(protocol.MethodStatus, r.handleStatus)
}

// --- Built-in handlers ---

func (r *MethodRouter) handleConnect(ctx context.Context, client *Client, req *protocol.RequestFrame) {
	// Parse connect params
	var params struct {
		Token    string `json:"token"`
		UserID   string `json:"user_id"`
		SenderID string `json:"sender_id"` // browser pairing: stored sender ID for reconnect
		Locale   string `json:"locale"`    // user's preferred locale (en, vi, zh)
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	// Set locale on client (persists across all requests for this connection)
	client.locale = i18n.Normalize(params.Locale)

	configToken := r.server.cfg.Gateway.Token

	// Path 1: Valid gateway token → admin (constant-time comparison)
	if configToken != "" && subtle.ConstantTimeCompare([]byte(params.Token), []byte(configToken)) == 1 {
		client.role = permissions.RoleAdmin
		client.authenticated = true
		client.userID = params.UserID
		r.sendConnectResponse(client, req.ID)
		return
	}

	// Path 1b: API key → role derived from scopes (uses shared cache)
	if params.Token != "" {
		if keyData, role := httpapi.ResolveAPIKey(ctx, params.Token); keyData != nil {
			scopes := make([]permissions.Scope, len(keyData.Scopes))
			for i, s := range keyData.Scopes {
				scopes[i] = permissions.Scope(s)
			}
			client.role = role
			client.scopes = scopes
			client.authenticated = true
			client.userID = params.UserID
			r.sendConnectResponse(client, req.ID)
			return
		}
	}

	// Path 2: No token configured → operator (backward compat)
	if configToken == "" {
		client.role = permissions.RoleOperator
		client.authenticated = true
		client.userID = params.UserID
		r.sendConnectResponse(client, req.ID)
		return
	}

	// Path 3: Token configured but not provided/wrong → check browser pairing
	ps := r.server.pairingService

	// Path 3a: Reconnecting with a previously-paired sender_id
	if ps != nil && params.SenderID != "" {
		paired, pairErr := ps.IsPaired(params.SenderID, "browser")
		if pairErr != nil {
			slog.Warn("security.pairing_check_failed",
				"sender_id", params.SenderID, "error", pairErr)
			// Fail-closed: deny access on DB error instead of granting operator role.
			locale := i18n.Normalize(client.locale)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, pairErr.Error())))
			return
		}
		if paired {
			client.role = permissions.RoleOperator
			client.authenticated = true
			client.userID = params.UserID
			client.pairedSenderID = params.SenderID
			client.pairedChannel = "browser"
			slog.Info("browser pairing authenticated", "sender_id", params.SenderID, "client", client.id)
			r.sendConnectResponse(client, req.ID)
			return
		}
	}

	// Path 3b: No token, no valid pairing → initiate browser pairing (if service available)
	if ps != nil && params.Token == "" {
		code, err := ps.RequestPairing(client.id, "browser", "", "default", nil)
		if err != nil {
			slog.Warn("browser pairing request failed", "error", err, "client", client.id)
			// Fall through to viewer role
		} else {
			client.pairingCode = code
			client.pairingPending = true
			// Not authenticated — can only call browser.pairing.status
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
				"protocol":     protocol.ProtocolVersion,
				"status":       "pending_pairing",
				"pairing_code": code,
				"sender_id":    client.id,
				"server": map[string]any{
					"name":    "goclaw",
					"version": "0.2.0",
				},
			}))
			return
		}
	}

	// Path 4: Fallback → viewer (wrong token or pairing not available)
	client.role = permissions.RoleViewer
	client.authenticated = true
	client.userID = params.UserID
	r.sendConnectResponse(client, req.ID)
}

func (r *MethodRouter) sendConnectResponse(client *Client, reqID string) {
	client.SendResponse(protocol.NewOKResponse(reqID, map[string]any{
		"protocol": protocol.ProtocolVersion,
		"role":     string(client.role),
		"user_id":  client.userID,
		"server": map[string]any{
			"name":    "goclaw",
			"version": "0.2.0",
		},
	}))
}

func (r *MethodRouter) handleHealth(ctx context.Context, client *Client, req *protocol.RequestFrame) {
	s := r.server
	uptimeMs := time.Since(s.startedAt).Milliseconds()

	mode := "managed"

	// Database status (real ping)
	dbStatus := "n/a"
	if s.db != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := s.db.PingContext(pingCtx); err != nil {
			dbStatus = "error"
		} else {
			dbStatus = "ok"
		}
	}

	// Connected clients list
	type clientInfo struct {
		ID          string `json:"id"`
		RemoteAddr  string `json:"remoteAddr"`
		UserID      string `json:"userId"`
		Role        string `json:"role"`
		ConnectedAt string `json:"connectedAt"`
	}
	clients := s.ClientList()
	clientList := make([]clientInfo, 0, len(clients))
	for _, c := range clients {
		clientList = append(clientList, clientInfo{
			ID:          c.ID(),
			RemoteAddr:  c.RemoteAddr(),
			UserID:      c.UserID(),
			Role:        string(c.Role()),
			ConnectedAt: c.ConnectedAt().UTC().Format(time.RFC3339),
		})
	}

	// Tool count
	toolCount := 0
	if s.tools != nil {
		toolCount = s.tools.Count()
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"status":    "ok",
		"version":   s.version,
		"uptime":    uptimeMs,
		"mode":      mode,
		"database":  dbStatus,
		"tools":     toolCount,
		"clients":   clientList,
		"currentId": client.ID(),
	}))
}

func (r *MethodRouter) handleStatus(ctx context.Context, client *Client, req *protocol.RequestFrame) {
	agents := r.server.agents.ListInfo()

	sessionCount := 0
	if r.server.sessions != nil {
		sessionCount = len(r.server.sessions.List(""))
	}

	// Agents are lazily resolved — router only has loaded agents.
	// Query the DB store for the real total count.
	agentTotal := len(agents)
	if r.server.agentStore != nil {
		if dbAgents, err := r.server.agentStore.List(ctx, ""); err == nil && len(dbAgents) > agentTotal {
			agentTotal = len(dbAgents)
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"agents":     agents,
		"agentTotal": agentTotal,
		"clients":    len(r.server.clients),
		"sessions":   sessionCount,
	}))
}
