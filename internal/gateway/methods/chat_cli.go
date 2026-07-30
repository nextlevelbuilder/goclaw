package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
	"github.com/nextlevelbuilder/goclaw/internal/clisession"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// CLIChat serves chat.send for an interactive-CLI conversation
// (sessions.BuildCLISessionKey → internal/clisession).
//
// It is NOT an agent run: there is no loop, no provider, no system prompt and no
// token accounting — the user is talking to a coding CLI running in the sandbox
// on their own credential. What this file does is make that conversation look
// like a normal chat to everything downstream:
//
//   - the SAME event types the UI already renders (chunk / tool.call /
//     tool.result / run.started / run.completed), carrying the same routing
//     fields the gateway's event filter needs (see cliRelay.emit);
//   - the SAME persistence shape (user row, then assistant row + its tool rows),
//     so a page reload replays the conversation via chat.history;
//   - the SAME detached-context discipline as handleSend, so a ten-minute CLI
//     turn survives the client navigating away.
//
// Everything here is reached ONLY through the early branch in handleSend, keyed
// on sessions.IsCLISession. A non-CLI session never touches this file.
type CLIChat struct {
	deps CLIChatDeps

	mu     sync.Mutex
	relays map[string]*cliRelay
}

// CLIChatDeps is what the layer needs from the process. Manager, Connections and
// Sandbox are required (Ready reports whether they are all present); the rest
// degrade honestly when nil.
type CLIChatDeps struct {
	// Manager owns the live CLI sessions, one per chat session key. Constructed
	// once at startup and Shutdown on gateway stop.
	Manager *clisession.Manager
	// Connections is the tenant catalogue; every lookup here is tenant-scoped.
	Connections store.CLIConnectionStore
	// Sandbox runs the CLI. Network is enabled for these containers (a coding CLI
	// must reach its API and git), which is why they are keyed apart from the
	// network-isolated exec sandbox — see tools.InteractiveSandboxKey.
	Sandbox   sandbox.Manager
	Workspace string
	// Approvals is the broker that turns a CLI permission ask into a dashboard
	// prompt. REQUIRED for a connection whose permission_mode is manual — Send
	// refuses such a conversation up front when it is nil, rather than opening a
	// session in which every action would be denied.
	Approvals *tools.ExecApprovalManager
	// ApprovalTimeout overrides how long one action waits for a human. Zero uses
	// cliApprovalTimeout. An unanswered request is always DENIED, never assumed —
	// see cliRelay.permission.
	ApprovalTimeout time.Duration
	// Sessions persists the transcript, exactly as the agent path does.
	Sessions store.SessionStore
	// EventBus fans the CLI's narration out to the user's connected clients.
	EventBus bus.EventPublisher
	// Runs registers the turn as an active run so chat.session.status,
	// chat.abort, chat.activeSessions and runs.subscribe all work on a CLI
	// conversation the same way they do on an agent one. Nil-safe.
	Runs *agent.Router
	// Platform Anthropic credentials, used only when the connection has no
	// per-user credential of its own — same fallback order as the delegate path
	// (see tools.DelegateExternalTool.runCLI), so a connection that works for
	// delegation also works for chat.
	AnthropicKey        string
	AnthropicOAuthToken string
	// GitHubToken gives the CLI clone/push access, as in the delegate path.
	GitHubToken string
}

// NewCLIChat wires the layer. It does not start anything; the first message on a
// CLI session key is what starts a process.
func NewCLIChat(deps CLIChatDeps) *CLIChat {
	return &CLIChat{deps: deps, relays: map[string]*cliRelay{}}
}

// Ready reports whether an interactive CLI conversation can actually be served.
// Used to answer connections.chat.open / chat.send honestly on a deployment that
// has no sandbox or no connection catalogue, instead of handing back a session
// key that could never work.
func (c *CLIChat) Ready() bool {
	return c != nil && c.deps.Manager != nil && c.deps.Connections != nil && c.deps.Sandbox != nil
}

// Shutdown closes every live CLI session. Called from the gateway's shutdown
// path alongside the other stop hooks; idempotent.
// approvalTimeout is how long one action waits for a human.
func (c *CLIChat) approvalTimeout() time.Duration {
	if c.deps.ApprovalTimeout > 0 {
		return c.deps.ApprovalTimeout
	}
	return cliApprovalTimeout
}

func (c *CLIChat) Shutdown() {
	if c == nil || c.deps.Manager == nil {
		return
	}
	c.deps.Manager.Shutdown()
}

const (
	// cliChatChannel is the channel these sessions are recorded under. "ws" is
	// deliberate: the conversation belongs to the web client, and sessions.list
	// (which filters channel='ws' for WS callers) is what puts it in the sidebar.
	cliChatChannel = "ws"

	// cliApprovalTimeout is how long one action waits for a human before it is
	// DENIED. Long enough for a user to read a prompt and decide, short enough
	// that a forgotten tab does not pin a container for an hour.
	cliApprovalTimeout = 5 * time.Minute

	// cliTurnWait bounds how long we wait for ONE turn to finish before giving
	// up on it. A real coding turn (clone + build + edit loop) runs for many
	// minutes, so this is generous; it exists to stop a wedged CLI from parking
	// a request goroutine forever, not to cut work short.
	cliTurnWait = 30 * time.Minute

	// cliToolResultCap truncates a persisted tool result. The live event already
	// carried the full text to the UI; the stored copy only has to be enough to
	// re-render the chip after a reload.
	cliToolResultCap = 2000

	// cliLabelCap bounds the auto-generated conversation label.
	cliLabelCap = 60
)

