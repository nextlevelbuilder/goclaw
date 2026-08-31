package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// wireEventSubscribers registers team task audit and team progress notification subscribers on the message bus.
// Must be called after pgStores and msgBus are initialized.
func (d *gatewayDeps) wireEventSubscribers() {
	d.wireTeamProgressNotifySubscriber()
}

// wireTeamProgressNotifySubscriber forwards task events to chat channels.
// Reads team.settings.notifications config; direct mode sends outbound, leader mode
// injects into leader agent session. Notifications are batched per chat
// with 2s debounce to avoid spamming users when multiple tasks dispatch at once.
func (d *gatewayDeps) wireTeamProgressNotifySubscriber() {
	if d.pgStores.Teams == nil {
		return
	}
	notifyTeamStore := d.pgStores.Teams
	notifyAgentStore := d.pgStores.Agents
	teamNotifyQueue := tools.NewTeamNotifyQueue(2000, d.drainTeamProgressNotify)
	deliverNotification := func(evt bus.Event) {
		payload, ok := evt.Payload.(protocol.TeamTaskEventPayload)
		if !ok || payload.TeamID == "" || payload.Channel == "" {
			return
		}
		var notifyType string
		switch evt.Name {
		case protocol.EventTeamTaskDispatched:
			notifyType = "dispatched"
		case protocol.EventTeamTaskAssigned:
			notifyType = "dispatched" // same config flag — human assign also notifies
		case protocol.EventTeamTaskFailed:
			notifyType = "failed"
		case protocol.EventTeamTaskProgress:
			notifyType = "progress"
		case protocol.EventTeamTaskCompleted:
			notifyType = "completed"
		case protocol.EventTeamTaskCommented:
			notifyType = "commented"
		case protocol.EventTeamTaskCreated:
			// Only notify for human-created tasks (agent-created go through dispatch).
			if payload.ActorType != "human" {
				return
			}
			notifyType = "new_task"
		default:
			return
		}

		teamUUID, err := uuid.Parse(payload.TeamID)
		if err != nil {
			return
		}
		team, err := notifyTeamStore.GetTeamUnscoped(context.Background(), teamUUID)
		if err != nil || team == nil {
			return
		}
		teamNotifyCfg := tools.ParseTeamNotifyConfig(team.Settings)

		// Check if this notification type is enabled.
		switch notifyType {
		case "dispatched":
			if !teamNotifyCfg.Dispatched {
				return
			}
		case "failed":
			if !teamNotifyCfg.Failed {
				return
			}
		case "progress":
			if !teamNotifyCfg.Progress {
				return
			}
		case "completed":
			if !teamNotifyCfg.Completed {
				return
			}
		case "commented":
			if !teamNotifyCfg.Commented {
				return
			}
		case "new_task":
			if !teamNotifyCfg.NewTask {
				return
			}
		}

		// Skip internal channels.
		if payload.Channel == tools.ChannelSystem || payload.Channel == tools.ChannelTeammate {
			return
		}

		// Resolve lead agent key (needed for leader mode routing + completed-by-leader skip).
		var leadAgentKey string
		if notifyAgentStore != nil {
			if la, err := notifyAgentStore.GetByIDUnscoped(context.Background(), team.LeadAgentID); err == nil {
				leadAgentKey = la.AgentKey
			}
		}

		// Skip completed notification if task was completed by the leader
		// (leader is already talking to the user, notification would be redundant).
		if notifyType == "completed" && payload.OwnerAgentKey == leadAgentKey {
			return
		}

		// Build notification message.
		var content string
		agentName := payload.OwnerAgentKey
		if payload.OwnerDisplayName != "" {
			agentName = payload.OwnerDisplayName
		}
		switch evt.Name {
		case protocol.EventTeamTaskDispatched:
			if payload.ActorID == "dispatch_unblocked" {
				content = fmt.Sprintf("▶️ Task #%d \"%s\" → unblocked, dispatched to %s", payload.TaskNumber, payload.Subject, agentName)
			} else {
				content = fmt.Sprintf("📋 Task #%d \"%s\" → dispatched to %s", payload.TaskNumber, payload.Subject, agentName)
			}
		case protocol.EventTeamTaskAssigned:
			content = fmt.Sprintf("📋 Task #%d \"%s\" → assigned to %s", payload.TaskNumber, payload.Subject, agentName)
		case protocol.EventTeamTaskCompleted:
			content = fmt.Sprintf("✅ Task #%d \"%s\" completed", payload.TaskNumber, payload.Subject)
		case protocol.EventTeamTaskProgress:
			if payload.ProgressStep != "" {
				content = fmt.Sprintf("⏳ Task #%d \"%s\": %d%% — %s", payload.TaskNumber, payload.Subject, payload.ProgressPercent, payload.ProgressStep)
			} else {
				content = fmt.Sprintf("⏳ Task #%d \"%s\": %d%%", payload.TaskNumber, payload.Subject, payload.ProgressPercent)
			}
		case protocol.EventTeamTaskFailed:
			reason := payload.Reason
			if len(reason) > 200 {
				reason = reason[:200] + "..."
			}
			content = fmt.Sprintf("❌ Task #%d \"%s\" failed: %s", payload.TaskNumber, payload.Subject, reason)
		case protocol.EventTeamTaskCommented:
			actor := payload.ActorID
			if actor == "" {
				actor = "unknown"
			}
			content = fmt.Sprintf("💬 Task #%d \"%s\": comment from %s", payload.TaskNumber, payload.Subject, actor)
		case protocol.EventTeamTaskCreated:
			content = fmt.Sprintf("📋 New task #%d \"%s\" created", payload.TaskNumber, payload.Subject)
		}

		// In leader mode, require resolved agent key for routing.
		if teamNotifyCfg.Mode == "leader" && leadAgentKey == "" {
			return
		}

		teamNotifyQueue.Enqueue(content, tools.NotifyRoutingMeta{
			TenantID:  evt.TenantID,
			TeamID:    payload.TeamID,
			Mode:      teamNotifyCfg.Mode,
			Channel:   payload.Channel,
			ChatID:    payload.ChatID,
			UserID:    payload.UserID,
			LeadAgent: leadAgentKey,
			PeerKind:  payload.PeerKind,
			LocalKey:  payload.LocalKey,
		})
	}
	d.msgBus.Subscribe("consumer.team-notify", func(evt bus.Event) {
		if isUserFacingTaskNotificationEvent(evt.Name) {
			deliverNotification(evt)
		}
	})
	slog.Info("team progress notification subscriber registered")
}

