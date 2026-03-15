package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// consumeInboundMessages reads inbound messages from channels (Telegram, Discord, etc.)
// and routes them through the scheduler/agent loop, then publishes the response back.
// Also handles subagent announcements: routes them through the parent agent's session
// (matching TS subagent-announce.ts pattern) so the agent can reformulate for the user.
func consumeInboundMessages(ctx context.Context, msgBus *bus.MessageBus, agents *agent.Router, cfg *config.Config, sched *scheduler.Scheduler, channelMgr *channels.Manager, teamStore store.TeamStore, quotaChecker *channels.QuotaChecker, sessStore store.SessionStore, agentStore store.AgentStore, contactCollector *store.ContactCollector, postTurn tools.PostTurnProcessor) {
	slog.Info("inbound message consumer started")

	// Inbound message deduplication (matching TS src/infra/dedupe.ts + inbound-dedupe.ts).
	// TTL=20min, max=5000 entries — prevents webhook retries / double-taps from duplicating agent runs.
	dedupe := bus.NewDedupeCache(20*time.Minute, 5000)

	// Per-session announce serialization: prevents concurrent announce runs from
	// reading stale session history. Without this, Announce #2 can start while
	// Announce #1 is still running, read history that doesn't include Announce #1's
	// messages (written only after agent loop completes), and generate responses
	// with wrong context (e.g. "waiting for Tiểu La" when Tiểu La already finished).
	var announceMu sync.Map // sessionKey → *sync.Mutex
	getAnnounceMu := func(key string) *sync.Mutex {
		v, _ := announceMu.LoadOrStore(key, &sync.Mutex{})
		return v.(*sync.Mutex)
	}

	// Track running teammate tasks so they can be cancelled when the task is
	// cancelled/failed externally (e.g. lead cancels via team_tasks tool).
	var taskRunSessions sync.Map // taskID (string) → sessionKey (string)
	msgBus.Subscribe("consumer.team-task-cancel", func(event bus.Event) {
		if event.Name != protocol.EventTeamTaskCancelled && event.Name != protocol.EventTeamTaskFailed {
			return
		}
		payload, ok := event.Payload.(protocol.TeamTaskEventPayload)
		if !ok {
			return
		}
		if sessKey, ok := taskRunSessions.Load(payload.TaskID); ok {
			if cancelled := sched.CancelSession(sessKey.(string)); cancelled {
				slog.Info("team task cancelled: stopped running agent",
					"task_id", payload.TaskID, "session", sessKey)
			}
			taskRunSessions.Delete(payload.TaskID)
		}
	})

	// processNormalMessage handles routing, scheduling, and response delivery for a single
	// (possibly merged) inbound message. Called directly by the debouncer's flush callback.
	processNormalMessage := func(msg bus.InboundMessage) {
		// Determine target agent via bindings or explicit AgentID
		agentID := msg.AgentID
		if agentID == "" {
			agentID = resolveAgentRoute(cfg, msg.Channel, msg.ChatID, msg.PeerKind)
		}

		// Check handoff routing override
		if teamStore != nil && msg.AgentID == "" {
			if route, _ := teamStore.GetHandoffRoute(ctx, msg.Channel, msg.ChatID); route != nil {
				agentID = route.ToAgentKey
				slog.Info("inbound: handoff route active",
					"channel", msg.Channel, "chat", msg.ChatID, "to", agentID)
			}
		}

		agentLoop, err := agents.Get(agentID)
		if err != nil {
			slog.Warn("inbound: agent not found", "agent", agentID, "channel", msg.Channel)
			return
		}

		// Build session key based on scope config (matching TS buildAgentPeerSessionKey).
		peerKind := msg.PeerKind
		if peerKind == "" {
			peerKind = string(sessions.PeerDirect) // default to DM
		}
		sessionKey := sessions.BuildScopedSessionKey(agentID, msg.Channel, sessions.PeerKind(peerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)

		// Forum topic: override session key to isolate per-topic history.
		// TS ref: buildTelegramGroupPeerId() in src/telegram/bot/helpers.ts
		if msg.Metadata["is_forum"] == "true" && peerKind == string(sessions.PeerGroup) {
			var topicID int
			fmt.Sscanf(msg.Metadata["message_thread_id"], "%d", &topicID)
			if topicID > 0 {
				sessionKey = sessions.BuildGroupTopicSessionKey(agentID, msg.Channel, msg.ChatID, topicID)
			}
		}

		// DM thread: override session key to isolate per-thread history in private chats.
		if msg.Metadata["dm_thread_id"] != "" && peerKind == string(sessions.PeerDirect) {
			var threadID int
			fmt.Sscanf(msg.Metadata["dm_thread_id"], "%d", &threadID)
			if threadID > 0 {
				sessionKey = sessions.BuildDMThreadSessionKey(agentID, msg.Channel, msg.ChatID, threadID)
			}
		}

		// Group-scoped UserID: context files, memory, traces, and seeding scope.
		// - Discord guilds: "guild:{guildID}:user:{senderID}" — per-user per-server,
		//   shared across all channels within the same server. Session key stays per-channel.
		// - Other platforms: "group:{channel}:{chatID}" — shared by all users in the chat.
		// Individual senderID is preserved in InboundMessage for pairing/dedup/mention gate.
		userID := msg.UserID
		if peerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
			if guildID := msg.Metadata["guild_id"]; guildID != "" && msg.SenderID != "" {
				// Discord guild: per-user scope so each member has own profile
				// across all channels in the same server.
				userID = fmt.Sprintf("guild:%s:user:%s", guildID, msg.SenderID)
			} else {
				groupID := msg.ChatID
				userID = fmt.Sprintf("group:%s:%s", msg.Channel, groupID)
			}
		}

		// Persist friendly names from channel metadata into session + user profile.
		sessionMeta := extractSessionMetadata(msg, peerKind)
		if len(sessionMeta) > 0 {
			sessStore.SetSessionMetadata(sessionKey, sessionMeta)
			if agentStore != nil {
				if agentUUID, err := uuid.Parse(agentID); err == nil && agentUUID != uuid.Nil {
					_ = agentStore.UpdateUserProfileMetadata(ctx, agentUUID, userID, sessionMeta)
				}
			}
		}

		// Auto-collect channel contacts for the contact selector.
		if contactCollector != nil && msg.SenderID != "" {
			senderNumericID := msg.SenderID
			if idx := strings.IndexByte(senderNumericID, '|'); idx > 0 {
				senderNumericID = senderNumericID[:idx]
			}
			channelType := channelMgr.ChannelTypeForName(msg.Channel)
			if channelType == "" {
				channelType = msg.Channel // fallback to instance name
			}
			displayName := sessionMeta["display_name"]
			username := sessionMeta["username"]
			contactCollector.EnsureContact(ctx, channelType, msg.Channel, senderNumericID, userID, displayName, username, peerKind)
		}

		// --- Quota check ---
		if quotaChecker != nil {
			qResult := quotaChecker.Check(ctx, userID, msg.Channel, agentLoop.ProviderName())
			if !qResult.Allowed {
				slog.Warn("security.quota_exceeded",
					"user_id", userID,
					"channel", msg.Channel,
					"window", qResult.Window,
					"used", qResult.Used,
					"limit", qResult.Limit,
				)
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  msg.Channel,
					ChatID:   msg.ChatID,
					Content:  formatQuotaExceeded(qResult),
					Metadata: msg.Metadata,
				})
				return
			}
			quotaChecker.Increment(userID)
		}

		// Auto-clear followup reminders when user sends a message on a real channel.
		// Fire-and-forget: don't block message processing.
		if teamStore != nil && msg.Channel != tools.ChannelSystem && msg.Channel != tools.ChannelDelegate && msg.Channel != tools.ChannelDashboard {
			go func(ch, cid string) {
				if n, err := teamStore.ClearFollowupByScope(ctx, ch, cid); err != nil {
					slog.Warn("auto-clear followup failed", "channel", ch, "chat_id", cid, "error", err)
				} else if n > 0 {
					slog.Info("auto-clear followup: cleared", "channel", ch, "chat_id", cid, "count", n)
				}
			}(msg.Channel, msg.ChatID)
		}

		slog.Info("inbound: scheduling message (main lane)",
			"channel", msg.Channel,
			"chat_id", msg.ChatID,
			"peer_kind", peerKind,
			"agent", agentID,
			"session", sessionKey,
			"user_id", userID,
		)

		// Enable streaming when the channel supports it (so agent emits chunk events).
		// The channel decides per chat type via separate dm_stream / group_stream flags.
		isGroup := peerKind == string(sessions.PeerGroup)
		enableStream := channelMgr != nil && channelMgr.IsStreamingChannel(msg.Channel, isGroup)

		// Group chats allow concurrent runs (multiple users can chat simultaneously).
		maxConcurrent := 1
		if peerKind == string(sessions.PeerGroup) {
			maxConcurrent = 3
		}

		runID := fmt.Sprintf("inbound-%s-%s-%s", msg.Channel, msg.ChatID, uuid.NewString()[:8])

		// Build outbound metadata for reply-to + thread routing BEFORE RegisterRun
		// so block.reply handler can use it for routing intermediate messages.
		outMeta := make(map[string]string)
		if isGroup {
			if mid := msg.Metadata["message_id"]; mid != "" {
				outMeta["reply_to_message_id"] = mid
			}
		}
		for _, k := range []string{"message_thread_id", "local_key", "placeholder_key", "group_id"} {
			if v := msg.Metadata[k]; v != "" {
				outMeta[k] = v
			}
		}

		// Register run with channel manager for streaming/reaction event forwarding.
		// Use localKey (composite key with topic suffix) so streaming/reaction events
		// route to the correct per-topic state in the channel.
		messageID := msg.Metadata["message_id"]
		chatIDForRun := msg.ChatID
		if lk := msg.Metadata["local_key"]; lk != "" {
			chatIDForRun = lk
		}
		blockReply := channelMgr != nil && channelMgr.ResolveBlockReply(msg.Channel, cfg.Gateway.BlockReply)
		toolStatus := cfg.Gateway.ToolStatus == nil || *cfg.Gateway.ToolStatus // default true
		if channelMgr != nil {
			channelMgr.RegisterRun(runID, msg.Channel, chatIDForRun, messageID, outMeta, enableStream, blockReply, toolStatus)
		}

		// Group-aware system prompt: help the LLM adapt tone and behavior for group chats.
		var extraPrompt string
		if peerKind == string(sessions.PeerGroup) {
			extraPrompt = "You are in a GROUP chat (multiple participants), not a private 1-on-1 DM.\n" +
				"- Messages may include a [Chat messages since your last reply] section with recent group history. Each history line shows \"sender [time]: message\".\n" +
				"- The current message includes a [From: sender_name] tag identifying who @mentioned you.\n" +
				"- Keep responses concise and focused; long replies are disruptive in groups.\n" +
				"- Address the group naturally. If the history shows a multi-person conversation, consider the full context before answering."
		}

		// Append per-topic system prompt (from group/topic config hierarchy).
		if tsp := msg.Metadata["topic_system_prompt"]; tsp != "" {
			if extraPrompt != "" {
				extraPrompt += "\n\n"
			}
			extraPrompt += tsp
		}

		// Per-topic skill filter override (from group/topic config hierarchy).
		var skillFilter []string
		if ts := msg.Metadata["topic_skills"]; ts != "" {
			skillFilter = strings.Split(ts, ",")
		}

		// Delegation announces carry media as ForwardMedia (not deleted, forwarded to output).
		// User-uploaded media goes in Media (loaded as images for LLM, then deleted).
		var reqMedia, fwdMedia []bus.MediaFile
		if msg.Metadata["delegation_id"] != "" || msg.Metadata["subagent_id"] != "" {
			fwdMedia = msg.Media
		} else {
			reqMedia = msg.Media
		}

		// Intent classify fast-path: when agent is busy on DM, classify user intent
		// to detect status queries, cancel requests, or steer/new_task for mid-run injection.
		// Only for DM (maxConcurrent=1) where messages queue behind the active run.
		if maxConcurrent == 1 && agents.IsSessionBusy(sessionKey) {
			if loop, ok := agentLoop.(*agent.Loop); ok && loop.Provider() != nil {
				locale := msg.Metadata["locale"]
				if locale == "" {
					locale = "en"
				}
				intent := agent.ClassifyIntent(ctx, loop.Provider(), loop.Model(), msg.Content)
				switch intent {
				case agent.IntentStatusQuery:
					status := agents.GetActivity(sessionKey)
					reply := agent.FormatStatusReply(status, locale)
					msgBus.PublishOutbound(bus.OutboundMessage{
						Channel:  msg.Channel,
						ChatID:   msg.ChatID,
						Content:  reply,
						Metadata: outMeta,
					})
					return
				case agent.IntentCancel:
					aborted := agents.AbortRunsForSession(sessionKey)
					if len(aborted) > 0 {
						slog.Info("inbound: cancelled runs via intent classify",
							"session", sessionKey, "aborted", aborted)
						msgBus.PublishOutbound(bus.OutboundMessage{
							Channel:  msg.Channel,
							ChatID:   msg.ChatID,
							Content:  i18n.T(locale, i18n.MsgCancelledReply),
							Metadata: outMeta,
						})
					}
					return
				case agent.IntentSteer, agent.IntentNewTask:
					// Mid-run injection: inject into the running loop instead of queueing.
					injected := agents.InjectMessage(sessionKey, agent.InjectedMessage{
						Content: msg.Content,
						UserID:  userID,
					})
					if injected {
						slog.Info("inbound: injected mid-run message",
							"intent", string(intent), "session", sessionKey)
						msgBus.PublishOutbound(bus.OutboundMessage{
							Channel:  msg.Channel,
							ChatID:   msg.ChatID,
							Content:  i18n.T(locale, i18n.MsgInjectedAck),
							Metadata: outMeta,
						})
						return
					}
					// Fallback: injection failed (channel full) → fall through to scheduler queue
					slog.Info("inbound: injection failed, queueing as normal",
						"intent", string(intent), "session", sessionKey)
				}
			}
		}

		// Inject post-turn dispatch tracker so team task creates are deferred.
		ptd := tools.NewPendingTeamDispatch()
		schedCtx := tools.WithPendingTeamDispatch(ctx, ptd)

		// Schedule through main lane (per-session concurrency controlled by maxConcurrent)
		outCh := sched.ScheduleWithOpts(schedCtx, "main", agent.RunRequest{
			SessionKey:        sessionKey,
			Message:           msg.Content,
			Media:             reqMedia,
			ForwardMedia:      fwdMedia,
			Channel:           msg.Channel,
			ChannelType:       resolveChannelType(channelMgr, msg.Channel),
			ChatID:            msg.ChatID,
			PeerKind:          peerKind,
			LocalKey:          msg.Metadata["local_key"],
			UserID:            userID,
			SenderID:          msg.SenderID,
			RunID:             runID,
			Stream:            enableStream,
			HistoryLimit:      msg.HistoryLimit,
			ToolAllow:         msg.ToolAllow,
			ExtraSystemPrompt: extraPrompt,
			SkillFilter:       skillFilter,
		}, scheduler.ScheduleOpts{
			MaxConcurrent: maxConcurrent,
		})

		// Handle result asynchronously to not block the flush callback.
		go func(agentKey, channel, chatID, session, rID string, meta map[string]string, blockReplyEnabled bool, ptd *tools.PendingTeamDispatch) {
			outcome := <-outCh

			// Post-turn: dispatch pending team tasks created during this turn.
			if postTurn != nil {
				for teamID, taskIDs := range ptd.Drain() {
					if err := postTurn.ProcessPendingTasks(ctx, teamID, taskIDs); err != nil {
						slog.Warn("post_turn: failed", "team_id", teamID, "error", err)
					}
				}
			}

			// Clean up run tracking (in case HandleAgentEvent didn't fire for terminal events)
			if channelMgr != nil {
				channelMgr.UnregisterRun(rID)
			}

			if outcome.Err != nil {
				// Don't send error for cancelled runs (/stop command) —
				// publish empty outbound to clean up thinking/typing indicators.
				if errors.Is(outcome.Err, context.Canceled) {
					slog.Info("inbound: run cancelled", "channel", channel, "session", session)
					msgBus.PublishOutbound(bus.OutboundMessage{
						Channel:  channel,
						ChatID:   chatID,
						Content:  "",
						Metadata: meta,
					})
					return
				}
				slog.Error("inbound: agent run failed", "error", outcome.Err, "channel", channel)
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  channel,
					ChatID:   chatID,
					Content:  formatAgentError(outcome.Err),
					Metadata: meta,
				})
				return
			}

			// Suppress empty/NO_REPLY responses (matching TS normalize-reply.ts).
			// Still publish an empty outbound so channels can clean up placeholder/thinking indicators.
			if outcome.Result.Content == "" || agent.IsSilentReply(outcome.Result.Content) {
				slog.Info("inbound: suppressed silent/empty reply",
					"channel", channel,
					"chat_id", chatID,
					"session", session,
				)
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  channel,
					ChatID:   chatID,
					Content:  "",
					Metadata: meta,
				})
				return
			}

			// Dedup: if block replies were delivered and the final content matches the last
			// block reply, suppress the final message to avoid duplicate delivery.
			// Only applies when blockReply is enabled (otherwise nothing was delivered).
			if blockReplyEnabled && outcome.Result.BlockReplies > 0 && outcome.Result.Content == outcome.Result.LastBlockReply && len(outcome.Result.Media) == 0 {
				slog.Debug("inbound: dedup final message (matches last block reply)",
					"channel", channel, "run_id", rID)
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  channel,
					ChatID:   chatID,
					Content:  "",
					Metadata: meta,
				})
				return
			}

			// Publish response back to the channel
			outMsg := bus.OutboundMessage{
				Channel:  channel,
				ChatID:   chatID,
				Content:  outcome.Result.Content,
				Metadata: meta,
			}

			appendMediaToOutbound(&outMsg, outcome.Result.Media)

			msgBus.PublishOutbound(outMsg)

			// Auto-set followup when lead agent replies on a real channel with in_progress tasks.
			if teamStore != nil && channel != tools.ChannelSystem && channel != tools.ChannelDelegate && channel != tools.ChannelDashboard {
				go autoSetFollowup(ctx, teamStore, agentStore, agentKey, channel, chatID, outcome.Result.Content)
			}
		}(agentID, msg.Channel, msg.ChatID, sessionKey, runID, outMeta, blockReply, ptd)
	}

	// Inbound debounce: merge rapid messages from the same sender before processing.
	// Matching TS createInboundDebouncer from src/auto-reply/inbound-debounce.ts.
	debounceMs := cfg.Gateway.InboundDebounceMs
	if debounceMs == 0 {
		debounceMs = 1000 // default: 1000ms
	}
	debouncer := bus.NewInboundDebouncer(
		time.Duration(debounceMs)*time.Millisecond,
		processNormalMessage,
	)
	defer debouncer.Stop()

	slog.Info("inbound debounce configured", "debounce_ms", debounceMs)

	for {
		msg, ok := msgBus.ConsumeInbound(ctx)
		if !ok {
			slog.Info("inbound message consumer stopped")
			return
		}

		// --- Dedup: skip duplicate inbound messages (matching TS shouldSkipDuplicateInbound) ---
		if msgID := msg.Metadata["message_id"]; msgID != "" {
			dedupeKey := fmt.Sprintf("%s|%s|%s|%s", msg.Channel, msg.SenderID, msg.ChatID, msgID)
			if dedupe.IsDuplicate(dedupeKey) {
				slog.Debug("dedup: skipping duplicate message", "key", dedupeKey)
				continue
			}
		}

		// --- Subagent announce: bypass debounce, inject into parent agent session ---
		if msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "subagent:") {
			origChannel := msg.Metadata["origin_channel"]
			origPeerKind := msg.Metadata["origin_peer_kind"]
			origLocalKey := msg.Metadata["origin_local_key"]
			origChannelType := resolveChannelType(channelMgr, origChannel)
			parentAgent := msg.Metadata["parent_agent"]
			if parentAgent == "" {
				parentAgent = "default"
			}
			if origPeerKind == "" {
				origPeerKind = string(sessions.PeerDirect)
			}

			if origChannel == "" || msg.ChatID == "" {
				slog.Warn("subagent announce: missing origin", "sender", msg.SenderID)
				continue
			}

			// Use exact origin session key if available (WS uses non-standard format).
			sessionKey := msg.Metadata["origin_session_key"]
			if sessionKey == "" {
				// Fallback: rebuild session key from origin metadata (works for Telegram, Discord, etc.)
				sessionKey = sessions.BuildScopedSessionKey(parentAgent, origChannel, sessions.PeerKind(origPeerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)
				sessionKey = overrideSessionKeyFromLocalKey(sessionKey, origLocalKey, parentAgent, origChannel, msg.ChatID, origPeerKind)
			}

			slog.Info("subagent announce → scheduler (subagent lane)",
				"subagent", msg.SenderID,
				"label", msg.Metadata["subagent_label"],
				"session", sessionKey,
			)

			// Extract parent trace context for announce linking
			var parentTraceID, parentRootSpanID uuid.UUID
			if tid := msg.Metadata["origin_trace_id"]; tid != "" {
				parentTraceID, _ = uuid.Parse(tid)
			}
			if sid := msg.Metadata["origin_root_span_id"]; sid != "" {
				parentRootSpanID, _ = uuid.Parse(sid)
			}

			// Group-scoped UserID for subagent announce (same logic as main lane).
			announceUserID := msg.UserID
			if origPeerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
				announceUserID = fmt.Sprintf("group:%s:%s", origChannel, msg.ChatID)
			}

			// Build outbound metadata for topic/thread routing.
			outMeta := buildAnnounceOutMeta(origLocalKey)

			// Build request before goroutine to capture msg fields.
			// WS channel has no outbound handler — media converted to markdown URLs
			// and appended to the assistant response via ContentSuffix, which the
			// agent loop applies BEFORE saving to session and emitting run.completed.
			fwdMedia := msg.Media
			contentSuffix := ""
			if origChannel == "ws" && len(msg.Media) > 0 {
				contentSuffix = mediaToMarkdownFromPaths(msg.Media, cfg)
				fwdMedia = nil // WS: images delivered via ContentSuffix, not ForwardMedia
			}

			announceReq := agent.RunRequest{
				SessionKey:       sessionKey,
				Message:          msg.Content,
				ForwardMedia:     fwdMedia,
				ContentSuffix:    contentSuffix,
				Channel:          origChannel,
				ChannelType:      origChannelType,
				ChatID:           msg.ChatID,
				PeerKind:         origPeerKind,
				LocalKey:         origLocalKey,
				UserID:           announceUserID,
				RunID:            fmt.Sprintf("announce-%s", msg.SenderID),
				RunKind:          "announce",
				HideInput:        true, // don't persist raw system message in chat history
				Stream:           false,
				ParentTraceID:    parentTraceID,
				ParentRootSpanID: parentRootSpanID,
			}
			// Handle announce asynchronously with per-session serialization.
			// The mutex ensures concurrent announces for the same session wait for
			// each other, so each reads up-to-date session history.
			go func(sessionKey, origCh, chatID, senderID, label string, meta map[string]string, req agent.RunRequest) {
				mu := getAnnounceMu(sessionKey)
				mu.Lock()
				defer mu.Unlock()

				outCh := sched.Schedule(ctx, scheduler.LaneSubagent, req)
				outcome := <-outCh
				if outcome.Err != nil {
					if errors.Is(outcome.Err, context.Canceled) {
						slog.Info("subagent announce: run cancelled", "subagent", senderID)
						return
					}
					slog.Error("subagent announce: agent run failed", "error", outcome.Err)
					msgBus.PublishOutbound(bus.OutboundMessage{
						Channel:  origCh,
						ChatID:   chatID,
						Content:  formatAgentError(outcome.Err),
						Metadata: meta,
					})
					return
				}

				// Suppress empty/NO_REPLY (matching TS normalize-reply.ts / tokens.ts).
				isSilent := outcome.Result.Content == "" || agent.IsSilentReply(outcome.Result.Content)
				if isSilent && len(outcome.Result.Media) == 0 {
					slog.Info("subagent announce: suppressed silent/empty reply",
						"subagent", senderID,
						"label", label,
					)
					return
				}

				// Deliver agent's reformulated response to origin channel.
				announceContent := outcome.Result.Content
				if isSilent {
					announceContent = "" // suppress NO_REPLY text but still send media
				}

				outMsg := bus.OutboundMessage{
					Channel:  origCh,
					ChatID:   chatID,
					Content:  announceContent,
					Metadata: meta,
				}
				appendMediaToOutbound(&outMsg, outcome.Result.Media)
				msgBus.PublishOutbound(outMsg)
			}(sessionKey, origChannel, msg.ChatID, msg.SenderID, msg.Metadata["subagent_label"], outMeta, announceReq)
			continue
		}

		// --- Delegate announce: bypass debounce, inject into parent agent session ---
		// Same pattern as subagent announce above, using "delegate" lane.
		if msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "delegate:") {
			origChannel := msg.Metadata["origin_channel"]
			origPeerKind := msg.Metadata["origin_peer_kind"]
			origLocalKey := msg.Metadata["origin_local_key"]
			origChannelType := resolveChannelType(channelMgr, origChannel)
			parentAgent := msg.Metadata["parent_agent"]
			if parentAgent == "" {
				parentAgent = "default"
			}
			if origPeerKind == "" {
				origPeerKind = string(sessions.PeerDirect)
			}

			if origChannel == "" || msg.ChatID == "" {
				slog.Warn("delegate announce: missing origin", "sender", msg.SenderID)
				continue
			}

			// Use exact origin session key if available (WS uses non-standard format).
			sessionKey := msg.Metadata["origin_session_key"]
			if sessionKey == "" {
				// Fallback: rebuild session key from origin metadata (works for Telegram, Discord, etc.)
				sessionKey = sessions.BuildScopedSessionKey(parentAgent, origChannel, sessions.PeerKind(origPeerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)
				sessionKey = overrideSessionKeyFromLocalKey(sessionKey, origLocalKey, parentAgent, origChannel, msg.ChatID, origPeerKind)
			}

			slog.Info("delegate announce → scheduler (delegate lane)",
				"delegation", msg.SenderID,
				"target", msg.Metadata["target_agent"],
				"session", sessionKey,
			)

			announceUserID := msg.UserID
			if origPeerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
				announceUserID = fmt.Sprintf("group:%s:%s", origChannel, msg.ChatID)
			}

			// Extract parent trace context for announce linking (same as subagent announce)
			var parentTraceID, parentRootSpanID uuid.UUID
			if tid := msg.Metadata["origin_trace_id"]; tid != "" {
				parentTraceID, _ = uuid.Parse(tid)
			}
			if sid := msg.Metadata["origin_root_span_id"]; sid != "" {
				parentRootSpanID, _ = uuid.Parse(sid)
			}

			// Build outbound metadata for topic/thread routing.
			outMeta := buildAnnounceOutMeta(origLocalKey)

			// WS channel has no outbound handler — media injected into session after run.
			// WS channel has no outbound handler — media delivered via ContentSuffix.
			fwdMedia := msg.Media
			contentSuffix := ""
			if origChannel == "ws" && len(msg.Media) > 0 {
				contentSuffix = mediaToMarkdownFromPaths(msg.Media, cfg)
				fwdMedia = nil // WS: images delivered via ContentSuffix, not ForwardMedia
			}

			announceReq := agent.RunRequest{
				SessionKey:       sessionKey,
				Message:          msg.Content,
				ForwardMedia:     fwdMedia,
				ContentSuffix:    contentSuffix,
				Channel:          origChannel,
				ChannelType:      origChannelType,
				ChatID:           msg.ChatID,
				PeerKind:         origPeerKind,
				LocalKey:         origLocalKey,
				UserID:           announceUserID,
				RunID:            fmt.Sprintf("delegate-announce-%s", msg.Metadata["delegation_id"]),
				RunKind:          "announce",
				HideInput:        true, // don't persist raw system message in chat history
				Stream:           false,
				ParentTraceID:    parentTraceID,
				ParentRootSpanID: parentRootSpanID,
			}

			// Same per-session serialization as subagent announce above.
			go func(sessionKey, origCh, chatID, senderID string, meta map[string]string, req agent.RunRequest) {
				mu := getAnnounceMu(sessionKey)
				mu.Lock()
				defer mu.Unlock()

				outCh := sched.Schedule(ctx, scheduler.LaneDelegate, req)
				outcome := <-outCh
				if outcome.Err != nil {
					if errors.Is(outcome.Err, context.Canceled) {
						slog.Info("delegate announce: run cancelled", "delegation", senderID)
						return
					}
					slog.Error("delegate announce: agent run failed", "error", outcome.Err)
					msgBus.PublishOutbound(bus.OutboundMessage{
						Channel:  origCh,
						ChatID:   chatID,
						Content:  formatAgentError(outcome.Err),
						Metadata: meta,
					})
					return
				}
				isSilent := outcome.Result.Content == "" || agent.IsSilentReply(outcome.Result.Content)
				if isSilent && len(outcome.Result.Media) == 0 {
					slog.Info("delegate announce: suppressed silent/empty reply", "delegation", senderID)
					return
				}

				announceContent := outcome.Result.Content
				if isSilent {
					announceContent = "" // suppress NO_REPLY text but still send media
				}

				outMsg := bus.OutboundMessage{
					Channel:  origCh,
					ChatID:   chatID,
					Content:  announceContent,
					Metadata: meta,
				}
				appendMediaToOutbound(&outMsg, outcome.Result.Media)
				msgBus.PublishOutbound(outMsg)
			}(sessionKey, origChannel, msg.ChatID, msg.SenderID, outMeta, announceReq)
			continue
		}

		// --- Handoff announce: route initial message to target agent session ---
		// Same pattern as teammate message routing, using "delegate" lane.
		if msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "handoff:") {
			origChannel := msg.Metadata["origin_channel"]
			origPeerKind := msg.Metadata["origin_peer_kind"]
			origLocalKey := msg.Metadata["origin_local_key"]
			origChannelType := resolveChannelType(channelMgr, origChannel)
			targetAgent := msg.AgentID
			if targetAgent == "" {
				targetAgent = cfg.ResolveDefaultAgentID()
			}
			if origPeerKind == "" {
				origPeerKind = string(sessions.PeerDirect)
			}

			if origChannel == "" || msg.ChatID == "" {
				slog.Warn("handoff announce: missing origin", "sender", msg.SenderID)
				continue
			}

			sessionKey := sessions.BuildScopedSessionKey(targetAgent, origChannel, sessions.PeerKind(origPeerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)
			sessionKey = overrideSessionKeyFromLocalKey(sessionKey, origLocalKey, targetAgent, origChannel, msg.ChatID, origPeerKind)

			slog.Info("handoff announce → scheduler (delegate lane)",
				"handoff", msg.SenderID,
				"to", targetAgent,
				"session", sessionKey,
			)

			announceUserID := msg.UserID
			if origPeerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
				announceUserID = fmt.Sprintf("group:%s:%s", origChannel, msg.ChatID)
			}

			outMeta := buildAnnounceOutMeta(origLocalKey)

			outCh := sched.Schedule(ctx, scheduler.LaneDelegate, agent.RunRequest{
				SessionKey:  sessionKey,
				Message:     msg.Content,
				Channel:     origChannel,
				ChannelType: origChannelType,
				ChatID:      msg.ChatID,
				PeerKind:    origPeerKind,
				LocalKey:    origLocalKey,
				UserID:      announceUserID,
				RunID:       fmt.Sprintf("handoff-%s", msg.Metadata["handoff_id"]),
				Stream:      false,
			})

			go func(origCh, chatID string, meta map[string]string) {
				outcome := <-outCh
				if outcome.Err != nil {
					slog.Error("handoff announce: agent run failed", "error", outcome.Err)
					return
				}
				if (outcome.Result.Content == "" && len(outcome.Result.Media) == 0) || agent.IsSilentReply(outcome.Result.Content) {
					return
				}
				outMsg := bus.OutboundMessage{
					Channel:  origCh,
					ChatID:   chatID,
					Content:  outcome.Result.Content,
					Metadata: meta,
				}
				appendMediaToOutbound(&outMsg, outcome.Result.Media)
				msgBus.PublishOutbound(outMsg)
			}(origChannel, msg.ChatID, outMeta)
			continue
		}

		// --- Teammate message: bypass debounce, route to target agent session ---
		// Same pattern as delegate announce, using "delegate" lane.
		if msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "teammate:") {
			origChannel := msg.Metadata["origin_channel"]
			origPeerKind := msg.Metadata["origin_peer_kind"]
			origLocalKey := msg.Metadata["origin_local_key"]
			origChatID := msg.Metadata["origin_chat_id"] // original chat (e.g. Telegram chat ID)
			if origChatID == "" {
				origChatID = msg.ChatID // fallback to inbound ChatID (team UUID for old dispatches)
			}
			origChannelType := resolveChannelType(channelMgr, origChannel)
			targetAgent := msg.AgentID // team_message sets AgentID to the target agent key
			if targetAgent == "" {
				targetAgent = cfg.ResolveDefaultAgentID()
			}
			if origPeerKind == "" {
				origPeerKind = string(sessions.PeerDirect)
			}

			if origChannel == "" || origChatID == "" {
				slog.Warn("teammate message: missing origin — DROPPED",
					"sender", msg.SenderID,
					"target", targetAgent,
					"origin_channel", origChannel,
					"origin_chat_id", origChatID,
					"user_id", msg.UserID,
				)
				continue
			}

			// Use isolated team session key so member execution doesn't share
			// the user's direct chat session with this agent.
			// Scoped per agent + team + chatID, matching workspace isolation.
			sessionKey := sessions.BuildTeamSessionKey(targetAgent, msg.Metadata["team_id"], origChatID)

			slog.Info("teammate message → scheduler (delegate lane)",
				"from", msg.SenderID,
				"to", targetAgent,
				"session", sessionKey,
				"team_task_id", msg.Metadata["team_task_id"],
			)

			announceUserID := msg.UserID
			if origPeerKind == string(sessions.PeerGroup) && origChatID != "" {
				announceUserID = fmt.Sprintf("group:%s:%s", origChannel, origChatID)
			}

			outMeta := buildAnnounceOutMeta(origLocalKey)

			// Link member agent trace back to lead's trace for unified tracing.
			var linkedTraceID uuid.UUID
			if tid := msg.Metadata["origin_trace_id"]; tid != "" {
				linkedTraceID, _ = uuid.Parse(tid)
			}

			// Track task → session so the subscriber can cancel on task cancellation.
			taskIDStr := msg.Metadata["team_task_id"]
			if taskIDStr != "" {
				taskRunSessions.Store(taskIDStr, sessionKey)
			}

			outCh := sched.Schedule(ctx, scheduler.LaneDelegate, agent.RunRequest{
				SessionKey:      sessionKey,
				Message:         msg.Content,
				Channel:         origChannel,
				ChannelType:     origChannelType,
				ChatID:          origChatID,
				PeerKind:        origPeerKind,
				LocalKey:        origLocalKey,
				UserID:          announceUserID,
				RunID:           fmt.Sprintf("teammate-%s-%s", msg.Metadata["from_agent"], msg.Metadata["to_agent"]),
				Stream:          false,
				TeamTaskID:      msg.Metadata["team_task_id"],
				TeamWorkspace:   msg.Metadata["team_workspace"],
				WorkspaceChatID: origChatID,
				TeamID:          msg.Metadata["team_id"],
				LinkedTraceID:   linkedTraceID,
			})

			go func(origCh, origChatID, senderID, taskID string, meta, inMeta map[string]string) {
				// Lock renewal heartbeat: extend task lock every 10 min to prevent
				// the ticker from recovering long-running tasks as stale.
				var lockStop chan struct{}
				if taskIDStr := inMeta["team_task_id"]; taskIDStr != "" && teamStore != nil {
					teamTaskID, _ := uuid.Parse(taskIDStr)
					teamID, _ := uuid.Parse(inMeta["team_id"])
					if teamTaskID != uuid.Nil {
						lockStop = make(chan struct{})
						go func() {
							ticker := time.NewTicker(10 * time.Minute)
							defer ticker.Stop()
							for {
								select {
								case <-ticker.C:
									if err := teamStore.RenewTaskLock(ctx, teamTaskID, teamID); err != nil {
										slog.Warn("teammate lock renewal failed", "task_id", teamTaskID, "error", err)
										return
									}
									slog.Debug("teammate lock renewed", "task_id", teamTaskID)
								case <-lockStop:
									return
								case <-ctx.Done():
									return
								}
							}
						}()
					}
				}

				outcome := <-outCh

				// Clean up task → session tracking now that the agent has finished.
				if taskID != "" {
					taskRunSessions.Delete(taskID)
				}

				// Stop lock renewal now that the agent has finished.
				if lockStop != nil {
					close(lockStop)
				}

				// Auto-complete/fail the associated team task (v2 only).
				// Cache team lookup — reused later for announce routing.
				var cachedTeam *store.TeamData
				if taskIDStr := inMeta["team_task_id"]; taskIDStr != "" {
					teamTaskID, _ := uuid.Parse(taskIDStr)
					teamID, _ := uuid.Parse(inMeta["team_id"])
					if teamTaskID != uuid.Nil && teamStore != nil {
						cachedTeam, _ = teamStore.GetTeam(ctx, teamID)
						if cachedTeam != nil && isConsumerTeamV2(cachedTeam) {
							// Check current task status — agent may have already updated it via tool.
							currentTask, taskErr := teamStore.GetTask(ctx, teamTaskID)
							alreadyTerminal := taskErr == nil && currentTask != nil &&
								(currentTask.Status == store.TeamTaskStatusCompleted ||
									currentTask.Status == store.TeamTaskStatusFailed ||
									currentTask.Status == store.TeamTaskStatusCancelled)

							if !alreadyTerminal {
								toAgent := inMeta["to_agent"]
								now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
								if outcome.Err != nil {
									if err := teamStore.FailTask(ctx, teamTaskID, teamID, outcome.Err.Error()); err != nil {
										slog.Warn("auto-complete: FailTask error", "task_id", teamTaskID, "error", err)
									} else {
										msgBus.Broadcast(bus.Event{
											Name: protocol.EventTeamTaskFailed,
											Payload: protocol.TeamTaskEventPayload{
												TeamID:    teamID.String(),
												TaskID:    teamTaskID.String(),
												Status:    store.TeamTaskStatusFailed,
												Timestamp: now,
												ActorType: "agent",
												ActorID:   toAgent,
											},
										})
										// FailTask also unblocks dependent tasks.
										if postTurn != nil {
											postTurn.DispatchUnblockedTasks(ctx, teamID)
										}
									}
								} else {
									result := outcome.Result.Content
									if len(outcome.Result.Deliverables) > 0 {
										result = strings.Join(outcome.Result.Deliverables, "\n\n---\n\n")
									}
									if len(result) > 100_000 {
										result = result[:100_000] + "\n[truncated]"
									}
									if err := teamStore.CompleteTask(ctx, teamTaskID, teamID, result); err != nil {
										slog.Warn("auto-complete: CompleteTask error", "task_id", teamTaskID, "error", err)
									} else {
										msgBus.Broadcast(bus.Event{
											Name: protocol.EventTeamTaskCompleted,
											Payload: protocol.TeamTaskEventPayload{
												TeamID:        teamID.String(),
												TaskID:        teamTaskID.String(),
												Status:        store.TeamTaskStatusCompleted,
												OwnerAgentKey: toAgent,
												Timestamp:     now,
												ActorType:     "agent",
												ActorID:       toAgent,
											},
										})
										// Dispatch newly-unblocked dependent tasks.
										if postTurn != nil {
											postTurn.DispatchUnblockedTasks(ctx, teamID)
										}
									}
								}
							}
						}
					}
				}

				if outcome.Err != nil {
					slog.Error("teammate message: agent run failed", "error", outcome.Err)
					return
				}
				if (outcome.Result.Content == "" && len(outcome.Result.Media) == 0) || agent.IsSilentReply(outcome.Result.Content) {
					slog.Info("teammate message: suppressed silent/empty reply", "from", senderID)
					return
				}

				// Announce result to lead agent (same pattern as subagent announce).
				// The lead reformulates the result and presents to the user.
				// Extract parent trace context so the announce run nests under the lead's trace.
				var announceParentTraceID, announceParentRootSpanID uuid.UUID
				if tid := inMeta["origin_trace_id"]; tid != "" {
					announceParentTraceID, _ = uuid.Parse(tid)
				}
				if sid := inMeta["origin_root_span_id"]; sid != "" {
					announceParentRootSpanID, _ = uuid.Parse(sid)
				}

				// Resolve lead from team — reuse cachedTeam to avoid duplicate DB call.
				leadAgent := ""
				if cachedTeam != nil {
					if leadAg, err := agentStore.GetByID(ctx, cachedTeam.LeadAgentID); err == nil {
						leadAgent = leadAg.AgentKey
					}
				} else if teamIDStr := inMeta["team_id"]; teamIDStr != "" {
					if teamUUID, err := uuid.Parse(teamIDStr); err == nil {
						if team, err := teamStore.GetTeam(ctx, teamUUID); err == nil {
							if leadAg, err := agentStore.GetByID(ctx, team.LeadAgentID); err == nil {
								leadAgent = leadAg.AgentKey
							}
						}
					}
				}
				if leadAgent == "" {
					leadAgent = inMeta["from_agent"]
				}
				if leadAgent == "" {
					leadAgent = cfg.ResolveDefaultAgentID()
				}
				memberAgent := inMeta["to_agent"]

				announceContent := fmt.Sprintf(
					"[System Message] Team member %q completed task.\n\nResult:\n%s\n\n"+
						"Present this result to the user. Any media files are forwarded automatically. Do NOT search for files — the result above contains all relevant information.",
					memberAgent, outcome.Result.Content,
				)
				// Append team workspace path so lead can locate files without searching.
				if ws := inMeta["team_workspace"]; ws != "" {
					announceContent += fmt.Sprintf("\n[Team workspace: %s — use read_file with path relative to workspace root, e.g. read_file(path=\"teams/...\")]", ws)
				}

				// Route to the lead's session on the original channel/chat.
				if origChatID == "" {
					slog.Warn("teammate announce: no origin_chat_id, cannot announce to lead")
					return
				}
				leadSessionKey := sessions.BuildScopedSessionKey(leadAgent, origCh, sessions.PeerDirect, origChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)

				announceReq := agent.RunRequest{
					SessionKey:       leadSessionKey,
					Message:          announceContent,
					Channel:          origCh,
					ChatID:           origChatID,
					PeerKind:         string(sessions.PeerDirect),
					UserID:           inMeta["origin_user_id"],
					RunID:            fmt.Sprintf("teammate-announce-%s", memberAgent),
					RunKind:          "announce",
					HideInput:        true,
					Stream:           false,
					TeamID:           inMeta["team_id"],
					ParentTraceID:    announceParentTraceID,
					ParentRootSpanID: announceParentRootSpanID,
				}
				for _, mr := range outcome.Result.Media {
					announceReq.ForwardMedia = append(announceReq.ForwardMedia, bus.MediaFile{
						Path:     mr.Path,
						MimeType: mr.ContentType,
					})
				}

				// Inject post-turn tracker for announce run (leader may create new tasks).
				announcePtd := tools.NewPendingTeamDispatch()
				announceCtx := tools.WithPendingTeamDispatch(ctx, announcePtd)
				announceOutCh := sched.Schedule(announceCtx, scheduler.LaneSubagent, announceReq)
				announceOutcome := <-announceOutCh

				// Post-turn: dispatch pending team tasks created during announce.
				if postTurn != nil {
					for tid, tIDs := range announcePtd.Drain() {
						if err := postTurn.ProcessPendingTasks(ctx, tid, tIDs); err != nil {
							slog.Warn("post_turn(announce): failed", "team_id", tid, "error", err)
						}
					}
				}

				if announceOutcome.Err != nil {
					slog.Error("teammate announce: lead run failed", "error", announceOutcome.Err)
					return
				}

				isSilent := announceOutcome.Result.Content == "" || agent.IsSilentReply(announceOutcome.Result.Content)
				if isSilent && len(announceOutcome.Result.Media) == 0 {
					return
				}

				announceOut := announceOutcome.Result.Content
				if isSilent {
					announceOut = ""
				}
				outMsg := bus.OutboundMessage{
					Channel:  origCh,
					ChatID:   origChatID,
					Content:  announceOut,
					Metadata: meta,
				}
				appendMediaToOutbound(&outMsg, announceOutcome.Result.Media)
				msgBus.PublishOutbound(outMsg)
			}(origChannel, origChatID, msg.SenderID, taskIDStr, outMeta, msg.Metadata)
			continue
		}

		// --- Command: /reset — clear session history ---
		if msg.Metadata["command"] == "reset" {
			agentID := msg.AgentID
			if agentID == "" {
				agentID = resolveAgentRoute(cfg, msg.Channel, msg.ChatID, msg.PeerKind)
			}
			peerKind := msg.PeerKind
			if peerKind == "" {
				peerKind = string(sessions.PeerDirect)
			}
			sessionKey := sessions.BuildScopedSessionKey(agentID, msg.Channel, sessions.PeerKind(peerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)
			if msg.Metadata["is_forum"] == "true" && peerKind == string(sessions.PeerGroup) {
				var topicID int
				fmt.Sscanf(msg.Metadata["message_thread_id"], "%d", &topicID)
				if topicID > 0 {
					sessionKey = sessions.BuildGroupTopicSessionKey(agentID, msg.Channel, msg.ChatID, topicID)
				}
			}
			sessStore.Reset(sessionKey)
			sessStore.Save(sessionKey)
			providers.ResetCLISession("", sessionKey)
			slog.Info("inbound: /reset command", "session", sessionKey)
			continue
		}

		// --- Command: /stop — cancel oldest active run for this session ---
		// --- Command: /stopall — cancel ALL active runs + drain queue ---
		if cmd := msg.Metadata["command"]; cmd == "stop" || cmd == "stopall" {
			agentID := msg.AgentID
			if agentID == "" {
				agentID = resolveAgentRoute(cfg, msg.Channel, msg.ChatID, msg.PeerKind)
			}
			peerKind := msg.PeerKind
			if peerKind == "" {
				peerKind = string(sessions.PeerDirect)
			}
			sessionKey := sessions.BuildScopedSessionKey(agentID, msg.Channel, sessions.PeerKind(peerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)
			if msg.Metadata["is_forum"] == "true" && peerKind == string(sessions.PeerGroup) {
				var topicID int
				fmt.Sscanf(msg.Metadata["message_thread_id"], "%d", &topicID)
				if topicID > 0 {
					sessionKey = sessions.BuildGroupTopicSessionKey(agentID, msg.Channel, msg.ChatID, topicID)
				}
			}
			if msg.Metadata["dm_thread_id"] != "" && peerKind == string(sessions.PeerDirect) {
				var threadID int
				fmt.Sscanf(msg.Metadata["dm_thread_id"], "%d", &threadID)
				if threadID > 0 {
					sessionKey = sessions.BuildDMThreadSessionKey(agentID, msg.Channel, msg.ChatID, threadID)
				}
			}

			var cancelled bool
			if cmd == "stopall" {
				cancelled = sched.CancelSession(sessionKey)
				slog.Info("inbound: /stopall command", "session", sessionKey, "cancelled", cancelled)
			} else {
				cancelled = sched.CancelOneSession(sessionKey)
				slog.Info("inbound: /stop command", "session", sessionKey, "cancelled", cancelled)
			}

			// Publish feedback so the channel can show the result.
			var feedback string
			if cancelled {
				if cmd == "stopall" {
					feedback = "All tasks stopped."
				} else {
					feedback = "Task stopped."
				}
			} else {
				if cmd == "stopall" {
					feedback = "No active tasks to stop."
				} else {
					feedback = "No active task to stop."
				}
			}
			msgBus.PublishOutbound(bus.OutboundMessage{
				Channel:  msg.Channel,
				ChatID:   msg.ChatID,
				Content:  feedback,
				Metadata: msg.Metadata,
			})
			continue
		}

		// --- Normal messages: route through debouncer ---
		debouncer.Push(msg)
	}
}