// cliChatMemoryMB is the memory ceiling for an interactive CLI container. The
// sandbox default (512 MB) OOM-kills a Node CLI that is also running a build,
// which is exactly what a coding conversation does. Mirrors the delegate path's
// 1 GB default; override with GOCLAW_CLI_CHAT_MEMORY_MB.
func cliChatMemoryMB() int {
	if v := os.Getenv("GOCLAW_CLI_CHAT_MEMORY_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 1024
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

// sendRoute is which handler serves one chat.send.
type sendRoute int

const (
	// routeAgent is the normal path: resolve an agent and run the loop. This is
	// the default for EVERY session key, including an empty one.
	routeAgent sendRoute = iota
	// routeCLI is the interactive-CLI path in this file.
	routeCLI
)

// routeForSessionKey is the whole routing decision, extracted so it can be
// tested without standing up a handler. It must stay a pure function of the
// session key: anything else would make the normal path's behaviour depend on
// state the CLI feature introduced.
func routeForSessionKey(sessionKey string) sendRoute {
	if sessions.IsCLISession(sessionKey) {
		return routeCLI
	}
	return routeAgent
}

// dispatchCLISend is the early-branch target from handleSend. It performs the
// same ownership check the agent path does, then hands the turn to a goroutine —
// the WS read loop dispatches methods synchronously, so a handler that blocked
// would stall every other request on that connection.
func (m *ChatMethods) dispatchCLISend(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame, params chatSendParams) {
	locale := store.LocaleFromContext(ctx)

	userID := client.UserID()
	if userID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDRequired)))
		return
	}
	// Ownership: identical rule to the agent path — a non-admin may only send to
	// a session they own. A session that does not exist yet is not blocked (the
	// first message is what creates it).
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, userID) {
		if sess := m.sessions.Get(ctx, params.SessionKey); sess != nil && sess.UserID != userID {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}
	if m.cliChat == nil || !m.cliChat.Ready() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "interactive CLI chat is not available on this deployment (it needs the connection catalogue and a sandbox)")))
		return
	}

	go m.cliChat.Send(ctx, client, req, params.SessionKey, params.Message)
}

// SetCLIChat wires the interactive-CLI layer. Without it a CLI session key gets
// a clear "not available" error rather than falling through to the agent path.
func (m *ChatMethods) SetCLIChat(c *CLIChat) { m.cliChat = c }

// ---------------------------------------------------------------------------
// One turn
// ---------------------------------------------------------------------------

