package methods

import (
	"context"
	"encoding/json"
	"errors"

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
	mediastore "github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ChatMethods handles chat.send, chat.history, chat.abort, chat.inject.
type ChatMethods struct {
	agents      *agent.Router
	sessions    store.SessionStore
	cfg         *config.Config
	rateLimiter *gateway.RateLimiter
	eventBus    bus.EventPublisher
	postTurn    tools.PostTurnProcessor
	audioMgr    *audio.Manager  // for TTS auto-apply on WS responses (nil = disabled)
	tools       *tools.Registry // used to route chat.toolResult → pending client tool channels
	// mediaStore is consulted when normalizing client-supplied media
	// paths at the chat.send boundary. With S3-backed deployments the
	// store hydrates the local cache from S3 when the file isn't on
	// this instance yet (multi-instance prod ASG sibling-upload case).
	// Nil on single-instance/FSBackend deploys — boundary still strips
	// signed-URL wrappers, just skips the S3 fetch step.
	mediaStore *mediastore.Store
	// cliChat serves chat.send for an interactive-CLI conversation
	// (session keys headed "cli:"). Nil on a deployment without it —
	// such a session key then gets a clear "not available" error
	// instead of being run as an agent turn. See chat_cli.go.
	cliChat *CLIChat
	// subagentMgr is consulted by handleActiveSessions to enrich the
	// reload-snapshot with in-memory state for any subagents still
	// streaming under each active run. Nil-safe; when unset the
	// snapshot ships without the .subagents map (back-compat with
	// pre-Path-B clients/servers).
	subagentMgr *tools.SubagentManager
}

func NewChatMethods(agents *agent.Router, sess store.SessionStore, cfg *config.Config, rl *gateway.RateLimiter, eventBus bus.EventPublisher) *ChatMethods {
	return &ChatMethods{agents: agents, sessions: sess, cfg: cfg, rateLimiter: rl, eventBus: eventBus}
}

// SetAudioManager sets the audio manager for TTS auto-apply on WS responses.
func (m *ChatMethods) SetAudioManager(mgr *audio.Manager) {
	m.audioMgr = mgr
}

// SetToolRegistry sets the tool registry so handleToolResult can route client-tool
// results back to the agent goroutine that is blocked waiting on the call.
func (m *ChatMethods) SetToolRegistry(reg *tools.Registry) {
	m.tools = reg
}

// SetPostTurnProcessor sets the post-turn processor for team task dispatch.
func (m *ChatMethods) SetPostTurnProcessor(pt tools.PostTurnProcessor) {
	m.postTurn = pt
}

// SetMediaStore wires the media store so chat.send can normalize
// client-supplied media paths before they flow into the agent loop.
// Without this the boundary still strips signed-URL wrappers but
// can't hydrate the local cache from S3 — that case degrades to the
// current "ENOENT on sibling-instance upload" behaviour.
func (m *ChatMethods) SetMediaStore(s *mediastore.Store) {
	m.mediaStore = s
}

// SetSubagentManager wires the subagent manager so handleActiveSessions
// can attach live in-memory subagent state to each ActiveRunSnapshot
// (text + thinking + tool history per parent spawn tool_call.id).
// Nil-safe; without it the snapshot ships without a .subagents map.
func (m *ChatMethods) SetSubagentManager(sm *tools.SubagentManager) {
	m.subagentMgr = sm
}

// Register adds chat methods to the router.
func (m *ChatMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChatSend, m.handleSend)
	router.Register(protocol.MethodChatHistory, m.handleHistory)
	router.Register(protocol.MethodChatAbort, m.handleAbort)
	router.Register(protocol.MethodChatInject, m.handleInject)
	router.Register(protocol.MethodChatSessionStatus, m.handleSessionStatus)
	router.Register(protocol.MethodChatActiveSessions, m.handleActiveSessions)
	router.Register(protocol.MethodChatToolResult, m.handleToolResult)
	router.Register(protocol.MethodRunsSubscribe, m.handleRunsSubscribe)
}

