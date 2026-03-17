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
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// handleSubagentAnnounce processes subagent announce messages: bypass debounce,
// inject into parent agent session (matching TS subagent-announce.ts pattern).
// Returns true if the message was handled (caller should continue).
func handleSubagentAnnounce(
	ctx context.Context,
	msg bus.InboundMessage,
	cfg *config.Config,
	sched *scheduler.Scheduler,
	channelMgr *channels.Manager,
	msgBus *bus.MessageBus,
	getAnnounceMu func(string) *sync.Mutex,
) bool {
	if !(msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "subagent:")) {
		return false
	}

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
		return true
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

	return true
}

// handleDelegateAnnounce processes delegate announce messages: bypass debounce,
// inject into parent agent session using the "delegate" lane.
// Returns true if the message was handled (caller should continue).
func handleDelegateAnnounce(
	ctx context.Context,
	msg bus.InboundMessage,
	cfg *config.Config,
	sched *scheduler.Scheduler,
	channelMgr *channels.Manager,
	msgBus *bus.MessageBus,
	getAnnounceMu func(string) *sync.Mutex,
) bool {
	if !(msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "delegate:")) {
		return false
	}

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
		return true
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

	return true
}


// handleTeammateMessage processes teammate messages: bypass debounce, route to target
// agent session using the "delegate" lane, then announce result back to lead.
// Returns true if the message was handled (caller should continue).
func handleTeammateMessage(
	ctx context.Context,
	msg bus.InboundMessage,
	cfg *config.Config,
	sched *scheduler.Scheduler,
	channelMgr *channels.Manager,
	teamStore store.TeamStore,
	agentStore store.AgentStore,
	msgBus *bus.MessageBus,
	postTurn tools.PostTurnProcessor,
	taskRunSessions *sync.Map,
) bool {
	if !(msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "teammate:")) {
		return false
	}

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
		return true
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
						// Enrich event payload with task details for notifications.
						taskSubject := ""
						taskNumber := 0
						taskChannel := inMeta["origin_channel"]
						taskChatID := inMeta["origin_chat_id"]
						if currentTask != nil {
							taskSubject = currentTask.Subject
							taskNumber = currentTask.TaskNumber
							if currentTask.Channel != "" {
								taskChannel = currentTask.Channel
							}
							if currentTask.ChatID != "" {
								taskChatID = currentTask.ChatID
							}
						}
						if outcome.Err != nil {
							if err := teamStore.FailTask(ctx, teamTaskID, teamID, outcome.Err.Error()); err != nil {
								slog.Warn("auto-complete: FailTask error", "task_id", teamTaskID, "error", err)
							} else {
								msgBus.Broadcast(bus.Event{
									Name: protocol.EventTeamTaskFailed,
									Payload: protocol.TeamTaskEventPayload{
										TeamID:     teamID.String(),
										TaskID:     teamTaskID.String(),
										TaskNumber: taskNumber,
										Subject:    taskSubject,
										Status:     store.TeamTaskStatusFailed,
										Reason:     outcome.Err.Error(),
										Channel:    taskChannel,
										ChatID:     taskChatID,
										Timestamp:  now,
										ActorType:  "agent",
										ActorID:    toAgent,
									},
								})
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
										TaskNumber:    taskNumber,
										Subject:       taskSubject,
										Status:        store.TeamTaskStatusCompleted,
										OwnerAgentKey: toAgent,
										Channel:       taskChannel,
										ChatID:        taskChatID,
										Timestamp:     now,
										ActorType:     "agent",
										ActorID:       toAgent,
									},
								})
							}
						}
					}
					// Always dispatch unblocked tasks after member turn ends,
					// regardless of whether the task was already completed by the tool.
					// This ensures dependent tasks start only after the member's run finishes.
					if postTurn != nil {
						postTurn.DispatchUnblockedTasks(ctx, teamID)
					}
				}
			}
		}

		// Determine announce content: success result or failure error.
		var announceContent string
		var announceMedia []agent.MediaResult
		if outcome.Err != nil {
			slog.Error("teammate message: agent run failed", "error", outcome.Err)
			errMsg := outcome.Err.Error()
			if len(errMsg) > 500 {
				errMsg = errMsg[:500] + "..."
			}
			announceContent = fmt.Sprintf("[FAILED] %s", errMsg)
		} else if (outcome.Result.Content == "" && len(outcome.Result.Media) == 0) || agent.IsSilentReply(outcome.Result.Content) {
			slog.Info("teammate message: suppressed silent/empty reply", "from", senderID)
			return
		} else {
			announceContent = outcome.Result.Content
			announceMedia = outcome.Result.Media
		}

		// Announce result (or failure) to lead agent via announce queue.
		// Queue merges concurrent completions into a single batched announce.
		if origChatID == "" {
			slog.Warn("teammate announce: no origin_chat_id, cannot announce to lead")
			return
		}

		// Resolve lead agent.
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

		origPeerKind := inMeta["origin_peer_kind"]
		if origPeerKind == "" {
			origPeerKind = string(sessions.PeerDirect)
		}
		origLocalKey := inMeta["origin_local_key"]
		leadSessionKey := sessions.BuildScopedSessionKey(leadAgent, origCh, sessions.PeerKind(origPeerKind), origChatID, cfg.Sessions.Scope, cfg.Sessions.DmScope, cfg.Sessions.MainKey)
		leadSessionKey = overrideSessionKeyFromLocalKey(leadSessionKey, origLocalKey, leadAgent, origCh, origChatID, origPeerKind)

		// Extract trace context for announce linking.
		var parentTraceID, parentRootSpanID uuid.UUID
		if tid := inMeta["origin_trace_id"]; tid != "" {
			parentTraceID, _ = uuid.Parse(tid)
		}
		if sid := inMeta["origin_root_span_id"]; sid != "" {
			parentRootSpanID, _ = uuid.Parse(sid)
		}

		// Enqueue result. If we become the processor, run the announce loop.
		entry := announceEntry{
			MemberAgent: inMeta["to_agent"],
			Content:     announceContent,
			Media:       announceMedia,
		}
		q, isProcessor := enqueueAnnounce(leadSessionKey, entry)
		if !isProcessor {
			slog.Info("teammate announce: merged into pending batch",
				"member", entry.MemberAgent, "session", leadSessionKey)
			return
		}

		routing := announceRouting{
			LeadAgent:        leadAgent,
			LeadSessionKey:   leadSessionKey,
			OrigChannel:      origCh,
			OrigChatID:       origChatID,
			OrigPeerKind:     origPeerKind,
			OrigLocalKey:     origLocalKey,
			OriginUserID:     inMeta["origin_user_id"],
			TeamID:           inMeta["team_id"],
			TeamWorkspace:    inMeta["team_workspace"],
			OriginTraceID:    inMeta["origin_trace_id"],
			ParentTraceID:    parentTraceID,
			ParentRootSpanID: parentRootSpanID,
			OutMeta:          meta,
		}
		processAnnounceLoop(ctx, q, routing, sched, msgBus, teamStore, postTurn, cfg)
	}(origChannel, origChatID, msg.SenderID, taskIDStr, outMeta, msg.Metadata)

	return true
}