// Send delivers one user message to the connected CLI and streams the reply.
//
// It runs on its own goroutine and answers req exactly once. The context is
// DETACHED immediately: from here on the turn's lifetime is bounded by the CLI,
// by chat.abort, and by cliTurnWait — never by whether the client stayed.
func (c *CLIChat) Send(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame, sessionKey, message string) {
	locale := store.LocaleFromContext(ctx)
	fail := func(code, msg string) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, code, msg))
	}
	invalid := func(detail string) {
		fail(protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, detail))
	}

	userID := client.UserID()
	message = strings.TrimSpace(message)
	if message == "" {
		fail(protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMsgRequired))
		return
	}

	connIDStr, _, ok := sessions.ParseCLISessionKey(sessionKey)
	if !ok {
		invalid(fmt.Sprintf("%q is not a usable CLI chat session key — open a conversation with connections.chat.open", sessionKey))
		return
	}
	connID, err := uuid.Parse(connIDStr)
	if err != nil {
		invalid(fmt.Sprintf("CLI chat session key carries an unusable connection id %q", connIDStr))
		return
	}

	// Detach from the request context, preserving its values (tenant, locale,
	// user). Same reasoning as handleSend's runCtxBase: the client disconnecting
	// must not kill work the user asked for.
	turnCtx := context.WithoutCancel(ctx)
	turnCtx = store.WithUserID(turnCtx, userID)

	conn, spec, mode, detail := c.resolve(turnCtx, connID)
	if detail != "" {
		if conn == nil {
			fail(protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "connection", connIDStr))
			return
		}
		invalid(detail)
		return
	}

	env, detail := c.buildEnv(turnCtx, conn, spec)
	if detail != "" {
		invalid(detail)
		return
	}

	// Persist the user's message BEFORE the slow part (container + process), so a
	// reload mid-turn always shows what the user sent — the same guarantee the
	// agent path gives. Deliberately AFTER validation, so a misconfigured
	// connection doesn't leave an orphan message with no reply coming.
	if err := c.persistUser(turnCtx, sessionKey, userID, conn.Name, message, client.TenantID()); err != nil {
		fail(protocol.ErrInternal, "failed to persist user message: "+err.Error())
		return
	}

	netCfg := c.deps.Sandbox.BaseConfig()
	// A coding CLI must reach its own API (and git/npm/go proxies). Regular code
	// exec stays --network none; this container is keyed separately so the two
	// never share.
	netCfg.NetworkEnabled = true
	netCfg.MemoryMB = cliChatMemoryMB()
	sandboxKey := tools.InteractiveSandboxKey(turnCtx, conn.ID.String())
	sb, err := c.deps.Sandbox.Get(turnCtx, sandboxKey, c.deps.Workspace, &netCfg)
	if err != nil {
		slog.Warn("security.cli_chat_sandbox_unavailable", "provider", conn.Provider, "connection", conn.ID, "error", err)
		invalid(fmt.Sprintf("the sandbox for %s is unavailable: %v", conn.Name, err))
		return
	}

	relay := c.relayFor(sessionKey)
	relay.configure(conn, mode, userID, client.TenantID())

	// GetOrCreate joins the live process when there is one; SessionOpts are only
	// used on creation, which is why the relay (not the turn) is what the
	// callbacks close over — see relayFor.
	sess, err := c.deps.Manager.GetOrCreate(turnCtx, sessionKey, clisession.SessionOpts{
		Sandbox:    sb,
		Spec:       spec,
		Mode:       mode,
		Env:        env,
		OnEvent:    relay.onEvent,
		OnStderr:   relay.onStderr,
		Permission: relay.permission,
	})
	if err != nil {
		if errors.Is(err, clisession.ErrManagerStopped) {
			invalid("the gateway is shutting down — try again once it is back")
			return
		}
		slog.Warn("cli chat: session could not be started", "connection", conn.ID, "provider", conn.Provider, "error", err)
		invalid(fmt.Sprintf("%s could not be started: %v", conn.Name, err))
		return
	}

	// One turn at a time per conversation. A second message arriving mid-turn
	// waits here rather than interleaving with the first: the CLI's stdin would
	// accept both, but their events would then be indistinguishable and the
	// second reply could be attributed to the first turn.
	relay.turnMu.Lock()
	defer relay.turnMu.Unlock()

	runID := uuid.NewString()
	runCtx, cancel := context.WithCancel(turnCtx)
	defer cancel()
	if c.deps.Runs != nil {
		// Registering makes chat.session.status, chat.activeSessions and
		// chat.abort behave on a CLI conversation exactly as on an agent one.
		_ = c.deps.Runs.RegisterRun(turnCtx, runID, sessionKey, relay.agentID(), userID, cliChatChannel, cancel)
		defer c.deps.Runs.UnregisterRun(runID)
	}

	turn := relay.beginTurn(runID)
	defer relay.endTurn(turn)

	relay.emit(turn, protocol.AgentEventRunStarted, map[string]any{"message": message})

	if err := sess.Send(turnCtx, message); err != nil {
		relay.emit(turn, protocol.AgentEventRunFailed, map[string]any{"error": err.Error()})
		invalid(fmt.Sprintf("%s did not accept the message: %v", conn.Name, err))
		return
	}

	outcome := waitForCLITurn(turn, sess, runCtx)

	content, isError, records := turn.snapshot()
	// The terminal "result" event is authoritative; narration is the fallback for
	// a turn that was cut short (abort / timeout / the CLI exiting), where losing
	// what was already streamed would make a reload look like nothing happened.
	if content == "" {
		content = turn.narration()
	}
	if content != "" {
		if err := c.persistAssistant(turnCtx, sessionKey, content, records); err != nil {
			slog.Warn("cli chat: assistant message not persisted", "sessionKey", sessionKey, "error", err)
		}
	}

	switch outcome {
	case cliTurnAborted:
		relay.emit(turn, protocol.AgentEventRunCancelled, map[string]any{"content": content})
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"cancelled": true, "runId": runID}))
		return
	case cliTurnExited:
		relay.emit(turn, protocol.AgentEventRunFailed, map[string]any{"error": "the CLI exited"})
		fail(protocol.ErrInternal, fmt.Sprintf("%s exited (code %d) before finishing this turn. Anything it already wrote to the workspace is kept; send your message again to start a fresh session.", conn.Name, sess.ExitCode()))
		return
	case cliTurnTimedOut:
		relay.emit(turn, protocol.AgentEventRunFailed, map[string]any{"error": "turn timed out"})
		fail(protocol.ErrInternal, fmt.Sprintf("%s is still working after %s and stopped being waited on. The session is still open — its progress so far is above, and your next message continues the same conversation.", conn.Name, cliTurnWait))
		return
	}

	if content == "" {
		content = fmt.Sprintf("(%s finished this turn without producing a reply)", conn.Name)
	}
	relay.emit(turn, protocol.AgentEventRunCompleted, map[string]any{"content": content})
	resp := map[string]any{"runId": runID, "content": content}
	if isError {
		resp["stop_reason"] = "error"
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, resp))
}

// cliTurnOutcome says how one turn ended.
type cliTurnOutcome int

const (
	cliTurnFinished cliTurnOutcome = iota // the CLI emitted its terminal result
	cliTurnAborted                        // chat.abort cancelled the run
	cliTurnExited                         // the CLI process died mid-turn
	cliTurnTimedOut                       // no terminal result within cliTurnWait
)

