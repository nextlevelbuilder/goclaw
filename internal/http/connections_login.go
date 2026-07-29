package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// "Log in with Claude" (subscription OAuth) for a TENANT-LEVEL connection.
//
// The two-phase flow itself — the scripts, PTY/pyte rendering and token
// extraction — lives in agents_connection_login.go and is shared. What this file
// adds is the resolution, authorization and persistence:
//
//	resolve:   the cli_connections catalogue (tenant's own row or a global one)
//	authorize: tenant visibility + admin role (adminMiddleware)
//	persist:   CLIConnections.PutCredential scoped to the ACTING USER, so a
//	           shared connection still carries each user's own subscription token
//
// The login container key includes the user, so two users logging into the same
// connection at the same time cannot collide (or steal each other's token).

// handleStartConnectionLoginForConnection runs phase 1 and returns the
// authorization URL. POST /v1/connections/{id}/login
func (h *AgentsHandler) handleStartConnectionLoginForConnection(w http.ResponseWriter, r *http.Request) {
	conn, userID, ok := h.connectionLoginPreflight(w, r)
	if !ok {
		return
	}
	key := connectionLoginSandboxKey(conn.ID.String(), userID)
	sb, err := h.loginSandbox(r.Context(), key)
	if err != nil {
		slog.Warn("security.cli_connection_login_sandbox_unavailable", "connection", conn.ID, "error", err)
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, "login sandbox unavailable: "+err.Error())
		return
	}
	res, err := sb.Exec(r.Context(), []string{"bash", "-lc", startLoginScript}, "")
	if err != nil || res.ExitCode != 0 {
		stderr := ""
		if res != nil {
			stderr = res.Stderr
		}
		slog.Warn("cli_connection_login_start_exec_failed", "connection", conn.ID, "error", err, "exit", exitOf(res), "stderr", truncate(stderr, 300))
		h.releaseLoginSandbox(key)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, "could not start Claude login")
		return
	}
	url := strings.TrimSpace(res.Stdout)
	if !strings.HasPrefix(url, "https://") {
		slog.Warn("cli_connection_login_no_url", "connection", conn.ID, "stdout", truncate(res.Stdout, 200), "stderr", truncate(res.Stderr, 300))
		h.releaseLoginSandbox(key)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, "login did not produce an authorization URL")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     conn.ID.String(),
		"authorization_url": url,
		"credential_status": "pending",
	})
}

// handleSubmitConnectionLoginCodeForConnection runs phase 2: feed the pasted
// code, capture the token, store it as the acting user's oauth credential, and
// tear the login container down. POST /v1/connections/{id}/login/code
func (h *AgentsHandler) handleSubmitConnectionLoginCodeForConnection(w http.ResponseWriter, r *http.Request) {
	conn, userID, ok := h.connectionLoginPreflight(w, r)
	if !ok {
		return
	}
	var req submitLoginCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "invalid request body")
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "code is required")
		return
	}
	key := connectionLoginSandboxKey(conn.ID.String(), userID)
	// The container from phase 1 must still exist; Get reuses it by key.
	sb, err := h.loginSandbox(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, "login sandbox unavailable: "+err.Error())
		return
	}
	// Pass the code via env (never interpolated into the shell).
	res, err := sb.Exec(r.Context(), []string{"bash", "-lc", submitCodeScript}, "", sandbox.WithEnv(map[string]string{"CODE": code}))
	if err != nil || res.ExitCode != 0 {
		msg := "login did not complete — start the login again"
		if res != nil && res.ExitCode == 2 {
			msg = "that code was invalid or expired — start the login again"
		}
		// stderr is safe to log (script diagnostics, never the token); stdout is
		// not logged — on a token-less run it could still contain one.
		stderr := ""
		if res != nil {
			stderr = res.Stderr
		}
		slog.Warn("cli_connection_login_submit_failed", "connection", conn.ID, "error", err, "exit", exitOf(res), "stderr", truncate(stderr, 600))
		h.releaseLoginSandbox(key)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, msg)
		return
	}
	token := extractOAuthToken(res.Stdout)
	if token == "" {
		// Don't log stdout content (may contain the token); length only.
		slog.Warn("cli_connection_login_no_token", "connection", conn.ID, "stdout_len", len(res.Stdout))
		h.releaseLoginSandbox(key)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, "login finished but no token was returned; start again")
		return
	}
	// SHAPE only (prefix through the tag + total length) — never the secret.
	slog.Info("cli_connection_login_token_captured", "connection", conn.ID, "shape", tokenShape(token), "len", len(token))

	if err := h.cliConns.PutCredential(r.Context(), store.CLIConnectionCredential{
		ConnectionID: conn.ID,
		UserID:       userID, // the token is this user's subscription, not the tenant's
		TenantID:     tenantScope(r.Context()),
		Type:         "oauth",
		Inject:       injectForConnection(conn.Provider, "oauth"),
		Secret:       token,
	}); err != nil {
		slog.Error("cli_connection_login_store_failed", "connection", conn.ID, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to store credential")
		return
	}
	// The token is captured + stored; the login container is no longer needed.
	h.releaseLoginSandbox(key)
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     conn.ID.String(),
		"credential_type":   "oauth",
		"credential_status": "connected",
	})
}

// connectionLoginPreflight validates feature availability, resolves the
// connection within the caller's tenant scope, requires an identified user, and
// enforces that the provider supports subscription login. On failure it writes
// the response and returns ok=false.
func (h *AgentsHandler) connectionLoginPreflight(w http.ResponseWriter, r *http.Request) (*store.CLIConnection, string, bool) {
	if h.cliConns == nil || h.sandboxMgr == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrInternal, "\"Log in with Claude\" is not available on this deployment")
		return nil, "", false
	}
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "a user identity is required to save a login token")
		return nil, "", false
	}
	conn := h.resolveConnection(w, r)
	if conn == nil {
		return nil, "", false
	}
	if injectForConnection(conn.Provider, "oauth") == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "this connection does not support subscription login")
		return nil, "", false
	}
	return conn, userID, true
}

// connectionLoginSandboxKey builds the per-(connection, user) login container
// key. loginSandbox prefixes it with "external-login:" and the Docker manager
// sanitises + TRUNCATES the result at 50 chars — a full UUID plus a raw user id
// would be cut off, silently sharing one container between two users' logins
// (and handing one user the other's token). So both halves are shortened here:
// the connection's UUID prefix plus a hash of the user id, tagged "u" so the key
// can never collide with the per-agent flow's UUID-shaped key.
func connectionLoginSandboxKey(connID, userID string) string {
	sum := sha256.Sum256([]byte(userID))
	short := connID
	if len(short) > 8 {
		short = short[:8]
	}
	return short + "-u" + hex.EncodeToString(sum[:4])
}
