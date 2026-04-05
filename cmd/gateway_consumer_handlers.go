package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// handleSubagentAnnounce processes subagent announce messages: bypass debounce,
// inject into parent agent session (matching TS subagent-announce.ts pattern).
// Returns true if the message was handled (caller should continue).
func handleSubagentAnnounce(
	ctx context.Context,
	msg bus.InboundMessage,
	deps *ConsumerDeps,
) bool {
	if !(msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "subagent:")) {
		return false
	}

	// Inject tenant scope — same as processNormalMessage.
	if msg.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, msg.TenantID)
	} else {
		ctx = store.WithTenantID(ctx, store.MasterTenantID)
	}

	origChannel := msg.Metadata[tools.MetaOriginChannel]
	origPeerKind := msg.Metadata[tools.MetaOriginPeerKind]
	origLocalKey := msg.Metadata[tools.MetaOriginLocalKey]
	origChannelType := resolveChannelType(deps.ChannelMgr, origChannel)
	parentAgent := msg.Metadata[tools.MetaParentAgent]
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
	sessionKey := msg.Metadata[tools.MetaOriginSessionKey]
	if sessionKey == "" {
		// Fallback: rebuild session key from origin metadata (works for Telegram, Discord, etc.)
		sessionKey = sessions.BuildScopedSessionKey(parentAgent, origChannel, sessions.PeerKind(origPeerKind), msg.ChatID)
		sessionKey = overrideSessionKeyFromLocalKey(sessionKey, origLocalKey, parentAgent, origChannel, msg.ChatID, origPeerKind)
	}

	slog.Info("subagent announce → scheduler (subagent lane)",
		"subagent", msg.SenderID,
		"label", msg.Metadata[tools.MetaSubagentLabel],
		"session", sessionKey,
	)

	// Extract parent trace context for announce linking
	var parentTraceID, parentRootSpanID uuid.UUID
	if tid := msg.Metadata[tools.MetaOriginTraceID]; tid != "" {
		parentTraceID, _ = uuid.Parse(tid)
	}
	if sid := msg.Metadata[tools.MetaOriginRootSpanID]; sid != "" {
		parentRootSpanID, _ = uuid.Parse(sid)
	}

	// Group-scoped UserID for subagent announce (same logic as main lane).
	announceUserID := msg.UserID
	if origPeerKind == string(sessions.PeerGroup) && msg.ChatID != "" {
		announceUserID = fmt.Sprintf("group:%s:%s", origChannel, msg.ChatID)
	}

	// Build announce entry from raw metadata (avoids double-formatting).
	runtimeMs, _ := strconv.ParseInt(msg.Metadata[tools.MetaSubagentRuntime], 10, 64)
	iterations, _ := strconv.Atoi(msg.Metadata[tools.MetaSubagentIterations])
	inputToks, _ := strconv.ParseInt(msg.Metadata[tools.MetaSubagentInputToks], 10, 64)
	outputToks, _ := strconv.ParseInt(msg.Metadata[tools.MetaSubagentOutputToks], 10, 64)

	// Use raw result from metadata if available; fall back to formatted Content for backward compat.
	rawResult := msg.Metadata[tools.MetaSubagentResult]
	if rawResult == "" {
		rawResult = msg.Content
	}

	entry := subagentAnnounceEntry{
		Label:        msg.Metadata[tools.MetaSubagentLabel],
		Status:       msg.Metadata[tools.MetaSubagentStatus],
		Content:      rawResult,
		Media:        msg.Media,
		InputTokens:  inputToks,
		OutputTokens: outputToks,
		Runtime:      time.Duration(runtimeMs) * time.Millisecond,
		Iterations:   iterations,
	}

	queueKey := fmt.Sprintf("%s:%s", msg.TenantID, sessionKey)
	routing := subagentAnnounceRouting{
		QueueKey:         queueKey,
		SessionKey:       sessionKey,
		TenantID:         msg.TenantID,
		OrigChannel:      origChannel,
		OrigChannelType:  origChannelType,
		OrigChatID:       msg.ChatID,
		OrigPeerKind:     origPeerKind,
		OrigLocalKey:     origLocalKey,
		UserID:           announceUserID,
		ParentAgent:      parentAgent,
		ParentTraceID:    parentTraceID,
		ParentRootSpanID: parentRootSpanID,
		OutMeta:          buildAnnounceOutMeta(origLocalKey),
	}

	// Enqueue into producer-consumer queue using tenant-scoped key from routing.
	q, isProcessor := enqueueSubagentAnnounce(queueKey, entry)
	if isProcessor {
		deps.BgWg.Add(1)
		go func() {
			defer deps.BgWg.Done()
			defer safego.Recover(nil, "component", "subagent_announce_loop", "session", sessionKey)

			// Fetch live roster for merged announce context.
			roster := deps.SubagentMgr.RosterForParent(parentAgent)

			processSubagentAnnounceLoop(ctx, q, routing, roster, deps.SubagentMgr, deps.Sched, deps.MsgBus, deps.Cfg)
		}()
	}

	return true
}

