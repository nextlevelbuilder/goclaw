package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// delegateExecTimeoutSec is how long a delegated connected-agent run may take
// before the sandbox exec is killed. The sandbox default is 5 minutes, which is
// far too short for a real coding task (cloning + porting + a go-build loop over
// a whole repo). Default to 30 minutes; override with GOCLAW_DELEGATE_TIMEOUT_SEC.
func delegateExecTimeoutSec() int {
	if v := os.Getenv("GOCLAW_DELEGATE_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1800
}

// delegateMemoryMB is the memory ceiling for a delegated connected-agent run.
// The sandbox default (512 MB) OOM-kills a real coding task (Node runtime +
// git clone + go build). Default to 1 GB; override with GOCLAW_DELEGATE_MEMORY_MB.
// NOTE: this must fit the host — a limit larger than available RAM just moves
// the OOM to the whole instance. Size the staging/prod host accordingly.
// sanitizeWorker keeps a worker label safe for a docker container name suffix
// (letters, digits, dash, underscore, dot), capped in length.
func sanitizeWorker(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
		if b.Len() >= 32 {
			break
		}
	}
	if b.Len() == 0 {
		return "w"
	}
	return b.String()
}

func delegateMemoryMB() int {
	if v := os.Getenv("GOCLAW_DELEGATE_MEMORY_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1024
}

// DelegateMaxConcurrent bounds how many delegated coding runs execute AT ONCE
// across the whole process. The agent loop launches every tool call in one
// message in parallel, so a fan-out that splits a repo into N chunks would
// otherwise run N `go build` sandboxes at once and swamp the host.
//
// The default DERIVES FROM THE HOST'S vCPU COUNT, because a delegated port is
// CPU-bound in its build phase (measured on a 2-vCPU host: two building workers
// pegged both cores at ~99%/94%, while LLM-waiting workers idled at 2-4%; RAM
// was never the constraint — usage was ~300-400 MB against a 1.5 GB cap). One
// worker per vCPU, clamped to [2,8]: enough parallelism to overlap the long
// LLM-generation waits without oversubscribing the cores during builds.
//
// Deriving it (instead of hardcoding) means the box can be resized UP for a big
// port and back DOWN afterwards with no config change and no redeploy — the
// effective concurrency, and the recipe's chunk target that reads this, follow
// the instance size automatically. GOCLAW_DELEGATE_MAX_CONCURRENT still wins
// when set, for pinning a value regardless of host size.
//
// Exported so the system-prompt recipe can steer the model to split a port into
// ~this many chunks — matching the number of workers that actually run at once.
// Over-splitting past this just queues the extras into later waves, making the
// port slower rather than faster.
func DelegateMaxConcurrent() int {
	if v := os.Getenv("GOCLAW_DELEGATE_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	// runtime.NumCPU reflects the CPUs available to this process (host vCPUs
	// unless a cpuset pins it), so it tracks an instance resize.
	n := runtime.NumCPU()
	switch {
	case n < 2:
		return 2
	case n > 8:
		return 8
	default:
		return n
	}
}

// delegateSem is a process-wide semaphore capping concurrent delegated runs.
// Lazily sized on first use so a test/env override of GOCLAW_DELEGATE_MAX_CONCURRENT
// is honoured. The host — not any single agent — is the shared resource, so the
// cap is global rather than per-tool-instance.
var (
	delegateSemOnce sync.Once
	delegateSem     chan struct{}
)

// acquireDelegateSlot blocks until a concurrency slot is free (or ctx is
// cancelled), returning a release func to free the slot when the run finishes.
func acquireDelegateSlot(ctx context.Context) (func(), error) {
	delegateSemOnce.Do(func() { delegateSem = make(chan struct{}, DelegateMaxConcurrent()) })
	select {
	case delegateSem <- struct{}{}:
		return func() { <-delegateSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DelegateExternalTool hands a task to one of the CONNECTED external agents
// (Claude Code, Aider, …) available to the tenant — the specialists a user
// connects once under Connections and every agent in the tenant can use. v1
// supports the external_cli transport: run the connected agent's CLI inside the
// sandbox. MCP / A2A / HTTP transports are recognised but not yet dispatched.
type DelegateExternalTool struct {
	sandboxMgr sandbox.Manager
	workspace  string
	// cliConns is the TENANT-level connection catalogue (migration 000082) and is
	// the only source of truth: any agent in the tenant may delegate to any
	// enabled connection, with no per-agent wiring. It also holds the per-user
	// BYOK credentials (a user's own API key or subscription OAuth token), which
	// are resolved first; the platform creds below are the fallback. May be nil on
	// a deployment without the catalogue → delegation is unavailable.
	cliConns store.CLIConnectionStore
	// Platform Anthropic credentials, used when a connection has no credential
	// of its own. Either may be empty; at least one is required to run Claude
	// Code. When both are set the OAuth token (subscription billing) is
	// preferred — see runCLI.
	anthropicKey        string // GOCLAW_ANTHROPIC_API_KEY → ANTHROPIC_API_KEY
	anthropicOAuthToken string // GOCLAW_ANTHROPIC_OAUTH_TOKEN → CLAUDE_CODE_OAUTH_TOKEN
	// githubToken lets the delegated agent clone/push and open PRs. Injected as
	// GH_TOKEN (for gh) + an env-only git credential helper (the sandbox root is
	// read-only, so no ~/.gitconfig). Empty = no git write access.
	githubToken string // GOCLAW_GITHUB_TOKEN
}

// NewDelegateExternalTool wires the tool. sandboxMgr may be nil (no sandbox →
// external delegation returns a clear error rather than crashing). cliConns may
// be nil (no connection catalogue → delegation returns a clear error).
func NewDelegateExternalTool(sandboxMgr sandbox.Manager, workspace string, cliConns store.CLIConnectionStore, anthropicKey, anthropicOAuthToken, githubToken string) *DelegateExternalTool {
	return &DelegateExternalTool{
		sandboxMgr:          sandboxMgr,
		workspace:           workspace,
		cliConns:            cliConns,
		anthropicKey:        anthropicKey,
		anthropicOAuthToken: anthropicOAuthToken,
		githubToken:         githubToken,
	}
}

func (t *DelegateExternalTool) Name() string { return "delegate_external" }

func (t *DelegateExternalTool) Description() string {
	return "Hand a self-contained task to one of the connected external agents available to this workspace — specialists connected once and shared by every agent (e.g. Claude Code for software engineering). Runs the connected agent and returns its result. Use it when the work squarely fits a connected specialist; otherwise do the work yourself."
}

func (t *DelegateExternalTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"connection": map[string]any{
				"type":        "string",
				"description": "Which connected agent to delegate to — its name (e.g. \"Claude Code\") or connection id. If the agent has exactly one connection, this may be omitted.",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "The task for the connected agent, stated as a complete, self-contained instruction (it does not see this conversation).",
			},
			"worker": map[string]any{
				"type":        "string",
				"description": "Optional label to run this delegation in its OWN isolated sandbox so it can run in PARALLEL with other delegations to the same connection. Give each concurrent delegation a distinct worker (e.g. \"1\", \"2\", \"engine\", \"detector\"). They still share the workspace, so have each write to a different sub-directory. Omit for a single (non-parallel) delegation.",
			},
		},
		"required": []string{"task"},
	}
}

func (t *DelegateExternalTool) Execute(ctx context.Context, args map[string]any) *Result {
	task, _ := args["task"].(string)
	connArg, _ := args["connection"].(string)
	worker, _ := args["worker"].(string)
	if strings.TrimSpace(task) == "" {
		return ErrorResult("task is required")
	}
	if t.sandboxMgr == nil {
		return ErrorResult("external delegation requires a sandbox, which is not configured on this deployment")
	}

	conn, errMsg := t.resolveConnection(ctx, connArg)
	if errMsg != "" {
		return ErrorResult(errMsg)
	}

	switch conn.Kind {
	case "external_cli", "":
		return t.runCLI(ctx, conn, task, worker)
	default:
		return ErrorResult(fmt.Sprintf("connected-agent transport %q is recognised but not dispatched yet (v1 supports external_cli)", conn.Kind))
	}
}

// resolvedConn is a connection resolved from the tenant catalogue, normalised so
// the run path doesn't need the store row.
type resolvedConn struct {
	ID       string    // stable identity used for sandbox keying
	UUID     uuid.UUID // the catalogue row id
	Name     string
	Provider string
	Kind     string
	// Config is the connection's provider-specific overlay. It lets a wrong
	// built-in default (an argv flag, a credential env var) be corrected per
	// connection without a redeploy — see cliagent.Resolve.
	Config json.RawMessage
}

// resolveConnection finds the connection to delegate to. The TENANT catalogue is
// the only source: any agent may use any enabled connection, so a CLI connected
// once is usable everywhere. Returns a user-facing message as the second value
// on failure.
func (t *DelegateExternalTool) resolveConnection(ctx context.Context, connArg string) (*resolvedConn, string) {
	if t.cliConns == nil {
		return nil, "connected agents are not available on this deployment"
	}

	tenantID := store.TenantIDFromContext(ctx)
	var tenantPtr *uuid.UUID
	if tenantID != uuid.Nil {
		tenantPtr = &tenantID
	}
	// enabledOnly: a disabled connection must not be reachable by delegation.
	list, err := t.cliConns.ListForTenant(ctx, tenantPtr, true)
	if err != nil {
		slog.Warn("delegate_external: tenant connection lookup failed", "error", err)
		return nil, "could not look up the available connected agents — try again"
	}
	if c := matchCLIConnection(list, connArg); c != nil {
		return &resolvedConn{
			ID: c.ID.String(), UUID: c.ID, Name: c.Name,
			Provider: c.Provider, Kind: c.Kind, Config: c.Config,
		}, ""
	}

	available := make([]string, 0, len(list))
	for i := range list {
		available = append(available, list[i].Name)
	}
	if len(available) == 0 {
		return nil, "no connected agents are available — connect one (e.g. Claude Code) under Connections first"
	}
	return nil, fmt.Sprintf("no connected agent matches %q — available: %s", connArg, strings.Join(available, ", "))
}

// maxSandboxKeyLen mirrors the hard truncation in sandbox.sanitizeKey (docker.go):
// container keys longer than this are CUT, keeping the prefix. Exceeding it is
// silently destructive rather than an error — a key whose distinguishing suffix
// (the worker label) falls past the cut collapses every parallel worker into ONE
// shared container, disabling the fan-out without any visible failure. Any change
// to buildSandboxKey must keep the result within this budget; TestSandboxKey
// enforces it.
const maxSandboxKeyLen = 50

// buildSandboxKey composes the container identity for a delegated run. It must
// distinguish, in this order of importance:
//   - tenant + user — connections are tenant-global and their credentials are
//     per-user, so sharing a warm container across either would leak one user's
//     authenticated CLI session (the container persists HOME=/tmp between runs).
//   - connection — different CLIs must not share a container.
//   - worker — the parallel fan-out relies on one container per worker.
//
// Kept deliberately compact to fit maxSandboxKeyLen: the tenant+user pair is
// folded into an 8-hex digest (isolation without length), while the connection
// keeps a readable prefix so a container name is still recognisable when
// debugging on the host.
func buildSandboxKey(ctx context.Context, connID, worker string) string {
	scope := "t0-u0"
	tenant := store.TenantIDFromContext(ctx)
	user := strings.TrimSpace(store.UserIDFromContext(ctx))
	if tenant != uuid.Nil || user != "" {
		sum := sha256.Sum256([]byte(tenant.String() + "|" + user))
		scope = hex.EncodeToString(sum[:4]) // 8 hex chars
	}

	conn := sanitizeWorker(connID)
	if len(conn) > 8 {
		conn = conn[:8]
	}

	key := "ext:" + scope + ":" + conn
	if w := strings.TrimSpace(worker); w != "" {
		ws := sanitizeWorker(w)
		if len(ws) > 16 {
			ws = ws[:16]
		}
		key += ":" + ws
	}
	return key
}

// matchCLIConnection matches by id, name, or provider (case-insensitive). With
// exactly one connection and no argument, that one is used.
func matchCLIConnection(list []store.CLIConnection, arg string) *store.CLIConnection {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if len(list) == 1 {
			return &list[0]
		}
		return nil
	}
	for i := range list {
		c := &list[i]
		if strings.EqualFold(c.ID.String(), arg) || strings.EqualFold(c.Name, arg) || strings.EqualFold(c.Provider, arg) {
			return c
		}
	}
	return nil
}

// injectConnectionCredential loads THIS connection's own BYOK credential (if
// any) and injects it into env per its descriptor. Returns true when a usable
// credential was applied; false → the caller falls back to the platform
// credential. Never returns the secret to the caller.
func (t *DelegateExternalTool) injectConnectionCredential(ctx context.Context, conn *resolvedConn, spec cliagent.Spec, env map[string]string) bool {
	if t.cliConns == nil {
		return false
	}
	// The credential is per-USER, so the acting user's own key/token is preferred
	// and the tenant-shared row is the fallback (that resolution happens inside
	// the store).
	cred, err := t.cliConns.GetCredential(ctx, conn.UUID, store.UserIDFromContext(ctx))
	if err != nil {
		slog.Warn("security.cli_connection_credential_load_failed", "connection", conn.ID, "error", err)
		return false
	}
	if cred == nil || cred.Secret == "" {
		return false
	}
	// The spec decides WHERE the secret goes: the stored env:VAR descriptor wins,
	// otherwise the provider's preferred credential env var. It never logs or
	// returns the secret.
	if err := spec.ApplyCredential(cred.Secret, cred.Inject, env); err != nil {
		// e.g. the still-unwired file:PATH form — fall back to a platform
		// credential rather than failing the run outright.
		slog.Warn("connected-agent credential could not be injected",
			"connection", conn.ID, "provider", conn.Provider, "error", err)
		return false
	}
	return true
}

// runCLI runs a connected CLI agent inside the sandbox. Network is enabled ONLY
// for this exec (regular code exec stays --network none); the sandbox container
// is keyed separately so it never shares the network-isolated exec container.
func (t *DelegateExternalTool) runCLI(ctx context.Context, conn *resolvedConn, task, worker string) *Result {
	// Resolve the provider's invocation from the adapter layer: a built-in default
	// overlaid by this connection's config. Keeping it data rather than a switch
	// means an unfamiliar CLI can be connected, and a wrong built-in flag can be
	// corrected, without touching this code.
	spec, err := cliagent.Resolve(conn.Provider, conn.Config)
	if err != nil {
		return ErrorResult(fmt.Sprintf("connected agent %q cannot be run: %v", conn.Name, err))
	}
	// Approval mode is a per-connection choice, stored in the same config JSON as
	// the invocation overrides ({"permission_mode":"manual"}). Absent → auto, which
	// is how every existing connection already runs.
	mode := cliagent.PermissionModeFromConfig(conn.Config)
	command, err := spec.Command(task, mode)
	if err != nil {
		// A provider with no ask-before-acting flag must NOT be silently downgraded
		// to auto — the whole point of choosing manual is to withhold that trust. Say
		// which connection/provider is misconfigured and what to change.
		if errors.Is(err, cliagent.ErrManualApprovalUnsupported) {
			return ErrorResult(fmt.Sprintf(
				"connected agent %q is set to manual approval, but its provider (%s) cannot ask before acting, so the task was NOT run — switch this connection's permission_mode to %q (it runs confined to the sandbox), or set %q on the connection if this CLI does have an ask-before-acting flag. Details: %v",
				conn.Name, cliagent.CanonicalProvider(conn.Provider), cliagent.PermissionAuto, "manual_approve_args", err))
		}
		return ErrorResult(fmt.Sprintf("connected agent %q cannot be run: %v", conn.Name, err))
	}
	slog.Debug("connected-agent run", "connection", conn.ID, "provider", conn.Provider, "permission_mode", string(mode))

	// Static env from the spec — for Claude Code this is HOME=/tmp plus the Go
	// cache/module paths, without which a delegated `go build` fails against the
	// read-only sandbox root.
	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}

	// Credential resolution, in order:
	//   1. the connection's own per-user BYOK credential
	//   2. platform subscription OAuth token (Anthropic-only)
	//   3. platform API key (Anthropic-only)
	// Exactly ONE credential is injected: some CLIs have their own precedence
	// between accepted env vars, so setting several would make the choice implicit.
	switch {
	case t.injectConnectionCredential(ctx, conn, spec, env):
		// used the connection's own credential
	case cliagent.CanonicalProvider(conn.Provider) == "claude_code" && t.anthropicOAuthToken != "":
		env["CLAUDE_CODE_OAUTH_TOKEN"] = t.anthropicOAuthToken
	case cliagent.CanonicalProvider(conn.Provider) == "claude_code" && t.anthropicKey != "":
		env["ANTHROPIC_API_KEY"] = t.anthropicKey
	default:
		// No platform fallback exists for non-Anthropic CLIs, so say what to do
		// rather than letting the CLI fail with its own opaque auth error.
		return ErrorResult(fmt.Sprintf("%s has no credential — open Connections and add an API key for it (or use \"Log in with Claude\" where supported)", conn.Name))
	}

	// Give the delegated agent GitHub write access (clone/push/open PR) when a
	// token is configured. GH_TOKEN powers `gh`; the env-only git credential
	// helper powers `git` (the sandbox root is read-only, so no ~/.gitconfig).
	// Provider-agnostic: every coding CLI benefits.
	if t.githubToken != "" {
		env["GH_TOKEN"] = t.githubToken
		env["GITHUB_TOKEN"] = t.githubToken
		env["GIT_CONFIG_COUNT"] = "1"
		env["GIT_CONFIG_KEY_0"] = "credential.https://github.com.helper"
		env["GIT_CONFIG_VALUE_0"] = `!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`
	}

	// Network-enabled sandbox config for this exec only, dedicated container key.
	// Start from the per-session config if present, else the manager's base
	// config — otherwise a zero Config has an empty Image and docker run fails
	// with "invalid reference format".
	var netCfg sandbox.Config
	if cfg := SandboxConfigFromCtx(ctx); cfg != nil {
		netCfg = *cfg
	} else {
		netCfg = t.sandboxMgr.BaseConfig()
	}
	netCfg.NetworkEnabled = true
	// A real coding delegation (clone + port + build loop) runs far longer than
	// the sandbox's default 5-minute exec timeout, and needs more than the default
	// 512 MB (Node runtime + go build OOM under it). Give it generous limits.
	netCfg.TimeoutSec = delegateExecTimeoutSec()
	netCfg.MemoryMB = delegateMemoryMB()
	// A distinct worker → a distinct sandbox container, so several delegations to
	// the same connection can run concurrently (the loop already executes 2+ tool
	// calls in parallel goroutines). Without a worker they share one warm
	// container. The workspace volume is shared across all of them.
	// Sandbox identity spans tenant + user + connection + worker — see
	// buildSandboxKey for why each component is required and why the key must stay
	// short.
	sandboxKey := buildSandboxKey(ctx, conn.ID, worker)

	// Bound how many delegated runs execute at once so a wide fan-out queues
	// within host capacity instead of OOM-ing the box. Acquired BEFORE creating
	// the container so queued workers don't even spin one up until they have a
	// slot. Blocks here if all slots are busy; released when this run finishes.
	release, err := acquireDelegateSlot(ctx)
	if err != nil {
		return ErrorResult(fmt.Sprintf("delegation cancelled while waiting for a run slot: %v", err))
	}
	defer release()

	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, &netCfg)
	if err != nil {
		slog.Warn("security.external_delegate_sandbox_unavailable", "provider", conn.Provider, "error", err)
		return ErrorResult(fmt.Sprintf("external delegation sandbox unavailable: %v", err))
	}
	// Stream the run's progress to the user live. streamer parses each stream-json
	// line as it arrives, emits tool.call/tool.result chips under this delegation's
	// card, and captures the final "result" event as the delegation's output.
	streamer := newDelegateStreamer(ctx, conn.Name)
	onLine, finishStream := streamer.lineHandler(spec.Output)
	result, err := sb.Exec(ctx, command, "", sandbox.WithEnv(env), sandbox.WithStdoutLine(onLine))
	// Flush whatever the parser is still holding — matters most when the run was
	// killed at the timeout/OOM, where the last narration would otherwise be lost.
	finishStream()
	if err != nil {
		return ErrorResult(fmt.Sprintf("connected agent %q failed to run: %v", conn.Name, err))
	}
	slog.Info("external delegation completed", "provider", conn.Provider, "connection", conn.ID, "exit", result.ExitCode, "events", streamer.eventCount, "stdout_len", len(result.Stdout))

	// The final answer is the stream's "result" event. If it's missing, the run
	// didn't finish — classify why so the agent gets an actionable error instead
	// of a benign empty result (or, worse, a dump of raw stream-json).
	// Identify which worker this was. On a parallel fan-out several delegations
	// run at once, so an un-labelled message ("the connected agent timed out")
	// is ambiguous to the user AND indistinguishable between workers.
	who := conn.Name
	if w := strings.TrimSpace(worker); w != "" {
		who = fmt.Sprintf("%s [worker %s]", conn.Name, w)
	}

	out := strings.TrimSpace(streamer.finalResult)
	if out == "" {
		switch result.ExitCode {
		case -1:
			mins := delegateExecTimeoutSec() / 60
			// Partial work IS kept: the worker writes into the shared workspace as
			// it goes, so tell the agent to continue the workflow (re-delegate the
			// remainder, then integrate + publish) rather than treat this as fatal.
			return ErrorResult(fmt.Sprintf("%s ran past the %d-minute time limit and was stopped before finishing (%d steps done). Whatever it already wrote to the workspace is KEPT — inspect what remains, delegate the leftover slice as a smaller task, then continue with the integrate + publish steps. Do not abandon the run.", who, mins, streamer.eventCount))
		case 137:
			return ErrorResult(fmt.Sprintf("%s was killed for running out of memory (sandbox cap %d MB) after %d steps. Its partial output in the workspace is KEPT — delegate a smaller slice for the remainder, then continue with integrate + publish.", who, delegateMemoryMB(), streamer.eventCount))
		case 0:
			return NewResult(fmt.Sprintf("Delegated to %s (%s):\n\n(the connected agent finished but returned no result — after %d steps of activity)", who, conn.Provider, streamer.eventCount))
		default:
			return ErrorResult(fmt.Sprintf("%s did not finish (exit %d) after %d steps. stderr: %s", who, result.ExitCode, streamer.eventCount, truncate(result.Stderr, 400)))
		}
	}
	return NewResult(fmt.Sprintf("Delegated to %s (%s):\n\n%s", who, conn.Provider, out))
}
