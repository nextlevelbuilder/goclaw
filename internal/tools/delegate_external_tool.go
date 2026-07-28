package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
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

// delegateMaxConcurrent bounds how many delegated coding runs execute AT ONCE
// across the whole process. The agent loop happily launches every tool call in
// one message in parallel, so a fan-out that splits a repo into N chunks would
// try to run N memory-heavy `go build` sandboxes simultaneously and OOM the
// host. This cap lets the model split into as many independent chunks as it
// likes (better load-balancing) while the platform runs only as many at a time
// as the host RAM allows; the rest queue for a free slot. Size it to
// host_RAM / GOCLAW_DELEGATE_MEMORY_MB (8 GB host ÷ 3 GB ≈ 3). Override with
// GOCLAW_DELEGATE_MAX_CONCURRENT.
func delegateMaxConcurrent() int {
	if v := os.Getenv("GOCLAW_DELEGATE_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 3
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
	delegateSemOnce.Do(func() { delegateSem = make(chan struct{}, delegateMaxConcurrent()) })
	select {
	case delegateSem <- struct{}{}:
		return func() { <-delegateSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// DelegateExternalTool hands a task to one of the calling agent's CONNECTED
// external agents (Claude Code, Aider, …) — the specialists a user wires into
// an agent at creation time (agents.connected_agents). v1 supports the
// external_cli transport: run the connected agent's CLI inside the sandbox.
// MCP / A2A / HTTP transports are recognised but not yet dispatched.
type DelegateExternalTool struct {
	agents     store.AgentCRUDStore
	sandboxMgr sandbox.Manager
	workspace  string
	// creds holds per-connection BYOK credentials (a user's own API key or
	// subscription OAuth token). Resolved first; the platform creds below are the
	// fallback. May be nil (e.g. desktop/SQLite) → always fall back to platform.
	creds store.ConnectedAgentCredentialStore
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
// external delegation returns a clear error rather than crashing). creds may be
// nil (per-connection BYOK unavailable → platform fallback only).
func NewDelegateExternalTool(agents store.AgentCRUDStore, sandboxMgr sandbox.Manager, workspace string, creds store.ConnectedAgentCredentialStore, anthropicKey, anthropicOAuthToken, githubToken string) *DelegateExternalTool {
	return &DelegateExternalTool{
		agents:              agents,
		sandboxMgr:          sandboxMgr,
		workspace:           workspace,
		creds:               creds,
		anthropicKey:        anthropicKey,
		anthropicOAuthToken: anthropicOAuthToken,
		githubToken:         githubToken,
	}
}

func (t *DelegateExternalTool) Name() string { return "delegate_external" }

func (t *DelegateExternalTool) Description() string {
	return "Hand a self-contained task to one of THIS agent's connected external agents — specialists wired into the agent at creation time (e.g. Claude Code for software engineering). Runs the connected agent and returns its result. Use it when the work squarely fits a connected specialist; otherwise do the work yourself."
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

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return ErrorResult("no calling agent in context — cannot resolve connected agents")
	}
	agent, err := t.agents.GetByID(ctx, agentID)
	if err != nil || agent == nil {
		return ErrorResult("could not load the calling agent's configuration")
	}
	conns := agent.ParseConnectedAgents()
	if len(conns) == 0 {
		return ErrorResult("this agent has no connected agents — add one on the agent's \"Connected agents\" section first")
	}
	conn := findConnection(conns, connArg)
	if conn == nil {
		names := make([]string, 0, len(conns))
		for i := range conns {
			names = append(names, conns[i].Name)
		}
		return ErrorResult(fmt.Sprintf("no connected agent matches %q — available: %s", connArg, strings.Join(names, ", ")))
	}

	switch conn.Kind {
	case "external_cli", "":
		return t.runCLI(ctx, conn, task, worker)
	default:
		return ErrorResult(fmt.Sprintf("connected-agent transport %q is recognised but not dispatched yet (v1 supports external_cli)", conn.Kind))
	}
}

// findConnection matches by id, name, or provider (case-insensitive). If the
// agent has exactly one connection and no arg is given, that one is used.
func findConnection(conns []config.ConnectedAgentSpec, arg string) *config.ConnectedAgentSpec {
	arg = strings.TrimSpace(arg)
	if arg == "" && len(conns) == 1 {
		return &conns[0]
	}
	for i := range conns {
		c := &conns[i]
		if strings.EqualFold(c.ID, arg) || strings.EqualFold(c.Name, arg) || strings.EqualFold(c.Provider, arg) {
			return c
		}
	}
	if arg == "" && len(conns) == 1 {
		return &conns[0]
	}
	return nil
}

// injectConnectionCredential loads THIS connection's own BYOK credential (if
// any) and injects it into env per its descriptor. Returns true when a usable
// credential was applied; false → the caller falls back to the platform
// credential. Never returns the secret to the caller.
func (t *DelegateExternalTool) injectConnectionCredential(ctx context.Context, conn *config.ConnectedAgentSpec, env map[string]string) bool {
	if t.creds == nil {
		return false
	}
	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return false
	}
	cred, err := t.creds.Get(ctx, agentID, conn.ID)
	if err != nil {
		slog.Warn("security.connected_agent_credential_load_failed", "connection", conn.ID, "error", err)
		return false
	}
	if cred == nil || cred.Secret == "" {
		return false
	}
	kind, target, ok := strings.Cut(cred.Inject, ":")
	if ok && kind == "env" && target != "" {
		env[target] = cred.Secret
		return true
	}
	// file:PATH injection (for file-based CLIs like Codex/Gemini) is not wired
	// yet; fall back to a platform credential rather than failing the run.
	slog.Warn("connected-agent credential injection descriptor not supported yet",
		"connection", conn.ID, "inject", cred.Inject)
	return false
}

// runCLI runs a connected CLI agent inside the sandbox. Network is enabled ONLY
// for this exec (regular code exec stays --network none); the sandbox container
// is keyed separately so it never shares the network-isolated exec container.
func (t *DelegateExternalTool) runCLI(ctx context.Context, conn *config.ConnectedAgentSpec, task, worker string) *Result {
	var command []string
	env := map[string]string{}

	switch strings.ToLower(conn.Provider) {
	case "claude_code", "claude", "claudecode":
		// Headless, auto-approve inside the isolated sandbox. Plain-text output.
		// NOTE: not --bare, so CLAUDE_CODE_OAUTH_TOKEN is honoured (bare mode
		// ignores it and requires ANTHROPIC_API_KEY).
		// stream-json emits one JSON event per line AS the run progresses (tool
		// uses, results, final answer), which we tail to stream live progress back
		// to the user. --verbose is required for stream-json under -p.
		// --include-partial-messages adds token-level `stream_event` deltas so the
		// agent's narration streams word-by-word (real-time feel) instead of
		// arriving as a whole block only after each assistant message completes.
		command = []string{"claude", "-p", task, "--permission-mode", "bypassPermissions", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
		// The sandbox root is read-only; point everything that wants to write to a
		// config/cache dir at the writable tmpfs. HOME=/tmp lets Claude Code persist
		// its own state, and the Go env vars let a delegated `go build`/test loop
		// work (the Go toolchain otherwise writes its cache under ~/.cache and its
		// module cache under ~/go, both read-only here). Verified: with these,
		// Claude Code compiles Go in the locked-down sandbox.
		env["HOME"] = "/tmp"
		env["GOCACHE"] = "/tmp/go-build"
		env["GOPATH"] = "/tmp/go"
		env["GOMODCACHE"] = "/tmp/go/pkg/mod"
		// Credential resolution, in order:
		//   1. per-connection BYOK credential (the user's own key/token)
		//   2. platform subscription OAuth token (GOCLAW_ANTHROPIC_OAUTH_TOKEN)
		//   3. platform API key (GOCLAW_ANTHROPIC_API_KEY)
		// We inject exactly ONE credential per exec: the CLI's own precedence has
		// ANTHROPIC_API_KEY beating CLAUDE_CODE_OAUTH_TOKEN, so setting more than
		// one would make the choice implicit. NOTE: not --bare, so the OAuth token
		// env var is honoured (bare mode ignores it).
		switch {
		case t.injectConnectionCredential(ctx, conn, env):
			// used the connection's own credential
		case t.anthropicOAuthToken != "":
			env["CLAUDE_CODE_OAUTH_TOKEN"] = t.anthropicOAuthToken
		case t.anthropicKey != "":
			env["ANTHROPIC_API_KEY"] = t.anthropicKey
		default:
			return ErrorResult("Claude Code needs an Anthropic credential — connect the agent with your Anthropic API key or a \"Log in with Claude\" subscription, or set a platform credential (GOCLAW_ANTHROPIC_OAUTH_TOKEN / GOCLAW_ANTHROPIC_API_KEY)")
		}
		// Give the delegated agent GitHub write access (clone/push/open PR) when a
		// token is configured. GH_TOKEN powers `gh`; the env-only git credential
		// helper powers `git` (the sandbox root is read-only, so no ~/.gitconfig).
		if t.githubToken != "" {
			env["GH_TOKEN"] = t.githubToken
			env["GITHUB_TOKEN"] = t.githubToken
			env["GIT_CONFIG_COUNT"] = "1"
			env["GIT_CONFIG_KEY_0"] = "credential.https://github.com.helper"
			env["GIT_CONFIG_VALUE_0"] = `!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`
		}
	default:
		return ErrorResult(fmt.Sprintf("connected CLI provider %q is not available in the sandbox yet", conn.Provider))
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
	sandboxKey := "external:" + conn.ID
	if w := strings.TrimSpace(worker); w != "" {
		sandboxKey += ":" + sanitizeWorker(w)
	}

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
	result, err := sb.Exec(ctx, command, "", sandbox.WithEnv(env), sandbox.WithStdoutLine(streamer.onLine))
	if err != nil {
		return ErrorResult(fmt.Sprintf("connected agent %q failed to run: %v", conn.Name, err))
	}
	slog.Info("external delegation completed", "provider", conn.Provider, "connection", conn.ID, "exit", result.ExitCode, "events", streamer.eventCount, "stdout_len", len(result.Stdout))

	// The final answer is the stream's "result" event. If it's missing, the run
	// didn't finish — classify why so the agent gets an actionable error instead
	// of a benign empty result (or, worse, a dump of raw stream-json).
	out := strings.TrimSpace(streamer.finalResult)
	if out == "" {
		switch result.ExitCode {
		case -1:
			mins := delegateExecTimeoutSec() / 60
			return ErrorResult(fmt.Sprintf("the connected agent %q ran past the %d-minute time limit and was stopped before finishing. Give it a smaller, self-contained slice of the work (one module/package at a time) and delegate again, or raise GOCLAW_DELEGATE_TIMEOUT_SEC.", conn.Name, mins))
		case 137:
			return ErrorResult(fmt.Sprintf("the connected agent %q was killed for running out of memory (the sandbox cap is %d MB) before it finished. Give it a smaller slice of the work, or raise GOCLAW_DELEGATE_MEMORY_MB (and ensure the host has the RAM).", conn.Name, delegateMemoryMB()))
		case 0:
			return NewResult(fmt.Sprintf("Delegated to %s (%s):\n\n(the connected agent finished but returned no result — after %d steps of activity)", conn.Name, conn.Provider, streamer.eventCount))
		default:
			return ErrorResult(fmt.Sprintf("the connected agent %q did not finish (exit %d) after %d steps. stderr: %s", conn.Name, result.ExitCode, streamer.eventCount, truncate(result.Stderr, 400)))
		}
	}
	return NewResult(fmt.Sprintf("Delegated to %s (%s):\n\n%s", conn.Name, conn.Provider, out))
}
