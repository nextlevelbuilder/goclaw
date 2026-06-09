package http

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	mcpoauth "github.com/nextlevelbuilder/goclaw/internal/mcp/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// MCPOAuthHandler exposes OAuth endpoints for MCP server authentication.
type MCPOAuthHandler struct {
	mcpStore   store.MCPServerStore
	oauthStore store.MCPOAuthTokenStore
	discoverer *mcpoauth.Discoverer
	flowMgr    *mcpoauth.FlowManager
	refresher  *mcpoauth.Refresher
	eventBus   bus.EventPublisher
	publicURL  string // e.g. "https://goclaw.example.com"
	port       int
	evictor    MCPPoolEvictor
}

// MCPOAuthHandlerDeps contains all dependencies for the OAuth handler.
type MCPOAuthHandlerDeps struct {
	MCPStore   store.MCPServerStore
	OAuthStore store.MCPOAuthTokenStore
	Discoverer *mcpoauth.Discoverer
	FlowMgr    *mcpoauth.FlowManager
	Refresher  *mcpoauth.Refresher
	EventBus   bus.EventPublisher
	PublicURL  string
	Port       int
	Evictor    MCPPoolEvictor
}

// NewMCPOAuthHandler creates an MCPOAuthHandler.
func NewMCPOAuthHandler(deps MCPOAuthHandlerDeps) *MCPOAuthHandler {
	return &MCPOAuthHandler{
		mcpStore:   deps.MCPStore,
		oauthStore: deps.OAuthStore,
		discoverer: deps.Discoverer,
		flowMgr:    deps.FlowMgr,
		refresher:  deps.Refresher,
		eventBus:   deps.EventBus,
		publicURL:  deps.PublicURL,
		port:       deps.Port,
		evictor:    deps.Evictor,
	}
}

// SetEvictor sets the MCP pool evictor (called after construction when pool is available).
func (h *MCPOAuthHandler) SetEvictor(e MCPPoolEvictor) { h.evictor = e }

// RegisterRoutes registers all MCP OAuth routes on the given mux.
func (h *MCPOAuthHandler) RegisterRoutes(mux *http.ServeMux) {
	// The whole OAuth surface is admin-managed: handleStart mints tokens into the
	// tenant-scoped mcp_oauth_tokens table, so status (reads client_id/issuer/expiry
	// and probes other users' tokens via ?user_id=) and revoke (deletes the global
	// or any per-user token) must require the same RoleAdmin bar. Without it an
	// operator could revoke the tenant-global token and a viewer could enumerate
	// other users' OAuth state. Tenant isolation itself is enforced by the
	// WHERE tenant_id=$N clause in the store.
	mux.HandleFunc("POST /v1/mcp/oauth/start", requireAuth(permissions.RoleAdmin, h.handleStart))
	mux.HandleFunc("GET /v1/mcp/oauth/callback", h.handleCallback)
	mux.HandleFunc("GET /v1/mcp/oauth/status/{id}", requireAuth(permissions.RoleAdmin, h.handleStatus))
	mux.HandleFunc("DELETE /v1/mcp/oauth/token/{id}", requireAuth(permissions.RoleAdmin, h.handleRevoke))
	mux.HandleFunc("POST /v1/mcp/oauth/discover/{id}", requireAuth(permissions.RoleAdmin, h.handleDiscover))
}

// callbackURL derives the OAuth redirect URI from the incoming request.
//
// Priority:
//  1. Config public_url  — explicit admin override (wins when set)
//  2. X-Forwarded-Proto + X-Forwarded-Host  — nginx / reverse proxy
//  3. r.Host  — the backend's actual host:port (correct for local dev even when
//     the frontend runs on a different port than the backend)
//  4. localhost:port  — final fallback
//
// Using r.Host instead of the Origin header is intentional: Origin carries the
// *frontend* origin (e.g. Vite dev server on :5173), which differs from the
// backend address that OAuth providers must redirect back to.
func (h *MCPOAuthHandler) callbackURL(r *http.Request) string {
	const path = "/v1/mcp/oauth/callback"

	// 1. Explicit config override (highest priority).
	if h.publicURL != "" {
		return strings.TrimRight(h.publicURL, "/") + path
	}

	// 2. Nginx / reverse-proxy forwarded headers.
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		proto := r.Header.Get("X-Forwarded-Proto")
		if proto == "" {
			proto = "https"
		}
		return proto + "://" + strings.TrimRight(fwdHost, "/") + path
	}

	// 3. Backend's own host:port from the request (works for localhost:18790 and
	//    any custom host — correct even in split-port dev setups).
	if host := r.Host; host != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		return scheme + "://" + host + path
	}

	// 4. Fallback: localhost + configured port.
	port := h.port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("http://localhost:%d%s", port, path)
}

