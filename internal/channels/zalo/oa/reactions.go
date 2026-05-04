package oa

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	reactionDebounceMs = 700 * time.Millisecond
	// Late stale events within this window hit the terminal rc and short-circuit
	// instead of LoadOrStore-ing a fresh controller that would stomp the heart.
	reactionTombstoneTTL          = 60 * time.Second
	defaultReactionTerminalMinMs  = 800 * time.Millisecond
	defaultReactionTerminalMaxMs  = 2000 * time.Millisecond
	reactionLengthBonusPerCharMs  = 1 * time.Millisecond
	reactionLengthBonusCap        = 1500 * time.Millisecond
)

// Tone tuned for OA's B2C surface: one "received, working" ack on the
// first intermediate event plus a warm/sad terminal. tool/coding/web are
// intentionally NOT mapped — chatty mid-run reactions look unprofessional
// in customer chats and burn through the 50-per-message cap.
//
// Angry (`:-h`) is intentionally excluded — dropping an angry face on the
// customer's own message reads as blaming them, even on agent-side errors.
var statusReactionVariants = map[string][]string{
	"thinking": {reactionIconLike, reactionIconHeart},
	"done":     {reactionIconHeart, reactionIconLike},
	"error":    {reactionIconWorry, reactionIconCry},
}

func resolveReactionEmoji(status string) string {
	variants, ok := statusReactionVariants[status]
	if !ok {
		return ""
	}
	for _, v := range variants {
		if zaloSupportedReactions[v] {
			return v
		}
	}
	return ""
}

type zaloReactionController struct {
	ch              *Channel
	userID          string
	sourceMessageID string

	mu            sync.Mutex
	currentIcon   string
	lastStatus    string
	terminal      bool
	debounceTimer *time.Timer
	tombstoneOnce sync.Once
}

func newZaloReactionController(ch *Channel, userID, sourceMessageID string) *zaloReactionController {
	return &zaloReactionController{
		ch:              ch,
		userID:          userID,
		sourceMessageID: sourceMessageID,
	}
}

func (rc *zaloReactionController) SetStatus(ctx context.Context, status string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.terminal {
		return
	}
	rc.lastStatus = status

	if status == "done" || status == "error" {
		rc.terminal = true
		rc.cancelDebounceLocked()
		icon := resolveReactionEmoji(status)
		if icon == "" {
			return
		}
		select {
		case <-rc.ch.stopCh:
			return
		default:
		}
		rc.ch.reactionWG.Add(1)
		rc.debounceTimer = time.AfterFunc(rc.ch.terminalReactionDelay(rc.userID), func() {
			defer rc.ch.reactionWG.Done()
			rc.mu.Lock()
			defer rc.mu.Unlock()
			rc.applyReactionLocked(rc.ch.reactionCtx, icon)
		})
		return
	}

	if _, mapped := statusReactionVariants[status]; !mapped {
		return
	}

	rc.cancelDebounceLocked()
	// Re-check stopCh under lock: wg.Add after Stop's Wait would panic.
	select {
	case <-rc.ch.stopCh:
		return
	default:
	}
	rc.ch.reactionWG.Add(1)
	rc.debounceTimer = time.AfterFunc(reactionDebounceMs, func() {
		defer rc.ch.reactionWG.Done()
		rc.mu.Lock()
		defer rc.mu.Unlock()
		if rc.terminal {
			return
		}
		if icon := resolveReactionEmoji(rc.lastStatus); icon != "" {
			// Stop-aware ctx so Channel.Stop can drain in-flight HTTP calls.
			rc.applyReactionLocked(rc.ch.reactionCtx, icon)
		}
	})
}

func (rc *zaloReactionController) Stop() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.cancelDebounceLocked()
}

func (rc *zaloReactionController) cancelDebounceLocked() {
	if rc.debounceTimer != nil {
		// If Stop returns true the closure won't run; balance the Add.
		if rc.debounceTimer.Stop() {
			rc.ch.reactionWG.Done()
		}
		rc.debounceTimer = nil
	}
}