// waitForCLITurn blocks until the turn resolves one way or another. Split out so
// the ordering of the four exits is visible in one place: a finished turn wins
// even when the process exited immediately afterwards, which is the normal
// shutdown ordering for a CLI that answers and then quits.
func waitForCLITurn(turn *cliTurn, sess *clisession.Session, runCtx context.Context) cliTurnOutcome {
	timer := time.NewTimer(cliTurnWait)
	defer timer.Stop()
	select {
	case <-turn.done:
		return cliTurnFinished
	case <-sess.Done():
		// Drain race: the terminal event and the process exit can land together.
		select {
		case <-turn.done:
			return cliTurnFinished
		default:
			return cliTurnExited
		}
	case <-runCtx.Done():
		return cliTurnAborted
	case <-timer.C:
		return cliTurnTimedOut
	}
}

// ---------------------------------------------------------------------------
// Resolution: connection → spec → env
// ---------------------------------------------------------------------------

// resolve loads the connection (tenant-scoped) and works out how to run it.
// On failure it returns a user-facing detail; conn is nil ONLY when the
// connection is invisible/absent, which the caller reports as not-found so one
// tenant can never probe another's catalogue.
func (c *CLIChat) resolve(ctx context.Context, connID uuid.UUID) (*store.CLIConnection, cliagent.Spec, cliagent.PermissionMode, string) {
	conn, err := c.deps.Connections.GetByID(ctx, tenantScopeFromCtx(ctx), connID)
	if err != nil {
		slog.Error("cli chat: connection lookup failed", "connection", connID, "error", err)
		return &store.CLIConnection{}, cliagent.Spec{}, "", "could not look up that connection — try again"
	}
	if conn == nil {
		return nil, cliagent.Spec{}, "", "connection not found"
	}
	if !conn.Enabled {
		return conn, cliagent.Spec{}, "", fmt.Sprintf("%s is disabled — enable it under Connections first", conn.Name)
	}
	spec, err := cliagent.Resolve(conn.Provider, conn.Config)
	if err != nil {
		return conn, cliagent.Spec{}, "", fmt.Sprintf("%s cannot be run: %v", conn.Name, err)
	}
	if !spec.SupportsInteractive() {
		return conn, cliagent.Spec{}, "", cliNoInteractiveDetail(conn.Name, conn.Provider)
	}

	mode := cliagent.PermissionModeFromConfig(conn.Config)
	// Build the argv now, before anything is started: a provider that cannot ask
	// before acting must be refused here rather than discovered after the
	// container is warm.
	if _, err := spec.InteractiveCommand(mode); err != nil {
		if errors.Is(err, cliagent.ErrManualApprovalUnsupported) {
			return conn, cliagent.Spec{}, "", fmt.Sprintf(
				"%s is set to manual approval, but its provider (%s) has no ask-before-acting flag, so no conversation was started — switch this connection's permission_mode to %q (it runs confined to the sandbox), or set %q on the connection if this CLI does have one. Details: %v",
				conn.Name, cliagent.CanonicalProvider(conn.Provider), cliagent.PermissionAuto, "manual_approve_args", err)
		}
		return conn, cliagent.Spec{}, "", fmt.Sprintf("%s cannot be run interactively: %v", conn.Name, err)
	}
	if mode == cliagent.PermissionManual && c.deps.Approvals == nil {
		// Every action would be refused with "no approval channel". Say so instead
		// of opening a session that can only fail.
		return conn, cliagent.Spec{}, "", fmt.Sprintf("%s is set to manual approval, but this deployment has no approval broker wired, so nothing could be approved — set this connection's permission_mode to %q or enable approvals.", conn.Name, cliagent.PermissionAuto)
	}
	return conn, spec, mode, ""
}

// cliNoInteractiveDetail names the provider that cannot hold a conversation, and
// says what CAN be done with it, rather than opening a session that would
// silently swallow every message.
func cliNoInteractiveDetail(name, provider string) string {
	return fmt.Sprintf("%s (%s) has no interactive mode: this CLI accepts one task at a time and has no message stream on stdin, so it cannot hold a conversation. Delegate a single task to it instead, or set %q on the connection if this CLI does accept a stream.",
		name, cliagent.CanonicalProvider(provider), "interactive_args")
}

// buildEnv assembles the process environment: the spec's static env, then
// exactly ONE credential, then git access. Credential order matches the delegate
// path (see runCLI) so a connection that delegates also chats.
//
// The secret is never logged or returned in the detail string.
func (c *CLIChat) buildEnv(ctx context.Context, conn *store.CLIConnection, spec cliagent.Spec) (map[string]string, string) {
	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}

	switch {
	case c.injectCredential(ctx, conn, spec, env):
		// the connection's own per-user BYOK credential
	case cliagent.CanonicalProvider(conn.Provider) == cliagent.ProviderClaudeCode && c.deps.AnthropicOAuthToken != "":
		env["CLAUDE_CODE_OAUTH_TOKEN"] = c.deps.AnthropicOAuthToken
	case cliagent.CanonicalProvider(conn.Provider) == cliagent.ProviderClaudeCode && c.deps.AnthropicKey != "":
		env["ANTHROPIC_API_KEY"] = c.deps.AnthropicKey
	default:
		return nil, fmt.Sprintf("%s has no credential — open Connections and add an API key for it (or use \"Log in with Claude\" where supported)", conn.Name)
	}

	if c.deps.GitHubToken != "" {
		env["GH_TOKEN"] = c.deps.GitHubToken
		env["GITHUB_TOKEN"] = c.deps.GitHubToken
		env["GIT_CONFIG_COUNT"] = "1"
		env["GIT_CONFIG_KEY_0"] = "credential.https://github.com.helper"
		env["GIT_CONFIG_VALUE_0"] = `!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`
	}
	return env, ""
}

