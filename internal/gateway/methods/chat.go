package methods

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkconfig"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ChatMethods handles chat.send, chat.history, chat.abort, chat.inject.
type ChatMethods struct {
	agents           *agent.Router
	sessions         store.SessionStore
	cfg              *config.Config
	rateLimiter      *gateway.RateLimiter
	eventBus         bus.EventPublisher
	postTurn         tools.PostTurnProcessor
	audioMgr         *audio.Manager // for TTS auto-apply on WS responses (nil = disabled)
	usageCaps        *usagecaps.Service
	debouncer        *chatDebouncer
	agentStore       store.AgentStore
	teamStore        store.TeamStore
	linkStore        store.AgentLinkStore
	providerReg      *providers.Registry
	skillsLoader     *skills.Loader
	mcpStore         store.MCPAgentGrantBatchStore
	builtinToolStore store.BuiltinToolStore
	tenantToolStore  store.BuiltinToolTenantConfigStore
	toolPolicy       *tools.PolicyEngine
	toolRegistry     *tools.Registry
	runQueue         *chatRunQueue
	teamWorkCfg      *teamworkconfig.Resolver
	// shuttingDown latches at graceful shutdown (Phase 7 Decision 6). Once set, no
	// new chat.send is admitted: handleSend rejects with a terminal error before it
	// can buffer in the debouncer or reserve in the FIFO queue. Checked with an
	// atomic load so the signal-handler goroutine and WS request goroutines don't
	// race. It is a one-way latch — there is no "un-shutdown" in a process lifetime.
	shuttingDown atomic.Bool
}

func NewChatMethods(agents *agent.Router, sess store.SessionStore, cfg *config.Config, rl *gateway.RateLimiter, eventBus bus.EventPublisher) *ChatMethods {
	m := &ChatMethods{agents: agents, sessions: sess, cfg: cfg, rateLimiter: rl, eventBus: eventBus, runQueue: newChatRunQueue()}
	m.debouncer = newChatDebouncer(m.dispatchChatSends)
	return m
}

// SetAudioManager sets the audio manager for TTS auto-apply on WS responses.
func (m *ChatMethods) SetAudioManager(mgr *audio.Manager) {
	m.audioMgr = mgr
}

func (m *ChatMethods) SetUsageCapService(s *usagecaps.Service) {
	m.usageCaps = s
}

// SetTeamWorkClassification wires the roster/tool stores the one-call Team Work
// routing classifier reads. The classifier needs no embedding provider: routing
// is decided by a single LLM call over the durable roster, so Team Work stays
// fully available on tenants with no embedder configured.
func (m *ChatMethods) SetTeamWorkClassification(agentStore store.AgentStore, teamStore store.TeamStore, linkStore store.AgentLinkStore, skillsLoader *skills.Loader, mcpStore store.MCPAgentGrantBatchStore, builtinToolStore store.BuiltinToolStore, tenantToolStore store.BuiltinToolTenantConfigStore, toolPolicy *tools.PolicyEngine, toolRegistry *tools.Registry) {
	m.agentStore = agentStore
	m.teamStore = teamStore
	m.linkStore = linkStore
	m.skillsLoader = skillsLoader
	m.mcpStore = mcpStore
	m.builtinToolStore = builtinToolStore
	m.tenantToolStore = tenantToolStore
	m.toolPolicy = toolPolicy
	m.toolRegistry = toolRegistry
}

func (m *ChatMethods) SetProviderRegistry(registry *providers.Registry) {
	m.providerReg = registry
}

// SetTeamWorkConfigResolver wires the per-tenant Team Work classifier config
// resolver. When nil, applyTeamWorkGate falls back to the file-config values on
// m.cfg (preserving pre-isolation behavior for unit wiring that never sets it).
func (m *ChatMethods) SetTeamWorkConfigResolver(r *teamworkconfig.Resolver) {
	m.teamWorkCfg = r
}

// SetPostTurnProcessor sets the post-turn processor for team task dispatch.
func (m *ChatMethods) SetPostTurnProcessor(pt tools.PostTurnProcessor) {
	m.postTurn = pt
}

// Register adds chat methods to the router.
func (m *ChatMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChatSend, m.handleSend)
	router.Register(protocol.MethodChatHistory, m.handleHistory)
	router.Register(protocol.MethodChatAbort, m.handleAbort)
	router.Register(protocol.MethodChatInject, m.handleInject)
	router.Register(protocol.MethodChatSessionStatus, m.handleSessionStatus)
}

