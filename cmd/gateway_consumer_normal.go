package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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

// processNormalMessage handles routing, scheduling, and response delivery for a single
// (possibly merged) inbound message. Called directly by the debouncer's flush callback.
func processNormalMessage(
	ctx context.Context,
	msg bus.InboundMessage,
	agents *agent.Router,
	cfg *config.Config,
	sched *scheduler.Scheduler,
	channelMgr *channels.Manager,
	teamStore store.TeamStore,
	quotaChecker *channels.QuotaChecker,
	sessStore store.SessionStore,
	agentStore store.AgentStore,
	contactCollector *store.ContactCollector,
	postTurn tools.PostTurnProcessor,
	msgBus *bus.MessageBus,
) {
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
