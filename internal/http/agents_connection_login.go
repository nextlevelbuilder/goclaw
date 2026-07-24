package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// "Log in with Claude" (subscription OAuth) for a connected Claude Code agent.
//
// The real `claude setup-token` OAuth flow is driven INSIDE a per-connection
// sandbox (the user's own Claude Code on a remote machine they control — the
// ToS-aligned model). It needs a PTY (plain pipes emit nothing), so we wrap it
// with `script` and feed its stdin from a FIFO, all disowned inside the
// container so it survives between the two HTTP calls:
//
//	phase 1 (start): launch setup-token under a PTY; it prints an auth URL and
//	                 blocks. We poll the log and return the URL.
//	phase 2 (code):  the user authorises on claude.ai, gets a code, and posts it;
//	                 we write it to the FIFO, read the 1-year token, and store it
//	                 encrypted as an oauth credential.
//
// Validated: setup-token under `script` emits the URL and waits; a wide PTY
// (stty cols) keeps the URL on one line.

const loginSandboxKeyPrefix = "external-login:"

type submitLoginCodeRequest struct {
	Code string `json:"code"`
}

// handleStartConnectionLogin runs phase 1 and returns the authorization URL.
func (h *AgentsHandler) handleStartConnectionLogin(w http.ResponseWriter, r *http.Request) {
	conn, agentID, ok := h.loginPreflight(w, r)
	if !ok {
		return
	}
	sb, err := h.loginSandbox(r.Context(), conn.ID)
	if err != nil {
		slog.Warn("security.connected_login_sandbox_unavailable", "connection", conn.ID, "error", err)
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, "login sandbox unavailable: "+err.Error())
		return
	}
	res, err := sb.Exec(r.Context(), []string{"bash", "-lc", startLoginScript}, "")
	if err != nil || res.ExitCode != 0 {
		stderr := ""
		if res != nil {
			stderr = res.Stderr
		}
		slog.Warn("connected_login_start_exec_failed", "connection", conn.ID, "error", err, "exit", exitOf(res), "stderr", truncate(stderr, 300))
		h.releaseLoginSandbox(conn.ID)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, "could not start Claude login")
		return
	}
	url := strings.TrimSpace(res.Stdout)
	if !strings.HasPrefix(url, "https://") {
		slog.Warn("connected_login_no_url", "connection", conn.ID, "stdout", truncate(res.Stdout, 200), "stderr", truncate(res.Stderr, 300))
		h.releaseLoginSandbox(conn.ID)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, "login did not produce an authorization URL")
		return
	}
	_ = agentID // not needed for phase 1
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     conn.ID,
		"authorization_url": url,
		"credential_status": "pending",
	})
}

// handleSubmitConnectionLoginCode runs phase 2: feed the pasted code, capture the
// token, store it as an oauth credential, and tear the login container down.
func (h *AgentsHandler) handleSubmitConnectionLoginCode(w http.ResponseWriter, r *http.Request) {
	conn, agentID, ok := h.loginPreflight(w, r)
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
	// The container from phase 1 must still exist; Get reuses it by key.
	sb, err := h.loginSandbox(r.Context(), conn.ID)
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
		// not logged here — on a token-less run it could still contain one.
		stderr := ""
		if res != nil {
			stderr = res.Stderr
		}
		slog.Warn("connected_login_submit_failed", "connection", conn.ID, "error", err, "exit", exitOf(res), "stderr", truncate(stderr, 200))
		h.releaseLoginSandbox(conn.ID)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, msg)
		return
	}
	token := extractOAuthToken(res.Stdout)
	if token == "" {
		// Don't log stdout content (may contain the token); length only.
		slog.Warn("connected_login_no_token", "connection", conn.ID, "stdout_len", len(res.Stdout))
		h.releaseLoginSandbox(conn.ID)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, "login finished but no token was returned; start again")
		return
	}
	if err := h.credStore.Put(r.Context(), store.ConnectedAgentCredential{
		AgentID:      agentID,
		ConnectionID: conn.ID,
		Type:         "oauth",
		Inject:       injectForConnection(conn.Provider, "oauth"),
		Secret:       token,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to store credential")
		return
	}
	// The token is captured + stored; the login container is no longer needed.
	h.releaseLoginSandbox(conn.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"connection_id":     conn.ID,
		"credential_type":   "oauth",
		"credential_status": "connected",
	})
}

// loginPreflight validates auth availability, resolves the agent + connection,
// and enforces that the connection is a Claude Code CLI. On failure it writes the
// response and returns ok=false.
func (h *AgentsHandler) loginPreflight(w http.ResponseWriter, r *http.Request) (*connLogin, uuid.UUID, bool) {
	if h.credStore == nil || h.sandboxMgr == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrInternal, "\"Log in with Claude\" is not available on this deployment")
		return nil, uuid.Nil, false
	}
	agent, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return nil, uuid.Nil, false
	}
	connID := r.PathValue("connID")
	conn := findConnectedAgent(agent, connID)
	if conn == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, "no such connection on this agent")
		return nil, uuid.Nil, false
	}
	if injectForConnection(conn.Provider, "oauth") == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "this connection does not support subscription login")
		return nil, uuid.Nil, false
	}
	return &connLogin{ID: conn.ID, Provider: conn.Provider}, agent.ID, true
}