// handleResetCommand processes /reset command: clears session history.
// Returns true if the message was handled (caller should continue).
func handleResetCommand(
	msg bus.InboundMessage,
	cfg *config.Config,
	sessStore store.SessionStore,
) bool {
	if msg.Metadata["command"] != "reset" {
		return false
	}

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

	return true
}

// handleStopCommand processes /stop and /stopall commands: cancel active runs for a session.
// Returns true if the message was handled (caller should continue).
func handleStopCommand(
	msg bus.InboundMessage,
	cfg *config.Config,
	sched *scheduler.Scheduler,
	sessStore store.SessionStore,
	msgBus *bus.MessageBus,
) bool {
	cmd := msg.Metadata["command"]
	if cmd != "stop" && cmd != "stopall" {
		return false
	}

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

	// sessStore is referenced in the original code but not used in this branch beyond
	// session key construction; kept as parameter for API consistency.
	_ = sessStore

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

	return true
}

// buildTaskBoardSnapshot returns a formatted summary of batch task statuses
// for inclusion in the announce message to the leader. Scoped by (teamID, chatID)
// and filtered by origin_trace_id to show only tasks from the current batch.
func buildTaskBoardSnapshot(ctx context.Context, teamStore store.TeamStore, teamID uuid.UUID, chatID, originTraceID string) string {
	if teamStore == nil || originTraceID == "" {
		return ""
	}
	// Shared workspace: show all tasks across chats.
	snapshotChatID := chatID
	if team, err := teamStore.GetTeam(ctx, teamID); err == nil && tools.IsSharedWorkspace(team.Settings) {
		snapshotChatID = ""
	}
	allTasks, err := teamStore.ListTasks(ctx, teamID, "", store.TeamTaskFilterAll, "", "", snapshotChatID, 0)
	if err != nil || len(allTasks) == 0 {
		return ""
	}

	// Filter to current batch by origin_trace_id stored in task metadata.
	var active, completed int
	var activeLines []string
	for _, t := range allTasks {
		tid, _ := t.Metadata["origin_trace_id"].(string)
		if tid != originTraceID {
			continue
		}
		switch t.Status {
		case store.TeamTaskStatusCompleted, store.TeamTaskStatusCancelled, store.TeamTaskStatusFailed:
			completed++
		default:
			active++
			activeLines = append(activeLines, fmt.Sprintf("  #%d %s — %s", t.TaskNumber, t.Subject, t.Status))
		}
	}
	total := active + completed
	if total == 0 {
		return ""
	}
	if active == 0 {
		return fmt.Sprintf("=== Task board (this batch) ===\nAll %d tasks completed.", total)
	}
	return fmt.Sprintf("=== Task board (this batch) ===\nTask progress: %d/%d completed, %d active:\n%s",
		completed, total, active, strings.Join(activeLines, "\n"))
}