// injectCredential places the connection's per-user credential into env, if it
// has one. Returns false so the caller can fall back to a platform credential.
func (c *CLIChat) injectCredential(ctx context.Context, conn *store.CLIConnection, spec cliagent.Spec, env map[string]string) bool {
	cred, err := c.deps.Connections.GetCredential(ctx, conn.ID, store.UserIDFromContext(ctx))
	if err != nil {
		slog.Warn("security.cli_connection_credential_load_failed", "connection", conn.ID, "error", err)
		return false
	}
	if cred == nil || cred.Secret == "" {
		return false
	}
	if err := spec.ApplyCredential(cred.Secret, cred.Inject, env); err != nil {
		slog.Warn("cli chat: credential could not be injected", "connection", conn.ID, "provider", conn.Provider, "error", err)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

// persistUser writes the user's turn, mirroring handleSend's boundary write —
// identity + channel stamped BEFORE the message, so sessions.list (which filters
// user_id + channel='ws') shows the conversation and the sessions.Get-based
// ownership checks succeed.
func (c *CLIChat) persistUser(ctx context.Context, sessionKey, userID, connName, message string, tenantID uuid.UUID) error {
	if c.deps.Sessions == nil {
		return nil
	}
	c.deps.Sessions.SetAgentInfo(ctx, sessionKey, uuid.Nil, userID)
	c.deps.Sessions.UpdateMetadata(ctx, sessionKey, "", "", cliChatChannel)
	c.deps.Sessions.AddMessage(ctx, sessionKey, providers.Message{Role: "user", Content: message})

	// Label the conversation on its first message. The agent path spends an LLM
	// call on a title; there is no LLM in this path, so the label is the
	// connection plus the opening line — enough for the sidebar, and no
	// invented content.
	labelled := false
	label := c.deps.Sessions.GetLabel(ctx, sessionKey)
	if label == "" {
		label = cliTruncate(connName+": "+strings.ReplaceAll(message, "\n", " "), cliLabelCap)
		c.deps.Sessions.SetLabel(ctx, sessionKey, label)
		labelled = true
	}

	if err := c.deps.Sessions.Save(ctx, sessionKey); err != nil {
		return err
	}
	if labelled && c.deps.EventBus != nil {
		bus.BroadcastForTenant(c.deps.EventBus, protocol.EventSessionUpdated, tenantID,
			map[string]string{"sessionKey": sessionKey, "label": label, "userId": userID})
	}
	return nil
}

// persistAssistant writes the CLI's reply the way the agent path writes a turn:
// one assistant message carrying the tool calls it made, followed by a
// role="tool" message per answered call. That shape is what makes chat.history
// replay the tool chips after a reload; it also keeps the transcript
// well-formed, so nothing downstream has to special-case a CLI session.
func (c *CLIChat) persistAssistant(ctx context.Context, sessionKey, content string, records []cliToolRecord) error {
	if c.deps.Sessions == nil {
		return nil
	}
	msg := providers.Message{Role: "assistant", Content: content}
	for _, rec := range records {
		msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{ID: rec.ID, Name: rec.Name})
	}
	c.deps.Sessions.AddMessage(ctx, sessionKey, msg)
	for _, rec := range records {
		if !rec.Answered {
			// Interrupted mid-flight: no result exists, so writing one would
			// invent an outcome.
			continue
		}
		c.deps.Sessions.AddMessage(ctx, sessionKey, providers.Message{
			Role:       "tool",
			Content:    cliTruncate(rec.Result, cliToolResultCap),
			ToolCallID: rec.ID,
			IsError:    rec.IsError,
		})
	}
	return c.deps.Sessions.Save(ctx, sessionKey)
}

// ---------------------------------------------------------------------------
// Relay: the per-conversation bridge between a live session and the gateway
// ---------------------------------------------------------------------------

// relayFor returns the conversation's relay, creating it if needed.
//
// A relay exists per SESSION KEY and outlives any single turn, because
// clisession.SessionOpts (and therefore the OnEvent / Permission callbacks) is
// only read when the process is created: turn 2 of a conversation reuses the
// callbacks installed by turn 1. The relay is the indirection that lets those
// fixed callbacks reach the CURRENT turn.
func (c *CLIChat) relayFor(sessionKey string) *cliRelay {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Drop relays whose session is gone (closed, crashed, or reaped for idleness)
	// so an instance that has served thousands of conversations does not keep one
	// struct per conversation forever.
	for key, r := range c.relays {
		if key == sessionKey || r.hasTurn() {
			continue
		}
		if _, live := c.deps.Manager.Get(key); !live {
			delete(c.relays, key)
		}
	}

	if r, ok := c.relays[sessionKey]; ok {
		return r
	}
	r := &cliRelay{chat: c, sessionKey: sessionKey}
	c.relays[sessionKey] = r
	return r
}

// cliRelay bridges one CLI session to the gateway: it turns the CLI's events
// into the chat events the UI renders, and its permission asks into approval
// requests.
type cliRelay struct {
	chat       *CLIChat
	sessionKey string

	// turnMu serialises turns on this conversation. Held for the whole of a
	// turn, so it is never taken by the read-loop goroutine.
	turnMu sync.Mutex

	mu       sync.Mutex // guards everything below
	conn     *store.CLIConnection
	mode     cliagent.PermissionMode
	userID   string
	tenantID uuid.UUID
	cur      *cliTurn
}

// configure refreshes the identity/mode this relay speaks for. Called on every
// turn because a connection's permission_mode (or its name) can change between
// messages, and the next approval prompt must reflect what is configured NOW.
func (r *cliRelay) configure(conn *store.CLIConnection, mode cliagent.PermissionMode, userID string, tenantID uuid.UUID) {
	r.mu.Lock()
	r.conn = conn
	r.mode = mode
	r.userID = userID
	r.tenantID = tenantID
	r.mu.Unlock()
}

// agentID is the identity CLI events and runs are attributed to. It is
// deliberately NOT a real agent key — nothing may resolve it as an agent — while
// still being stable and recognisable per connection.
func (r *cliRelay) agentID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return "cli"
	}
	return sessions.CLISessionPrefix + r.conn.ID.String()
}

