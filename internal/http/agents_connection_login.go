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
		slog.Warn("connected_login_submit_failed", "connection", conn.ID, "error", err, "exit", exitOf(res), "stderr", truncate(stderr, 600))
		h.releaseLoginSandbox(conn.ID)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, msg)
		return
	}
	// DIAG (temporary): the submit script writes TOKENDBG rawhex lines to stderr;
	// log them so we can see the raw prefix bytes and fix the "o"-drop precisely.
	if strings.Contains(res.Stderr, "TOKENDBG") {
		slog.Info("connected_login_token_dbg", "connection", conn.ID, "stderr", truncate(res.Stderr, 500))
	}
	token := extractOAuthToken(res.Stdout)
	if token == "" {
		// Don't log stdout content (may contain the token); length only.
		slog.Warn("connected_login_no_token", "connection", conn.ID, "stdout_len", len(res.Stdout))
		h.releaseLoginSandbox(conn.ID)
		writeError(w, http.StatusBadGateway, protocol.ErrInternal, "login finished but no token was returned; start again")
		return
	}
	// Log the token's SHAPE (prefix through the tag + total length), never the
	// secret — so a future capture/format regression is diagnosable straight from
	// logs without ever exec'ing into the sandbox to reverse-engineer the format.
	slog.Info("connected_login_token_captured", "connection", conn.ID, "shape", tokenShape(token), "len", len(token))
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
    U=$(sed -E 's#\x1b\[[0-9;?]*[ -/]*[@-~]##g' "$D/out" 2>/dev/null | tr '\r' '\n' | grep -aoE 'https://[^[:space:]]+' | head -1)
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
# Send the code, then Enter as a SEPARATE write. Sending code+\r in one write
# makes Claude Code's Ink prompt treat it as a bracketed paste and absorb the
# Enter (especially once the prompt has sat idle while the user approves in the
# browser), so the code is typed but never submitted. Two writes = a paste
# followed by a distinct Enter keypress. Verified on the real sandbox image
# with a ~40s idle gap.
printf '%s' "$CODE" > "$D/in"
sleep 1
printf '\r' > "$D/in"
for i in $(seq 1 45); do
  # Sanitize robustly: strip full CSI escapes (params + intermediates + final),
  # then convert each \r to \n so every carriage-return redraw frame lands on its
  # OWN line. This never glues characters across frames (the bug that dropped a
  # token char, sk-ant-oat… → sk-ant-at…, yielding a 401) and never deletes a
  # frame (an earlier sed 's/.*\r//' wiped a line whose content preceded a
  # trailing \r). grep then finds the full token in whichever frame holds it.
  CLEAN=$(sed -E 's#\x1b\[[0-9;?]*[ -/]*[@-~]##g' "$D/out" 2>/dev/null | tr '\r' '\n')
  # Match the Anthropic token shape format-agnostically: sk-ant-<tag><NN>-<long>.
  # The real setup-token prefix is sk-ant-atNN- (NOT sk-ant-oatNN- — an earlier
  # over-strict "oat" regex rejected the valid token and hung the login), and the
  # auth URL above contains no "sk-ant-", so this can't false-match its params.
  # The {40,} body floor still rejects a partial mid-render frame.
  T=$(printf '%s' "$CLEAN" | grep -aoE 'sk-ant-[a-z]+[0-9]{2}-[A-Za-z0-9_-]{40,}' | tail -1)
  if [ -n "$T" ]; then
    # DIAG (temporary): dump the RAW (unsanitized) bytes at each "sk-ant"
    # occurrence — 14 bytes = prefix + 1 secret char — so we can see exactly how
    # the prefix renders (escapes/CR) and where the "o" of sk-ant-oat… is lost.
    # Goes to stderr (logged, never stored); reveals the drop mechanism.
    grep -aboE 'sk-ant' "$D/out" 2>/dev/null | head -4 | while IFS=: read OFF _; do
      printf 'TOKENDBG off=%s hex=' "$OFF" >&2
      dd if="$D/out" bs=1 skip="$OFF" count=14 2>/dev/null | od -An -tx1 | tr -d '\n' >&2
      printf '\n' >&2
    done
    printf '%s\n' "$T"; exit 0
  fi
  printf '%s' "$CLEAN" | grep -qiE 'invalid code|expired|not authorized|oauth error|request failed' && { echo "invalid or expired code" >&2; exit 2; }
  sleep 1
done
# Timed out. Emit a redacted tail (every long token run masked) so we can see
# the SHAPE of the success output in logs without ever leaking a real token.
echo "timed out waiting for token; redacted tail:" >&2
sed -E 's#\x1b\[[0-9;?]*[ -/]*[@-~]##g' "$D/out" 2>/dev/null | tr '\r' '\n' \
  | sed -E 's/[A-Za-z0-9_-]{20,}/<REDACTED>/g' | grep -viE '^[[:space:]]*$' | tail -15 >&2
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

// tokenShape returns a NON-secret descriptor of an sk-ant token: the prefix up
// to and including the "<tag><NN>-" segment, then an ellipsis. For
// sk-ant-at01-<90 secret chars> it returns "sk-ant-at01-…". If the value doesn't
// look like an sk-ant token, only the leading char class is described so no
// secret bytes leak. Safe to log.
func tokenShape(tok string) string {
	// Everything up to and including the 3rd hyphen is the non-secret prefix
	// (sk "-" ant "-" tag "-"); the body after it is the secret.
	hy := 0
	for i := 0; i < len(tok); i++ {
		if tok[i] == '-' {
			hy++
			if hy == 3 {
				return tok[:i+1] + "…"
			}
		}
	}
	return "(unrecognized token shape)"
}

// oauthTokenRe matches a Claude Code OAuth token format-agnostically:
// sk-ant-<tag><NN>-<long>. The real `claude setup-token` prefix is sk-ant-at<NN>-;
// an earlier version hard-required sk-ant-oat<NN>- on a mistaken "dropped-o
// corruption" theory, which rejected the VALID token and hung the login at
// "Finishing…". We keep a generous body-length floor so a partial mid-render
// frame still can't match, but we no longer assume the exact tag. tr '\r' '\n'
// in the capture loop is what actually prevents cross-frame character gluing.
var oauthTokenRe = regexp.MustCompile(`sk-ant-[a-z]+[0-9]{2}-[A-Za-z0-9_-]{40,}`)

// extractOAuthToken pulls the token out of the (already ANSI-stripped) phase-2
// output. Never logs the token.
func extractOAuthToken(out string) string {
	return oauthTokenRe.FindString(strings.TrimSpace(out))
}
