package cmd

import (
	"context"
	"encoding/json"
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

	// Inbound debounce: merge rapid messages from the same sender before processing.
	// Matching TS createInboundDebouncer from src/auto-reply/inbound-debounce.ts.
	debounceMs := cfg.Gateway.InboundDebounceMs
	if debounceMs == 0 {
		debounceMs = 1000 // default: 1000ms
	}
	debouncer := bus.NewInboundDebouncer(
		time.Duration(debounceMs)*time.Millisecond,
		func(msg bus.InboundMessage) {
			processNormalMessage(ctx, msg, agents, cfg, sched, channelMgr, teamStore, quotaChecker, sessStore, agentStore, contactCollector, postTurn, msgBus)
		},
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

		if handleSubagentAnnounce(ctx, msg, cfg, sched, channelMgr, msgBus, getAnnounceMu) {
			continue
		}
		if handleTeammateMessage(ctx, msg, cfg, sched, channelMgr, teamStore, agentStore, msgBus, postTurn, &taskRunSessions) {
			continue
		}
		if handleResetCommand(msg, cfg, sessStore) {
			continue
		}
		if handleStopCommand(msg, cfg, sched, sessStore, msgBus) {
			continue
		}

		// Blocker escalation messages bypass debounce — deliver immediately to leader.
		if msg.SenderID == "system:escalation" {
			go processNormalMessage(ctx, msg, agents, cfg, sched, channelMgr, teamStore, quotaChecker, sessStore, agentStore, contactCollector, postTurn, msgBus)
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
	// Caller (processNormalMessage) already injected tenant_id into ctx.
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
