package oa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// message is a single entry in the /v2.0/oa/listrecentchat response.
// Each row is a message (not a thread summary).
type message struct {
	MessageID       string `json:"message_id"`
	FromID          string `json:"from_id"`
	FromDisplayName string `json:"from_display_name,omitempty"`
	ToID            string `json:"to_id,omitempty"`
	Time            int64  `json:"time,omitempty"`
	Text            string `json:"message,omitempty"` // Zalo's field is "message", not "text"
	Type            string `json:"type,omitempty"`    // text/image/file/sticker
}

// listRecentChat fetches the most-recent N messages across all users.
// Zalo v2.0 encodes GET params as a single JSON blob in the `data` query
// parameter (e.g. ?data={"offset":0,"count":10}).
func (c *Channel) listRecentChat(ctx context.Context, offset, count int) ([]message, error) {
	tok, err := c.tokens.Access(ctx)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(map[string]int{"offset": offset, "count": count})
	if err != nil {
		return nil, fmt.Errorf("zalo_oa: marshal listrecentchat params: %w", err)
	}
	q := url.Values{"data": {string(data)}}
	raw, err := c.client.apiGet(ctx, pathListRecentChat, q, tok)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Data []message `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("zalo_oa: decode listrecentchat: %w", err)
	}
	return wrap.Data, nil
}

// pollOnce runs one polling cycle. Iterates oldest-first, filters OA
// echoes (from_id == OAID), dedups per-user by last-seen timestamp.
// Returns ErrRateLimit on HTTP 429; one auth retry via ForceRefresh.
// Burn-down loop pages until a partial page (caught up) or maxPages cap.
func (c *Channel) pollOnce(ctx context.Context) error {
	if c.skipPollIfAuthFailed() {
		return nil
	}

	pageSize := pollCountFromCfg(c.cfg.PollCount)
	maxPages := pollBurndownMaxPagesFromCfg(c.cfg.PollBurndownMaxPages)

	for page := range maxPages {
		if err := ctx.Err(); err != nil {
			return err
		}
		offset := page * pageSize
		msgs, err := c.listRecentChatRetryAuth(ctx, offset, pageSize)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			break
		}
		c.processMessages(msgs)
		if len(msgs) < pageSize {
			break // partial page — caught up
		}
		if page == maxPages-1 {
			slog.Warn("zalo_oa.poll.burndown_capped",
				"oa_id", c.creds().OAID,
				"max_pages", maxPages,
				"page_size", pageSize,
				"hint", "raise poll_burndown_max_pages, shorten poll_interval_seconds, or switch to webhook transport")
		}
	}
	return nil
}

// listRecentChatRetryAuth wraps listRecentChat with one retry on auth
// failure that forces a token refresh.
func (c *Channel) listRecentChatRetryAuth(ctx context.Context, offset, count int) ([]message, error) {
	msgs, err := c.listRecentChat(ctx, offset, count)
	if err == nil {
		return msgs, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.isAuth() {
		slog.Warn("zalo_oa.poll.token_rejected_forcing_refresh",
			"oa_id", c.creds().OAID, "zalo_code", apiErr.Code, "zalo_msg", apiErr.Message)
		c.tokens.ForceRefresh()
		return c.listRecentChat(ctx, offset, count)
	}
	return nil, err
}

// processMessages iterates a page oldest-first, filters OA echoes and
// malformed rows, dedups via (cursor, seenIDs), and dispatches via
// BaseChannel.HandleMessage.
func (c *Channel) processMessages(msgs []message) {
	// Oldest-first so the cursor advances monotonically.
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Time < msgs[j].Time })

	for _, m := range msgs {
		if m.FromID == "" || m.FromID == c.creds().OAID {
			continue
		}
		if m.Time == 0 && m.MessageID == "" {
			// No dedup signal — warn so a future Zalo field rename surfaces
			// instead of silently swallowing every inbound message.
			slog.Warn("zalo_oa.poll.dropped_no_dedup_signal",
				"from_id", m.FromID,
				"type", m.Type)
			continue
		}
		// Prefer (from_id, time) cursor; fall back to message_id LRU when
		// Zalo omits time (rare).
		if m.Time != 0 {
			if m.Time <= c.cursor.Get(m.FromID) {
				continue
			}
		} else if m.MessageID != "" && c.seenIDs.SeenOrAdd(m.MessageID) {
			continue
		}
		c.dispatchInbound(m)
		if m.Time != 0 {
			c.cursor.Advance(m.FromID, m.Time)
		}
	}
}

// dispatchInbound maps a Zalo message into a BaseChannel.HandleMessage call.
// Zalo OA is DM-only, so chatID == senderID. Text only; non-text is skipped.
func (c *Channel) dispatchInbound(m message) {
	if m.Type != "" && m.Type != "text" {
		slog.Info("zalo_oa.poll.non_text_skipped",
			"oa_id", c.creds().OAID, "user_id", m.FromID, "message_id", m.MessageID, "type", m.Type)
		return
	}
	if m.Text == "" {
		return
	}
	metadata := common.InboundMeta{
		MessageID:         m.MessageID,
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: m.FromDisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(m.FromID, m.FromID, m.Text, nil, metadata, "direct")
}

// skipPollIfAuthFailed stops polling once health is Failed/Auth so we
// don't hammer the API while waiting for operator re-auth.
func (c *Channel) skipPollIfAuthFailed() bool {
	snap := c.HealthSnapshot()
	return snap.State == channels.ChannelHealthStateFailed && snap.FailureKind == channels.ChannelFailureKindAuth
}

const (
	defaultPollInterval = 15 * time.Second
	rateLimitBackoff    = 30 * time.Second
	cursorFlushInterval = 60 * time.Second

	// Zalo /v2.0/oa/listrecentchat caps `count` at 10 (server returns -210 above).
	defaultPollCount            = 10
	pollCountFloor              = 1
	pollCountCeil               = 10
	defaultPollBurndownMaxPages = 10
	pollBurndownMaxPagesCeil    = 20
)

// pollIntervalFromCfg clamps cfg.PollIntervalSeconds to the safe range.
func pollIntervalFromCfg(s int) time.Duration {
	switch {
	case s < 5:
		return defaultPollInterval
	case s > 120:
		return 120 * time.Second
	default:
		return time.Duration(s) * time.Second
	}
}

// pollCountFromCfg clamps cfg.PollCount to [pollCountFloor, pollCountCeil].
// Zero/negative → defaultPollCount.
func pollCountFromCfg(n int) int {
	switch {
	case n <= 0:
		return defaultPollCount
	case n < pollCountFloor:
		return pollCountFloor
	case n > pollCountCeil:
		return pollCountCeil
	default:
		return n
	}
}

// pollBurndownMaxPagesFromCfg clamps cfg.PollBurndownMaxPages to [1, 20].
// Zero/negative → defaultPollBurndownMaxPages. 1 disables burn-down.
func pollBurndownMaxPagesFromCfg(n int) int {
	switch {
	case n <= 0:
		return defaultPollBurndownMaxPages
	case n > pollBurndownMaxPagesCeil:
		return pollBurndownMaxPagesCeil
	default:
		return n
	}
}
