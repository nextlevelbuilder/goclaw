package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ChannelSender posts a message to a named channel + chat. Implemented by
// channels.Manager.SendToChannel — kept as a small interface here so this
// package doesn't pull the whole channel manager, and tests can pass a
// fake sender.
type ChannelSender interface {
	SendToChannel(ctx context.Context, channelName, chatID, content string) error
}

// SubagentInterruptedMessage is the recovery notice we post into the
// origin chat when a previously-running subagent is found at boot.
//
// The message is intentionally short and human-grade — the recipient may
// be a Discord channel, a thread, a Telegram chat, or DM. We don't know
// which forge phase was running (recovery doesn't currently parse the
// task subject), so the wording stays generic and points the user at
// re-asking. A future PR can use task metadata to auto-resume idempotent
// phases (autoplan/review/qa) without prompting; today we punt to the
// human, which is the safest default.
const SubagentInterruptedMessage = "🔁 The agent pod restarted while a job was running and lost in-flight context. Please re-send the request — sorry for the noise."

// RecoverInterruptedSubagents scans for subagent tasks that were left in
// the `running` state by a previous goclaw process and:
//
//  1. Marks each row as `interrupted` in the store so the live
//     SubagentManager doesn't double-account them.
//  2. Best-effort posts a recovery notice to the originating chat (Discord
//     channel, Telegram chat, etc.) so the user knows their request died
//     and won't get a stale completion later.
//
// Both steps are best-effort: a single bad row (channel offline, malformed
// origin fields, transient DB error) must not block the rest of the boot
// sequence. We log every failure with enough detail to investigate
// post-hoc but never propagate the error up.
//
// Boot ordering is the load-bearing detail. Call this AFTER the channel
// manager and stores are wired but BEFORE we start consuming inbound
// messages — otherwise a fresh inbound message might race with the
// recovery post and the user sees the recovery notice AFTER their next
// reply, which is confusing.
//
// The function takes its own context so callers can put a deadline on
// recovery (don't block boot for >N seconds if the channel API is slow).
func RecoverInterruptedSubagents(
	ctx context.Context,
	taskStore store.SubagentTaskStore,
	sender ChannelSender,
	limit int,
) {
	if taskStore == nil {
		slog.Warn("subagent_recovery.skip", "reason", "no_task_store")
		return
	}

	tasks, err := taskStore.ListRunningAcrossTenants(ctx, limit)
	if err != nil {
		slog.Warn("subagent_recovery.list_failed", "error", err)
		return
	}
	if len(tasks) == 0 {
		slog.Info("subagent_recovery.clean", "msg", "no interrupted subagents found")
		return
	}

	slog.Info("subagent_recovery.start",
		"count", len(tasks),
		"oldest_started_at", tasks[0].CreatedAt.Format(time.RFC3339))

	var (
		marked int
		posted int
	)
	for i := range tasks {
		t := &tasks[i]

		// Step 1: mark interrupted. We use UpdateStatus so the store's
		// existing tenant guards apply (the store has the tenant from
		// the row). Pass a result message that captures why so an
		// operator inspecting the row later sees the recovery context.
		// We need a tenant-scoped ctx for the update call — synthesize
		// one from the row itself.
		tenantCtx := store.WithTenantID(ctx, t.TenantID)
		reason := "interrupted by pod restart"
		if uerr := taskStore.UpdateStatus(tenantCtx, t.ID, "interrupted", &reason, t.Iterations, t.InputTokens, t.OutputTokens); uerr != nil {
			slog.Warn("subagent_recovery.mark_failed",
				"id", t.ID,
				"tenant_id", t.TenantID,
				"error", uerr)
			// keep going — the post-message step is independently
			// useful even if the DB write failed transiently.
		} else {
			marked++
		}

		// Step 2: post recovery notice to origin chat. We need all three
		// of channel name, chat id, and a sender — if any is missing,
		// skip the post but still log so we can find these rows later.
		if sender == nil {
			continue
		}
		ch := derefStr(t.OriginChannel)
		chat := derefStr(t.OriginChatID)
		if ch == "" || chat == "" {
			slog.Info("subagent_recovery.no_origin",
				"id", t.ID,
				"reason", "missing OriginChannel or OriginChatID — silent recovery")
			continue
		}

		// Use a per-post timeout so a stuck channel API can't stall
		// recovery for the whole list. 5s is more than enough for a
		// single SendMessage / Discord webhook post.
		postCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := sender.SendToChannel(postCtx, ch, chat, SubagentInterruptedMessage)
		cancel()
		if err != nil {
			slog.Warn("subagent_recovery.post_failed",
				"id", t.ID,
				"channel", ch,
				"chat_id", chat,
				"error", err)
			continue
		}
		posted++
	}

	slog.Info("subagent_recovery.done",
		"total", len(tasks),
		"marked_interrupted", marked,
		"posted_notices", posted)
}

// derefStr returns "" for nil — keeps the caller readable.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