// --- POST /v1/mcp/oauth/start ---

type startOAuthReq struct {
	ServerID string `json:"server_id"`
	MCPURL   string `json:"mcp_url"`
	UserID   string `json:"user_id,omitempty"` // empty = global token
}

type startOAuthResp struct {
	AuthURL   string `json:"auth_url"`
	State     string `json:"state"`
	ClientID  string `json:"client_id"`
	Issuer    string `json:"issuer"`
	Completed bool   `json:"completed,omitempty"` // true for client_credentials — token already minted, no redirect needed
}

func (h *MCPOAuthHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startOAuthReq
	if !bindJSON(w, r, store.LocaleFromContext(r.Context()), &req) {
		return
	}
	if req.ServerID == "" || req.MCPURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "server_id and mcp_url are required"})
		return
	}

	serverID, err := uuid.Parse(req.ServerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server_id"})
		return
	}

	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	// Verify server belongs to tenant.
	srv, err := h.mcpStore.GetServer(ctx, serverID)
	if err != nil || srv == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP server not found"})
		return
	}

	// Discovery and the resulting resource indicator must bind to the server's
	// own registered URL, not a client-supplied one. Trusting req.MCPURL would
	// let the caller mint a token whose resource_uri / AS differ from the server
	// the agent actually connects to. Fall back to req.MCPURL only when the
	// server has no stored URL (legacy rows).
	mcpURL := srv.URL
	if mcpURL == "" {
		mcpURL = req.MCPURL
	}

	// Security: validate MCP URL before discovery.
	safeClient := security.NewSafeClient(15 * time.Second)
	// Reuse the shared, cache-backed discoverer when wired (5-min metadata cache);
	// fall back to a request-local one only when none was injected (e.g. tests).
	discoverer := h.discoverer
	if discoverer == nil {
		discoverer = mcpoauth.NewDiscoverer(safeClient)
	}
	disc, err := discoverer.Discover(ctx, mcpURL)
	if err != nil {
		slog.Warn("mcpoauth.discover_failed", "server_id", serverID, "url", mcpURL, "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "OAuth discovery failed: " + err.Error()})
		return
	}

	callbackURI := h.callbackURL(r)

	// Parse server settings — client_id/secret, scope, grant type.
	var oauthSettings struct {
		OAuth struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			Scope        string `json:"scope"`
			GrantType    string `json:"grant_type"`
		} `json:"oauth"`
	}
	if len(srv.Settings) > 0 {
		_ = json.Unmarshal(srv.Settings, &oauthSettings)
	}

	// Determine client credentials — priority:
	// 1. Manual client_id in server settings (DCR OFF): use as-is.
	// 2. DCR: always register fresh — avoids reusing stale credentials when MCP URL changes.
	// 3. Fallback: existing token credentials (legacy / no-DCR servers without manual config).
	clientID := oauthSettings.OAuth.ClientID
	clientSecret := oauthSettings.OAuth.ClientSecret

	if clientID == "" {
		if disc.RegistrationEndpoint != "" {
			dcrResp, err2 := mcpoauth.RegisterClient(ctx, safeClient, disc.RegistrationEndpoint, callbackURI)
			if err2 != nil {
				slog.Warn("mcpoauth.dcr_failed", "server_id", serverID, "error", err2)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Dynamic Client Registration failed: " + err2.Error()})
				return
			}
			clientID = dcrResp.ClientID
			clientSecret = dcrResp.ClientSecret
		} else {
			// No DCR endpoint — fall back to existing token's credentials if available.
			var existingTok *store.MCPOAuthToken
			if req.UserID == "" {
				existingTok, _ = h.oauthStore.GetOAuthToken(ctx, serverID, tenantID)
			} else {
				existingTok, _ = h.oauthStore.GetUserOAuthToken(ctx, serverID, tenantID, req.UserID)
			}
			if existingTok != nil {
				clientID = existingTok.DCRClientID
				clientSecret = existingTok.DCRClientSecret
			}
		}
	}

	if clientID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no client_id: DCR not supported and no manual credentials configured"})
		return
	}

	// Scope: use manual setting, fall back to AS-advertised scopes when DCR is used.
	scopes := oauthSettings.OAuth.Scope
	if scopes == "" && len(disc.ScopesSupported) > 0 {
		scopes = strings.Join(disc.ScopesSupported, " ")
	}

	// client_credentials is a non-interactive grant: there is no browser redirect.
	// Obtain the token server-side now and persist it, returning Completed=true so
	// the UI skips the popup. Routing it through StartFlow (authorization-code) would
	// produce a bogus response_type=code authorization URL.
	if oauthSettings.OAuth.GrantType == "client_credentials" {
		if clientSecret == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_credentials grant requires a client_secret"})
			return
		}
		tokens, ccErr := h.flowMgr.ClientCredentials(ctx, disc.TokenEndpoint, clientID, clientSecret, scopes, disc.ResourceURI)
		if ccErr != nil {
			slog.Warn("mcpoauth.client_credentials_failed", "server_id", serverID, "error", ccErr)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "client credentials grant failed: " + ccErr.Error()})
			return
		}
		now := time.Now()
		var expiresAt *time.Time
		if tokens.ExpiresIn > 0 {
			t := now.Add(time.Duration(tokens.ExpiresIn) * time.Second)
			expiresAt = &t
		}
		tok := &store.MCPOAuthToken{
			ServerID:        serverID,
			TenantID:        tenantID,
			UserID:          req.UserID,
			AccessToken:     tokens.AccessToken,
			RefreshToken:    tokens.RefreshToken,
			TokenType:       tokens.TokenType,
			Scopes:          tokens.Scope,
			ExpiresAt:       expiresAt,
			IssuedAt:        &now,
			DCRClientID:     clientID,
			DCRClientSecret: clientSecret,
			DCRIssuer:       disc.Issuer,
			TokenEndpoint:   disc.TokenEndpoint,
			ResourceURI:     disc.ResourceURI,
		}
		if err := h.activateToken(ctx, tok, srv.Name); err != nil {
			slog.Error("mcpoauth.save_token", "error", err, "server_id", serverID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save token"})
			return
		}
		writeJSON(w, http.StatusOK, startOAuthResp{Completed: true, ClientID: clientID, Issuer: disc.Issuer})
		return
	}

	authURL, state, err := h.flowMgr.StartFlow(ctx, mcpoauth.StartFlowParams{
		ServerID:         serverID,
		TenantID:         tenantID,
		UserID:           req.UserID,
		InitiatingUserID: store.UserIDFromContext(ctx),
		DiscoveryResult:  disc,
		ClientID:         clientID,
		ClientSecret:     clientSecret,
		RedirectURI:      callbackURI,
		Scopes:           scopes,
		GrantType:        oauthSettings.OAuth.GrantType,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, startOAuthResp{
		AuthURL:  authURL,
		State:    state,
		ClientID: clientID,
		Issuer:   disc.Issuer,
	})
}

