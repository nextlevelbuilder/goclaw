package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// triggerWordsTTL bounds how long parsed IDENTITY.md trigger words are cached
// before a re-read, so edits take effect without a restart.
const triggerWordsTTL = 60 * time.Second

// matchesTriggerWords reports whether the message names the bot by one of its
// agent's IDENTITY.md trigger-word aliases, in either the text or the media
// caption. Group-only gate; fails open (no match) when none are configured or
// the lookup errors — it never wakes spuriously and never crashes the handler.
func (c *Channel) matchesTriggerWords(ctx context.Context, msg *telego.Message) bool {
	if msg == nil {
		return false
	}
	chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)
	isGroup := msg.Chat.Type == "group" || msg.Chat.Type == "supergroup"
	set := c.agentTriggerWords(ctx, c.effectiveInboundAgent(ctx, chatIDStr, msg.MessageThreadID, isGroup))
	if len(set) == 0 {
		return false
	}
	return textHasWakeWord(msg.Text, set) || textHasWakeWord(msg.Caption, set)
}

// triggerWordsEntry caches one agent's normalized trigger-word set.
type triggerWordsEntry struct {
	words map[string]struct{}
	at    time.Time
}

// agentTriggerWords returns the normalized trigger-word set for the given agent
// (the group/topic-bound agent, or the channel default), parsed from its
// IDENTITY.md context file and cached per agent_key for triggerWordsTTL. One
// channel instance can serve multiple agents via group/topic bindings, so the
// cache is keyed by agent rather than holding a single set.
func (c *Channel) agentTriggerWords(ctx context.Context, ref boundAgentRef) map[string]struct{} {
	c.triggerMu.Lock()
	defer c.triggerMu.Unlock()

	if c.triggerWords == nil {
		c.triggerWords = make(map[string]triggerWordsEntry)
	}
	entry := c.triggerWords[ref.key]
	if time.Since(entry.at) < triggerWordsTTL {
		return entry.words
	}
	// Refresh timestamp up front: on error we keep the stale set but avoid
	// hammering the store on every message.
	entry.at = time.Now()
	defer func() { c.triggerWords[ref.key] = entry }()

	if ref.key == "" || c.agentStore == nil {
		entry.words = nil
		return nil
	}
	// The agent + context-file stores are tenant-scoped: without tenant in ctx
	// they error with "tenant_id required" and trigger words would silently never
	// load. Inject scope up front for both lookups.
	ctx = store.WithTenantID(ctx, c.TenantID())
	agentID := ref.id
	if agentID == uuid.Nil {
		var err error
		agentID, err = c.resolveAgentUUIDForKey(ctx, ref.key)
		if err != nil {
			slog.Debug("telegram: trigger words — resolve agent failed",
				"channel", c.Name(), "agent", ref.key, "error", err)
			return entry.words
		}
	}
	files, err := c.agentStore.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		slog.Debug("telegram: trigger words — load context files failed", "channel", c.Name(), "error", err)
		return entry.words
	}
	for _, f := range files {
		if f.FileName == bootstrap.IdentityFile {
			entry.words = normalizeWakeWords(bootstrap.ParseTriggerWords(f.Content))
			return entry.words
		}
	}
	entry.words = nil
	return nil
}

// normalizeWakeWords lowercases and trims the configured wake-words into a
// lookup set, dropping blanks. Matching is whole-word and case-insensitive.
func normalizeWakeWords(words []string) map[string]struct{} {
	if len(words) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		set[w] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// textHasWakeWord reports whether text contains any wake-word as a whole word.
// Tokenizes on runs of Unicode letters/digits (Unicode-safe, unlike ASCII \b)
// so "café" matches "café here" but not "cafés"; works for any script.
func textHasWakeWord(text string, set map[string]struct{}) bool {
	if text == "" || len(set) == 0 {
		return false
	}
	start := -1
	for i, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if _, ok := set[strings.ToLower(text[start:i])]; ok {
				return true
			}
			start = -1
		}
	}
	if start >= 0 {
		if _, ok := set[strings.ToLower(text[start:])]; ok {
			return true
		}
	}
	return false
}
