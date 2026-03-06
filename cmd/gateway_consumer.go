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
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// makeSchedulerRunFunc creates the RunFunc for the scheduler.
// It extracts the agentID from the session key and routes to the correct agent loop.
func makeSchedulerRunFunc(agents *agent.Router, cfg *config.Config) scheduler.RunFunc {
	return func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		// Extract agentID from session key (format: agent:{agentId}:{rest})
		agentID := cfg.ResolveDefaultAgentID()
		if parts := strings.SplitN(req.SessionKey, ":", 3); len(parts) >= 2 && parts[0] == "agent" {
			agentID = parts[1]
		}

		loop, err := agents.Get(agentID)
		if err != nil {
			return nil, fmt.Errorf("agent %s not found: %w", agentID, err)
		}
		return loop.Run(ctx, req)
	}
}

// consumeInboundMessages reads inbound messages from channels (Telegram, Discord, etc.)
// and routes them through the scheduler/agent loop, then publishes the response back.
// Also handles subagent announcements: routes them through the parent agent's session
// (matching TS subagent-announce.ts pattern) so the agent can reformulate for the user.
func consumeInboundMessages(ctx context.Context, msgBus *bus.MessageBus, agents *agent.Router, cfg *config.Config, sched *scheduler.Scheduler, channelMgr *channels.Manager, teamStore store.TeamStore, quotaChecker *channels.QuotaChecker, delegateMgr *tools.DelegateManager) {
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

		// Group-scoped UserID: treat the group as a single "virtual user" for
		// context files, memory, traces, and seeding. Individual senderID is
		// preserved in the InboundMessage for pairing/dedup/mention gate.
		// Format: "group:{channel}:{chatID}" — e.g., "group:telegram:-1002541239372"
		// For Discord: use guild_id so all channels in the same server share
		// context files, memory, and seeding (session key stays per-channel).
		userID := msg.UserID
		if peerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
			groupID := msg.ChatID
			if guildID := msg.Metadata["guild_id"]; guildID != "" {
				groupID = guildID
			}
			userID = fmt.Sprintf("group:%s:%s", msg.Channel, groupID)
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

		// Register run with channel manager for streaming/reaction event forwarding.
		// Use localKey (composite key with topic suffix) so streaming/reaction events
		// route to the correct per-topic state in the channel.
		messageID := msg.Metadata["message_id"]
		chatIDForRun := msg.ChatID
		if lk := msg.Metadata["local_key"]; lk != "" {
			chatIDForRun = lk
		}
		if channelMgr != nil {
			channelMgr.RegisterRun(runID, msg.Channel, chatIDForRun, messageID)
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
		var reqMedia, fwdMedia []string
		if msg.Metadata["delegation_id"] != "" || msg.Metadata["subagent_id"] != "" {
			fwdMedia = msg.Media
		} else {
			reqMedia = msg.Media
		}

		// Schedule through main lane (per-session concurrency controlled by maxConcurrent)
		outCh := sched.ScheduleWithOpts(ctx, "main", agent.RunRequest{
			SessionKey:        sessionKey,
			Message:           msg.Content,
			Media:             reqMedia,
			ForwardMedia:      fwdMedia,
			Channel:           msg.Channel,
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

		// Build outbound metadata for reply-to + thread routing.
		// Groups: reply to user's message so context is clear in busy chats.
		// DMs: no reply needed — response edits the placeholder or sends inline.
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

		// Handle result asynchronously to not block the flush callback.
		go func(channel, chatID, session, rID string, meta map[string]string) {
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

			// Publish response back to the channel
			outMsg := bus.OutboundMessage{
				Channel:  channel,
				ChatID:   chatID,
				Content:  outcome.Result.Content,
				Metadata: meta,
			}

			// Convert media results from agent run to outbound media attachments
			for _, mr := range outcome.Result.Media {
				outMsg.Media = append(outMsg.Media, bus.MediaAttachment{
					URL:         mr.Path,
					ContentType: mr.ContentType,
				})
				if mr.AsVoice {
					if outMsg.Metadata == nil {
						outMsg.Metadata = make(map[string]string)
					}
					outMsg.Metadata["audio_as_voice"] = "true"
				}
			}

			msgBus.PublishOutbound(outMsg)
		}(msg.Channel, msg.ChatID, sessionKey, runID, outMeta)
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

			// Use SAME session as user's original chat so agent has context.
			sessionKey := sessions.BuildScopedSessionKey(parentAgent, origChannel, sessions.PeerKind(origPeerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)

			// Override session key for forum topics / DM threads (same logic as main lane).
			sessionKey = overrideSessionKeyFromLocalKey(sessionKey, origLocalKey, parentAgent, origChannel, msg.ChatID, origPeerKind)

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
			announceReq := agent.RunRequest{
				SessionKey:       sessionKey,
				Message:          msg.Content,
				ForwardMedia:     msg.Media,
				Channel:          origChannel,
				ChatID:           msg.ChatID,
				PeerKind:         origPeerKind,
				LocalKey:         origLocalKey,
				UserID:           announceUserID,
				RunID:            fmt.Sprintf("announce-%s", msg.SenderID),
				RunKind:          "announce",
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
				for _, mr := range outcome.Result.Media {
					outMsg.Media = append(outMsg.Media, bus.MediaAttachment{
						URL:         mr.Path,
						ContentType: mr.ContentType,
					})
				}
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

			sessionKey := sessions.BuildScopedSessionKey(parentAgent, origChannel, sessions.PeerKind(origPeerKind), msg.ChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)

			// Override session key for forum topics / DM threads.
			sessionKey = overrideSessionKeyFromLocalKey(sessionKey, origLocalKey, parentAgent, origChannel, msg.ChatID, origPeerKind)

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

			announceReq := agent.RunRequest{
				SessionKey:       sessionKey,
				Message:          msg.Content,
				ForwardMedia:     msg.Media,
				Channel:          origChannel,
				ChatID:           msg.ChatID,
				PeerKind:         origPeerKind,
				LocalKey:         origLocalKey,
				UserID:           announceUserID,
				RunID:            fmt.Sprintf("delegate-announce-%s", msg.Metadata["delegation_id"]),
				RunKind:          "announce",
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
				for _, mr := range outcome.Result.Media {
					outMsg.Media = append(outMsg.Media, bus.MediaAttachment{
						URL:         mr.Path,
						ContentType: mr.ContentType,
					})
				}
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
				SessionKey: sessionKey,
				Message:    msg.Content,
				Channel:    origChannel,
				ChatID:     msg.ChatID,
				PeerKind:   origPeerKind,
				LocalKey:   origLocalKey,
				UserID:     announceUserID,
				RunID:      fmt.Sprintf("handoff-%s", msg.Metadata["handoff_id"]),
				Stream:     false,
			})

			go func(origCh, chatID string, meta map[string]string) {
				outcome := <-outCh
				if outcome.Err != nil {
					slog.Error("handoff announce: agent run failed", "error", outcome.Err)
					return
				}
				if outcome.Result.Content == "" || agent.IsSilentReply(outcome.Result.Content) {
					return
				}
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  origCh,
					ChatID:   chatID,
					Content:  outcome.Result.Content,
					Metadata: meta,
				})
			}(origChannel, msg.ChatID, outMeta)
			continue
		}

		// --- Teammate message: bypass debounce, route to target agent session ---
		// Same pattern as delegate announce, using "delegate" lane.
		if msg.Channel == "system" && strings.HasPrefix(msg.SenderID, "teammate:") {
			origChannel := msg.Metadata["origin_channel"]
			origPeerKind := msg.Metadata["origin_peer_kind"]
			origLocalKey := msg.Metadata["origin_local_key"]
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
				SessionKey: sessionKey,
				Message:    msg.Content,
				Channel:    origChannel,
				ChatID:     msg.ChatID,
				PeerKind:   origPeerKind,
				LocalKey:   origLocalKey,
				UserID:     announceUserID,
				RunID:      fmt.Sprintf("teammate-%s-%s", msg.Metadata["from_agent"], msg.Metadata["to_agent"]),
				Stream:     false,
			})

			go func(origCh, chatID, senderID string, meta map[string]string) {
				outcome := <-outCh
				if outcome.Err != nil {
					slog.Error("teammate message: agent run failed", "error", outcome.Err)
					return
				}
				if outcome.Result.Content == "" || agent.IsSilentReply(outcome.Result.Content) {
					slog.Info("teammate message: suppressed silent/empty reply", "from", senderID)
					return
				}
				// Deliver response to origin channel (same as delegate/subagent announce).
				// This allows the lead to respond to users after receiving teammate updates.
				msgBus.PublishOutbound(bus.OutboundMessage{
					Channel:  origCh,
					ChatID:   chatID,
					Content:  outcome.Result.Content,
					Metadata: meta,
				})
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

// resolveAgentRoute determines which agent should handle a message
// based on config bindings. Priority: peer → channel → default.
// Matching TS resolve-route.ts binding resolution.
func resolveAgentRoute(cfg *config.Config, channel, chatID, peerKind string) string {
	for _, binding := range cfg.Bindings {
		match := binding.Match
		if match.Channel != channel {
			continue
		}

		// Peer-level match (most specific)
		if match.Peer != nil {
			if match.Peer.Kind == peerKind && match.Peer.ID == chatID {
				return config.NormalizeAgentID(binding.AgentID)
			}
			continue // has peer constraint but doesn't match — skip
		}

		// Channel-level match (least specific, no peer constraint)
		return config.NormalizeAgentID(binding.AgentID)
	}

	return cfg.ResolveDefaultAgentID()
}

// overrideSessionKeyFromLocalKey extracts topic/thread ID from the composite
// local_key and returns the correct session key for forum topics or DM threads.
// If localKey is empty or has no suffix, the original sessionKey is returned unchanged.
func overrideSessionKeyFromLocalKey(sessionKey, localKey, agentID, channel, chatID, peerKind string) string {
	if localKey == "" {
		return sessionKey
	}
	if idx := strings.Index(localKey, ":topic:"); idx > 0 && peerKind == string(sessions.PeerGroup) {
		var topicID int
		fmt.Sscanf(localKey[idx+7:], "%d", &topicID)
		if topicID > 0 {
			return sessions.BuildGroupTopicSessionKey(agentID, channel, chatID, topicID)
		}
	} else if idx := strings.Index(localKey, ":thread:"); idx > 0 && peerKind == string(sessions.PeerDirect) {
		var threadID int
		fmt.Sscanf(localKey[idx+8:], "%d", &threadID)
		if threadID > 0 {
			return sessions.BuildDMThreadSessionKey(agentID, channel, chatID, threadID)
		}
	}
	return sessionKey
}

// buildAnnounceOutMeta builds outbound metadata for announce messages so that
// Send() can route replies to the correct forum topic or DM thread.
func buildAnnounceOutMeta(localKey string) map[string]string {
	if localKey == "" {
		return nil
	}
	meta := map[string]string{"local_key": localKey}
	if idx := strings.Index(localKey, ":topic:"); idx > 0 {
		meta["message_thread_id"] = localKey[idx+7:]
	} else if idx := strings.Index(localKey, ":thread:"); idx > 0 {
		meta["message_thread_id"] = localKey[idx+8:]
	}
	return meta
}