// --- GET /v1/mcp/oauth/callback ---

func (h *MCPOAuthHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	oauthErr := r.URL.Query().Get("error")
	oauthErrDesc := r.URL.Query().Get("error_description")

	locale := extractLocale(r)

	if oauthErr != "" {
		h.writeCallbackHTML(w, locale, false, oauthErrDesc)
		return
	}
	if code == "" || state == "" {
		h.writeCallbackHTML(w, locale, false, "missing code or state")
		return
	}

	ctx := r.Context()
	tokens, flow, err := h.flowMgr.ExchangeCode(ctx, state, code)
	if err != nil {
		slog.Warn("mcpoauth.exchange_failed", "error", err)
		h.writeCallbackHTML(w, locale, false, err.Error())
		return
	}

	now := time.Now()
	var expiresAt *time.Time
	if tokens.ExpiresIn > 0 {
		t := now.Add(time.Duration(tokens.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	tok := &store.MCPOAuthToken{
		ServerID:        flow.ServerID,
		TenantID:        flow.TenantID,
		UserID:          flow.UserID,
		AccessToken:     tokens.AccessToken,
		RefreshToken:    tokens.RefreshToken,
		TokenType:       tokens.TokenType,
		Scopes:          tokens.Scope,
		ExpiresAt:       expiresAt,
		IssuedAt:        &now,
		DCRClientID:     flow.ClientID,
		DCRClientSecret: flow.ClientSecret,
		DCRIssuer:       flow.Issuer,
		TokenEndpoint:   flow.TokenEndpoint,
		ResourceURI:     flow.ResourceURI,
	}
	srv, _ := h.mcpStore.GetServer(ctx, flow.ServerID)
	serverName := ""
	if srv != nil {
		serverName = srv.Name
	}
	if err := h.activateToken(ctx, tok, serverName); err != nil {
		slog.Error("mcpoauth.save_token", "error", err, "server_id", flow.ServerID)
		h.publishOAuthComplete(flow, "error", "failed to save token")
		h.writeCallbackHTML(w, locale, false, "failed to save token")
		return
	}

	// Notify the initiating WS client that OAuth completed successfully.
	h.publishOAuthComplete(flow, "success", "")
	h.writeCallbackHTML(w, locale, true, "")
}

// activateToken persists a freshly obtained OAuth token and makes it live:
// it upserts the row, evicts the refresher's in-memory cache, and drops the
// pooled MCP connection (shared + per-user) so the next request reconnects
// with the new Bearer token. Shared by the authorization-code callback and
// the client_credentials path in handleStart.
func (h *MCPOAuthHandler) activateToken(ctx context.Context, tok *store.MCPOAuthToken, serverName string) error {
	if err := h.oauthStore.UpsertOAuthToken(ctx, tok); err != nil {
		return err
	}
	if h.refresher != nil {
		h.refresher.InvalidateCache(tok.ServerID, tok.UserID)
	}
	if h.evictor != nil && serverName != "" {
		h.evictor.EvictServer(tok.TenantID, serverName)
	}
	// Broadcast an MCP cache-invalidate so all per-user pool connections are
	// evicted and agent Loop caches reload — picks up the new token for both the
	// shared (global) and per-user paths on the next request.
	h.emitMCPCacheInvalidate()
	return nil
}

// emitMCPCacheInvalidate broadcasts an MCP cache-invalidate event. The subscriber
// (cmd/gateway_managed.go) responds with agentRouter.InvalidateAll() +
// mcpPool.EvictAllUsers(), so freshly authorized/revoked OAuth tokens take effect
// immediately instead of waiting for idle TTL. Mirrors MCPHandler.emitCacheInvalidate.
func (h *MCPOAuthHandler) emitMCPCacheInvalidate() {
	if h.eventBus == nil {
		return
	}
	h.eventBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindMCP},
	})
}