// connLogin is the minimal connection info the login handlers need.
type connLogin struct {
	ID       string
	Provider string
}

// loginSandbox returns (creating if needed) the network-enabled per-connection
// container used to run the login CLI. Starts from the manager's base config so
// the sandbox image is preserved, flipping network on for this container.
func (h *AgentsHandler) loginSandbox(ctx context.Context, connID string) (sandbox.Sandbox, error) {
	cfg := h.sandboxMgr.BaseConfig()
	cfg.NetworkEnabled = true
	// The login runs `claude setup-token` and touches nothing in the workspace,
	// so skip the workspace mount entirely. This also avoids a container-create
	// failure if the configured workspace path can't be mounted for this
	// one-off container (the earlier bug: a non-absolute workspace template).
	cfg.WorkspaceAccess = sandbox.AccessNone
	return h.sandboxMgr.Get(ctx, loginSandboxKeyPrefix+connID, "", &cfg)
}

func (h *AgentsHandler) releaseLoginSandbox(connID string) {
	if h.sandboxMgr != nil {
		_ = h.sandboxMgr.Release(context.Background(), loginSandboxKeyPrefix+connID)
	}
}

// startLoginScript launches `claude setup-token` under a PTY (via `script`) with
// FIFO stdin, all disowned so it outlives this exec, then polls for the auth URL.
// A wide PTY (stty cols) keeps the URL on one line.
// NOTE: no `set -e` — the `[ -n "$U" ] && { …; exit 0 }` test fails on early
// loop iterations (URL not printed yet), which under `set -e` would exit the
// script with code 1 even though it later captures the URL fine.
const startLoginScript = `D=/tmp/claude-login
rm -rf "$D"; mkdir -p "$D"; mkfifo "$D/in"
setsid sh -c "sleep 1800 > $D/in" </dev/null >/dev/null 2>&1 &
setsid script -qfc "stty cols 1000 rows 50 2>/dev/null; claude setup-token" "$D/out" < "$D/in" >/dev/null 2>&1 &
for i in $(seq 1 40); do
  if [ -f "$D/out" ]; then
    U=$(sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' "$D/out" 2>/dev/null | tr -d '\r' | grep -aoE 'https://[^[:space:]]+' | head -1)
    [ -n "$U" ] && { printf '%s\n' "$U"; exit 0; }
  fi
  sleep 1
done
echo "timed out waiting for login URL" >&2
exit 1`

// submitCodeScript writes the pasted code (from $CODE) to the FIFO and polls for
// the printed OAuth token.
const submitCodeScript = `D=/tmp/claude-login
[ -p "$D/in" ] || { echo "no login in progress" >&2; exit 1; }
# \r (carriage return) is the Enter key in the PTY. \n (LF) does NOT submit the
# code in Claude Code's Ink input prompt — it just fills the field and waits.
printf '%s\r' "$CODE" > "$D/in"
for i in $(seq 1 45); do
  CLEAN=$(sed 's/\x1b\[[0-9;?]*[a-zA-Z]//g' "$D/out" 2>/dev/null | tr -d '\r')
  T=$(printf '%s' "$CLEAN" | grep -aoE 'sk-ant-oat[0-9A-Za-z_-]+' | tail -1)
  [ -n "$T" ] && { printf '%s\n' "$T"; exit 0; }
  printf '%s' "$CLEAN" | grep -qiE 'invalid code|expired|not authorized|failed to' && { echo "invalid or expired code" >&2; exit 2; }
  sleep 1
done
echo "timed out waiting for token" >&2
exit 1`

func exitOf(res *sandbox.ExecResult) int {
	if res == nil {
		return -1
	}
	return res.ExitCode
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// oauthTokenRe matches a Claude Code OAuth token. Primary form is the sk-ant-oat
// prefix; the bare long-token fallback covers a prefix change.
var oauthTokenRe = regexp.MustCompile(`sk-ant-oat[0-9A-Za-z_-]+`)
var oauthTokenFallbackRe = regexp.MustCompile(`^[A-Za-z0-9_-]{40,}$`)

// extractOAuthToken pulls the token out of the (already ANSI-stripped) phase-2
// output. Never logs the token.
func extractOAuthToken(out string) string {
	out = strings.TrimSpace(out)
	if m := oauthTokenRe.FindString(out); m != "" {
		return m
	}
	// Fallback: last line that looks like a bare long token.
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		s := strings.TrimSpace(lines[i])
		if oauthTokenFallbackRe.MatchString(s) {
			return s
		}
	}
	return ""
}
