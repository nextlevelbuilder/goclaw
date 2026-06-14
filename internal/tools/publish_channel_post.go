package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// channelPoster is the subset of *channels.Manager the publish tool needs.
// An interface keeps the tool unit-testable without a live Manager.
type channelPoster interface {
	PublishChannelPost(ctx context.Context, channelName, chatID, imagePath, captionHTML string, buttons []channels.PostButton) (int64, error)
}

// managerSender adapts a channelPoster (+ the telegram instance name that hosts
// the Meow channels) to meow.Sender, mapping meow.Button → channels.PostButton.
type managerSender struct {
	mgr         channelPoster
	channelName string
}

func (s managerSender) SendChannelPost(ctx context.Context, chatID, imagePath, captionHTML string, buttons []meow.Button) (int64, error) {
	pb := make([]channels.PostButton, len(buttons))
	for i, b := range buttons {
		pb[i] = channels.PostButton{Label: b.Label, URL: b.URL}
	}
	return s.mgr.PublishChannelPost(ctx, s.channelName, chatID, imagePath, captionHTML, pb)
}

// PublishChannelPostTool runs the deterministic, exactly-once Meow publish path
// for one channel-day on demand. Owner authorization is enforced at the command
// layer (telegram), not here: this tool is only registered for the owner-tenant
// Meow agent.
type PublishChannelPostTool struct {
	pub      *meow.Publisher
	store    store.MeowStore
	tenantID uuid.UUID
}

// NewPublishChannelPostTool wires the tool. channelInstance is the telegram
// channel-manager instance name that hosts the Meow channels; allowedRoots are
// the directories an image_path may resolve inside.
func NewPublishChannelPostTool(ms store.MeowStore, mgr channelPoster, channelInstance string, tenantID uuid.UUID, allowedRoots []string) *PublishChannelPostTool {
	return &PublishChannelPostTool{
		pub: &meow.Publisher{
			Store:        ms,
			Sender:       managerSender{mgr: mgr, channelName: channelInstance},
			AllowedRoots: allowedRoots,
			AllowedHosts: meow.DefaultButtonHostAllowlist(),
		},
		store:    ms,
		tenantID: tenantID,
	}
}

func (t *PublishChannelPostTool) Name() string { return "publish_channel_post" }

func (t *PublishChannelPostTool) Description() string {
	return `Publish the approved Meow post for a channel on a date. Deterministic and
exactly-once: re-running is a safe no-op once published. Send a JSON object:
{ "handle": "@TonSlotOfficial", "date": "2026-06-16", "force": false }
- handle: target channel handle (required)
- date: channel-local calendar date YYYY-MM-DD (required)
- force: also publish a draft (on-demand), default false
Pre-launch channels and unvetted image/button content are rejected.`
}

func (t *PublishChannelPostTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"handle": map[string]any{"type": "string", "description": "channel handle, e.g. @TonSlotOfficial"},
			"date":   map[string]any{"type": "string", "description": "channel-local date, YYYY-MM-DD"},
			"force":  map[string]any{"type": "boolean", "description": "also publish a draft (on-demand)"},
		},
		"required": []any{"handle", "date"},
	}
}

func (t *PublishChannelPostTool) Execute(ctx context.Context, args map[string]any) *Result {
	handle, _ := args["handle"].(string)
	dateStr, _ := args["date"].(string)
	force, _ := args["force"].(bool)
	if handle == "" || dateStr == "" {
		return ErrorResult("publish_channel_post: 'handle' and 'date' are required")
	}
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("publish_channel_post: bad date %q (want YYYY-MM-DD)", dateStr))
	}
	ch, err := t.store.GetChannelByHandle(ctx, t.tenantID, handle)
	if err != nil {
		return ErrorResult(fmt.Sprintf("publish_channel_post: channel %q: %v", handle, err))
	}
	res, err := t.pub.PublishDue(ctx, t.tenantID, ch.ID, date, force)
	if err != nil {
		return ErrorResult(fmt.Sprintf("publish_channel_post: %v", err))
	}
	if res == nil {
		return NewResult(fmt.Sprintf("No publishable post for %s on %s (nothing approved, or already published).", handle, dateStr))
	}
	out, _ := json.Marshal(map[string]any{"published": true, "message_id": res.MessageID, "link": res.Link})
	return NewResult(string(out))
}
