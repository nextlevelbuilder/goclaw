package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

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
	d.wireTeamTaskAuditSubscriber()
	d.wireTeamProgressNotifySubscriber()
}

// wireTeamTaskAuditSubscriber persists team task lifecycle events to the team_task_events table.
func (d *gatewayDeps) wireTeamTaskAuditSubscriber() {
	if d.pgStores.Teams == nil {
		return
	}
	teamEventStore := d.pgStores.Teams
	d.msgBus.Subscribe(bus.TopicTeamTaskAudit, func(evt bus.Event) {
		eventType := teamTaskEventType(evt.Name)
		if eventType == "" {
			return
		}
		payload, ok := evt.Payload.(protocol.TeamTaskEventPayload)
		if !ok {
			return
		}
		taskID, err := uuid.Parse(payload.TaskID)
		if err != nil {
			return
		}

		// Propagate tenant from bus event to ensure correct tenant isolation.
		auditCtx := store.WithTenantID(context.Background(), evt.TenantID)

		// Populate data field with event-specific context for audit trail.
		var data json.RawMessage
		switch evt.Name {
		case protocol.EventTeamTaskFailed, protocol.EventTeamTaskRejected, protocol.EventTeamTaskCancelled:
			if payload.Reason != "" {
				data, _ = json.Marshal(map[string]string{"reason": payload.Reason})
			}
		case protocol.EventTeamTaskCommented:
			if payload.CommentText != "" {
				data, _ = json.Marshal(map[string]string{"comment_text": payload.CommentText})
			}
		case protocol.EventTeamTaskProgress:
			data, _ = json.Marshal(map[string]any{"progress_percent": payload.ProgressPercent, "progress_step": payload.ProgressStep})
		}

		if err := teamEventStore.RecordTaskEvent(auditCtx, &store.TeamTaskEventData{
			TaskID:    taskID,
			EventType: eventType,
			ActorType: payload.ActorType,
			ActorID:   payload.ActorID,
			Data:      data,
		}); err != nil {
			slog.Warn("team_task_audit.record_failed", "task_id", payload.TaskID, "event", eventType, "error", err)
		}
	})
	slog.Info("team task event subscriber registered")
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
	teamNotifyQueue := tools.NewTeamNotifyQueue(2000, func(items []string, meta tools.NotifyRoutingMeta) {
		content := tools.FormatBatchedNotify(items)
		if meta.Mode == "leader" {
			leaderContent := fmt.Sprintf("[Auto-status — relay to user, NO task actions]\n%s\n\nBriefly inform the user. Do NOT create, retry, reassign, or modify any tasks.", content)
			d.msgBus.TryPublishInbound(bus.InboundMessage{
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
		} else {
			d.msgBus.PublishOutbound(bus.OutboundMessage{
				Channel:  meta.Channel,
				ChatID:   meta.ChatID,
				Content:  content,
				Metadata: buildAnnounceOutMeta(meta.LocalKey),
			})
		}
	})
	d.msgBus.Subscribe("consumer.team-notify", func(evt bus.Event) {
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

		batchKey := payload.TeamID + ":" + payload.ChatID
		teamNotifyQueue.Enqueue(batchKey, content, tools.NotifyRoutingMeta{
			Mode:      teamNotifyCfg.Mode,
			Channel:   payload.Channel,
			ChatID:    payload.ChatID,
			UserID:    payload.UserID,
			LeadAgent: leadAgentKey,
			PeerKind:  payload.PeerKind,
			LocalKey:  payload.LocalKey,
		})
	})
	slog.Info("team progress notification subscriber registered")
}

// auditActivityStore returns the ActivityStore for audit persistence.
//
// d.pgStores is the unified store container produced by setupStoresAndTracing
// for every backend: "postgres", "sqlite", and "-tags sqliteonly" (desktop/Lite)
// all return the same *store.Stores, and its Activity field is set by the
// corresponding store factory (PGActivityStore or SQLiteActivityStore — see
// internal/store/sqlitestore/factory.go:67). So on desktop builds
// d.pgStores.Activity is the SQLiteActivityStore and audit rows ARE persisted
// to the local SQLite database.
func (d *gatewayDeps) auditActivityStore() store.ActivityStore {
	if d.pgStores == nil {
		return nil
	}
	return d.pgStores.Activity
}

// wireAuditSubscriber sets up the audit log subscriber that persists events to activity_logs.
// Uses a buffered channel with a single worker to avoid unbounded goroutines.
// Returns the audit channel so the shutdown goroutine can close it to flush pending entries.
func (d *gatewayDeps) wireAuditSubscriber() chan bus.AuditEventPayload {
	activityStore := d.auditActivityStore()
	if activityStore == nil {
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
			if err := activityStore.Log(auditCtx, &store.ActivityLog{
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

// wireAuditRetentionSweep starts a daily goroutine that deletes audit rows
// older than the configured retention window. Disabled when RetentionDays <= 0
// (the default) — 0 means "keep forever". Mirrors the workstation activity sink
// retention pattern (24h ticker + stop channel).
func (d *gatewayDeps) wireAuditRetentionSweep() {
	if d.cfg == nil || d.cfg.Audit.RetentionDays <= 0 {
		return
	}
	activityStore := d.auditActivityStore()
	if activityStore == nil {
		return
	}
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				before := time.Now().Add(-time.Duration(d.cfg.Audit.RetentionDays) * 24 * time.Hour)
				n, err := activityStore.Prune(context.Background(), before)
				if err != nil {
					slog.Warn("audit.prune_error", "error", err)
				} else if n > 0 {
					slog.Info("audit.pruned", "rows", n, "before", before.Format(time.RFC3339))
				}
			case <-stopCh:
				return
			}
		}
	}()
	d.auditRetentionStop = func() { close(stopCh) }
	slog.Info("audit retention sweep registered", "retention_days", d.cfg.Audit.RetentionDays)
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

// teamTaskEventType maps bus event names to team_task_events.event_type values.
// Returns empty string for non-task events (caller should skip).
func teamTaskEventType(eventName string) string {
	switch eventName {
	case protocol.EventTeamTaskCreated:
		return "created"
	case protocol.EventTeamTaskClaimed:
		return "claimed"
	case protocol.EventTeamTaskAssigned:
		return "assigned"
	case protocol.EventTeamTaskDispatched:
		return "dispatched"
	case protocol.EventTeamTaskCompleted:
		return "completed"
	case protocol.EventTeamTaskFailed:
		return "failed"
	case protocol.EventTeamTaskCancelled:
		return "cancelled"
	case protocol.EventTeamTaskReviewed:
		return "reviewed"
	case protocol.EventTeamTaskApproved:
		return "approved"
	case protocol.EventTeamTaskRejected:
		return "rejected"
	case protocol.EventTeamTaskCommented:
		return "commented"
	case protocol.EventTeamTaskProgress:
		return "progress"
	case protocol.EventTeamTaskUpdated:
		return "updated"
	case protocol.EventTeamTaskStale:
		return "stale"
	default:
		return ""
	}
}