// autoSetFollowup sets followup reminders on in_progress tasks when the lead agent
// replies on a real channel. Only sets followup if the task doesn't already have one
// (respects LLM-initiated ask_user). Fire-and-forget, logs errors.
func autoSetFollowup(ctx context.Context, teamStore store.TeamStore, agentStore store.AgentStore, agentKey, channel, chatID, content string) {
	if agentStore == nil {
		return
	}
	// agentKey may be a slug ("default") or a UUID string (from WS clients).
	var ag *store.AgentData
	var err error
	if id, parseErr := uuid.Parse(agentKey); parseErr == nil {
		ag, err = agentStore.GetByID(ctx, id)
	} else {
		ag, err = agentStore.GetByKey(ctx, agentKey)
	}
	if err != nil || ag == nil {
		return
	}
	team, err := teamStore.GetTeamForAgent(ctx, ag.ID)
	if err != nil || team == nil || team.LeadAgentID != ag.ID {
		return // only lead agent triggers auto-set
	}
	// Followup is a v2 feature.
	if !isConsumerTeamV2(team) {
		return
	}

	// Skip auto-followup when lead is waiting for teammates (not user).
	if hasMember, _ := teamStore.HasActiveMemberTasks(ctx, team.ID, ag.ID); hasMember {
		slog.Debug("auto-followup: skipping, active member tasks exist", "team_id", team.ID)
		return
	}

	interval, max := parseFollowupSettings(team)
	followupAt := time.Now().Add(interval)
	msg := truncateForReminder(content, 200)

	n, err := teamStore.SetFollowupForActiveTasks(ctx, team.ID, channel, chatID, followupAt, max, msg)
	if err != nil {
		slog.Warn("auto-set followup failed", "channel", channel, "chat_id", chatID, "error", err)
	} else if n > 0 {
		slog.Info("auto-set followup: set", "channel", channel, "chat_id", chatID, "count", n, "followup_at", followupAt)
	}
}

