package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
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
	handlers    map[string]MethodHandler
	server      *Server
	tenantStore store.TenantStore      // optional, for enriching connect response
	permCache   *cache.PermissionCache // optional, for caching tenant membership checks
}

func NewMethodRouter(server *Server) *MethodRouter {
	r := &MethodRouter{
		handlers: make(map[string]MethodHandler),
		server:   server,
	}
	r.registerDefaults()
	return r
}

// SetTenantStore sets the tenant store for enriching connect responses with tenant name/slug.
func (r *MethodRouter) SetTenantStore(ts store.TenantStore) { r.tenantStore = ts }

// SetPermissionCache sets the permission cache for tenant membership checks.
func (r *MethodRouter) SetPermissionCache(pc *cache.PermissionCache) { r.permCache = pc }

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
					i18n.T(locale, i18n.MsgPermissionDenied, req.Method+" requires "+string(permissions.MethodRole(req.Method))+" role"),
				))
				return
			}
		}
	}

	// Inject locale + tenant into context.
	// All connect paths guarantee client.tenantID is set (owner defaults to MasterTenantID).
	ctx = store.WithLocale(ctx, i18n.Normalize(client.locale))
	if client.TenantID() != uuid.Nil {
		ctx = store.WithTenantID(ctx, client.TenantID())
	}
	if slug := client.TenantSlug(); slug != "" {
		ctx = store.WithTenantSlug(ctx, slug)
	}

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
		Token       string `json:"token"`
		UserID      string `json:"user_id"`
		SenderID    string `json:"sender_id"`    // browser pairing: stored sender ID for reconnect
		Locale      string `json:"locale"`       // user's preferred locale (en, vi, zh)
		TenantHint  string `json:"tenant_hint"`  // optional tenant slug for browser pairing multi-tenant
		TenantID    string `json:"tenant_id"`    // cross-tenant admin: narrow scope to specific tenant (UUID or slug)
		TenantScope string `json:"tenant_scope"` // deprecated: alias for tenant_id (backward compat)
		// Node-host fields
		Mode        string   `json:"mode"`
		Role        string   `json:"role"`
		InstanceID  string   `json:"instanceId"`
		DisplayName string   `json:"clientDisplayName"`
		Commands    []string `json:"commands"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	// Set locale on client (persists across all requests for this connection)
	client.locale = i18n.Normalize(params.Locale)

	configToken := r.server.cfg.Gateway.Token

	// Helper: send connect response and register node if applicable.
	sendAndRegisterNode := func(reqID string) {
		r.sendConnectResponse(ctx, client, reqID)
		r.registerNodeIfApplicable(client, params.Mode, params.InstanceID, params.DisplayName, params.Commands)
	}

	// Path 1: Valid gateway token → admin (constant-time comparison)
	if configToken != "" && subtle.ConstantTimeCompare([]byte(params.Token), []byte(configToken)) == 1 {
		client.role = permissions.RoleAdmin
		client.authenticated = true
		client.userID = params.UserID

		// Owner IDs get RoleOwner; others keep RoleAdmin but are scoped to their tenant.
		isOwner := isOwnerID(params.UserID, r.server.cfg.Gateway.OwnerIDs)
		if isOwner {
			client.role = permissions.RoleOwner
			// Owner can narrow scope to a specific tenant via param
			tenantScope := params.TenantID
			if tenantScope == "" {
				tenantScope = params.TenantScope // backward compat
			}
			r.applyTenantScope(ctx, client, tenantScope)
			if client.tenantID == uuid.Nil {
				client.tenantID = store.MasterTenantID
			}
		} else {
			// Non-owner with gateway token: resolve tenant via hint or membership
			hint := params.TenantID
			if hint == "" {
				hint = params.TenantHint
			}
			if hint == "" {
				hint = params.TenantScope // deprecated
			}
			tid, errCode := r.resolveTenantHint(ctx, hint, params.UserID)
			if errCode != "" {
				client.SendResponse(protocol.NewErrorResponse(req.ID, errCode, "tenant access revoked"))
				return
			}
			client.tenantID = tid
		}
		sendAndRegisterNode(req.ID)
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
			// If the key has a bound owner, force user_id to owner_id.
			if keyData.OwnerID != "" {
				if params.UserID != "" && params.UserID != keyData.OwnerID {
					slog.Warn("security.ws_api_key_owner_override",
						"param_user_id", params.UserID,
						"owner_id", keyData.OwnerID,
					)
				}
				client.userID = keyData.OwnerID
			} else {
				client.userID = params.UserID
			}
			if keyData.TenantID == uuid.Nil {
				// System-level API keys keep their scope-derived role and may
				// optionally narrow to a tenant without gaining owner privileges.
				apiKeyScope := params.TenantID
				if apiKeyScope == "" {
					apiKeyScope = params.TenantScope // backward compat
				}
				r.applyTenantScope(ctx, client, apiKeyScope)
				if client.tenantID == uuid.Nil {
					client.tenantID = store.MasterTenantID
				}
				slog.Debug("security.ws_connect_resolved",
					"client", client.id,
					"role", string(client.role),
					"tenant_id", client.tenantID.String(),
				)
			} else {
				client.tenantID = keyData.TenantID
				slog.Debug("security.ws_connect_resolved",
					"client", client.id,
					"role", string(client.role),
					"tenant_id", client.tenantID.String(),
				)
			}
			sendAndRegisterNode(req.ID)
			return
		}
	}

	// Path 2: No token configured → operator (backward compat)
	if configToken == "" {
		client.role = permissions.RoleOperator
		client.authenticated = true
		client.userID = params.UserID
		client.tenantID = store.MasterTenantID
		sendAndRegisterNode(req.ID)
		return
	}

	// Path 3: Token configured but not provided/wrong → check browser pairing
	ps := r.server.pairingService

	// Path 3a: Reconnecting with a previously-paired sender_id
	if ps != nil && params.SenderID != "" {
		paired, pairErr := ps.IsPaired(ctx, params.SenderID, "browser")
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
			tid, errCode := r.resolveTenantHint(ctx, params.TenantHint, params.UserID)
			if errCode != "" {
				client.SendResponse(protocol.NewErrorResponse(req.ID, errCode, "tenant access revoked"))
				return
			}
			client.tenantID = tid
			slog.Info("browser pairing authenticated", "sender_id", params.SenderID, "client", client.id, "tenant_id", client.tenantID)
			sendAndRegisterNode(req.ID)
			return
		}
	}

	// Path 3b: No token, no valid pairing → initiate browser pairing (if service available)
	if ps != nil && params.Token == "" {
		code, err := ps.RequestPairing(ctx, client.id, "browser", "", "default", nil)
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
					"version": r.server.version,
				},
			}))
			return
		}
	}

	// Path 4: Fallback → viewer (wrong token or pairing not available)
	client.role = permissions.RoleViewer
	client.authenticated = true
	client.userID = params.UserID
	tid, errCode := r.resolveTenantHint(ctx, params.TenantHint, params.UserID)
	if errCode != "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, errCode, "tenant access revoked"))
		return
	}
	client.tenantID = tid
	sendAndRegisterNode(req.ID)
}

// registerNodeIfApplicable checks if this connect is from a node-host and registers it.
// Only admin/owner roles may register as nodes to prevent unauthorized command execution.
func (r *MethodRouter) registerNodeIfApplicable(client *Client, mode, instanceID, displayName string, commands []string) {
	if mode != "node" || r.server.nodeRegistry == nil {
		return
	}
	if !permissions.HasMinRole(client.role, permissions.RoleAdmin) {
		slog.Warn("security.node_registration_denied", "role", client.role, "client", client.id)
		return
	}
	nodeID := instanceID
	if nodeID == "" {
		nodeID = client.id
	}
	r.server.nodeRegistry.Register(client, nodeID, displayName, commands)
	slog.Info("node-host registered", "nodeId", nodeID, "displayName", displayName, "commands", commands, "client", client.id)
}

func (r *MethodRouter) sendConnectResponse(ctx context.Context, client *Client, reqID string) {
	resp := map[string]any{
		"protocol":  protocol.ProtocolVersion,
		"role":      string(client.role),
		"user_id":   client.userID,
		"tenant_id": client.tenantID.String(),
		"is_owner":  client.IsOwner(),
		"server": map[string]any{
			"name":    "goclaw",
			"version": r.server.version,
		},
	}

	// Enrich with tenant name/slug if tenant store available and tenant is set
	if r.tenantStore != nil && client.tenantID != uuid.Nil {
		if t, err := r.tenantStore.GetTenant(ctx, client.tenantID); err == nil && t != nil {
			resp["tenant_name"] = t.Name
			resp["tenant_slug"] = t.Slug
			client.tenantName = t.Name
			client.tenantSlug = t.Slug
		}
	}

	client.SendResponse(protocol.NewOKResponse(reqID, resp))
}

// isOwnerID checks if the given user ID is in the configured owner list.
// If no owner IDs configured, only "system" is treated as owner (fail-closed).
func isOwnerID(userID string, ownerIDs []string) bool {
	if userID == "" {
		return false
	}
	if len(ownerIDs) == 0 {
		return userID == "system"
	}
	return slices.Contains(ownerIDs, userID)
}

// resolveTenantHint resolves a tenant slug/UUID hint to a UUID with membership validation.
// Non-admin users must be a member of the tenant.
// Returns (uuid.Nil, ErrTenantAccessRevoked) when the user is not a member of the requested tenant.
// Returns (MasterTenantID, "") when no hint is provided.
func (r *MethodRouter) resolveTenantHint(ctx context.Context, hint, userID string) (uuid.UUID, string) {
	if hint == "" || r.tenantStore == nil {
		return store.MasterTenantID, ""
	}
	t, err := r.tenantStore.GetTenantBySlug(ctx, hint)
	if err != nil || t == nil {
		slog.Debug("tenant_hint not resolved, falling back to master", "hint", hint)
		return store.MasterTenantID, ""
	}

	// Validate membership: user must belong to the requested tenant.
	// Deny tenant access for anonymous users (no userID) — fail-closed.
	if userID == "" {
		slog.Warn("security.tenant_hint_denied_anonymous", "hint", hint, "tenant_id", t.ID)
		return uuid.Nil, protocol.ErrTenantAccessRevoked
	}
	role, err := r.getUserTenantRole(ctx, t.ID, userID)
	if err != nil || role == "" {
		slog.Warn("security.tenant_access_revoked",
			"hint", hint, "user", userID, "tenant_id", t.ID, "error", err)
		return uuid.Nil, protocol.ErrTenantAccessRevoked
	}
	return t.ID, ""
}

// getUserTenantRole returns the user's role in a tenant, using permission cache if available.
func (r *MethodRouter) getUserTenantRole(ctx context.Context, tenantID uuid.UUID, userID string) (string, error) {
	// Check cache first
	if r.permCache != nil {
		if role, ok := r.permCache.GetTenantRole(ctx, tenantID, userID); ok {
			slog.Debug("perm_cache.tenant_role.hit", "tenant", tenantID, "user", userID, "role", role)
			return role, nil
		}
		slog.Debug("perm_cache.tenant_role.miss", "tenant", tenantID, "user", userID)
	}

	// Fallback to DB
	role, err := r.tenantStore.GetUserRole(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}

	// Cache the result (including empty role = not a member)
	if r.permCache != nil {
		r.permCache.SetTenantRole(ctx, tenantID, userID, role)
	}
	return role, nil
}

// applyTenantScope narrows an owner client's data scope to a specific tenant.
// Sets client.tenantID so the router injects WithTenantID for data filtering.
// Accepts both UUID and slug values.
func (r *MethodRouter) applyTenantScope(ctx context.Context, client *Client, tenantVal string) {
	if tenantVal == "" || r.tenantStore == nil {
		return
	}
	var t *store.TenantData
	var err error
	if tid, parseErr := uuid.Parse(tenantVal); parseErr == nil {
		t, err = r.tenantStore.GetTenant(ctx, tid)
	} else {
		t, err = r.tenantStore.GetTenantBySlug(ctx, tenantVal)
	}
	if err != nil || t == nil {
		slog.Debug("tenant scope not resolved, keeping master", "value", tenantVal)
		return
	}
	client.tenantID = t.ID
	slog.Info("tenant scope applied", "client", client.id, "tenant", t.Slug, "tenant_id", t.ID)
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

	resp := map[string]any{
		"status":    "ok",
		"version":   s.version,
		"uptime":    uptimeMs,
		"mode":      mode,
		"database":  dbStatus,
		"tools":     toolCount,
		"clients":   clientList,
		"currentId": client.ID(),
	}
	if s.updateChecker != nil {
		if info := s.updateChecker.Info(); info != nil {
			resp["latestVersion"] = info.LatestVersion
			resp["updateAvailable"] = info.UpdateAvailable
			resp["updateUrl"] = info.UpdateURL
			resp["releaseNotes"] = info.ReleaseNotes
		}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, resp))
}

func (r *MethodRouter) handleStatus(ctx context.Context, client *Client, req *protocol.RequestFrame) {
	agents := r.server.agents.ListInfo()

	sessionCount := 0
	if r.server.sessions != nil {
		sessionCount = len(r.server.sessions.List(ctx, ""))
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
