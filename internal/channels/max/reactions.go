package max

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// reactionRefreshInterval is how often we re-send the typing action to keep
// the indicator visible. Max docs do not specify the action TTL, but typing
// indicators in messengers commonly expire after ~5 seconds. We refresh at
// 4s to stay below the threshold without spamming the API.
const reactionRefreshInterval = 4 * time.Second

// reactionStatus maps goclaw agent status strings to Max bot action verbs.
//
// goclaw statuses (from internal/agent and internal/channels/events):
//   - "thinking"   → bot is generating a response
//   - "tool_exec"  → bot is calling a tool (browser, search, etc.)
//   - "compacting" → context window is being compressed
//   - "done"       → run completed successfully
//   - "error"      → run failed
//   - "stall"      → no progress for a while
//
// Max actions (POST /chats/{id}/actions):
//   - "typing_on"      → typing indicator
//   - "sending_photo"  → "sending photo" status
//   - "sending_video"  → "sending video" status
//   - "sending_audio"  → "sending audio" status
//   - "sending_file"   → "sending file" status
//   - "mark_seen"      → marks chat as read
//
// Mapping: in-flight statuses → typing_on (indicates work is happening),
// terminal statuses (done/error) → no action (the actual reply will arrive
// shortly via Send()).
func reactionAction(status string) string {
	switch status {
	case "thinking", "tool_exec", "compacting", "stall":
		return "typing_on"
	case "done", "error":
		return ""
	}
	return ""
}

// OnReactionEvent updates the typing indicator in the chat to reflect the
// current agent status. Implements channels.ReactionChannel.
//
// chatID is the bus chat identifier (numeric string for Max — DM thread or
// group ID). messageID is the original user message that triggered the run;
// not used by Max (no per-message reactions in the API), but kept for
// interface compatibility.
//
// Behavior:
//   - In-flight statuses spawn a refresh goroutine that re-sends typing_on
//     every reactionRefreshInterval until ClearReaction is called or the
//     status changes to a terminal one.
//   - Terminal statuses (done/error) stop the refresher; the actual reply
//     will arrive via Send() shortly.
//
// Concurrency: refreshers are tracked per chatID. Spawning a new one for the
// same chat replaces the previous (e.g. status changes from thinking →
// tool_exec); only one refresher runs at a time per chat.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID string, messageID string, status string) error {
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil || chatIDInt == 0 {
		// Not a Max chat ID; silently no-op rather than error — interfaces
		// may pass through events for any channel.
		return nil
	}

	action := reactionAction(status)
	if action == "" {
		// Terminal or unknown status: stop any active refresher.
		c.stopReactionRefresher(chatID)
		return nil
	}

	// Send immediately so the user sees the indicator without delay.
	if err := c.client.PostAction(ctx, chatIDInt, action); err != nil {
		// Don't surface errors aggressively — typing is best-effort UX.
		slog.Debug("max: post action failed (non-fatal)",
			"channel", c.Name(), "chat_id", chatID, "action", action, "error", err)
	}

	// Spawn or replace refresher.
	c.startReactionRefresher(chatID, chatIDInt, action)
	return nil
}

// ClearReaction stops the typing indicator for the given chat.
// Implements channels.ReactionChannel.
//
// Max has no explicit "clear typing" action; the indicator expires on its
// own a few seconds after the last typing_on. We just stop refreshing.
func (c *Channel) ClearReaction(ctx context.Context, chatID string, messageID string) error {
	c.stopReactionRefresher(chatID)
	return nil
}

// startReactionRefresher launches (or replaces) a goroutine that re-sends
// the action every reactionRefreshInterval. Uses a per-chat cancel/done pair
// stored in the channel's reactionRefreshers map.
func (c *Channel) startReactionRefresher(chatIDStr string, chatIDInt int64, action string) {
	// Stop any prior refresher for this chat.
	c.stopReactionRefresher(chatIDStr)

	ctx, cancel := context.WithCancel(c.pollContext())
	rr := &reactionRefresher{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	c.reactionRefreshers.Store(chatIDStr, rr)

	go c.runReactionRefresher(ctx, rr, chatIDInt, action)
}

// runReactionRefresher is the goroutine body — refreshes typing every
// reactionRefreshInterval until ctx is cancelled.
func (c *Channel) runReactionRefresher(
	ctx context.Context,
	rr *reactionRefresher,
	chatIDInt int64,
	action string,
) {
	defer close(rr.done)
	t := time.NewTicker(reactionRefreshInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.client.PostAction(ctx, chatIDInt, action); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				slog.Debug("max: action refresh failed (non-fatal)",
					"channel", c.Name(),
					"chat_id", chatIDInt,
					"action", action,
					"error", err)
			}
		}
	}
}

// stopReactionRefresher cancels and waits for the refresher (if any) for
// the given chat. No-op if no refresher exists.
//
// Bounded wait — if the refresher doesn't exit within 1s we abandon it.
// This shouldn't happen in practice but defends against goroutine leaks
// in failure modes.
func (c *Channel) stopReactionRefresher(chatIDStr string) {
	v, ok := c.reactionRefreshers.LoadAndDelete(chatIDStr)
	if !ok {
		return
	}
	rr, ok := v.(*reactionRefresher)
	if !ok {
		return
	}
	rr.cancel()
	select {
	case <-rr.done:
	case <-time.After(1 * time.Second):
		slog.Warn("max: reaction refresher did not exit promptly",
			"channel", c.Name(), "chat_id", chatIDStr)
	}
}

// reactionRefresher tracks an active typing-action goroutine for one chat.
type reactionRefresher struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// stopAllReactionRefreshers cancels every active refresher. Called from
// Channel.Stop to prevent goroutine leaks on shutdown.
func (c *Channel) stopAllReactionRefreshers() {
	var wg sync.WaitGroup
	c.reactionRefreshers.Range(func(k, v any) bool {
		rr, ok := v.(*reactionRefresher)
		if !ok {
			return true
		}
		c.reactionRefreshers.Delete(k)
		rr.cancel()
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-rr.done:
			case <-time.After(1 * time.Second):
			}
		}()
		return true
	})
	wg.Wait()
}