// handleSessionStatus returns the running state and activity for a session.
// Used by the frontend to restore UI state after switching between sessions.
func (m *ChatMethods) handleSessionStatus(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		SessionKey string `json:"sessionKey"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.SessionKey == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "sessionKey")))
		return
	}

	// Ownership check: non-admin users can only query their own sessions.
	if !requireSessionOwner(ctx, m.sessions, m.cfg, client, req.ID, params.SessionKey) {
		return
	}

	// A session is "running" from the client's perspective whenever the router
	// holds an active run OR the run queue holds a reservation — a batch that is
	// queued behind an active run, or one the FIFO worker has dequeued and is
	// classifying/setting up before its run registers with the router. Reporting
	// only the router state would flash the session idle during those transition
	// windows and let the UI submit a "fresh" send that actually queues (Phase 7
	// review 7A-H4).
	isRunning := m.agents.IsSessionBusy(params.SessionKey)
	if !isRunning && m.runQueue != nil {
		isRunning = m.runQueue.HasReservation(params.SessionKey)
	}
	var runId string
	if rid, ok := m.agents.SessionRunID(params.SessionKey); ok {
		runId = rid
	}
	var activity map[string]any
	if status := m.agents.GetActivity(params.SessionKey); status != nil {
		activity = map[string]any{
			"phase":     status.Phase,
			"tool":      status.Tool,
			"iteration": status.Iteration,
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"isRunning": isRunning,
		"runId":     runId,
		"activity":  activity,
	}))
}

// chatMediaItem represents a media file attached to a chat message.
type chatMediaItem struct {
	Path     string `json:"path"`
	Filename string `json:"filename,omitempty"`
}

type chatSendParams struct {
	Message    string          `json:"message"`
	AgentID    string          `json:"agentId"`
	SessionKey string          `json:"sessionKey"`
	Stream     bool            `json:"stream"`
	Media      json.RawMessage `json:"media,omitempty"` // []string (legacy) or []chatMediaItem
}

// parseMedia handles both legacy string paths and new {path,filename} objects.
func (p *chatSendParams) parseMedia() []chatMediaItem {
	if len(p.Media) == 0 {
		return nil
	}
	// Try new format: [{path, filename}]
	var items []chatMediaItem
	if err := json.Unmarshal(p.Media, &items); err == nil {
		return items
	}
	// Fallback: legacy ["path1", "path2"]
	var paths []string
	if err := json.Unmarshal(p.Media, &paths); err == nil {
		for _, path := range paths {
			items = append(items, chatMediaItem{Path: path})
		}
		return items
	}
	return nil
}

func (m *ChatMethods) handleSend(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	// Graceful shutdown gate (Phase 7 Decision 6 point 1): once the process is
	// shutting down, reject new sends with a terminal error BEFORE they can buffer
	// in the debouncer or reserve in the FIFO queue. This is the single admission
	// point for chat.send, so latching here is sufficient to stop new work; batches
	// already buffered/queued/running are drained by Shutdown() itself. The RPC ID
	// is resolved immediately, so a client submitting during shutdown never hangs.
	if m.shuttingDown.Load() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "gateway shutting down")))
		return
	}
	// Rate limit check per user/client
	if m.rateLimiter != nil && m.rateLimiter.Enabled() {
		key := client.UserID()
		if key == "" {
			key = client.ID()
		}
		if !m.rateLimiter.Allow(key) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRateLimitExceeded)))
			return
		}
	}

	var params chatSendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.AgentID == "" {
		// Extract agent key from session key (format: "agent:{key}:{rest}")
		// so resuming an existing session routes to the correct agent.
		if params.SessionKey != "" {
			if agentKey, _ := sessions.ParseSessionKey(params.SessionKey); agentKey != "" {
				params.AgentID = agentKey
			}
		}
		if params.AgentID == "" {
			params.AgentID = "default"
		}
	}

	loop, err := m.agents.Get(ctx, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	userID := client.UserID()
	if userID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDRequired)))
		return
	}

	providedSessionKey := params.SessionKey != ""
	sessionKey := params.SessionKey
	if sessionKey == "" {
		sessionKey = sessions.BuildWSSessionKey(params.AgentID, uuid.NewString())
	}
	params.SessionKey = sessionKey

	// Ownership check: when resuming an existing session, verify the caller owns it.
	// Skip for new sessions (Get returns nil) so first-message creation is not blocked.
	if providedSessionKey && !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, userID) {
		if sess := m.sessions.Get(ctx, sessionKey); sess != nil && sess.UserID != userID {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	item := chatSendRequest{
		ctx:        ctx,
		client:     client,
		requestID:  req.ID,
		params:     params,
		loop:       loop,
		userID:     userID,
		sessionKey: sessionKey,
		// One latch per inbound chat.send, shared by pointer across every copy the
		// request makes through the debouncer, FIFO queue, and serialized run, so
		// the request ID receives exactly one terminal RPC response no matter which
		// path resolves it first (Phase 7 review deviation #1 / trace item C).
		respondedOnce: &sync.Once{},
		// Stable logical-turn identity assigned BEFORE enqueue/reserve (Phase 7
		// Decision 4). Each inbound send starts with its own turnLifecycle; when the
		// debouncer merges several sends into one batch, the canonical turn is the
		// primary (last) request — mergeChatSendRequests' rule — so the emitter and
		// queued ack read requests[len-1].turnLifecycle and every copy in the batch
		// shares one turnID and one terminal latch. The terminal sync.Once is DISTINCT
		// from respondedOnce: respondedOnce bounds RPC-response cardinality, terminal
		// bounds lifecycle-event cardinality, and neither may suppress the other
		// (Decision 4 point 8).
		turnLifecycle: &turnLifecycle{turnID: uuid.NewString()},
	}
	debounceKey := chatDebounceKey(userID, sessionKey)
	if m.debouncer == nil {
		m.debouncer = newChatDebouncer(m.dispatchChatSends)
	}
	// Cancel is a control op. Treat it as one whenever the session is occupied —
	// either the router holds an active run OR the FIFO queue holds a reservation
	// (a batch queued behind an active run, or one dequeued and being classified
	// before its run registers). Checking only the router would miss that
	// transition window and let a cancel keyword be dispatched as an ordinary send
	// (Phase 7 review 7A-H4).
	sessionOccupied := m.agents.IsSessionBusy(sessionKey) ||
		(m.runQueue != nil && m.runQueue.HasReservation(sessionKey))
	if sessionOccupied && agent.IsExactCancelKeyword(params.Message) {
		// Resolve any buffered follow-ups (still in the debounce window) and any
		// batches queued in the reservation as cancelled, so their chat.send
		// promises do not hang until the client timeout (Phase 7 review 7A-H5).
		// Take (not Discard) hands the buffered items back so we can respond.
		if buffered := m.debouncer.Take(debounceKey); len(buffered) > 0 {
			sendChatCancelled(buffered)
		}
		if m.runQueue != nil {
			m.runQueue.Cancel(sessionKey)
		}
		m.abortChatSession(req.ID, client, sessionKey)
		return
	}
	// Media-bearing sends route through the same debouncer path as text.
	// The media floor in chatDebounceDelay guarantees a non-zero window when
	// the operator has disabled debouncing, so multi-attachment bursts coalesce
	// into a single dispatch (issue #63).
	hasMedia := len(params.parseMedia()) > 0
	delay := chatDebounceDelay(m.cfg, loop.OtherConfig(), hasMedia)
	// Push return value is the authoritative admission decision (Phase 7 closure
	// item 4): the early m.shuttingDown latch fails fast, but a request can still
	// pass that check, have CloseAndDrain run to completion, and only then reach
	// Push. A false return means the debouncer is closed and the item was neither
	// buffered nor flushed, so settle it here with a terminal shutdown error +
	// failed lifecycle instead of calling any flush path.
	if delay > 0 {
		if !m.debouncer.Push(debounceKey, delay, item) {
			sendChatError([]chatSendRequest{item}, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "gateway shutting down"))
			emitTurnLifecycle([]chatSendRequest{item}, protocol.ChatTurnFailed, "")
		}
		return
	}
	// delay == 0: Push merges into existing buffer (if any) or dispatches.
	if !m.debouncer.Push(debounceKey, 0, item) {
		sendChatError([]chatSendRequest{item}, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "gateway shutting down"))
		emitTurnLifecycle([]chatSendRequest{item}, protocol.ChatTurnFailed, "")
	}
}

func (m *ChatMethods) abortChatSession(reqID string, client *gateway.Client, sessionKey string) {
	results := m.agents.AbortRunsForSession(sessionKey)
	aborted := false
	for _, r := range results {
		if r.Stopped || r.Forced {
			aborted = true
			break
		}
	}
	client.SendResponse(protocol.NewOKResponse(reqID, map[string]any{
		"cancelled": true,
		"aborted":   aborted,
	}))
}

// Shutdown gracefully quiesces WS chat dispatch at process teardown (Phase 7
// Decision 6). It is called from the gateway signal handler BEFORE the scheduler
// and providers are torn down, so every in-flight chat.send is resolved with a
// terminal outcome instead of hanging until the client-side timeout. The steps,
// in order:
//
//  1. Latch shuttingDown so handleSend rejects any NEW send from here on. This is
//     the single admission point, so latching first guarantees the drains below
//     race against a bounded, non-growing set of work.
//  2. DrainAll the debouncer: requests still inside a debounce window never left
//     to the queue, so they have neither a queued ack nor a started run. Resolve
//     each with a terminal shutdown error (its RPC ID is still unanswered). We do
//     NOT flush them — flushing would push new batches into a queue that is
//     already shutting down.
//  3. runQueue.Shutdown(): blocks further Submits, drains every not-yet-started
//     batch with a terminal error + failed lifecycle, and cancels the batch a
//     worker has popped and is classifying (its cancelCurrent handle) so it stops
//     before RegisterRun. A queued turn that was already {queued:true}-acked gets
//     its terminal lifecycle here, never a second RPC.
//  4. Bounded cancel of active registered runs via the router's AbortAllRuns
//     (every session with an active run). AbortRun already applies the 3s grace +
//     force-release, so a stuck run cannot wedge shutdown; the run goroutine's own
//     runCtx.Err() branch resolves its chat.send cancelled and emits the terminal
//     lifecycle. We reuse the existing abort path rather than add a new one.
//
// Idempotent: a second call latches again (no-op), CloseAndDrain/Shutdown return
// nothing, and there are no active runs left to abort.
func (m *ChatMethods) Shutdown() {
	m.shuttingDown.Store(true)

	// (2) Atomically close the debouncer and resolve buffered-but-never-dispatched
	// requests. CloseAndDrain sets closed + drains in one critical section, so a
	// Push that raced past handleSend's early latch is rejected (returns false)
	// rather than buffering into the just-drained debouncer (Phase 7 closure item 4).
	if m.debouncer != nil {
		for _, batch := range m.debouncer.CloseAndDrain() {
			locale := ""
			if len(batch) > 0 {
				locale = store.LocaleFromContext(batch[0].ctx)
			}
			sendChatError(batch, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "gateway shutting down"))
			// A buffered request never reached the queue, so it never got a queued
			// ack and never emitted a queued lifecycle. Emit a failed terminal so any
			// turnId the client already holds resolves; the terminal latch makes this
			// safe even in the impossible case a lifecycle was already emitted.
			emitTurnLifecycle(batch, protocol.ChatTurnFailed, "")
		}
	}

	// (3) Drain queued batches + cancel the classifying batch.
	if m.runQueue != nil {
		m.runQueue.Shutdown()
	}

	// (4) Bounded cancel of active registered runs. AbortAllRuns aborts every
	// in-flight run; AbortRun's built-in 3s grace + force-release is the bound, so a
	// stuck run cannot wedge shutdown. The run goroutine resolves its own chat.send +
	// terminal lifecycle from runCtx.Err().
	if m.agents != nil {
		m.agents.AbortAllRuns()
	}
}

type chatTeamWorkGateOutcome struct {
	message         string
	directive       *agent.TeamWorkDirective
	disableTeamWork bool
	blockedTools    []string
	auditID         uuid.UUID
	nonExecutable   bool
}

// resolveTeamWorkSettings returns the per-tenant Team Work classifier settings
// for the request tenant. When no resolver is wired (unit wiring), it falls back
// to the file-config values on m.cfg so behavior is identical to the
// pre-isolation shared-cfg read.
func (m *ChatMethods) resolveTeamWorkSettings(ctx context.Context) teamworkconfig.Settings {
	if m.teamWorkCfg != nil {
		return m.teamWorkCfg.Resolve(ctx)
	}
	if m.cfg == nil {
		return teamworkconfig.Settings{}
	}
	s := teamworkconfig.Settings{
		ClassifierProvider: m.cfg.Gateway.TeamWorkClassifyProvider,
		ClassifierModel:    m.cfg.Gateway.TeamWorkClassifyModel,
	}
	if m.cfg.Gateway.TeamWorkClassify != nil {
		s.Enabled = *m.cfg.Gateway.TeamWorkClassify
		s.EnabledSet = true
	}
	return s
}

func (m *ChatMethods) applyTeamWorkGate(ctx context.Context, params chatSendParams, loop agent.Agent, sessionKey, runID string) chatTeamWorkGateOutcome {
	out := chatTeamWorkGateOutcome{message: params.Message}
	twSettings := m.resolveTeamWorkSettings(ctx)
	if !twSettings.ClassifyEnabled() {
		return out
	}
	agentUUID := loop.UUID()
	if agentUUID == uuid.Nil {
		return out
	}
	mode := agent.ResolveOrchestrationMode(ctx, agentUUID, m.teamStore, m.linkStore)
	if mode == agent.ModeSpawn {
		slog.Info("team_work_classify: ws skipped; no team/delegate capability", "session", sessionKey, "agent", params.AgentID)
		return out
	}
	input := teamworkclassify.BuildInputFromStores(ctx, teamworkclassify.ProfileStores{
		Agents:            m.agentStore,
		Teams:             m.teamStore,
		AgentLinks:        m.linkStore,
		PinnedSkills:      m.skillsLoader,
		MCP:               m.mcpStore,
		BuiltinTools:      m.builtinToolStore,
		TenantToolConfigs: m.tenantToolStore,
		ToolPolicy:        m.toolPolicy,
		ToolRegistry:      m.toolRegistry,
	}, teamworkclassify.BuildInputOptions{
		Mode:           teamworkclassify.Mode(mode),
		Message:        params.Message,
		RecentMessages: m.recentMessagesForTeamWorkGate(ctx, sessionKey),
		AgentID:        agentUUID,
		Timeout:        twSettings.ClassifyTimeout,
	})
	selection := providerresolve.ResolveTeamWorkClassifier(ctx, m.providerReg, twSettings.ClassifierProvider, twSettings.ClassifierModel, loop.Provider(), loop.Model())
	if selection.Warning != "" {
		slog.Warn("team_work_classify: ws classifier provider override fallback", "session", sessionKey, "agent", params.AgentID, "warning", selection.Warning, "source", selection.Source)
	} else if selection.Source != "agent_default" {
		slog.Info("team_work_classify: ws classifier provider selected", "session", sessionKey, "agent", params.AgentID, "classifier_provider", selection.ProviderName, "classifier_model", selection.Model, "source", selection.Source)
	}
	result := teamworkclassify.ClassifyRouteWithLLM(ctx, input, selection.Provider, selection.Model, m.usageCaps)
	agentUUIDForAudit := agentUUID
	decision, auditID := agent.BuildAuditedTeamWorkGateDecision(ctx, m.teamStore, result, input, teamworkclassify.ClassificationAuditInput{
		Ingress:            store.TeamWorkIngressWS,
		RunID:              runID,
		SessionKey:         sessionKey,
		AgentID:            &agentUUIDForAudit,
		OriginalMessage:    params.Message,
		ClassifierProvider: selection.ProviderName,
		ClassifierModel:    selection.Model,
	})
	out.directive = decision.Directive
	out.disableTeamWork = decision.DisableTeamWork
	out.blockedTools = decision.BlockedTools
	out.auditID = auditID
	out.nonExecutable = decision.NonExecutable
	slog.Info("team_work_classify: ws decision",
		"session", sessionKey,
		"agent", params.AgentID,
		"mode", mode,
		"decision", result.Decision,
		"intent_relation", result.IntentRelation,
		"workflow_mode", result.WorkflowMode,
		"review_required", result.EffectiveReviewRequired,
		"owner", result.BestTeamOwner,
		"workflow_step_count", func() int {
			if result.Plan == nil {
				return 0
			}
			return len(result.Plan.Steps)
		}(),
		"revised", result.PlannerRepaired,
		"disable_team_work", decision.DisableTeamWork,
		"degraded_reason", result.DegradedReasonCode,
		"staffing_gaps", result.StaffingGaps,
		"validation_reason", result.PlannerValidationReason)
	return out
}

func (m *ChatMethods) recentMessagesForTeamWorkGate(ctx context.Context, sessionKey string) []providers.Message {
	if m == nil || m.sessions == nil || sessionKey == "" {
		return nil
	}
	return m.sessions.GetHistory(ctx, sessionKey)
}

func (m *ChatMethods) dispatchChatSends(requests []chatSendRequest) {
	// Top-level entry (debouncer flush): the batch is not yet owned by a worker, so
	// there is no batch-cancellation context yet. The FIFO worker supplies one when
	// it re-enters serialized (Phase 7 review mandatory fix #5).
	m.dispatchChatSendsInternal(nil, requests, false)
}

// dispatchChatSendsInternal enqueues (serialized=false) or classifies+runs
// (serialized=true) a batch. cancelCtx is the worker's per-batch cancellation
// context, non-nil only on the serialized path: its cancellation (from a cancel
// keyword arriving while the batch is popped but before its run registers with
// the router — the window Cancel's queue drain and AbortRunsForSession both miss)
// stops the batch before RegisterRun and, via a watcher, cancels the run if the
// cancel lands right around registration (Phase 7 review mandatory fix #5).
func (m *ChatMethods) dispatchChatSendsInternal(cancelCtx context.Context, requests []chatSendRequest, serialized bool) <-chan struct{} {
	if len(requests) == 0 {
		return nil
	}
	// A debounce window is one logical turn (Decision 4 point 1), even when it
	// contains several chat.send requests. Canonicalize the entire batch to the
	// primary (last) request's pre-enqueue turnLifecycle — matching
	// mergeChatSendRequests' primary rule — so every copy through the FIFO worker
	// shares one stable turnID and one terminal sync.Once by pointer. Earlier
	// per-send turnLifecycle objects become unreachable and intentionally emit no
	// lifecycle of their own: the merged batch classifies/runs exactly once.
	canonicalTurn := requests[len(requests)-1].turnLifecycle
	for i := range requests {
		requests[i].turnLifecycle = canonicalTurn
	}
	primary := requests[len(requests)-1]
	sessionKey := primary.sessionKey

	if !serialized {
		if m.runQueue == nil {
			m.runQueue = newChatRunQueue()
		}
		// Atomic reserve-or-join under the queue lock. This single locked decision
		// closes the reserve-before-classify race (Phase 7 review 7A-H2): two
		// concurrent initial batches for an idle session can no longer both observe
		// "idle" and both classify+run. The first creates the reservation and starts
		// exactly one FIFO worker; every later batch joins it as a subsequent queue
		// entry. The classification unit is the DEQUEUED BATCH, not the individual
		// send (Phase 7 review 7A-M1): `requests` here is one debounce window, which
		// may already have merged several sends into a single turn, and the worker
		// classifies+runs each such batch exactly once at dequeue against the latest
		// history. This also removes the old InjectMessage steering: a busy follow-up
		// no longer mutates the in-flight turn. Each queued chat.send stays open and
		// resolves with its batch's run result (deferred result contract).
		res := m.runQueue.Submit(sessionKey, requests,
			func() <-chan struct{} {
				// Evaluated under the queue lock only when a new reservation is
				// created. If a run started OUTSIDE this queue (inbound consumer,
				// workflow finalize/recovery) already owns the session, the worker
				// waits on its Done before the first batch; nil starts it immediately.
				if active, ok := m.agents.SessionActiveRun(sessionKey); ok {
					return active.Done
				}
				return nil
			},
			func(batchCtx context.Context, batch []chatSendRequest) <-chan struct{} {
				return m.dispatchChatSendsInternal(batchCtx, batch, true)
			},
		)
		switch res {
		case submitJoined, submitStartedWaiting:
			// Busy follow-up (submitJoined): a reservation already existed, so this
			// turn is queued behind the active run.
			//
			// External-busy first turn (submitStartedWaiting, Phase 7 closure item 3):
			// this send created the FIFO reservation, but a run registered OUTSIDE this
			// queue (ordinary inbound, workflow finalize, or workflow recovery) already
			// owns the session, so the worker must wait on that external run's Done
			// before this batch can start. From the client's perspective the turn is
			// queued exactly like a busy follow-up, so it takes the same contract.
			//
			// Per Phase 7 review deviation #1, an accepted queued turn is acknowledged
			// immediately with a structural {queued:true} — NOT a deferred result. The
			// batch's assistant output is delivered later, exactly once, via the normal
			// run/event/history path when the FIFO worker dequeues and runs it. This ack
			// claims the per-request exactly-one-response latch, so that serialized run's
			// terminal sendChatOK is suppressed and the client never receives a second
			// RPC response for the same ID.
			sendChatQueuedAck(requests)
			// Non-terminal lifecycle hint: the turn is queued behind the active run.
			// Distinct from the RPC ack above — the ack consumes the request's
			// respondedOnce latch, this only publishes a status event keyed by the
			// stable turnId so the client can follow queued → running → terminal even
			// though no further RPC response is coming (Decision 4 points 4 & 8).
			emitTurnLifecycle(requests, protocol.ChatTurnQueued, "")
		case submitRejected:
			// Backpressure: the session already holds the max queued batches.
			sendChatError(requests, protocol.ErrInternal,
				i18n.T(store.LocaleFromContext(primary.ctx), i18n.MsgInternalError, "session busy: too many queued messages"))
		case submitShutdown:
			sendChatError(requests, protocol.ErrInternal,
				i18n.T(store.LocaleFromContext(primary.ctx), i18n.MsgInternalError, "gateway shutting down"))
		}
		// submitStarted: this send created the reservation AND the session was idle,
		// so its worker runs the batch immediately (the primary turn) and its chat.send
		// resolves with the run result through the serialized path below — the
		// pre-existing, non-queued behavior the review deliberately leaves unchanged.
		// submitStartedWaiting (handled in the switch above) also created the
		// reservation, but an external run (inbound/finalize/recovery) still owns the
		// session, so the worker waits on that run's Done before the first batch; the
		// spec (closure item 3) requires that first WS turn take the same queued-ack
		// contract as submitJoined rather than a deferred RPC. submitProbeMiss cannot
		// occur here because run is non-nil.
		return nil
	}

	// serialized == true: a batch dequeued by the FIFO worker. Classify + run it
	// exactly once against the latest history.
	//
	// Pre-classify cancellation check (Phase 7 review mandatory fix #5): if a cancel
	// keyword already cancelled this popped batch's context, resolve it cancelled and
	// do NOT run the Team Work classifier (an LLM call + durable audit write) or
	// register a run. This is the window between the worker popping the batch and
	// RegisterRun, which the reservation drain (Cancel) and AbortRunsForSession
	// (router only) both miss. Returning a nil done channel lets the worker advance
	// to the next batch immediately.
	if cancelCtx != nil {
		if err := cancelCtx.Err(); err != nil {
			sendChatCancelled(requests)
			// Terminal lifecycle for a turn cancelled while queued/classifying, before
			// any run registered (Decision 4 point 6). The terminal latch makes this a
			// no-op if a queued-ack path already emitted a terminal, and it is emitted
			// regardless of whether the RPC latch was already consumed by the ack.
			emitTurnLifecycle(requests, protocol.ChatTurnCancelled, "")
			return nil
		}
	}

	params := mergeChatSendRequests(requests)
	userID := primary.userID
	loop := primary.loop

	// Detach from HTTP request context so agent runs survive page navigation/reconnect.
	// WithoutCancel preserves all context values (locale, user ID, etc.)
	// but HTTP request cancellation no longer propagates.
	// Explicit abort via chat.abort still works through the per-run cancel().
	runCtxBase := context.WithoutCancel(primary.ctx)
	if userID != "" {
		runCtxBase = store.WithUserID(runCtxBase, userID)
	}
	// Team Work gate runs before Loop.injectContext; inject the resolved agent
	// budget now so every classifier sees the same authority.
	runCtxBase = agent.WithAgentBudget(runCtxBase, loop)
	if uid := loop.UUID(); uid != uuid.Nil {
		runCtxBase = store.WithAgentID(runCtxBase, uid)
	}
	// Generate the run ID before the gate so the classification audit records the
	// run it authorized and a workflow created during the run can link back to it.
	//
	// ORDERING INVARIANT (Phase 6 audit-before-schedule): applyTeamWorkGate writes
	// the durable classification audit synchronously. It MUST stay ahead of both
	// RegisterRun (below) and loop.Run — the run must not start before its audit is
	// persisted. Do not move the gate call after RegisterRun.
	runID := uuid.NewString()
	gate := func() chatTeamWorkGateOutcome {
		if cancelCtx == nil {
			return m.applyTeamWorkGate(runCtxBase, params, loop, sessionKey, runID)
		}
		// Preserve every request-scoped value from the detached base (tenant, user,
		// locale, agent budget/ID), but bridge the FIFO batch cancellation into the
		// classifier. A queue Cancel or Shutdown must interrupt a provider blocked in
		// the gate rather than waiting for that provider to return on its own.
		gateCtx, cancelGate := context.WithCancel(runCtxBase)
		stopGateBridge := context.AfterFunc(cancelCtx, cancelGate)
		defer func() {
			stopGateBridge()
			cancelGate()
		}()
		return m.applyTeamWorkGate(gateCtx, params, loop, sessionKey, runID)
	}()
	params.Message = gate.message

	// Post-classify cancellation recheck (Phase 7 review mandatory fix #5). The
	// pre-classify check above covers a cancel that arrived before the classifier;
	// this covers a cancel that lands WHILE applyTeamWorkGate blocks — it is an LLM
	// call plus a durable audit write and can run arbitrarily long. Without this
	// recheck the batch would register and run even though the user cancelled during
	// classification: the post-register watcher only races the cancel, whereas this
	// deterministically stops the batch before RegisterRun (no router run created, no
	// team-dispatch lock acquired). The audit already written by the gate is
	// intentionally kept: it records the classification decision, not that the run
	// executed. Resolve the batch cancelled and let the worker advance.
	if cancelCtx != nil {
		if err := cancelCtx.Err(); err != nil {
			sendChatCancelled(requests)
			// Terminal lifecycle for a turn cancelled during classification, still
			// before RegisterRun (Decision 4 point 6). Independent of the RPC latch.
			emitTurnLifecycle(requests, protocol.ChatTurnCancelled, "")
			return nil
		}
	}

	// Non-executable coordinated request (Correction A fail-closed): the classifier
	// decided this is a coordinated team request but the team cannot run it (missing
	// canonical lead, insufficient members, blocked tools/permissions). Do NOT fall
	// through to an ordinary self run — that would silently execute the work without
	// the coordination the request requires. Fail the turn closed with one
	// user-facing configuration error: an error frame on the chat.send RPC plus a
	// failed terminal lifecycle event, and NO RegisterRun, so no run ever starts.
	if gate.nonExecutable {
		locale := store.LocaleFromContext(runCtxBase)
		sendChatError(requests, protocol.ErrFailedPrecondition, i18n.T(locale, i18n.MsgTeamNotExecutable))
		emitTurnLifecycle(requests, protocol.ChatTurnFailed, "")
		return nil
	}

	// Inject team dispatch tracker: gates team_tasks create (must search/list first)
	// and defers task dispatch to post-turn.
	runCtxBase, drainTeamDispatch := tools.InjectTeamDispatch(runCtxBase, m.postTurn)
	// A coordinator directive carries the canonical lead as BestTeamOwner, but it
	// must NOT receive the single-owner constraint: the lead authors a multi-task
	// DAG (team_tasks create_dag) whose tasks fan out to many assignees, so
	// locking every create to the lead would break the workflow. Mode is the
	// single discriminator.
	if gate.directive != nil && strings.TrimSpace(gate.directive.BestTeamOwner) != "" && gate.directive.Mode != agent.TeamWorkDirectiveModeCoordinator {
		runCtxBase = tools.WithTeamWorkOwnerConstraint(runCtxBase, tools.TeamWorkOwnerConstraint{
			OwnerID:  gate.directive.BestTeamOwnerID,
			OwnerKey: strings.TrimSpace(gate.directive.BestTeamOwner),
		})
	}
	if gate.auditID != uuid.Nil {
		runCtxBase = tools.WithTeamWorkClassificationAuditID(runCtxBase, gate.auditID)
	}
	if gate.directive != nil && gate.directive.Mode == agent.TeamWorkDirectiveModeCoordinator && gate.directive.ReviewRequired {
		runCtxBase = tools.WithTeamWorkReviewRequired(runCtxBase, true)
	}

	// Create cancellable context for abort support (matching TS AbortController pattern).
	runCtx, cancel := context.WithCancel(runCtxBase)
	// Register with a forced-abort callback (Phase 7 closure item 2). If this run
	// goroutine gets genuinely stuck and never returns, AbortRun's 3s-timeout branch
	// invokes this callback at the force boundary to settle the original chat.send
	// RPC + emit one cancelled terminal — work only this goroutine would otherwise
	// do. If the goroutine later returns, its ErrRunOwnershipLost path re-enters
	// sendChatCancelled/emitTurnLifecycle, but the respondedOnce + terminal latches
	// suppress the duplicate, so the callback and the late return never double-settle.
	injectCh, generation := m.agents.RegisterRunWithOptions(runCtxBase, runID, sessionKey, params.AgentID, "", cancel, func() {
		sendChatCancelled(requests)
		emitTurnLifecycle(requests, protocol.ChatTurnCancelled, runID)
	})

	// Non-terminal lifecycle: the turn has passed classification and registered a
	// run, so link its stable turnId to the freshly assigned runId (Decision 4
	// point 5). Emitted after RegisterRun so the runId is real; not latched (only
	// terminal states use the terminal sync.Once).
	emitTurnLifecycle(requests, protocol.ChatTurnRunning, runID)

	// Bridge a batch cancellation that lands right around registration into the
	// run's own cancel (Phase 7 review mandatory fix #5). runCtx derives from
	// runCtxBase (WithoutCancel of the HTTP ctx), so the worker's per-batch
	// cancelCtx does NOT propagate into it automatically. The pre-gate check above
	// covers a cancel that arrives before classify; this watcher covers the narrow
	// window where the cancel lands after the check but before/just-after
	// RegisterRun — it cancels the run so runCtx.Err() becomes non-nil and the run
	// goroutine returns the cancelled response instead of running to completion. The
	// watcher exits when the run's context ends (normal completion cancels runCtx
	// via the goroutine's defer), so it cannot leak.
	if cancelCtx != nil {
		go func() {
			select {
			case <-cancelCtx.Done():
				cancel()
			case <-runCtx.Done():
			}
		}()
	}
	// The FIFO worker must wait on the ROUTER's Done, not a local completion
	// channel (Phase 7 review mandatory fix #3). A forced abort (AbortRun's 3s
	// grace timeout) closes the router Done via UnregisterRun even when the run
	// goroutine is genuinely stuck and never reaches its own defers. If the worker
	// waited on a local channel closed only by those defers, a stuck run would hold
	// the session's FIFO reservation forever — follow-ups would never run and the
	// router would report idle while the queue stayed wedged. Capturing routerDone
	// here (right after RegisterRun, while the run is guaranteed present) means both
	// the normal exit (goroutine defer → UnregisterRun) and the forced-abort exit
	// release the worker. The local runDone is retained only as a defensive fallback
	// for the impossible case where the router entry vanished before we read it.
	routerDone, haveRouterDone := m.agents.RunDone(runID)
	runDone := make(chan struct{})

	// Run agent asynchronously - events are broadcast via the event system
	go func() {
		// Outermost safety net (registered first => runs last): recover from any
		// panic in the run body OR the cleanup defers below so the batch's chat.send
		// promises resolve with a terminal error instead of hanging until the client
		// timeout, and a nil-provider / malformed-result panic never crashes the
		// gateway. runBatch's recover only covers the synchronous classify/setup that
		// happens before this goroutine is spawned; once the run is async, only this
		// recover can catch it (Phase 7 review 7A-C1, async-run arm). By the time it
		// runs, cancel + UnregisterRun have already released the router run and the
		// FIFO worker, so responding here cannot deadlock the queue.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("chat run goroutine panicked", "panic", r, "session", sessionKey, "run", runID)
				sendChatError(requests, protocol.ErrInternal, i18n.T(store.LocaleFromContext(primary.ctx), i18n.MsgInternalError, "chat run failed"))
				// Terminal lifecycle for the async-run panic arm (Decision 4 point 4/8).
				// Independent of the RPC latch: a turn whose RPC was already consumed by a
				// {queued:true} ack still emits exactly one failed lifecycle here.
				emitTurnLifecycle(requests, protocol.ChatTurnFailed, runID)
			}
		}()
		defer close(runDone)
		defer m.agents.UnregisterRun(runID)
		defer cancel()
		defer drainTeamDispatch() // dispatch pending team tasks + release lock (even on panic)

		// Parse media items (supports both legacy string paths and new {path,filename} objects).
		items := params.parseMedia()

		// Convert media items to bus.MediaFile with MIME detection.
		var mediaFiles []bus.MediaFile
		var mediaInfos []media.MediaInfo
		for _, item := range items {
			mimeType := media.DetectMIMEType(item.Path)
			mediaFiles = append(mediaFiles, bus.MediaFile{Path: item.Path, MimeType: mimeType, Filename: item.Filename})
			mediaInfos = append(mediaInfos, media.MediaInfo{
				Type:        media.MediaKindFromMime(mimeType),
				FilePath:    item.Path,
				ContentType: mimeType,
				FileName:    item.Filename,
			})
		}

		// Prepend media tags so the LLM knows what media is attached.
		message := params.Message
		if len(mediaInfos) > 0 {
			if tags := media.BuildMediaTags(mediaInfos); tags != "" {
				if message != "" {
					message = tags + "\n\n" + message
				} else {
					message = tags
				}
			}
		}

		result, err := loop.Run(runCtx, agent.RunRequest{
			SessionKey:        sessionKey,
			Message:           message,
			Media:             mediaFiles,
			Channel:           "ws",
			ChatID:            userID, // use stable userID for team/workspace isolation (not ephemeral client.ID())
			WorkspaceChatID:   userID, // mirror ChatID so vault chat_id isolation activates for WS direct flow
			RunID:             runID,
			UserID:            userID,
			Stream:            params.Stream,
			TeamWorkDirective: gate.directive,
			DisableTeamWork:   gate.disableTeamWork,
			BlockedTools:      gate.blockedTools,
			InjectCh:          injectCh,
			// Wire trace ID back to the active run so force-abort can mark the
			// correct trace as cancelled if the goroutine does not exit within 3s.
			OnTraceCreated: func(traceID uuid.UUID) {
				m.agents.SetRunTraceID(runID, traceID)
			},
			// Ownership fence (Phase 7 Decision 3): the loop consults this before
			// every user-visible commit so a run whose ownership was lost — force
			// aborted or superseded by a replacement run on this session — cannot
			// append history, save the session, or emit a final success event into
			// a session another run now owns.
			IsCurrentOwner: func() bool { return m.agents.IsCurrentOwner(sessionKey, runID, generation) },
		})

		if err != nil {
			// Outer-delivery fence (Phase 7 closure item 1): a run that lost session
			// ownership (force-aborted or superseded) returns ErrRunOwnershipLost. Its
			// inner guards already suppressed history/session commit and run.completed;
			// here we settle the original chat.send as cancelled and emit exactly one
			// cancelled terminal, then return BEFORE title/TTS/content/completed. The
			// replacement/current owner delivers the real answer. This must precede the
			// runCtx.Err() and generic error branches so a stale success cannot leak.
			if errors.Is(err, agent.ErrRunOwnershipLost) {
				sendChatCancelled(requests)
				emitTurnLifecycle(requests, protocol.ChatTurnCancelled, runID)
				return
			}
			// Send cancelled response so the frontend's chat.send promise resolves
			// instead of hanging until the 600s timeout.
			if runCtx.Err() != nil {
				sendChatOK(requests, map[string]any{"cancelled": true})
				// A run cancelled after it registered (abort / superseded) emits a
				// terminal cancelled lifecycle (Decision 4 point 6). runCtx.Err() is the
				// authoritative cancel signal; the RPC latch is independent.
				emitTurnLifecycle(requests, protocol.ChatTurnCancelled, runID)
				return
			}
			sendChatError(requests, protocol.ErrInternal, err.Error())
			emitTurnLifecycle(requests, protocol.ChatTurnFailed, runID)
			return
		}

		// Auto-generate conversation title on first message (label empty = never titled).
		if label := m.sessions.GetLabel(primary.ctx, sessionKey); label == "" {
			agentProvider := loop.Provider()
			agentModel := loop.Model()
			userMsg := params.Message
			// Use runCtxBase (WithoutCancel + tenant-aware) so title save uses correct tenant.
			titleCtx := runCtxBase
			go func() {
				if uid := loop.UUID(); uid != uuid.Nil {
					titleCtx = store.WithAgentID(titleCtx, uid)
				}
				title := agent.GenerateTitleWithUsageCaps(titleCtx, m.usageCaps, agentProvider, agentModel, userMsg)
				if title == "" {
					return
				}
				m.sessions.SetLabel(titleCtx, sessionKey, title)
				if err := m.sessions.Save(titleCtx, sessionKey); err != nil {
					slog.Warn("failed to save session title", "sessionKey", sessionKey, "error", err)
					return
				}
				bus.BroadcastForTenant(m.eventBus, protocol.EventSessionUpdated,
					primary.client.TenantID(),
					map[string]string{"sessionKey": sessionKey, "label": title, "userId": userID})
			}()
		}

		// TTS auto-apply: convert [[tts]] tagged responses to voice audio
		content := result.Content
		var ttsAudio *agent.MediaResult
		if m.audioMgr != nil && content != "" {
			// For WS, we don't have voice inbound info - use "tagged" mode only
			ttsResult, _ := m.audioMgr.AutoApplyToText(runCtx, content, "ws", false, "")
			if ttsResult != nil && ttsResult.AudioPath != "" {
				// Include audio in media results
				ttsAudio = &agent.MediaResult{
					Path:        httpapi.SignMediaPath(ttsResult.AudioPath, httpapi.FileSigningKey()),
					ContentType: ttsResult.AudioMime,
					AsVoice:     true,
				}
				content = ttsResult.Text // Use stripped text
			} else if ttsResult != nil {
				content = ttsResult.Text // Strip directives even if TTS not applied
			}
		}

		resp := map[string]any{
			"runId":   result.RunID,
			"content": content,
			"usage":   result.Usage,
		}
		if result.Thinking != "" {
			resp["thinking"] = result.Thinking
		}
		// Combine existing media with TTS audio
		mediaResults := result.Media
		if ttsAudio != nil {
			mediaResults = append([]agent.MediaResult{*ttsAudio}, mediaResults...)
		}
		if len(mediaResults) > 0 {
			resp["media"] = mediaResults
		}
		sendChatOK(requests, resp)
		// Terminal lifecycle: the run committed its assistant result through the
		// normal history/run-event path. This chat.turn frame is a status hint only
		// and carries NO assistant content (Decision 4 point 7); the terminal latch
		// guarantees it is the single terminal for this turn. Independent of the RPC
		// latch, so a turn whose RPC was consumed by a {queued:true} ack still emits
		// its completed lifecycle here (Decision 4 point 8).
		emitTurnLifecycle(requests, protocol.ChatTurnCompleted, runID)
	}()
	// The worker waits on routerDone: closed by UnregisterRun on BOTH the normal
	// goroutine exit and the forced-abort path (mandatory fix #3), so a genuinely
	// stuck run no longer wedges the FIFO. runDone is the fallback only if the
	// router entry was somehow absent at RunDone time (should not happen: we just
	// registered it).
	if haveRouterDone {
		return routerDone
	}
	return runDone
}

// respondChatOnce sends resp for one request iff its exactly-one-RPC latch has
// not already fired (Phase 7 review deviation #1 / trace item C). The latch is
// shared by pointer across every copy of the request, so whichever terminal
// path runs first — the queued ack, the run result, a cancel, or a panic-path
// error — wins and every later send for the same request ID is dropped. A nil
// latch means "no latch wired" (direct send), preserving the pre-deviation
// single-response behavior for callers that build requests inline.
func respondChatOnce(request chatSendRequest, resp *protocol.ResponseFrame) {
	if request.respondedOnce == nil {
		request.client.SendResponse(resp)
		return
	}
	request.respondedOnce.Do(func() {
		request.client.SendResponse(resp)
	})
}

func sendChatOK(requests []chatSendRequest, payload map[string]any) {
	for _, request := range requests {
		respondChatOnce(request, protocol.NewOKResponse(request.requestID, payload))
	}
}

func sendChatError(requests []chatSendRequest, code, message string) {
	for _, request := range requests {
		respondChatOnce(request, protocol.NewErrorResponse(request.requestID, code, message))
	}
}

// sendChatCancelled resolves every request in a batch with the same cancelled
// payload the abort path returns, so a follow-up drained from the reservation
// queue (Phase 7 review 7A-H5) or superseded by cancellation never leaves its
// chat.send promise hanging until the client-side timeout.
func sendChatCancelled(requests []chatSendRequest) {
	for _, request := range requests {
		respondChatOnce(request, protocol.NewOKResponse(request.requestID, map[string]any{"cancelled": true}))
	}
}

// sendChatQueuedAck emits the structural {queued:true, turnId} acknowledgement
// for a busy follow-up the moment it joins the per-session FIFO (Phase 7 review
// deviation #1 + Decision 4). It is NOT the turn's result: the assistant output
// is delivered later, exactly once, through the normal run/event/history path
// when the FIFO worker dequeues and runs the batch. Claiming the latch here is
// what guarantees the batch's serialized run does not emit a second RPC response
// for the same ID. The turnId lets the client correlate this ack with the
// subsequent chat.turn lifecycle events (queued → running → terminal) even after
// the RPC latch is consumed — Decision 4's reason for a stable logical turn
// identity distinct from the request ID. Requests without a latch (should not
// happen on the queued path) fall back to a direct send.
func sendChatQueuedAck(requests []chatSendRequest) {
	payload := map[string]any{"queued": true}
	if len(requests) > 0 {
		if tl := requests[len(requests)-1].turnLifecycle; tl != nil {
			payload["turnId"] = tl.turnID
		}
	}
	for _, request := range requests {
		respondChatOnce(request, protocol.NewOKResponse(request.requestID, payload))
	}
}

// emitTurnLifecycle pushes one chat.turn lifecycle frame (Phase 7 Decision 4) to
// the client that owns the batch's canonical turn. A debounce batch is one
// logical turn; its canonical turn is the primary (last) request — matching
// mergeChatSendRequests — so every copy shares one turnID and one terminal latch
// by pointer. The frame is a status/refetch hint keyed by the stable turnID: the
// assistant result still flows through the normal run/history path and is NEVER
// duplicated here (Decision 4 point 7).
//
// Terminal states (completed | cancelled | failed) are guarded by the turn's
// terminal sync.Once so EXACTLY ONE terminal frame is emitted per turn no matter
// which path resolves it — the serialized run's success/error/cancel exits, the
// pre/post-classify cancel checks, or the queue-owned drain/shutdown/panic
// callback. That latch is DISTINCT from respondedOnce: respondedOnce bounds RPC
// cardinality, terminal bounds lifecycle cardinality, and neither suppresses the
// other (Decision 4 point 8) — so a turn whose RPC was already consumed by a
// {queued:true} ack still emits its terminal lifecycle when it is later run or
// cancelled. Non-terminal states (queued, running) are emitted directly; running
// carries the runID so the client can link turnId → runId (Decision 4 point 5).
//
// A nil turnLifecycle disables the emit (direct/unit sends that build requests
// inline), preserving pre-Decision-4 behavior.
func emitTurnLifecycle(requests []chatSendRequest, state, runID string) {
	if len(requests) == 0 {
		return
	}
	canonical := requests[len(requests)-1]
	tl := canonical.turnLifecycle
	if tl == nil || canonical.client == nil {
		return
	}
	emit := func() {
		payload := map[string]any{
			"turnId":     tl.turnID,
			"state":      state,
			"sessionKey": canonical.sessionKey,
		}
		if runID != "" {
			payload["runId"] = runID
		}
		canonical.client.SendEvent(*protocol.NewEvent(protocol.EventChatTurn, payload))
	}
	switch state {
	case protocol.ChatTurnCompleted, protocol.ChatTurnCancelled, protocol.ChatTurnFailed:
		tl.terminal.Do(emit)
	default:
		emit()
	}
}

type chatHistoryParams struct {
	AgentID    string `json:"agentId"`
	SessionKey string `json:"sessionKey"`
}

func (m *ChatMethods) handleHistory(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params chatHistoryParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.AgentID == "" {
		params.AgentID = "default"
	}

	sessionKey := params.SessionKey
	if sessionKey == "" {
		sessionKey = sessions.BuildWSSessionKey(params.AgentID, uuid.NewString())
	}

	// Ownership check: non-admin users can only read their own session history.
	if params.SessionKey != "" && !requireSessionOwner(ctx, m.sessions, m.cfg, client, req.ID, sessionKey) {
		return
	}

	history := m.sessions.GetHistory(ctx, sessionKey)

	// Sign file URLs before delivery — sessions store clean paths.
	secret := httpapi.FileSigningKey()
	for i := range history {
		history[i].Content = httpapi.SignFileURLs(history[i].Content, secret)
		for j := range history[i].MediaRefs {
			history[i].MediaRefs[j].Path = httpapi.SignMediaPath(history[i].MediaRefs[j].Path, secret)
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"messages": history,
	}))
}

// handleInject injects a message into a session transcript without running the agent.
// Matching TS chat.inject (src/gateway/server-methods/chat.ts:686-746).
func (m *ChatMethods) handleInject(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		SessionKey string `json:"sessionKey"`
		Message    string `json:"message"`
		Label      string `json:"label"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.SessionKey == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "sessionKey")))
		return
	}
	if params.Message == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMsgRequired)))
		return
	}

	// Ownership check: non-admin users can only inject into their own sessions.
	if !requireSessionOwner(ctx, m.sessions, m.cfg, client, req.ID, params.SessionKey) {
		return
	}

	// Truncate label
	if len(params.Label) > 100 {
		params.Label = params.Label[:100]
	}

	// Build content text
	text := params.Message
	if params.Label != "" {
		text = "[" + params.Label + "]\n\n" + params.Message
	}

	// Create an assistant message with gateway-injected metadata
	messageID := uuid.NewString()
	m.sessions.AddMessage(ctx, params.SessionKey, providers.Message{
		Role:    "assistant",
		Content: text,
	})

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":        true,
		"messageId": messageID,
	}))
}