// drainTeamProgressNotify is the TeamNotifyQueue drain callback. In leader mode it
// injects a batched auto-status hint into the lead agent's inbound session; in
// direct mode it publishes the batched content outbound. Leader-mode injection is
// best-effort: when TryPublishInbound rejects the message (buffer full / bus
// shutting down) the batch is dropped and surfaced via slog.Warn so operators can
// see leader relays going missing. Durable workflow escalation follows the separate
// BlockWorkflowTaskAttempt path, not this status hint.
func (d *gatewayDeps) drainTeamProgressNotify(items []string, meta tools.NotifyRoutingMeta) {
	content := tools.FormatBatchedNotify(items)
	if meta.Mode == "leader" {
		leaderContent := fmt.Sprintf("[Auto-status — relay to user, NO task actions]\n%s\n\nBriefly inform the user. Do NOT create, retry, reassign, or modify any tasks.", content)
		ok := d.msgBus.TryPublishInbound(bus.InboundMessage{
			TenantID: meta.TenantID,
			Channel:  meta.Channel,
			SenderID: "notification:progress",
			ChatID:   meta.ChatID,
			AgentID:  meta.LeadAgent,
			UserID:   meta.UserID,
			PeerKind: meta.PeerKind,
			Content:  leaderContent,
			Metadata: func() map[string]string {
				m := map[string]string{"run_kind": tools.RunKindNotification}
				if meta.LocalKey != "" {
					m["local_key"] = meta.LocalKey
				}
				return m
			}(),
		})
		if !ok {
			slog.Warn("team progress leader inject dropped",
				"tenant_id", meta.TenantID.String(),
				"team_id", meta.TeamID,
				"lead_agent", meta.LeadAgent,
				"channel", meta.Channel,
				"peer_kind", meta.PeerKind,
				"chat_id", meta.ChatID,
				"local_key", meta.LocalKey,
				"batch_size", len(items),
			)
		}
		return
	}
	d.msgBus.PublishOutbound(bus.OutboundMessage{
		Channel:  meta.Channel,
		ChatID:   meta.ChatID,
		Content:  content,
		Metadata: buildAnnounceOutMeta(meta.LocalKey),
	})
}

