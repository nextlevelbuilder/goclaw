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
	"github.com/nextlevelbuilder/goclaw/internal/channels/telegram/voiceguard"
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
	deps *ConsumerDeps,
) {
	// Inject tenant from channel instance into context so all store operations
	// (agent lookup, session creation, etc.) are tenant-scoped.
	if msg.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, msg.TenantID)
	} else {
		ctx = store.WithTenantID(ctx, store.MasterTenantID)
	}

	// Determine target agent via bindings or explicit AgentID
	agentID := msg.AgentID
	if agentID == "" {
		agentID = resolveAgentRoute(deps.Cfg, msg.Channel, msg.ChatID, msg.PeerKind)
	}

	agentLoop, err := deps.Agents.Get(ctx, agentID)
	if err != nil {
		slog.Warn("inbound: agent not found", "agent", agentID, "channel", msg.Channel)
		return
	}

	// Build session key based on scope config (matching TS buildAgentPeerSessionKey).
	peerKind := msg.PeerKind
	if peerKind == "" {
		peerKind = string(sessions.PeerDirect) // default to DM
	}
	sessionKey := sessions.BuildScopedSessionKey(agentID, msg.Channel, sessions.PeerKind(peerKind), msg.ChatID)

	// Thread-based isolation override (e.g. Slack DM threads, AI Panel)
	if lk := msg.Metadata["local_key"]; lk != "" && strings.Contains(lk, ":thread:") {
		parts := strings.SplitN(lk, ":thread:", 2)
		if len(parts) == 2 {
			sessionKey = sessions.BuildScopedThreadSessionKey(agentID, msg.Channel, sessions.PeerKind(peerKind), msg.ChatID, parts[1])
		}
	}

	// Forum topic: override session key to isolate per-topic history.
	// TS ref: buildTelegramGroupPeerId() in src/telegram/bot/helpers.ts
	if msg.Metadata[tools.MetaIsForum] == "true" && peerKind == string(sessions.PeerGroup) {
		var topicID int
		fmt.Sscanf(msg.Metadata[tools.MetaMessageThreadID], "%d", &topicID)
		if topicID > 0 {
			sessionKey = sessions.BuildGroupTopicSessionKey(agentID, msg.Channel, msg.ChatID, topicID)
		}
	}

	// DM thread: override session key to isolate per-thread history in private chats.
	if msg.Metadata[tools.MetaDMThreadID] != "" && peerKind == string(sessions.PeerDirect) {
		var threadID int
		fmt.Sscanf(msg.Metadata[tools.MetaDMThreadID], "%d", &threadID)
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
		deps.SessStore.SetSessionMetadata(ctx, sessionKey, sessionMeta)
		if deps.AgentStore != nil {
			if agentUUID, err := uuid.Parse(agentID); err == nil && agentUUID != uuid.Nil {
				_ = deps.AgentStore.UpdateUserProfileMetadata(ctx, agentUUID, userID, sessionMeta)
			}
		}
	}

	// Set session label for Pancake channels: "Pancake:{SenderName}:{PageName}"
	if msg.Metadata["pancake_mode"] != "" {
		label := buildPancakeSessionLabel(msg.Metadata["display_name"], msg.Metadata["page_name"])
		deps.SessStore.SetLabel(ctx, sessionKey, label)
	}

	// Auto-collect channel contacts for the contact selector.
	// Skip internal senders (system:*, notification:*, teammate:*, ticker:*, session_send_tool).
	if deps.ContactCollector != nil && msg.SenderID != "" && !bus.IsInternalSender(msg.SenderID) {
		senderNumericID := msg.SenderID
		if idx := strings.IndexByte(senderNumericID, '|'); idx > 0 {
			senderNumericID = senderNumericID[:idx]
		}
		channelType := deps.ChannelMgr.ChannelTypeForName(msg.Channel)
		if channelType == "" {
			channelType = msg.Channel // fallback to instance name
		}
		displayName := sessionMeta["display_name"]
		username := sessionMeta["username"]
		deps.ContactCollector.EnsureContact(ctx, channelType, msg.Channel, senderNumericID, userID, displayName, username, peerKind, "user", "", "")

		// Also collect group chat as a contact (for group permission management / merge).
		// Group IDs (e.g., Telegram "-100456") differ from user IDs — no UNIQUE conflict.
		if peerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
			groupTitle := msg.Metadata[tools.MetaChatTitle] // Telegram: message.Chat.Title
			deps.ContactCollector.EnsureContact(ctx, channelType, msg.Channel, msg.ChatID, "", groupTitle, "", "group", "group", "", "")
		}
	}

	// --- Resolve merged tenant user identity ---
	// If the sender has been merged to a tenant_user, use the tenant user's ID
	// for DM sessions. This enables per-user features (MCP creds, SecureCLI creds).
	// Group sessions keep the group-scoped userID; sender resolution happens via SenderID.
	if deps.ContactCollector != nil && peerKind == string(sessions.PeerDirect) && msg.SenderID != "" && !bus.IsInternalSender(msg.SenderID) {
		senderNumeric := msg.SenderID
		if idx := strings.IndexByte(senderNumeric, '|'); idx > 0 {
			senderNumeric = senderNumeric[:idx]
		}
		chType := deps.ChannelMgr.ChannelTypeForName(msg.Channel)
		if chType == "" {
			chType = msg.Channel
		}
		if resolved, err := deps.ContactCollector.ResolveTenantUserID(ctx, chType, senderNumeric); err == nil && resolved != "" {
			slog.Debug("contact.resolved_tenant_user", "sender", senderNumeric, "tenant_user", resolved)
			userID = resolved
		}
	}

	// --- Quota check ---
	if deps.QuotaChecker != nil {
		qResult := deps.QuotaChecker.Check(ctx, userID, msg.Channel, agentLoop.ProviderName())
		if !qResult.Allowed {
			slog.Warn("security.quota_exceeded",
				"user_id", userID,
				"channel", msg.Channel,
				"window", qResult.Window,
				"used", qResult.Used,
				"limit", qResult.Limit,
			)
			deps.MsgBus.PublishOutbound(bus.OutboundMessage{
				Channel:  msg.Channel,
				ChatID:   msg.ChatID,
				Content:  formatQuotaExceeded(qResult),
				Metadata: msg.Metadata,
			})
			return
		}
		deps.QuotaChecker.Increment(userID)
	}

	// Auto-clear followup reminders when user sends a message on a real channel.
	// Fire-and-forget: don't block message processing.
	if deps.TeamStore != nil && msg.Channel != tools.ChannelSystem && msg.Channel != tools.ChannelTeammate && msg.Channel != tools.ChannelDashboard {
		go func(ch, cid string) {
			if n, err := deps.TeamStore.ClearFollowupByScope(ctx, ch, cid); err != nil {
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
	enableStream := deps.ChannelMgr != nil && deps.ChannelMgr.IsStreamingChannel(msg.Channel, isGroup)

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
	for _, k := range []string{tools.MetaMessageThreadID, "local_key", "placeholder_key", "group_id"} {
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
	blockReply := deps.ChannelMgr != nil && deps.ChannelMgr.ResolveBlockReply(msg.Channel, deps.Cfg.Gateway.BlockReply)
	toolStatus := deps.Cfg.Gateway.ToolStatus == nil || *deps.Cfg.Gateway.ToolStatus // default true
	if deps.ChannelMgr != nil {
		deps.ChannelMgr.RegisterRun(runID, msg.Channel, chatIDForRun, messageID, outMeta, enableStream, blockReply, toolStatus)
	}

	// Group-aware system prompt: help the LLM adapt tone and behavior for group chats.
	var extraPrompt string
	if peerKind == string(sessions.PeerGroup) {
		extraPrompt = "You are in a GROUP chat (multiple participants), not a private 1-on-1 DM.\n" +
			"- Messages may include a [Chat messages since your last reply] section with recent group history. Each history line shows \"sender [time]: message\".\n" +
			"- The current message includes a [From: sender_name] tag identifying who @mentioned you.\n" +
			"- Keep responses concise and focused; long replies are disruptive in groups.\n" +
			"- Write like a human. Avoid Markdown tables. Use real line breaks sparingly.\n" +
			"- Address the group naturally. If the history shows a multi-person conversation, consider the full context before answering."
	}

	// Append per-topic system prompt (from group/topic config hierarchy).
	if tsp := msg.Metadata[tools.MetaTopicSystemPrompt]; tsp != "" {
		if extraPrompt != "" {
			extraPrompt += "\n\n"
		}
		extraPrompt += tsp
	}

	// Per-topic skill filter override (from group/topic config hierarchy).
	var skillFilter []string
	if ts := msg.Metadata[tools.MetaTopicSkills]; ts != "" {
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
	if maxConcurrent == 1 && deps.Agents.IsSessionBusy(sessionKey) {
		if loop, ok := agentLoop.(*agent.Loop); ok && loop.Provider() != nil {
			locale := msg.Metadata["locale"]
			if locale == "" {
				locale = "en"
			}
			intent := agent.ClassifyIntent(ctx, loop.Provider(), loop.Model(), msg.Content)
			switch intent {
			case agent.IntentStatusQuery:
				status := deps.Agents.GetActivity(sessionKey)
				reply := agent.FormatStatusReply(status, locale)
				deps.MsgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  msg.Channel,
					ChatID:   msg.ChatID,
					Content:  reply,
					Metadata: outMeta,
				})
				return
			case agent.IntentCancel:
				aborted := deps.Agents.AbortRunsForSession(sessionKey)
				if len(aborted) > 0 {
					slog.Info("inbound: cancelled runs via intent classify",
						"session", sessionKey, "aborted", aborted)
					deps.MsgBus.PublishOutbound(bus.OutboundMessage{
						Channel:  msg.Channel,
						ChatID:   msg.ChatID,
						Content:  i18n.T(locale, i18n.MsgCancelledReply),
						Metadata: outMeta,
					})
				}
				return
			case agent.IntentSteer:
				// Steer: inject into running loop to redirect/add to current task.
				injected := deps.Agents.InjectMessage(sessionKey, agent.InjectedMessage{
					Content: msg.Content,
					UserID:  userID,
				})
				if injected {
					slog.Info("inbound: injected steer message",
						"session", sessionKey)
					deps.MsgBus.PublishOutbound(bus.OutboundMessage{
						Channel:  msg.Channel,
						ChatID:   msg.ChatID,
						Content:  i18n.T(locale, i18n.MsgInjectedAck),
						Metadata: outMeta,
					})
					return
				}
				// Fallback: injection failed (channel full) → fall through to scheduler queue
				slog.Info("inbound: steer injection failed, queueing as normal",
					"session", sessionKey)
			case agent.IntentNewTask:
				// New unrelated request: fall through to scheduler queue
				slog.Info("inbound: new task queued behind active run",
					"session", sessionKey)
			}
		}
	}

	// Inject tenant context from channel instance so all store queries are tenant-scoped.
	if msg.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, msg.TenantID)
	}

	// Inject post-turn dispatch tracker so team task creates are deferred.
	ptd := tools.NewPendingTeamDispatch()
	schedCtx := tools.WithPendingTeamDispatch(ctx, ptd)

	// Propagate run_kind from metadata (e.g. "notification" for team task status relays).
	if rk := msg.Metadata["run_kind"]; rk != "" {
		schedCtx = tools.WithRunKind(schedCtx, rk)
	}

	// Schedule through main lane (per-session concurrency controlled by maxConcurrent)
	outCh := deps.Sched.ScheduleWithOpts(schedCtx, "main", agent.RunRequest{
		SessionKey:        sessionKey,
		Message:           msg.Content,
		Media:             reqMedia,
		ForwardMedia:      fwdMedia,
		Channel:           msg.Channel,
		ChannelType:       resolveChannelType(deps.ChannelMgr, msg.Channel),
		ChatTitle:         msg.Metadata[tools.MetaChatTitle],
		ChatID:            msg.ChatID,
		PeerKind:          peerKind,
		LocalKey:          msg.Metadata["local_key"],
		UserID:            userID,
		SenderID:          msg.SenderID,
		SenderName:        resolveSenderName(msg),
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
	go func(agentKey, channel, chatID, session, rID, peerKind, inboundContent string, meta map[string]string, blockReplyEnabled bool, ptd *tools.PendingTeamDispatch) {
		outcome := <-outCh

		// Release team create lock — tasks already visible in DB, other goroutines can list.
		ptd.ReleaseTeamLock()

		// Post-turn: dispatch pending team tasks created during this turn.
		if deps.PostTurn != nil {
			for teamID, taskIDs := range ptd.Drain() {
				if err := deps.PostTurn.ProcessPendingTasks(ctx, teamID, taskIDs); err != nil {
					slog.Warn("post_turn: failed", "team_id", teamID, "error", err)
				}
			}
		}

		// Clean up run tracking (in case HandleAgentEvent didn't fire for terminal events)
		if deps.ChannelMgr != nil {
			deps.ChannelMgr.UnregisterRun(rID)
		}

		if outcome.Err != nil {
			// Don't send error for cancelled runs (/stop command) —
			// publish empty outbound to clean up thinking/typing indicators.
			if errors.Is(outcome.Err, context.Canceled) {
				slog.Info("inbound: run cancelled", "channel", channel, "session", session)
				deps.MsgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  channel,
					ChatID:   chatID,
					Content:  "",
					Metadata: meta,
				})
				return
			}
			slog.Error("inbound: agent run failed", "error", outcome.Err, "channel", channel)
			deps.MsgBus.PublishOutbound(bus.OutboundMessage{
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
			deps.MsgBus.PublishOutbound(bus.OutboundMessage{
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
			deps.MsgBus.PublishOutbound(bus.OutboundMessage{
				Channel:  channel,
				ChatID:   chatID,
				Content:  "",
				Metadata: meta,
			})
			return
		}

		// Sanitize voice agent replies: replace technical errors with user-friendly fallback.
		replyContent := voiceguard.SanitizeReply(
			deps.Cfg.Channels.Telegram.VoiceAgentID, agentKey,
			channel, peerKind, inboundContent, outcome.Result.Content,
			deps.Cfg.Channels.Telegram.AudioGuardFallbackTranscript,
			deps.Cfg.Channels.Telegram.AudioGuardFallbackNoTranscript,
			deps.Cfg.Channels.Telegram.AudioGuardErrorMarkers,
		)

		// Publish response back to the channel
		outMsg := bus.OutboundMessage{
			Channel:  channel,
			ChatID:   chatID,
			Content:  replyContent,
			Metadata: meta,
		}

		appendMediaToOutbound(&outMsg, outcome.Result.Media)

		deps.MsgBus.PublishOutbound(outMsg)

		// Auto-set followup when lead agent replies on a real channel with in_progress tasks.
		if deps.TeamStore != nil && channel != tools.ChannelSystem && channel != tools.ChannelTeammate && channel != tools.ChannelDashboard {
			go autoSetFollowup(ctx, deps.TeamStore, deps.AgentStore, agentKey, channel, chatID, replyContent)
		}
	}(agentID, msg.Channel, msg.ChatID, sessionKey, runID, peerKind, msg.Content, outMeta, blockReply, ptd)
}