// isConsumerTeamV2 delegates to tools.IsTeamV2 for version checking.
var isConsumerTeamV2 = tools.IsTeamV2

// parseFollowupSettings extracts followup interval and max reminders from team settings.
func parseFollowupSettings(team *store.TeamData) (time.Duration, int) {
	const (
		defaultIntervalMins = 30
		defaultMax          = 0 // unlimited
	)
	if team.Settings == nil {
		return time.Duration(defaultIntervalMins) * time.Minute, defaultMax
	}
	var settings map[string]any
	if json.Unmarshal(team.Settings, &settings) != nil {
		return time.Duration(defaultIntervalMins) * time.Minute, defaultMax
	}
	interval := defaultIntervalMins
	if v, ok := settings["followup_interval_minutes"].(float64); ok && v > 0 {
		interval = int(v)
	}
	max := defaultMax
	if v, ok := settings["followup_max_reminders"].(float64); ok && v >= 0 {
		max = int(v)
	}
	return time.Duration(interval) * time.Minute, max
}

// truncateForReminder truncates content to maxLen chars, taking the last line as context.
func truncateForReminder(content string, maxLen int) string {
	// Use last non-empty line as it's typically the most relevant.
	lines := strings.Split(strings.TrimSpace(content), "\n")
	msg := lines[len(lines)-1]
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "..."
	}
	return msg
}

// appendMediaToOutbound converts agent MediaResults to outbound MediaAttachments
// on the given OutboundMessage. Handles voice annotation when applicable.
func appendMediaToOutbound(msg *bus.OutboundMessage, media []agent.MediaResult) {
	for _, mr := range media {
		msg.Media = append(msg.Media, bus.MediaAttachment{
			URL:         mr.Path,
			ContentType: mr.ContentType,
		})
		if mr.AsVoice {
			if msg.Metadata == nil {
				msg.Metadata = make(map[string]string)
			}
			msg.Metadata["audio_as_voice"] = "true"
		}
	}
}