func isUserFacingTaskNotificationEvent(eventName string) bool {
	switch eventName {
	case protocol.EventTeamTaskDispatched,
		protocol.EventTeamTaskAssigned,
		protocol.EventTeamTaskProgress,
		protocol.EventTeamTaskCompleted,
		protocol.EventTeamTaskFailed,
		protocol.EventTeamTaskCommented,
		protocol.EventTeamTaskCreated:
		return true
	default:
		return false
	}
}

// wireAuditSubscriber sets up the audit log subscriber that persists events to activity_logs.
// Uses a buffered channel with a single worker to avoid unbounded goroutines.
// Returns the audit channel so the shutdown goroutine can close it to flush pending entries.
func (d *gatewayDeps) wireAuditSubscriber() chan bus.AuditEventPayload {
	if d.pgStores.Activity == nil {
		return nil
	}
	auditCh := make(chan bus.AuditEventPayload, 256)
	d.msgBus.Subscribe(bus.TopicAudit, func(evt bus.Event) {
		if evt.Name != protocol.EventAuditLog {
			return
		}
		payload, ok := evt.Payload.(bus.AuditEventPayload)
		if !ok {
			return
		}
		select {
		case auditCh <- payload:
		default:
			slog.Warn("audit.queue_full", "action", payload.Action)
		}
	})
	go func() {
		for payload := range auditCh {
			auditCtx := store.WithTenantID(context.Background(), payload.TenantID)
			if err := d.pgStores.Activity.Log(auditCtx, &store.ActivityLog{
				ActorType:  payload.ActorType,
				ActorID:    payload.ActorID,
				Action:     payload.Action,
				EntityType: payload.EntityType,
				EntityID:   payload.EntityID,
				IPAddress:  payload.IPAddress,
				Details:    payload.Details,
			}); err != nil {
				slog.Warn("audit.log_failed", "action", payload.Action, "error", err)
			}
		}
	}()
	slog.Info("audit subscriber registered")
	return auditCh
}

// wireChannelStreamingSubscriber subscribes to agent events for channel streaming/reaction forwarding.
// Events emitted by agent loops are broadcast to the bus; we forward them to the channel manager
// which routes to StreamingChannel/ReactionChannel. Also updates the Router activity registry.
func (d *gatewayDeps) wireChannelStreamingSubscriber() {
	d.msgBus.Subscribe(bus.TopicChannelStreaming, func(event bus.Event) {
		if event.Name != protocol.EventAgent {
			return
		}
		agentEvent, ok := event.Payload.(agent.AgentEvent)
		if !ok {
			return
		}
		d.channelMgr.HandleAgentEvent(agentEvent.Type, agentEvent.RunID, agentEvent.Payload)

		// Route activity events to Router (status registry) and DelegateManager (progress tracking).
		if agentEvent.Type == protocol.AgentEventActivity {
			payloadMap, _ := agentEvent.Payload.(map[string]any)
			phase, _ := payloadMap["phase"].(string)
			tool, _ := payloadMap["tool"].(string)
			iteration := 0
			if v, ok := payloadMap["iteration"].(int); ok {
				iteration = v
			}
			if sessionKey := d.agentRouter.SessionKeyForRun(agentEvent.RunID); sessionKey != "" {
				d.agentRouter.UpdateActivity(sessionKey, agentEvent.RunID, phase, tool, iteration)
			}
		}

		// Clear activity on terminal events
		if agentEvent.Type == protocol.AgentEventRunCompleted ||
			agentEvent.Type == protocol.AgentEventRunFailed ||
			agentEvent.Type == protocol.AgentEventRunCancelled {
			if sessionKey := d.agentRouter.SessionKeyForRun(agentEvent.RunID); sessionKey != "" {
				d.agentRouter.ClearActivity(sessionKey)
			}
		}
	})
}