func (r *cliRelay) beginTurn(runID string) *cliTurn {
	t := &cliTurn{runID: runID, done: make(chan struct{})}
	r.mu.Lock()
	r.cur = t
	r.mu.Unlock()
	return t
}

// endTurn detaches the turn. Events that arrive after it (a CLI still narrating
// past its own result event) are dropped rather than attributed to whatever turn
// comes next.
func (r *cliRelay) endTurn(t *cliTurn) {
	r.mu.Lock()
	if r.cur == t {
		r.cur = nil
	}
	r.mu.Unlock()
	t.finish()
}

func (r *cliRelay) hasTurn() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur != nil
}

func (r *cliRelay) turn() *cliTurn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur
}

// onEvent is the session's event sink. It runs on the stdout read goroutine, in
// order, and must not block — everything here is a map build plus a broadcast.
func (r *cliRelay) onEvent(ev cliagent.Event) {
	turn := r.turn()
	if turn == nil {
		return
	}
	switch ev.Kind {
	case cliagent.EventText:
		if ev.Text == "" {
			return
		}
		turn.appendNarration(ev.Text)
		// Same event + payload shape as a streaming agent turn
		// (map[string]string{"content": …}), which is what the router's
		// AppendContent and the SPA's chunk handler both expect.
		r.emit(turn, protocol.ChatEventChunk, map[string]string{"content": ev.Text})
	case cliagent.EventToolCall:
		turn.recordCall(ev.ToolID, ev.ToolName)
		var args map[string]any
		if json.Unmarshal(ev.ToolInput, &args) != nil || args == nil {
			args = map[string]any{}
		}
		r.emit(turn, protocol.AgentEventToolCall, map[string]any{
			"name":      ev.ToolName,
			"id":        ev.ToolID,
			"arguments": args,
		})
	case cliagent.EventToolResult:
		name := turn.recordResult(ev.ToolID, ev.IsError, ev.ToolResult)
		r.emit(turn, protocol.AgentEventToolResult, map[string]any{
			"name":     name,
			"id":       ev.ToolID,
			"is_error": ev.IsError,
			"result":   cliTruncate(ev.ToolResult, cliToolResultCap),
		})
	case cliagent.EventFinal:
		turn.setFinal(ev.Text, ev.IsError)
		turn.finish()
	}
}

// onStderr surfaces the CLI's diagnostics in the server log. They are NOT sent
// to the client: stderr is where a CLI prints credential and start-up failures,
// and forwarding it verbatim into a chat bubble risks echoing something the user
// should not see.
func (r *cliRelay) onStderr(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	slog.Debug("cli chat stderr", "sessionKey", r.sessionKey, "line", cliTruncate(line, 400))
}