// handleRunsSubscribe is the resumable-stream replay endpoint. The
// client supplies (runId, sinceSeq); we return every buffered event for
// that run whose Seq is greater than sinceSeq, in emit order. Live
// events arriving after the response continue through the normal
// broadcast → WS fan-out, so the client receives the gap-fill AND the
// live tail seamlessly.
//
// When the run is no longer in the in-memory map (already evicted /
// never existed), we return an empty `events` array. The caller's UI
// state machine still has whatever it accumulated, plus the saved
// final assistant content from sessions.preview if it wants more.
//
// No ownership check: the WS gateway only delivers events the client
// would already be allowed to see (per clientCanReceiveEvent). Sub-
// scribing to someone else's run just returns nothing — events are
// filtered by user/tenant on broadcast.
func (m *ChatMethods) handleRunsSubscribe(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		RunID    string `json:"runId"`
		SinceSeq int64  `json:"sinceSeq"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.RunID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "runId")))
		return
	}
	events := m.agents.EventsSince(params.RunID, params.SinceSeq)
	// Always emit a (possibly empty) array — null would force a special
	// case on the client.
	if events == nil {
		events = []agent.BufferedEvent{}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runId":  params.RunID,
		"events": events,
	}))
}

// handleActiveSessions returns the calling user's currently-running agent
// runs across all of their sessions, including the partially-streamed
// assistant content/thinking so the SPA can rebuild the in-flight chat
// bubble after a page reload.
//
// Filtered to the caller's userID + tenant — admins do NOT see other
// users' runs here. (chat.session.status is single-session and uses a
// different ownership check for that.)
func (m *ChatMethods) handleActiveSessions(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tenantID := client.TenantID()
	userID := client.UserID()
	snapshots := m.agents.ActiveSessionsForUser(tenantID, userID)
	if snapshots == nil {
		snapshots = []agent.ActiveRunSnapshot{}
	}

	// Path B reload-recovery: attach in-memory subagent state to each
	// run's snapshot so the SPA can rehydrate the nested mini-chat
	// (text + thinking + tool steps) symmetrically with the parent's
	// own bubble. Without this, after a page reload the parent's bubble
	// shows but the subagent panel is empty until the next live event
	// from each child arrives (sometimes never, if the child already
	// finished mid-stream). Keyed by parent spawn tool_call.id so the
	// frontend can attach each snapshot to its existing chip.
	//
	// One pass over the in-memory task map per call — bounded by the
	// active-subagent budget and only fires on WS connect/reconnect,
	// so the cost is negligible.
	if m.subagentMgr != nil && len(snapshots) > 0 {
		viewsByPTC := m.subagentMgr.SnapshotsByParentToolCallID()
		if len(viewsByPTC) > 0 {
			for i := range snapshots {
				snap := &snapshots[i]
				var subagents map[string]agent.SubagentSnapshot
				for _, tc := range snap.ToolCalls {
					view, ok := viewsByPTC[tc.ID]
					if !ok {
						continue
					}
					if subagents == nil {
						subagents = make(map[string]agent.SubagentSnapshot, len(snap.ToolCalls))
					}
					subagents[tc.ID] = toAgentSubagentSnapshot(view)
				}
				if len(subagents) > 0 {
					snap.Subagents = subagents
				}
			}
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runs": snapshots,
	}))
}

// toAgentSubagentSnapshot maps the tools-package value view onto the
// agent-package wire type. Kept in chat.go (not agent.go) so the agent
// package doesn't have to know about tools internals — Router stays
// decoupled from the subagent layer and this handler is the only seam.
func toAgentSubagentSnapshot(view tools.SubagentSnapshotView) agent.SubagentSnapshot {
	out := agent.SubagentSnapshot{
		ID:           view.ID,
		Label:        view.Label,
		Task:         view.Task,
		Model:        view.Model,
		Status:       view.Status,
		Content:      view.Result,
		Thinking:     view.Thinking,
		InputTokens:  view.TotalInputTokens,
		OutputTokens: view.TotalOutputTokens,
	}
	if len(view.ToolHistory) > 0 {
		out.ToolHistory = make([]agent.SubagentToolHistoryEntry, 0, len(view.ToolHistory))
		for _, rec := range view.ToolHistory {
			out.ToolHistory = append(out.ToolHistory, agent.SubagentToolHistoryEntry{
				Name:       rec.Name,
				Status:     rec.Status,
				DurationMs: rec.DurationMs,
			})
		}
	}
	return out
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

	isRunning := m.agents.IsSessionBusy(params.SessionKey)
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
	PageHint   *pageHint       `json:"pageHint,omitempty"`
	Model      string          `json:"model,omitempty"`
	// ClientKind identifies the WS caller so the tool palette can be tailored:
	// "extension" exposes browser page-tools (execute_action, execute_js, etc.),
	// any other value (e.g. "website") strips them. Empty = legacy/extension
	// behavior — backward compatible until all clients tag themselves.
	ClientKind string `json:"clientKind,omitempty"`
}

// pageHint carries the URL+title of the user's currently active browser tab.
// Sent by the web-agent extension on every chat.send so the LLM can decide
// whether to call refresh_page_content without paying for an unconditional
// HTML snapshot per turn.
type pageHint struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
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

	// EARLY BRANCH — interactive CLI conversation.
	//
	// A "cli:" session key is a conversation with a connected coding CLI, not an
	// agent: there is no agent to resolve, no loop to run and no provider to
	// call. Routing here — before the agent is resolved and before ANY of the
	// work below (run registration, message persistence, team-dispatch
	// injection, the run goroutine) — is what keeps the normal path unchanged:
	// routeForSessionKey is a pure function of the session key, so for every
	// existing session it returns routeAgent and execution falls straight
	// through as it did before this branch existed.
	//
	// Nothing below this point may be moved above it.
	if routeForSessionKey(params.SessionKey) == routeCLI {
		m.dispatchCLISend(ctx, client, req, params)
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

	runID := uuid.NewString()
	sessionKey := params.SessionKey
	if sessionKey == "" {
		sessionKey = sessions.BuildWSSessionKey(params.AgentID, uuid.NewString())
	}

	// Ownership check: when resuming an existing session, verify the caller owns it.
	// Skip for new sessions (Get returns nil) so first-message creation is not blocked.
	if params.SessionKey != "" && !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, userID) {
		if sess := m.sessions.Get(ctx, sessionKey); sess != nil && sess.UserID != userID {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	// Detach from HTTP request context so agent runs survive page navigation/reconnect.
	// WithoutCancel preserves all context values (locale, user ID, etc.)
	// but HTTP request cancellation no longer propagates.
	// Explicit abort via chat.abort still works through the per-run cancel().
	runCtxBase := context.WithoutCancel(ctx)
	if userID != "" {
		runCtxBase = store.WithUserID(runCtxBase, userID)
	}

	// Mid-run injection: if session already has an active run, inject the message
	// into the running loop instead of starting a new concurrent run.
	if m.agents.IsSessionBusy(sessionKey) {
		// Exact cancel keyword detection: auto-abort when user sends "stop", "cancel", etc.
		if agent.IsExactCancelKeyword(params.Message) {
			results := m.agents.AbortRunsForSession(sessionKey)
			aborted := false
			for _, r := range results {
				if r.Stopped || r.Forced {
					aborted = true
					break
				}
			}
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
				"cancelled": true,
				"aborted":   aborted,
			}))
			return
		}

		injected := m.agents.InjectMessage(sessionKey, agent.InjectedMessage{
			Content: params.Message,
			UserID:  userID,
		})
		if injected {
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
				"injected": true,
			}))
			return
		}
		// Fallback: injection failed (channel full), proceed with new run
	}

	// Inject team dispatch tracker: gates team_tasks create (must search/list first)
	// and defers task dispatch to post-turn.
	runCtxBase, drainTeamDispatch := tools.InjectTeamDispatch(runCtxBase, m.postTurn)

	// Persist the user message + session row SYNCHRONOUSLY at the request
	// boundary, before any async work spawns. Industry-standard chat
	// pattern (see WebSocket.org chat-app guides, Render real-time-AI
	// chat) — write user input to durable storage BEFORE handing off
	// to the worker. Guarantees:
	//   * page reload mid-generation always shows the user's own message
	//     (sessions.preview reads it back)
	//   * chat.abort + sessions.delete + sessions.preview ownership
	//     checks (which call sessions.Get) succeed for sessions that
	//     would otherwise be in-memory only
	//   * Save errors propagate to the client as 5xx instead of
	//     silently letting the run start against undurable state
	// Media refs are attached to this same message later in
	// makeEnrichMedia via SetLastUserMessageMediaRefs — no double write.
	//
	// `hasMedia` covers the image-without-text case (and any other
	// media-only send): the boundary still needs a fresh user row so that
	// (a) sessions.list shows the chat after reload, and (b) the upcoming
	// SetLastUserMessageMediaRefs attaches refs to THIS turn's row instead
	// of falling back to the most recent prior user message (which would
	// visually paste the new image onto the previous turn AND let the next
	// assistant reply merge with the previous one in the SPA's
	// preview-load accumulator).
	hasMedia := len(params.parseMedia()) > 0
	if params.Message != "" || hasMedia {
		// Stamp identity + channel BEFORE the message + Save. Without
		// this the row lands in PG with empty user_id / channel, so
		// sessions.list (which filters by user_id + channel='ws' for
		// non-admin WS clients) drops it — the user reloads and their
		// own chat is invisible. sessions.Get-based ownership checks
		// (chat.abort, sessions.delete, sessions.preview) ALSO fail
		// because session.UserID != client.UserID() collapses to a
		// 401/404. agent_uuid is set later by loop.Run; uuid.Nil here
		// is intentional (SetAgentInfo skips Nil overwrites).
		m.sessions.SetAgentInfo(runCtxBase, sessionKey, uuid.Nil, userID)
		m.sessions.UpdateMetadata(runCtxBase, sessionKey, "", "", "ws")
		m.sessions.AddMessage(runCtxBase, sessionKey, providers.Message{
			Role:    "user",
			Content: params.Message,
		})
		if err := m.sessions.Save(runCtxBase, sessionKey); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
				"failed to persist user message: "+err.Error()))
			return
		}
	}

	// Create cancellable context for abort support (matching TS AbortController pattern).
	runCtx, cancel := context.WithCancel(runCtxBase)
	injectCh := m.agents.RegisterRun(runCtxBase, runID, sessionKey, params.AgentID, userID, "ws", cancel)

	// Run agent asynchronously - events are broadcast via the event system
	//
	// loopErr is captured from loop.Run below and read by the partial-save
	// defer. Gating on loop.Run's actual return (rather than ActiveRun state
	// flags) avoids the race where AbortRun could CAS Aborting=1 in the
	// millisecond between flushMessages persisting normally and the
	// goroutine running its defers — which would have caused a duplicate
	// assistant turn.
	var loopErr error
	go func() {
		defer m.agents.UnregisterRun(runID)
		defer cancel()
		// Persist whatever the LLM streamed before an external abort
		// interrupted the normal end-of-turn flushMessages. Without this,
		// the partial assistant text exists in client UI + server buffer
		// only — reload after Stop wipes it. Fires BEFORE cancel +
		// UnregisterRun (LIFO), so RunBuffer can still read the ActiveRun.
		defer func() {
			// Gate on runCtx.Err() — it is non-nil iff external cancellation
			// (AbortRun → run.Cancel) reached this run. Reliable across all
			// provider/transport error wrapping, unlike errors.Is(loopErr,
			// context.Canceled) which depends on the error chain implementing
			// Unwrap properly. The local `defer cancel()` fires AFTER this
			// defer (LIFO), so its contribution to runCtx.Err() is not yet
			// visible — what we read here reflects only external aborts.
			//
			// Fallback: also fire if loopErr wraps context.Canceled, since
			// some providers may bubble the cancel signal through their own
			// error path even before runCtx is cancelled at the agent level.
			cancelled := runCtx.Err() != nil ||
				(loopErr != nil && errors.Is(loopErr, context.Canceled))
			if !cancelled {
				return
			}
			if m.agents == nil {
				return
			}
			content, thinking, toolCalls, ok := m.agents.RunBuffer(runID)
			slog.Info("partial-save: entering defer",
				"sessionKey", sessionKey, "runID", runID,
				"ctxErr", runCtx.Err(), "loopErr", loopErr,
				"bufferOk", ok, "contentLen", len(content),
				"thinkingLen", len(thinking), "toolCalls", len(toolCalls))
			// Skip only if absolutely nothing was streamed (no text, no
			// tool calls). A turn that did tools but no final text is
			// still worth persisting so the reload-recovery UI can show
			// the tool indicators.
			if !ok || (content == "" && len(toolCalls) == 0) {
				return
			}
			// Idempotency: if flushMessages happened to land the same
			// assistant turn before cancellation propagated, the tail of
			// history already contains it — don't double-write.
			hist := m.sessions.GetHistory(runCtxBase, sessionKey)
			for i := len(hist) - 1; i >= 0; i-- {
				if hist[i].Role == "user" {
					break
				}
				if hist[i].Role == "assistant" && hist[i].Content == content && content != "" {
					slog.Info("partial-save: skipped (already in history)",
						"sessionKey", sessionKey)
					return
				}
			}
			// Synthesise tool_calls on the assistant message so the
			// reload-recovery UI sees the same indicators it saw during
			// the live stream. Args are omitted (router buffer doesn't
			// store them) — UI only needs id + name for the indicator.
			msg := providers.Message{Role: "assistant", Content: content}
			if thinking != "" {
				msg.Thinking = thinking
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = make([]providers.ToolCall, 0, len(toolCalls))
				for _, tc := range toolCalls {
					msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{
						ID:   tc.ID,
						Name: tc.Name,
					})
				}
			}
			m.sessions.AddMessage(runCtxBase, sessionKey, msg)
			// Append a role="tool" message for every tool call that
			// produced a result so the conversation tail is well-formed.
			// Running calls (status="running") are skipped — they were
			// interrupted before completion.
			for _, tc := range toolCalls {
				if tc.Status != "done" && tc.Status != "error" {
					continue
				}
				m.sessions.AddMessage(runCtxBase, sessionKey, providers.Message{
					Role:       "tool",
					Content:    tc.Result,
					ToolCallID: tc.ID,
					IsError:    tc.Status == "error",
				})
			}
			if err := m.sessions.Save(runCtxBase, sessionKey); err != nil {
				slog.Warn("partial-save: Save failed",
					"sessionKey", sessionKey, "error", err)
				return
			}
			slog.Info("partial-save: persisted",
				"sessionKey", sessionKey, "contentLen", len(content),
				"toolCallsSaved", len(toolCalls))
		}()
		defer drainTeamDispatch() // dispatch pending team tasks + release lock (even on panic)

		// Parse media items (supports both legacy string paths and new {path,filename} objects).
		items := params.parseMedia()

		// Normalize media paths at the chat.send boundary so downstream
		// code (vision pipeline, read_image tool, document extractors)
		// trusts the path it gets.
		//
		// Three real shapes arrive here from clients:
		//   • Clean local path on the SAME instance that uploaded the file
		//     (FSBackend, or S3Backend with a warm cache). Returned as-is.
		//   • `/v1/files/<path>?ft=<token>` signed URL — emitted by the
		//     chat layer when serving messages to the SPA. The SPA echoes
		//     this exact string back as the attachment path on the next
		//     chat.send, which is how the URL leaked into the file path
		//     and broke vision / read_image on prod.
		//   • Clean local path that DOESN'T exist on this instance because
		//     the file was uploaded on a sibling EC2 in the prod ASG. The
		//     S3 backend has the bytes; we just need to ask it to populate
		//     the local cache.
		//
		// mediastore.ResolveLocalPath strips the URL wrapper and, when
		// the store is wired, asks LocalPath(id) to hydrate the cache.
		// Errors are non-fatal — downstream still gets the path the
		// client sent and produces the canonical "no such file" error
		// if the resolver couldn't do better.
		for i := range items {
			if resolved, err := mediastore.ResolveLocalPath(items[i].Path, m.mediaStore); err == nil {
				items[i].Path = resolved
			}
		}

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

		// Prepend page_hint so the LLM sees the user's current browser tab URL+title
		// on every turn. Lets the model decide whether to call refresh_page_content
		// instead of paying for an unconditional HTML snapshot per message.
		if params.PageHint != nil && params.PageHint.URL != "" {
			hint := "[current page: " + params.PageHint.URL
			if params.PageHint.Title != "" {
				hint += " — " + params.PageHint.Title
			}
			hint += "]"
			if message != "" {
				message = hint + "\n\n" + message
			} else {
				message = hint
			}
		}

		// Auto-generate conversation title on first message (label empty
		// = never titled). Hoisted BEFORE loop.Run so the title-gen
		// goroutine fires even when the user clicks Stop mid-stream —
		// the title only needs params.Message, not the assistant output,
		// and runCtxBase (WithoutCancel) is not cancelled by AbortRun.
		// GenerateTitle has its own user-message fallback for the empty-
		// LLM-content case, so a title always lands.
		if label := m.sessions.GetLabel(ctx, sessionKey); label == "" {
			agentProvider := loop.Provider()
			agentModel := loop.Model()
			userMsg := params.Message
			titleCtx := runCtxBase
			// Attach actor headers for the outbound /v1/chat/completions
			// call so the downstream service-token endpoint can attribute
			// this background turn to the right user/org.
			if userID != "" {
				orgID := loop.ExternalOrgID()
				if orgID == "" {
					orgID = store.TenantSlugFromContext(runCtxBase)
				}
				if orgID == "" {
					orgID = loop.TenantSlug()
				}
				if orgID != "" {
					titleCtx = providers.WithActorHeaders(titleCtx, map[string]string{
						"X-Actor-User-ID": userID,
						"X-Actor-Org-ID":  orgID,
					})
				}
			}
			go func() {
				title := agent.GenerateTitle(titleCtx, agentProvider, agentModel, userMsg)
				if title == "" {
					return
				}
				m.sessions.SetLabel(titleCtx, sessionKey, title)
				if err := m.sessions.Save(titleCtx, sessionKey); err != nil {
					slog.Warn("failed to save session title", "sessionKey", sessionKey, "error", err)
					return
				}
				bus.BroadcastForTenant(m.eventBus, protocol.EventSessionUpdated,
					client.TenantID(),
					map[string]string{"sessionKey": sessionKey, "label": title, "userId": userID})
			}()
		}

		var result *agent.RunResult
		var err error
		// Note: err is also assigned to the outer loopErr below so the
		// partial-save defer can distinguish cancellation from normal exit.
		result, err = loop.Run(runCtx, agent.RunRequest{
			SessionKey:    sessionKey,
			Message:       message,
			Media:         mediaFiles,
			Channel:       "ws",
			ChatID:        userID, // use stable userID for team/workspace isolation (not ephemeral client.ID())
			RunID:         runID,
			UserID:        userID,
			Stream:        params.Stream,
			InjectCh:      injectCh,
			ModelOverride: params.Model,
			ClientKind:    params.ClientKind,
			// Wire trace ID back to the active run so force-abort can mark the
			// correct trace as cancelled if the goroutine does not exit within 3s.
			OnTraceCreated: func(traceID uuid.UUID) {
				m.agents.SetRunTraceID(runID, traceID)
			},
		})
		loopErr = err // surface loop result to partial-save defer

		if err != nil {
			// Send cancelled response so the frontend's chat.send promise resolves
			// instead of hanging until the 600s timeout.
			if runCtx.Err() != nil {
				client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
					"cancelled": true,
				}))
				return
			}
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
			return
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
		// stop_reason lets the client offer "Continue" when the agent ran out of
		// its per-run tool-iteration budget (vs finishing the task on its own).
		if result.StopReason != "" {
			resp["stop_reason"] = result.StopReason
		}
		// Combine existing media with TTS audio
		mediaResults := result.Media
		if ttsAudio != nil {
			mediaResults = append([]agent.MediaResult{*ttsAudio}, mediaResults...)
		}
		// Sign every media path before delivery, mirroring sessions.preview.
		// Without this the frontend got a raw /app/workspace/... path on the
		// real-time chat.send response and only saw the signed /v1/files/…?ft=
		// form after a manual page reload. Tts already pre-signed itself, so
		// skip it to avoid double-signing.
		if len(mediaResults) > 0 {
			secret := httpapi.FileSigningKey()
			for i := range mediaResults {
				if ttsAudio != nil && i == 0 {
					continue
				}
				mediaResults[i].Path = httpapi.SignMediaPath(mediaResults[i].Path, secret)
			}
			resp["media"] = mediaResults
		}
		client.SendResponse(protocol.NewOKResponse(req.ID, resp))
	}()
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

// chatToolResultParams is the payload the browser extension sends after executing
// a client tool (refresh_page_content, execute_action). The goclaw agent loop is
// blocked on a channel keyed by toolCallId; we route `content` into that channel
// and let the loop continue.
type chatToolResultParams struct {
	ToolCallID string `json:"toolCallId"`
	Content    string `json:"content"`
	IsError    bool   `json:"isError"`
}

// handleToolResult accepts a client-tool response from the extension and routes
// it into the matching pending tool-call channel on the registry. Returns
// ok=true when the waiting goroutine was still there, ok=false when the call
// had already timed out or been cleaned up.
//
// There is no ownership check by sessionKey here: tool_call_ids are UUIDs minted
// per LLM invocation and are not guessable. Misrouting is impossible because the
// channel map is scoped to this process and the ID must be an exact match.
func (m *ChatMethods) handleToolResult(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params chatToolResultParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.ToolCallID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "toolCallId")))
		return
	}
	if m.tools == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "tool registry not wired"))
		return
	}

	var result *tools.Result
	if params.IsError {
		result = tools.ErrorResult(params.Content)
	} else {
		result = tools.NewResult(params.Content)
	}

	routed := m.tools.RouteClientToolResult(params.ToolCallID, result)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":    routed,
		"stale": !routed,
	}))
}
