package cmd

import (
	"context"
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
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// consumeInboundMessages reads inbound messages from channels (Telegram, Discord, etc.)
// and routes them through the scheduler/agent loop, then publishes the response back.
// Also handles subagent announcements: routes them through the parent agent's session
// (matching TS subagent-announce.ts pattern) so the agent can reformulate for the user.
func consumeInboundMessages(ctx context.Context, msgBus *bus.MessageBus, agents *agent.Router, cfg *config.Config, sched *scheduler.Scheduler, channelMgr *channels.Manager, teamStore store.TeamStore, quotaChecker *channels.QuotaChecker, delegateMgr *tools.DelegateManager, sessStore store.SessionStore, agentStore store.AgentStore, contactCollector *store.ContactCollector) {
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
		// to detect status queries, cancel requests, etc. without queueing.
		// Only for DM (maxConcurrent=1) where messages queue behind the active run.
		intentClassifyEnabled := cfg.Agents.Defaults.IntentClassify == nil || *cfg.Agents.Defaults.IntentClassify
		if intentClassifyEnabled && maxConcurrent == 1 && agents.IsSessionBusy(sessionKey) {
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
				default:
					// steer / new_task → queue as normal
				}
			}
		}

		// Schedule through main lane (per-session concurrency controlled by maxConcurrent)
		outCh := sched.ScheduleWithOpts(ctx, "main", agent.RunRequest{
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
		go func(channel, chatID, session, rID string, meta map[string]string, blockReplyEnabled bool) {
			outcome := <-outCh

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
		}(msg.Channel, msg.ChatID, sessionKey, runID, outMeta, blockReply)
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
		if msg.Channel == "system" && strings.HasPrefix(msg.SenderID, "subagent:") {
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
		if msg.Channel == "system" && strings.HasPrefix(msg.SenderID, "delegate:") {
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
		if msg.Channel == "system" && strings.HasPrefix(msg.SenderID, "handoff:") {
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
		if msg.Channel == "system" && strings.HasPrefix(msg.SenderID, "teammate:") {
			origChannel := msg.Metadata["origin_channel"]
			origPeerKind := msg.Metadata["origin_peer_kind"]
			origLocalKey := msg.Metadata["origin_local_key"]
			origChannelType := resolveChannelType(channelMgr, origChannel)
			targetAgent := msg.AgentID // team_message sets AgentID to the target agent key
			if targetAgent == "" {
				targetAgent = cfg.ResolveDefaultAgentID()
			}
			if origPeerKind == "" {
				origPeerKind = string(sessions.PeerDirect)
			}

			if origChannel == "" || msg.ChatID == "" {
				slog.Warn("teammate message: missing origin", "sender", msg.SenderID)
				continue
			}

			sessionKey := sessions.BuildScopedSessionKey(targetAgent, origChannel, sessions.PeerKind(origPeerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)
			sessionKey = overrideSessionKeyFromLocalKey(sessionKey, origLocalKey, targetAgent, origChannel, msg.ChatID, origPeerKind)

			slog.Info("teammate message → scheduler (delegate lane)",
				"from", msg.SenderID,
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
				RunID:       fmt.Sprintf("teammate-%s-%s", msg.Metadata["from_agent"], msg.Metadata["to_agent"]),
				Stream:      false,
			})

			go func(origCh, chatID, senderID string, meta map[string]string) {
				outcome := <-outCh
				if outcome.Err != nil {
					slog.Error("teammate message: agent run failed", "error", outcome.Err)
					return
				}
				if (outcome.Result.Content == "" && len(outcome.Result.Media) == 0) || agent.IsSilentReply(outcome.Result.Content) {
					slog.Info("teammate message: suppressed silent/empty reply", "from", senderID)
					return
				}
				// Deliver response to origin channel (same as delegate/subagent announce).
				// This allows the lead to respond to users after receiving teammate updates.
				outMsg := bus.OutboundMessage{
					Channel:  origCh,
					ChatID:   chatID,
					Content:  outcome.Result.Content,
					Metadata: meta,
				}
				appendMediaToOutbound(&outMsg, outcome.Result.Media)
				msgBus.PublishOutbound(outMsg)
			}(origChannel, msg.ChatID, msg.SenderID, outMeta)
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
				// Also cancel async delegations for this chat (they bypass the scheduler)
				if delegateMgr != nil {
					dc := delegateMgr.CancelForOrigin(msg.Channel, msg.ChatID)
					if dc > 0 {
						cancelled = true
					}
				}
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