// emit publishes one chat event for this turn.
//
// The payload shapes are exactly the ones an agent run emits, because the UI
// slice is the same:
//
//	chunk        map[string]string{"content"}
//	tool.call    map[string]any{"name","id","arguments"}
//	tool.result  map[string]any{"name","id","is_error","result"}
//	run.*        map[string]any{"content"|"message"|"error"}
//
// The routing fields matter as much as the payload: the gateway's event filter
// drops an `agent` event whose TenantID does not match the client, and scopes it
// by UserID, so an event without both would either vanish or leak.
func (r *cliRelay) emit(turn *cliTurn, eventType string, payload any) {
	if turn == nil {
		return
	}
	r.mu.Lock()
	userID, tenantID := r.userID, r.tenantID
	agentID := sessions.CLISessionPrefix
	if r.conn != nil {
		agentID += r.conn.ID.String()
	}
	r.mu.Unlock()

	ev := agent.AgentEvent{
		Type:       eventType,
		AgentID:    agentID,
		RunID:      turn.runID,
		Payload:    payload,
		UserID:     userID,
		Channel:    cliChatChannel,
		ChatID:     userID,
		SessionKey: r.sessionKey,
		TenantID:   tenantID,
	}

	if runs := r.chat.deps.Runs; runs != nil {
		// Feed the reload-recovery snapshot, exactly as the gateway's agent-event
		// hook does for a normal run, then stamp the per-run Seq so
		// runs.subscribe can replay a gap.
		switch eventType {
		case protocol.ChatEventChunk:
			if p, ok := payload.(map[string]string); ok {
				runs.AppendContent(ev.RunID, p["content"])
			}
		case protocol.AgentEventToolCall:
			if p, ok := payload.(map[string]any); ok {
				id, _ := p["id"].(string)
				name, _ := p["name"].(string)
				runs.AppendToolCall(ev.RunID, id, name)
			}
		case protocol.AgentEventToolResult:
			if p, ok := payload.(map[string]any); ok {
				id, _ := p["id"].(string)
				isErr, _ := p["is_error"].(bool)
				result, _ := p["result"].(string)
				runs.AppendToolResult(ev.RunID, id, isErr, result)
			}
		}
		ev = runs.StampAndBufferEvent(ev)
	}

	if pub := r.chat.deps.EventBus; pub != nil {
		pub.Broadcast(bus.Event{Name: protocol.EventAgent, Payload: ev, TenantID: ev.TenantID})
	}
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

// permission is the clisession.PermissionFunc: it turns one CLI permission ask
// into an approval the user answers from the dashboard.
//
// The zero PermissionDecision is DENY, so every branch that cannot establish
// consent falls back to refusing — and says why, because the reason is shown to
// the model as the tool's error result and is what lets it adapt.
func (r *cliRelay) permission(ctx context.Context, pr clisession.PermissionRequest) clisession.PermissionDecision {
	r.mu.Lock()
	mode, tenantID, userID := r.mode, r.tenantID, r.userID
	connName := ""
	if r.conn != nil {
		connName = r.conn.Name
	}
	r.mu.Unlock()

	if mode != cliagent.PermissionManual {
		// Auto mode: the connection's own setting is "act unattended inside the
		// sandbox", and the CLI is started with bypassPermissions, so it should
		// never ask at all. If a future build asks anyway, honour the setting
		// rather than raising a prompt the user never opted into — and never
		// silently deny, which would look like a broken CLI.
		slog.Debug("cli chat: permission ask in auto mode, allowed without prompting",
			"sessionKey", r.sessionKey, "tool", pr.ToolName)
		return clisession.PermissionDecision{Allow: true}
	}

	broker := r.chat.deps.Approvals
	if broker == nil {
		// Send refuses a manual-mode conversation when the broker is missing, so
		// this is unreachable in practice; keep it a denial rather than a
		// fallthrough to allow.
		slog.Warn("security.cli_chat_approval_broker_missing", "sessionKey", r.sessionKey, "tool", pr.ToolName)
		return clisession.PermissionDecision{DenyReason: "This action needs the user's approval, but no approval channel is connected to this deployment, so it could not be authorised."}
	}

	areq := tools.ApprovalRequest{
		ToolName: cliApprovalToolName(pr.ToolName),
		// Command is left EMPTY on purpose even for a shell action. The broker
		// treats a non-empty Command as an exec command and, on "allow always",
		// adds its binary to the exec tool's dynamic allowlist — so approving one
		// `git push` from a CLI would silently widen what the AGENT's own exec
		// tool may run unattended. Everything the user needs to see is in Detail.
		Detail:     cliApprovalDetail(connName, pr),
		AgentID:    r.agentID(),
		SessionKey: r.sessionKey,
		UserID:     userID,
		TenantID:   tenantID,
	}

	// RequestApproval blocks for up to cliApprovalTimeout, which is far longer
	// than a gateway shutdown may take — and Session.Close waits for pending
	// approvals. So wait on the SESSION's context too: when the session ends we
	// stop waiting immediately and refuse, instead of pinning Close for minutes.
	type answer struct {
		outcome tools.ApprovalOutcome
		err     error
	}
	timeout := r.chat.approvalTimeout()
	ch := make(chan answer, 1)
	go func() {
		o, err := broker.RequestApprovalOutcome(areq, timeout)
		ch <- answer{o, err}
	}()

	select {
	case <-ctx.Done():
		return clisession.PermissionDecision{DenyReason: "The conversation ended before this action was approved, so it was not run."}
	case a := <-ch:
		switch {
		case a.err != nil:
			// The broker's only error today is the timeout, and a timeout MUST
			// deny: nobody consented. Say that plainly so the model asks again
			// rather than assuming a transport failure.
			slog.Info("cli chat: approval not answered, denying", "sessionKey", r.sessionKey, "tool", pr.ToolName, "error", a.err)
			return clisession.PermissionDecision{
				DenyReason: fmt.Sprintf("Nobody approved this within %s, so it was NOT run (an unanswered request is refused, never assumed). Ask the user to confirm before trying again.", timeout),
			}
		case a.outcome.Decision == tools.ApprovalDeny:
			// A redirect ("don't push, open a PR instead") is far more useful to the
			// model than a bare refusal, so the user's own words replace the generic
			// sentence when they gave any. Prefixed rather than passed raw: the model
			// sees this as a tool result, and unattributed text there reads like an
			// instruction from the system instead of a human overruling it.
			if reason := strings.TrimSpace(a.outcome.Reason); reason != "" {
				return clisession.PermissionDecision{
					DenyReason: cliTruncate("The user declined this action and said: "+reason, 2000),
				}
			}
			return clisession.PermissionDecision{DenyReason: "The user declined this action. Do not retry it — continue without it or ask what they would prefer."}
		default:
			// allow-once and allow-always both permit THIS action. The broker's
			// always-allow memory is keyed on an exec command, which this path
			// deliberately does not set, so "always" grants once here.
			return clisession.PermissionDecision{Allow: true}
		}
	}
}

// cliApprovalToolName is what the prompt says is asking. The CLI's own tool name
// ("Bash", "Edit") is the most informative thing available; it is prefixed so a
// row in the approvals list is never confused with the platform's own exec tool.
func cliApprovalToolName(toolName string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return "cli"
	}
	return "cli:" + toolName
}