// handleAbort cancels running agent invocations.
// Matching TS chat-abort.ts: validates sessionKey, supports per-runId or per-session abort.
//
// Params:
//
//	{ sessionKey: string, runId?: string }
//
// Response:
//
//	{ ok: true, aborted: bool, stopped: bool, forced: bool,
//	  alreadyAborting: bool, notFound: bool, unauthorized: bool, runIds: []string }
func (m *ChatMethods) handleAbort(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		RunID      string `json:"runId"`
		SessionKey string `json:"sessionKey"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.SessionKey == "" && params.RunID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "sessionKey or runId")))
		return
	}

	// Non-admin users must provide sessionKey for ownership verification.
	if params.SessionKey == "" && params.RunID != "" && !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "sessionKey")))
		return
	}

	// Ownership check: non-admin users can only abort their own sessions.
	if params.SessionKey != "" && !requireSessionOwner(ctx, m.sessions, m.cfg, client, req.ID, params.SessionKey) {
		return
	}

	isAdmin := canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID())

	// Collect abort results.
	var results []agent.AbortResult
	if params.RunID != "" {
		results = []agent.AbortResult{m.agents.AbortRun(params.RunID, params.SessionKey)}
	} else {
		results = m.agents.AbortRunsForSession(params.SessionKey)
	}

	// Aggregate counts and run IDs.
	var runIDs []string
	stopped, forced, alreadyAborting, notFound, unauthorized := 0, 0, 0, 0, 0
	for _, r := range results {
		runIDs = append(runIDs, r.RunID)
		switch {
		case r.Stopped:
			stopped++
		case r.Forced:
			forced++
		case r.AlreadyAborting:
			alreadyAborting++
		case r.NotFound:
			notFound++
		case r.Unauthorized:
			unauthorized++
			slog.Warn("chat.abort: unauthorized run abort attempt",
				"runId", r.RunID, "userID", client.UserID())
		}
	}

	// Security: collapse Unauthorized → NotFound for non-admin callers so run
	// existence is not leaked to unprivileged clients.
	respUnauthorized := unauthorized
	if !isAdmin && unauthorized > 0 {
		notFound += unauthorized
		respUnauthorized = 0
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":              true,
		"aborted":         stopped+forced > 0,
		"stopped":         stopped > 0,
		"forced":          forced > 0,
		"alreadyAborting": alreadyAborting > 0,
		"notFound":        notFound > 0 && stopped+forced+alreadyAborting == 0,
		"unauthorized":    respUnauthorized > 0,
		"runIds":          runIDs,
	}))
}