// handleTeammateMessage processes teammate messages: bypass debounce, route to target
// agent session using the "team" lane, then announce result back to lead.
// Returns true if the message was handled (caller should continue).
func handleTeammateMessage(
	ctx context.Context,
	msg bus.InboundMessage,
	deps *ConsumerDeps,
) bool {
	if !(msg.Channel == tools.ChannelSystem && strings.HasPrefix(msg.SenderID, "teammate:")) {
		return false
	}

	// Inject tenant scope — same as processNormalMessage.
	if msg.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, msg.TenantID)
	} else {
		ctx = store.WithTenantID(ctx, store.MasterTenantID)
	}

	origChannel := msg.Metadata[tools.MetaOriginChannel]
	origPeerKind := msg.Metadata[tools.MetaOriginPeerKind]
	origLocalKey := msg.Metadata[tools.MetaOriginLocalKey]
	origChatID := msg.Metadata[tools.MetaOriginChatID] // original chat (e.g. Telegram chat ID)
	if origChatID == "" {
		origChatID = msg.ChatID // fallback to inbound ChatID (team UUID for old dispatches)
	}
	origChannelType := resolveChannelType(deps.ChannelMgr, origChannel)
	targetAgent := msg.AgentID // dispatch sets AgentID to the target agent key
	if targetAgent == "" {
		targetAgent = deps.Cfg.ResolveDefaultAgentID()
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
	sessionKey := sessions.BuildTeamSessionKey(targetAgent, msg.Metadata[tools.MetaTeamID], origChatID)

	slog.Info("teammate message → scheduler (team lane)",
		"from", msg.SenderID,
		"to", targetAgent,
		"session", sessionKey,
		"team_task_id", msg.Metadata[tools.MetaTeamTaskID],
	)

	announceUserID := msg.UserID
	if origPeerKind == string(sessions.PeerGroup) && origChatID != "" {
		announceUserID = fmt.Sprintf("group:%s:%s", origChannel, origChatID)
	}

	outMeta := buildAnnounceOutMeta(origLocalKey)

	// Link member agent trace back to lead's trace for unified tracing.
	var linkedTraceID uuid.UUID
	if tid := msg.Metadata[tools.MetaOriginTraceID]; tid != "" {
		linkedTraceID, _ = uuid.Parse(tid)
	}

	// Track task → session so the subscriber can cancel on task cancellation.
	taskIDStr := msg.Metadata[tools.MetaTeamTaskID]
	if taskIDStr != "" {
		deps.TaskRunSessions.Store(taskIDStr, sessionKey)
	}

	// Inject action flags into context so team_tasks tool calls record what happened.
	// The post-turn goroutine reads these flags to decide auto-complete vs skip.
	taskActionFlags := &tools.TaskActionFlags{}
	schedCtx := tools.WithTaskActionFlags(ctx, taskActionFlags)

	outCh := deps.Sched.Schedule(schedCtx, scheduler.LaneTeam, agent.RunRequest{
		SessionKey:      sessionKey,
		Message:         msg.Content,
		Channel:         origChannel,
		ChannelType:     origChannelType,
		ChatID:          origChatID,
		PeerKind:        origPeerKind,
		LocalKey:        origLocalKey,
		UserID:          announceUserID,
		RunID:           fmt.Sprintf("teammate-%s-%s", msg.Metadata[tools.MetaFromAgent], msg.Metadata[tools.MetaToAgent]),
		Stream:          false,
		TeamTaskID:      msg.Metadata[tools.MetaTeamTaskID],
		TeamWorkspace:   msg.Metadata[tools.MetaTeamWorkspace],
		LeaderAgentID:   msg.Metadata[tools.MetaLeaderAgentID],
		WorkspaceChatID: origChatID,
		TeamID:          msg.Metadata[tools.MetaTeamID],
		LinkedTraceID:   linkedTraceID,
	})

	deps.BgWg.Add(1)
	go func(origCh, origChatID, senderID, taskID string, outMeta, inMeta map[string]string) {
		defer deps.BgWg.Done()
		defer safego.Recover(nil, "component", "teammate_message", "task_id", taskID)

		// Lock renewal heartbeat: extend task lock every 5 min to prevent
		// the ticker from recovering long-running tasks as stale.
		var lockStop func()
		if taskIDStr := inMeta[tools.MetaTeamTaskID]; taskIDStr != "" && deps.TeamStore != nil {
			teamTaskID, _ := uuid.Parse(taskIDStr)
			teamID, _ := uuid.Parse(inMeta[tools.MetaTeamID])
			lockStop = startTaskLockRenewal(ctx, deps.TeamStore, teamTaskID, teamID)
		}

		outcome := <-outCh

		// Clean up task → session tracking now that the agent has finished.
		if taskID != "" {
			deps.TaskRunSessions.Delete(taskID)
		}

		// Stop lock renewal now that the agent has finished.
		if lockStop != nil {
			lockStop()
		}

		// Auto-complete/fail the associated team task (v2 only).
		// Cache team lookup — reused later for announce routing.
		var cachedTeam *store.TeamData
		if taskIDStr := inMeta[tools.MetaTeamTaskID]; taskIDStr != "" {
			teamTaskID, _ := uuid.Parse(taskIDStr)
			teamID, _ := uuid.Parse(inMeta[tools.MetaTeamID])
			if teamTaskID != uuid.Nil {
				meta := teammateTaskMeta{
					TaskID:   teamTaskID,
					TeamID:   teamID,
					ToAgent:  inMeta[tools.MetaToAgent],
					Channel:  inMeta[tools.MetaOriginChannel],
					ChatID:   inMeta[tools.MetaOriginChatID],
					PeerKind: inMeta[tools.MetaOriginPeerKind],
				}
				cachedTeam = resolveTeamTaskOutcome(ctx, deps, outcome, taskActionFlags, meta)
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
		} else if outcome.Result == nil {
			slog.Warn("teammate message: nil result without error", "from", senderID)
			return
		} else if (outcome.Result.Content == "" && len(outcome.Result.Media) == 0) || agent.IsSilentReply(outcome.Result.Content) {
			slog.Info("teammate message: suppressed silent/empty reply", "from", senderID)
			return
		} else {
			announceContent = outcome.Result.Content
			announceMedia = outcome.Result.Media
		}

		// Append member comments & attachments so leader sees them in the announce.
		if taskIDStr := inMeta[tools.MetaTeamTaskID]; taskIDStr != "" && deps.TeamStore != nil {
			if taskUUID, err := uuid.Parse(taskIDStr); err == nil {
				if comments, err := deps.TeamStore.ListRecentTaskComments(ctx, taskUUID, 5); err == nil && len(comments) > 0 {
					var parts []string
					for _, c := range comments {
						author := c.AgentKey
						if author == "" {
							author = "system"
						}
						text := c.Content
						if len([]rune(text)) > 500 {
							text = string([]rune(text)[:500]) + "..."
						}
						parts = append(parts, fmt.Sprintf("- [%s]: %s", author, text))
					}
					announceContent += "\n\n[Member notes]\n" + strings.Join(parts, "\n")
				}
				if attachments, err := deps.TeamStore.ListTaskAttachments(ctx, taskUUID); err == nil && len(attachments) > 0 {
					announceContent += "\n\n[Attached files in team workspace]"
					for _, a := range attachments {
						announceContent += "\n- " + filepath.Base(a.Path)
					}
				}
			}
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
			if leadAg, err := deps.AgentStore.GetByID(ctx, cachedTeam.LeadAgentID); err == nil {
				leadAgent = leadAg.AgentKey
			}
		} else if teamIDStr := inMeta[tools.MetaTeamID]; teamIDStr != "" {
			if teamUUID, err := uuid.Parse(teamIDStr); err == nil {
				if team, err := deps.TeamStore.GetTeam(ctx, teamUUID); err == nil {
					if leadAg, err := deps.AgentStore.GetByID(ctx, team.LeadAgentID); err == nil {
						leadAgent = leadAg.AgentKey
					}
				}
			}
		}
		if leadAgent == "" {
			leadAgent = inMeta[tools.MetaFromAgent]
		}
		if leadAgent == "" {
			leadAgent = deps.Cfg.ResolveDefaultAgentID()
		}

		origPeerKind := inMeta[tools.MetaOriginPeerKind]
		if origPeerKind == "" {
			origPeerKind = string(sessions.PeerDirect)
		}
		origLocalKey := inMeta[tools.MetaOriginLocalKey]
		// Use exact origin session key if available (WS uses non-standard format).
		leadSessionKey := inMeta[tools.MetaOriginSessionKey]
		if leadSessionKey == "" {
			// Fallback: rebuild session key from origin metadata (works for Telegram, Discord, etc.)
			leadSessionKey = sessions.BuildScopedSessionKey(leadAgent, origCh, sessions.PeerKind(origPeerKind), origChatID)
			leadSessionKey = overrideSessionKeyFromLocalKey(leadSessionKey, origLocalKey, leadAgent, origCh, origChatID, origPeerKind)
		}

		// Extract trace context for announce linking.
		var parentTraceID, parentRootSpanID uuid.UUID
		if tid := inMeta[tools.MetaOriginTraceID]; tid != "" {
			parentTraceID, _ = uuid.Parse(tid)
		}
		if sid := inMeta[tools.MetaOriginRootSpanID]; sid != "" {
			parentRootSpanID, _ = uuid.Parse(sid)
		}

		// Cap announce content to prevent context blowup for the leader agent.
		if len([]rune(announceContent)) > 50_000 {
			announceContent = string([]rune(announceContent)[:50_000]) + "\n[truncated]"
		}

		// Enqueue result. If we become the processor, run the announce loop.
		entry := announceEntry{
			MemberAgent:       inMeta[tools.MetaToAgent],
			MemberDisplayName: inMeta[tools.MetaToAgentDisplay],
			Content:           announceContent,
			Media:             announceMedia,
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
			OriginUserID:     inMeta[tools.MetaOriginUserID],
			TeamID:           inMeta[tools.MetaTeamID],
			TeamWorkspace:    inMeta[tools.MetaTeamWorkspace],
			OriginTraceID:    inMeta[tools.MetaOriginTraceID],
			ParentTraceID:    parentTraceID,
			ParentRootSpanID: parentRootSpanID,
			OutMeta:          outMeta,
		}
		processAnnounceLoop(ctx, q, routing, deps.Sched, deps.MsgBus, deps.TeamStore, deps.PostTurn, deps.Cfg)
	}(origChannel, origChatID, msg.SenderID, taskIDStr, outMeta, msg.Metadata)

	return true
}

// handleResetCommand processes /reset command: clears session history.
// Returns true if the message was handled (caller should continue).
func handleResetCommand(
	msg bus.InboundMessage,
	deps *ConsumerDeps,
) bool {
	if msg.Metadata[tools.MetaCommand] != "reset" {
		return false
	}

	agentID := msg.AgentID
	if agentID == "" {
		agentID = resolveAgentRoute(deps.Cfg, msg.Channel, msg.ChatID, msg.PeerKind)
	}
	peerKind := msg.PeerKind
	if peerKind == "" {
		peerKind = string(sessions.PeerDirect)
	}
	sessionKey := sessions.BuildScopedSessionKey(agentID, msg.Channel, sessions.PeerKind(peerKind), msg.ChatID)
	if msg.Metadata[tools.MetaIsForum] == "true" && peerKind == string(sessions.PeerGroup) {
		var topicID int
		fmt.Sscanf(msg.Metadata[tools.MetaMessageThreadID], "%d", &topicID)
		if topicID > 0 {
			sessionKey = sessions.BuildGroupTopicSessionKey(agentID, msg.Channel, msg.ChatID, topicID)
		}
	}
	ctx := store.WithTenantID(context.Background(), msg.TenantID)
	deps.SessStore.Reset(ctx, sessionKey)
	deps.SessStore.Save(ctx, sessionKey)
	providers.ResetCLISession("", sessionKey)
	slog.Info("inbound: /reset command", "session", sessionKey)

	return true
}

// handleStopCommand processes /stop and /stopall commands: cancel active runs for a session.
// Returns true if the message was handled (caller should continue).
func handleStopCommand(
	msg bus.InboundMessage,
	deps *ConsumerDeps,
) bool {
	cmd := msg.Metadata[tools.MetaCommand]
	if cmd != "stop" && cmd != "stopall" {
		return false
	}

	agentID := msg.AgentID
	if agentID == "" {
		agentID = resolveAgentRoute(deps.Cfg, msg.Channel, msg.ChatID, msg.PeerKind)
	}
	peerKind := msg.PeerKind
	if peerKind == "" {
		peerKind = string(sessions.PeerDirect)
	}
	sessionKey := sessions.BuildScopedSessionKey(agentID, msg.Channel, sessions.PeerKind(peerKind), msg.ChatID)
	if msg.Metadata[tools.MetaIsForum] == "true" && peerKind == string(sessions.PeerGroup) {
		var topicID int
		fmt.Sscanf(msg.Metadata[tools.MetaMessageThreadID], "%d", &topicID)
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
	_ = deps.SessStore

	var cancelled bool
	if cmd == "stopall" {
		cancelled = deps.Sched.CancelSession(sessionKey)
		slog.Info("inbound: /stopall command", "session", sessionKey, "cancelled", cancelled)
	} else {
		cancelled = deps.Sched.CancelOneSession(sessionKey)
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
	deps.MsgBus.PublishOutbound(bus.OutboundMessage{
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
	allTasks, err := teamStore.ListTasks(ctx, teamID, "", store.TeamTaskFilterAll, "", "", snapshotChatID, 0, 0)
	if err != nil || len(allTasks) == 0 {
		return ""
	}

	// Filter to current batch by origin_trace_id stored in task metadata.
	var active, completed int
	var activeLines []string
	for _, t := range allTasks {
		tid, _ := t.Metadata[tools.TaskMetaOriginTrace].(string)
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