// cliApprovalDetail renders "what would run" for a human. The CLI's own labels
// are preferred — it knows best how to describe its action — with a summary of
// the tool input as the fallback.
func cliApprovalDetail(connName string, pr clisession.PermissionRequest) string {
	label := strings.TrimSpace(pr.DisplayName)
	if label == "" {
		label = strings.TrimSpace(pr.ToolName)
	}
	if label == "" {
		label = "an action"
	}
	head := label
	if connName != "" {
		head = connName + " → " + label
	}
	what := strings.TrimSpace(pr.Description)
	if what == "" {
		what = summariseCLIToolInput(pr.Input)
	}
	if what == "" {
		return head
	}
	return cliTruncate(head+": "+what, 500)
}

// summariseCLIToolInput picks the field that actually says what would happen.
// The order is by how consequential the field is, so a Bash command is never
// hidden behind an incidental one; anything unrecognised falls back to the
// compact JSON, which is still better than nothing to approve against.
func summariseCLIToolInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return ""
	}
	for _, key := range []string{"command", "file_path", "path", "notebook_path", "url", "pattern", "query", "prompt"} {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Turn state
// ---------------------------------------------------------------------------

// cliToolRecord is one action the CLI took during a turn, kept so the persisted
// transcript can show the same chips the live stream did.
type cliToolRecord struct {
	ID       string
	Name     string
	Result   string
	IsError  bool
	Answered bool
}

// cliTurn accumulates one turn's output. Written from the read-loop goroutine
// (onEvent) and read by the waiting request goroutine, hence the mutex on every
// field.
type cliTurn struct {
	runID string

	mu       sync.Mutex
	narrated strings.Builder
	final    string
	finalErr bool
	records  []cliToolRecord
	byToolID map[string]int
	doneOnce sync.Once
	done     chan struct{}
}

func (t *cliTurn) appendNarration(text string) {
	t.mu.Lock()
	t.narrated.WriteString(text)
	t.mu.Unlock()
}

func (t *cliTurn) narration() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(t.narrated.String())
}

func (t *cliTurn) recordCall(id, name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byToolID == nil {
		t.byToolID = map[string]int{}
	}
	if _, ok := t.byToolID[id]; ok {
		return
	}
	t.byToolID[id] = len(t.records)
	t.records = append(t.records, cliToolRecord{ID: id, Name: name})
}

// recordResult attaches a result to its call and returns the call's tool name,
// which the CLI does not repeat on the result event but the UI chip needs.
func (t *cliTurn) recordResult(id string, isError bool, result string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx, ok := t.byToolID[id]
	if !ok {
		return ""
	}
	t.records[idx].Result = result
	t.records[idx].IsError = isError
	t.records[idx].Answered = true
	return t.records[idx].Name
}

func (t *cliTurn) setFinal(text string, isError bool) {
	t.mu.Lock()
	if t.final == "" {
		t.final = strings.TrimSpace(text)
		t.finalErr = isError
	}
	t.mu.Unlock()
}

// snapshot returns the turn's terminal answer plus a copy of its tool records.
func (t *cliTurn) snapshot() (final string, isError bool, records []cliToolRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]cliToolRecord, len(t.records))
	copy(out, t.records)
	return t.final, t.finalErr, out
}

// finish unblocks the waiter. Idempotent — the terminal event and endTurn both
// call it.
func (t *cliTurn) finish() {
	t.doneOnce.Do(func() { close(t.done) })
}

// cliTruncate shortens s to at most n bytes, marking that it was cut. It backs
// up to the nearest rune boundary so a truncated tool result or label is still
// valid UTF-8 — a half rune would be mangled by the JSON encoder and render as a
// replacement character in the UI.
func cliTruncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