func (h *MCPOAuthHandler) writeCallbackHTML(w http.ResponseWriter, locale string, success bool, errMsg string) {
	status := "success"
	if !success {
		status = "error"
	}

	var bodyMsg string
	if success {
		bodyMsg = i18n.T(locale, i18n.MsgOAuthCallbackSuccess)
	} else {
		bodyMsg = i18n.T(locale, i18n.MsgOAuthCallbackFailed)
	}

	// Build the postMessage payload with json.Marshal. errMsg is attacker-controlled
	// (reflected from the OAuth provider's `error_description` query param on this
	// unauthenticated callback endpoint), so it must never be concatenated into the
	// page. encoding/json escapes <, >, & to </>/& by default, which
	// neutralises </script> breakout attempts inside the <script> block (prevents
	// reflected XSS). bodyMsg comes from the trusted i18n catalog but is HTML-escaped
	// for defence in depth before landing in the <p> element.
	payload := struct {
		Type   string `json:"type"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}{Type: "mcp-oauth-complete", Status: status, Error: errMsg}
	msgJSON, err := json.Marshal(payload)
	if err != nil {
		msgJSON = []byte(`{"type":"mcp-oauth-complete","status":"error"}`)
	}

	htmlPage := fmt.Sprintf(`<!DOCTYPE html><html><body>
<script>
(function(){
  var msg = %s;
  try {
    var bc = new BroadcastChannel('mcp-oauth');
    bc.postMessage(msg);
    bc.close();
  } catch(e) {}
  if(window.opener){window.opener.postMessage(msg,'*');}
  setTimeout(function(){window.close();},1000);
})();
</script>
<p>%s</p>
</body></html>`, msgJSON, html.EscapeString(bodyMsg))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, htmlPage)
}

// publishOAuthComplete broadcasts an mcp.oauth_complete WS event to the initiating client.
// Only called when we have a resolved pendingFlow (i.e. after ExchangeCode succeeds).
// Error cases before ExchangeCode (missing state, provider error) have no flow and are
// handled client-side by the pollClosed interval.
func (h *MCPOAuthHandler) publishOAuthComplete(flow *mcpoauth.PendingFlow, status, errMsg string) {
	if h.eventBus == nil {
		return
	}
	bus.BroadcastForTenant(h.eventBus, protocol.EventMCPOAuthComplete, flow.TenantID,
		protocol.MCPOAuthCompletePayload{
			ServerID:         flow.ServerID.String(),
			UserID:           flow.UserID,
			InitiatingUserID: flow.InitiatingUserID,
			Status:           status,
			Error:            errMsg,
		},
	)
}

// --- GET /v1/mcp/oauth/status/{id} ---

type oauthStatusResp struct {
	HasToken  bool    `json:"has_token"`
	ClientID  string  `json:"client_id,omitempty"`
	Issuer    string  `json:"issuer,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
	Expired   bool    `json:"expired,omitempty"`
}

func (h *MCPOAuthHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server id"})
		return
	}
	userID := r.URL.Query().Get("user_id")

	var tok *store.MCPOAuthToken
	if userID == "" {
		tok, err = h.oauthStore.GetOAuthToken(ctx, serverID, tenantID)
	} else {
		tok, err = h.oauthStore.GetUserOAuthToken(ctx, serverID, tenantID, userID)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tok == nil {
		writeJSON(w, http.StatusOK, oauthStatusResp{HasToken: false})
		return
	}

	resp := oauthStatusResp{
		HasToken: true,
		ClientID: tok.DCRClientID,
		Issuer:   tok.DCRIssuer,
	}
	if tok.ExpiresAt != nil {
		s := tok.ExpiresAt.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &s
		resp.Expired = time.Now().After(*tok.ExpiresAt)
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- DELETE /v1/mcp/oauth/token/{id} ---

func (h *MCPOAuthHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server id"})
		return
	}
	userID := r.URL.Query().Get("user_id")

	if userID == "" {
		err = h.oauthStore.DeleteOAuthToken(ctx, serverID, tenantID)
	} else {
		err = h.oauthStore.DeleteUserOAuthToken(ctx, serverID, tenantID, userID)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Invalidate cached token.
	if h.refresher != nil {
		h.refresher.InvalidateCache(serverID, userID)
	}

	// Evict pool connection so next request doesn't reuse the revoked token.
	if h.evictor != nil {
		srv, _ := h.mcpStore.GetServer(r.Context(), serverID)
		if srv != nil {
			h.evictor.EvictServer(tenantID, srv.Name)
		}
	}

	// Broadcast MCP cache-invalidate → EvictAllUsers + agent reload so the revoked
	// token is dropped everywhere and OAuth servers re-gate (hidden until re-auth).
	h.emitMCPCacheInvalidate()

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- POST /v1/mcp/oauth/discover/{id} ---

type discoverResp struct {
	Issuer                string   `json:"issuer,omitempty"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

func (h *MCPOAuthHandler) handleDiscover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	serverID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid server id"})
		return
	}
	srv, err := h.mcpStore.GetServer(ctx, serverID)
	if err != nil || srv == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP server not found"})
		return
	}
	if srv.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "server has no URL"})
		return
	}

	discoverer := h.discoverer
	if discoverer == nil {
		discoverer = mcpoauth.NewDiscoverer(security.NewSafeClient(15 * time.Second))
	}
	disc, err := discoverer.Discover(ctx, srv.URL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "discovery failed: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, discoverResp{
		Issuer:                disc.Issuer,
		AuthorizationEndpoint: disc.AuthorizationEndpoint,
		TokenEndpoint:         disc.TokenEndpoint,
		RegistrationEndpoint:  disc.RegistrationEndpoint,
		ScopesSupported:       disc.ScopesSupported,
	})
}
