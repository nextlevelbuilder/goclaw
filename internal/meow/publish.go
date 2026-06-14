package meow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var (
	// ErrChannelNotLaunched blocks publishing to a pre-launch channel — those
	// never auto-post (and never get engagement), to avoid platform bans.
	ErrChannelNotLaunched = errors.New("meow: channel not launched")
	// ErrChatIDUnresolved means the channel's numeric chat id hasn't been
	// resolved yet, so there's nowhere to send.
	ErrChatIDUnresolved = errors.New("meow: channel chat_id not resolved")
)

// Button is one inline-keyboard button (label + URL).
type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Sender delivers a finished channel post. The telegram layer implements it;
// the interface keeps PublishDue testable without telegram wiring.
type Sender interface {
	SendChannelPost(ctx context.Context, chatID, imagePath, captionHTML string, buttons []Button) (messageID int64, err error)
}

// Publisher runs the deterministic, exactly-once publish path. No LLM is on
// this path — the scheduler calls PublishDue directly.
type Publisher struct {
	Store        store.MeowStore
	Sender       Sender
	AllowedRoots []string        // roots an image_path must resolve inside
	AllowedHosts map[string]bool // first-layer host allowlist for buttons
}

// PublishResult summarizes a successful publish.
type PublishResult struct {
	PostID    uuid.UUID
	MessageID int64
	Link      string
}

// PublishDue publishes the eligible post for (channel, date): validate → claim
// → send → persist message id + link. Returns (nil, nil) when nothing is
// claimable (safe no-op / already published). On any failure after the claim,
// the post is left 'publishing' for reconciliation and manual review — it is
// never blind-retried or silently skipped.
func (p *Publisher) PublishDue(ctx context.Context, tenantID, channelID uuid.UUID, date time.Time, force bool) (*PublishResult, error) {
	ch, err := p.Store.GetChannel(ctx, tenantID, channelID)
	if err != nil {
		return nil, err
	}
	if !ch.Launched {
		return nil, ErrChannelNotLaunched
	}
	if ch.ChatID == nil || strings.TrimSpace(*ch.ChatID) == "" {
		return nil, ErrChatIDUnresolved
	}
	registered, err := registeredURLs(ch.ButtonSet)
	if err != nil {
		return nil, err
	}

	post, err := p.Store.ClaimPostForPublish(ctx, tenantID, channelID, date, force)
	if errors.Is(err, store.ErrMeowNoClaimablePost) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Validate inputs before sending. The post is already claimed
	// ('publishing'); a validation failure surfaces for manual review rather
	// than ever publishing an unvetted image or button.
	imgPath, err := ValidateImagePath(post.ImagePath, p.AllowedRoots)
	if err != nil {
		return nil, fmt.Errorf("publish post %s: %w", post.ID, err)
	}
	buttons, err := parseButtons(post.Buttons)
	if err != nil {
		return nil, fmt.Errorf("publish post %s: %w", post.ID, err)
	}
	for _, b := range buttons {
		if err := ValidateButtonURL(b.URL, registered, p.AllowedHosts); err != nil {
			return nil, fmt.Errorf("publish post %s: %w", post.ID, err)
		}
	}

	caption := BuildCaption(post.KoText, post.EnText)
	msgID, err := p.Sender.SendChannelPost(ctx, *ch.ChatID, imgPath, caption, buttons)
	if err != nil {
		return nil, fmt.Errorf("publish post %s: send: %w", post.ID, err)
	}

	link := postLink(ch.Handle, msgID)
	if err := p.Store.UpdatePostStatus(ctx, tenantID, post.ID, store.MpPostPublished, &msgID, link); err != nil {
		return nil, fmt.Errorf("publish post %s: persist after send: %w", post.ID, err)
	}
	return &PublishResult{PostID: post.ID, MessageID: msgID, Link: link}, nil
}

// registeredURLs is the exact-URL allowlist for a channel, from its button set.
func registeredURLs(buttonSet json.RawMessage) (map[string]bool, error) {
	btns, err := parseButtons(buttonSet)
	if err != nil {
		return nil, fmt.Errorf("channel button set: %w", err)
	}
	urls := make([]string, 0, len(btns))
	for _, b := range btns {
		urls = append(urls, b.URL)
	}
	return NewURLSet(urls), nil
}

func parseButtons(raw json.RawMessage) ([]Button, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var btns []Button
	if err := json.Unmarshal(raw, &btns); err != nil {
		return nil, fmt.Errorf("parse buttons: %w", err)
	}
	return btns, nil
}

// postLink builds the public t.me/<handle>/<id> link for a published message.
func postLink(handle string, msgID int64) string {
	h := strings.TrimPrefix(strings.TrimSpace(handle), "@")
	return fmt.Sprintf("https://t.me/%s/%d", h, msgID)
}
