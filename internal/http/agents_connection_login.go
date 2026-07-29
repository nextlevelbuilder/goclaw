package http

import (
	"context"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// Shared "Log in with Claude" (subscription OAuth) machinery. The HTTP handlers
// that drive it live in connections_login.go (tenant-level connections); the
// superseded per-agent handlers that used to live here were removed.
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

# The setup-token success screen does NOT print the token left-to-right: Ink
# lays it out with absolute cursor moves (e.g. ESC[10G — Cursor Horizontal
# Absolute to column 10), so naive escape-stripping GLUES fragments together and
# drops the character the cursor jumped over (sk-ant-oat… → sk-ant-at…, a token
# Anthropic then rejects with 401). To recover the true text we replay the byte
# stream through a tiny terminal-cursor emulator: honour CR / BS / CHA(G) /
# CUF(C) / CUB(D), write each printable char at its real column, strip the rest.
# python3 ships in the sandbox image, so no rebuild is needed.
cat > "$D/emu.py" <<'PYEOF'
import sys, re
data = sys.stdin.buffer.read().decode('utf-8', 'replace')
CSI = re.compile(r'\x1b\[([0-9;?]*)[ -/]*([@-~])')
def num(p, d=1):
    try:
        return int(p) if p else d
    except ValueError:
        return d
def render(cells):
    if not cells:
        return ''
    return ''.join(cells.get(k, ' ') for k in range(max(cells) + 1)).rstrip()
lines, cells, col, i, n = [], {}, 0, 0, len(data)
while i < n:
    ch = data[i]
    if ch == '\x1b':
        m = CSI.match(data, i)
        if m:
            f, p = m.group(2), m.group(1)
            if f == 'G':
                col = max(0, num(p) - 1)
            elif f == 'C':
                col += num(p)
            elif f == 'D':
                col = max(0, col - num(p))
            i = m.end()
            continue
        nxt = data[i + 1] if i + 1 < n else ''
        i += 3 if nxt in '()' else 2      # skip charset designators / 2-byte escapes
        continue
    i += 1
    if ch == '\n':
        lines.append(render(cells)); cells, col = {}, 0; continue
    if ch == '\r':
        col = 0; continue
    if ch == '\b':
        col = max(0, col - 1); continue
    if ord(ch) < 32:
        continue
    cells[col] = ch; col += 1
lines.append(render(cells))
sys.stdout.write('\n'.join(lines))
PYEOF

# Prefer pyte, a real/complete VT emulator, over the minimal hand-rolled one.
# The login sandbox has network, and pyte may not be baked into the image yet,
# so best-effort pip-install it into a tmpfs dir (no-op if already present or if
# offline — we then fall back to emu.py). Screen geometry matches the PTY's
# stty cols 1000 rows 50 so absolute cursor moves land on the same columns.
pip install --quiet --disable-pip-version-check --target /tmp/pylib pyte >/dev/null 2>&1 || true
cat > "$D/render.py" <<'PYEOF'
import sys, pyte
data = open('/tmp/claude-login/out', 'rb').read().decode('utf-8', 'replace')
screen = pyte.Screen(1000, 50)
stream = pyte.Stream(screen)
stream.feed(data)
sys.stdout.write('\n'.join(screen.display))
PYEOF

render() {
  if PYTHONPATH=/tmp/pylib python3 -c 'import pyte' 2>/dev/null; then
    PYTHONPATH=/tmp/pylib python3 "$D/render.py" 2>/dev/null
  else
    python3 "$D/emu.py" < "$D/out" 2>/dev/null
  fi
}

for i in $(seq 1 45); do
  CLEAN=$(render)
  # Match the Anthropic token shape format-agnostically: sk-ant-<tag><NN>-<long>.
  # The auth URL above contains no "sk-ant-", so this can't false-match its
  # params; the {40,} body floor rejects a partial mid-render frame.
  T=$(printf '%s' "$CLEAN" | grep -aoE 'sk-ant-[a-z]+[0-9]{2}-[A-Za-z0-9_-]{40,}' | tail -1)
  if [ -n "$T" ]; then
    # Log the reconstructed prefix (through the tag + 4 body chars, rest masked)
    # so we can confirm the emulator recovered sk-ant-oat01- and not sk-ant-at01-.
    printf 'TOKENDBG recon=%s\n' "$(printf '%s' "$T" | sed -E 's/(sk-ant-[a-z]+[0-9]{2}-.{4}).*/\1<MASK>/')" >&2
    printf '%s\n' "$T"; exit 0
  fi
  printf '%s' "$CLEAN" | grep -qiE 'invalid code|expired|not authorized|oauth error|request failed' && { echo "invalid or expired code" >&2; exit 2; }
  sleep 1
done
# Timed out. Emit a redacted tail (every long token run masked) so we can see
# the SHAPE of the success output in logs without ever leaking a real token.
echo "timed out waiting for token; redacted tail:" >&2
render | sed -E 's/[A-Za-z0-9_-]{20,}/<REDACTED>/g' | grep -viE '^[[:space:]]*$' | tail -15 >&2
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