// applyReactionLocked: caller MUST hold rc.mu. On error, leaves currentIcon
// unset so the next transition retries. Never flips channel health.
func (rc *zaloReactionController) applyReactionLocked(ctx context.Context, icon string) {
	if icon == rc.currentIcon {
		return
	}
	if _, err := rc.ch.SendReaction(ctx, rc.userID, rc.sourceMessageID, icon); err != nil {
		slog.Debug("zalo_oa.reaction.set_failed",
			"user_id", rc.userID,
			"source_message_id", rc.sourceMessageID,
			"icon", icon,
			"error", err)
		return
	}
	rc.currentIcon = icon
}

func (c *Channel) terminalReactionDelay(chatID string) time.Duration {
	minD := defaultReactionTerminalMinMs
	maxD := defaultReactionTerminalMaxMs
	if c.cfg.ReactionTerminalDelayMinMs > 0 {
		minD = time.Duration(c.cfg.ReactionTerminalDelayMinMs) * time.Millisecond
	}
	if c.cfg.ReactionTerminalDelayMaxMs > 0 {
		maxD = time.Duration(c.cfg.ReactionTerminalDelayMaxMs) * time.Millisecond
	}
	if maxD < minD {
		maxD = minD
	}
	d := minD
	if maxD > minD {
		d += time.Duration(rand.Int64N(int64(maxD-minD) + 1))
	}
	if v, ok := c.lastReplyChars.Load(chatID); ok {
		if n, ok := v.(int); ok && n > 0 {
			bonus := time.Duration(n) * reactionLengthBonusPerCharMs
			if bonus > reactionLengthBonusCap {
				bonus = reactionLengthBonusCap
			}
			d += bonus
		}
	}
	return d
}

func (c *Channel) recordReplyLen(chatID string, n int) {
	if chatID == "" || n <= 0 {
		return
	}
	c.lastReplyChars.Store(chatID, n)
}

// chatID for Zalo OA is the user_id (1:1 DM), so it doubles as recipient.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID, messageID, status string) error {
	if c.cfg.ReactionLevel == "" || c.cfg.ReactionLevel == "off" {
		return nil
	}
	if c.cfg.ReactionLevel == "minimal" && status != "done" && status != "error" {
		return nil
	}
	if chatID == "" || messageID == "" {
		return nil
	}
	// Webhook entry is fenced by router drain; event-bus entry isn't, so
	// reactionWG.Add can race past Stop()'s Wait without this gate.
	select {
	case <-c.stopCh:
		return nil
	default:
	}

	key := chatID + ":" + messageID
	val, _ := c.reactions.LoadOrStore(key, newZaloReactionController(c, chatID, messageID))
	rc, ok := val.(*zaloReactionController)
	if !ok {
		return nil
	}
	rc.SetStatus(ctx, status)

	if status == "done" || status == "error" {
		// One tombstone per controller — duplicate terminal events used to
		// each spawn a fresh 60s goroutine.
		rc.tombstoneOnce.Do(func() {
			// Re-check stopCh inside Once: Stop() may have closed it
			// between the entry gate and Add — Add after Wait panics.
			select {
			case <-c.stopCh:
				return
			default:
			}
			c.reactionWG.Add(1)
			go func() {
				defer c.reactionWG.Done()
				t := time.NewTimer(reactionTombstoneTTL)
				defer t.Stop()
				select {
				case <-t.C:
					c.reactions.CompareAndDelete(key, rc)
				case <-c.stopCh:
				}
			}()
		})
	}
	return nil
}

func (c *Channel) ClearReaction(ctx context.Context, chatID, messageID string) error {
	if chatID == "" || messageID == "" {
		return nil
	}
	key := chatID + ":" + messageID
	if val, ok := c.reactions.LoadAndDelete(key); ok {
		if rc, ok := val.(*zaloReactionController); ok {
			rc.Stop()
		}
	}
	return c.SendClearReaction(ctx, chatID, messageID)
}
