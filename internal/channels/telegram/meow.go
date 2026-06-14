package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const meowDateLayout = "2006-01-02"

// meowPublishFn publishes the approved post for a handle + date, returning a
// human-readable result line. Injected by the gateway; nil = /meow post off.
type meowPublishFn func(ctx context.Context, handle, date string, force bool) (string, error)

// SetMeowConfig enables the owner-gated /meow command family by giving the
// channel the system config store that holds owner-gate state. nil = disabled.
func (c *Channel) SetMeowConfig(cs store.SystemConfigStore) { c.meowCfg = cs }

// SetMeowPublisher injects the owner-gated publish entrypoint used by /meow post.
func (c *Channel) SetMeowPublisher(fn meowPublishFn) { c.meowPublish = fn }

// SetMeowOps wires the store-backed content commands (queue/preview/edit/approve/
// skip/ingest). assetRoot is the publish allowed-root images are copied into;
// inboxRoot is the server dir holding draft bundles awaiting ingest. A nil store
// leaves those subcommands disabled (they report ops-unavailable).
func (c *Channel) SetMeowOps(ms store.MeowStore, tenantID uuid.UUID, assetRoot, inboxRoot string) {
	c.meowStore = ms
	c.meowTenantID = tenantID
	c.meowAssetRoot = assetRoot
	c.meowInboxRoot = inboxRoot
}

// meowOwnerAllowed reports whether senderID may run owner-gated /meow commands.
// Closed by default: a nil config store, or an unconfigured/unverified owner,
// denies everyone. senderID must be the bare numeric Telegram id.
func (c *Channel) meowOwnerAllowed(ctx context.Context, senderID string) bool {
	if c.meowCfg == nil {
		return false
	}
	return meow.LoadOwnerGate(ctx, c.meowCfg).Allowed(senderID)
}

// handleMeowCommand processes a /meow subcommand and returns the reply text.
// senderID is the bare numeric Telegram id. Pure (no telego I/O) so it is
// unit-testable; the command case wires the reply send.
func (c *Channel) handleMeowCommand(ctx context.Context, senderID string, args []string) string {
	if c.meowCfg == nil {
		return "Meow is not enabled on this instance."
	}
	loc := store.LocaleFromContext(ctx)
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "verify":
		ok, err := meow.VerifyRoundTrip(ctx, c.meowCfg, senderID)
		if err != nil {
			return "Verification failed: " + err.Error()
		}
		if ok {
			return "✅ Owner verified. Owner-gated /meow commands are now enabled."
		}
		return "❌ Not authorized. Ask an admin to set the Meow owner chat id, then run /meow verify from that chat."
	case "post":
		// Owner-gated: closed by default until configured + verified.
		if !c.meowOwnerAllowed(ctx, senderID) {
			return "❌ Not authorized. Owner-gated — run /meow verify from the configured owner chat first."
		}
		if c.meowPublish == nil {
			return "Publishing is not wired on this instance."
		}
		if len(args) < 3 {
			return "Usage: /meow post <@handle> <YYYY-MM-DD> [force]"
		}
		handle, date := args[1], args[2]
		force := len(args) > 3 && strings.EqualFold(args[3], "force")
		res, err := c.meowPublish(ctx, handle, date, force)
		if err != nil {
			return "Publish failed: " + err.Error()
		}
		return res
	case "queue":
		return c.meowQueue(ctx, loc, senderID, args)
	case "preview":
		return c.meowPreview(ctx, loc, senderID, args)
	case "approve":
		return c.meowApprove(ctx, loc, senderID, args)
	case "skip":
		return c.meowSkip(ctx, loc, senderID, args)
	case "edit":
		return c.meowEdit(ctx, loc, senderID, args)
	case "ingest":
		return c.meowIngest(ctx, loc, senderID, args)
	default:
		return "Meow commands:\n" +
			"/meow verify — verify you are the owner\n" +
			"/meow queue <@handle> — list draft/approved posts\n" +
			"/meow preview <@handle> <YYYY-MM-DD> — show a post\n" +
			"/meow edit <@handle> <YYYY-MM-DD> ko|en <text> — edit a draft's caption\n" +
			"/meow approve <@handle> <YYYY-MM-DD> — approve a draft\n" +
			"/meow skip <@handle> <YYYY-MM-DD> — skip a draft/approved post\n" +
			"/meow ingest <bundle.json> — ingest a draft bundle\n" +
			"/meow post <@handle> <YYYY-MM-DD> [force] — publish a channel post"
	}
}

// meowReady reports whether the content store is wired; if not it returns the
// owner-gate-or-ops-unavailable reply. ok=false means caller should return msg.
func (c *Channel) meowReady(ctx context.Context, loc, senderID string) (msg string, ok bool) {
	if !c.meowOwnerAllowed(ctx, senderID) {
		return i18n.T(loc, i18n.MsgMeowNotAuthorized), false
	}
	if c.meowStore == nil {
		return i18n.T(loc, i18n.MsgMeowOpsUnavailable), false
	}
	return "", true
}

func (c *Channel) meowQueue(ctx context.Context, loc, senderID string, args []string) string {
	if msg, ok := c.meowReady(ctx, loc, senderID); !ok {
		return msg
	}
	if len(args) < 2 {
		return i18n.T(loc, i18n.MsgMeowQueueUsage)
	}
	handle := args[1]
	ch, err := c.meowStore.GetChannelByHandle(ctx, c.meowTenantID, handle)
	if errors.Is(err, store.ErrMeowChannelNotFound) {
		return i18n.T(loc, i18n.MsgMeowChannelUnknown, handle)
	}
	if err != nil {
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	posts, err := c.meowStore.ListPostsByChannel(ctx, c.meowTenantID, ch.ID)
	if err != nil {
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	var lines []string
	for _, p := range posts {
		if p.Status == store.MpPostDraft || p.Status == store.MpPostApproved {
			lines = append(lines, fmt.Sprintf("%s  [%s]  %s",
				p.ScheduledDate.Format(meowDateLayout), p.Status, meowSnippet(p)))
		}
	}
	if len(lines) == 0 {
		return i18n.T(loc, i18n.MsgMeowQueueEmpty, handle)
	}
	return i18n.T(loc, i18n.MsgMeowQueueHeader, handle) + "\n" + strings.Join(lines, "\n")
}

func (c *Channel) meowPreview(ctx context.Context, loc, senderID string, args []string) string {
	if msg, ok := c.meowReady(ctx, loc, senderID); !ok {
		return msg
	}
	if len(args) < 3 {
		return i18n.T(loc, i18n.MsgMeowPreviewUsage)
	}
	handle, dateStr := args[1], args[2]
	ch, match, errMsg := c.meowChannelPostsForDate(ctx, loc, handle, dateStr)
	if errMsg != "" {
		return errMsg
	}
	// Prefer the live/active row over a skipped one for the same day.
	p := meowPick(match, store.MpPostPublished, store.MpPostPublishing,
		store.MpPostApproved, store.MpPostDraft, store.MpPostSkipped)
	if p == nil {
		return i18n.T(loc, i18n.MsgMeowPostNone, handle, dateStr)
	}
	// Structured field dump for the owner — labels are stable technical names.
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s · [%s]", ch.Handle, p.ScheduledDate.Format(meowDateLayout), p.Status)
	if t := strings.TrimSpace(p.KoText); t != "" {
		fmt.Fprintf(&b, "\nKO: %s", t)
	}
	if t := strings.TrimSpace(p.EnText); t != "" {
		fmt.Fprintf(&b, "\nEN: %s", t)
	}
	for _, btn := range meowButtons(p.Buttons) {
		fmt.Fprintf(&b, "\n• %s → %s", btn.Label, btn.URL)
	}
	if p.ImagePath != "" {
		fmt.Fprintf(&b, "\nImage: %s", p.ImagePath)
	}
	if p.TgLink != "" {
		fmt.Fprintf(&b, "\nLink: %s", p.TgLink)
	}
	return b.String()
}

func (c *Channel) meowApprove(ctx context.Context, loc, senderID string, args []string) string {
	if msg, ok := c.meowReady(ctx, loc, senderID); !ok {
		return msg
	}
	if len(args) < 3 {
		return i18n.T(loc, i18n.MsgMeowApproveUsage)
	}
	handle, dateStr := args[1], args[2]
	_, match, errMsg := c.meowChannelPostsForDate(ctx, loc, handle, dateStr)
	if errMsg != "" {
		return errMsg
	}
	d := meowPick(match, store.MpPostDraft)
	if d == nil {
		return i18n.T(loc, i18n.MsgMeowNoDraftToAct, handle, dateStr)
	}
	if err := c.meowStore.ApprovePost(ctx, c.meowTenantID, d.ID, senderID); err != nil {
		if errors.Is(err, store.ErrMeowPostNotFound) {
			return i18n.T(loc, i18n.MsgMeowNoDraftToAct, handle, dateStr)
		}
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	return i18n.T(loc, i18n.MsgMeowApproved, handle, dateStr)
}

func (c *Channel) meowSkip(ctx context.Context, loc, senderID string, args []string) string {
	if msg, ok := c.meowReady(ctx, loc, senderID); !ok {
		return msg
	}
	if len(args) < 3 {
		return i18n.T(loc, i18n.MsgMeowSkipUsage)
	}
	handle, dateStr := args[1], args[2]
	_, match, errMsg := c.meowChannelPostsForDate(ctx, loc, handle, dateStr)
	if errMsg != "" {
		return errMsg
	}
	p := meowPick(match, store.MpPostDraft, store.MpPostApproved)
	if p == nil {
		return i18n.T(loc, i18n.MsgMeowNoPostToSkip, handle, dateStr)
	}
	if err := c.meowStore.SkipPost(ctx, c.meowTenantID, p.ID); err != nil {
		if errors.Is(err, store.ErrMeowPostNotFound) {
			return i18n.T(loc, i18n.MsgMeowNoPostToSkip, handle, dateStr)
		}
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	return i18n.T(loc, i18n.MsgMeowSkipped, handle, dateStr)
}

func (c *Channel) meowEdit(ctx context.Context, loc, senderID string, args []string) string {
	if msg, ok := c.meowReady(ctx, loc, senderID); !ok {
		return msg
	}
	// edit <@handle> <YYYY-MM-DD> ko|en <text...>
	if len(args) < 5 {
		return i18n.T(loc, i18n.MsgMeowEditUsage)
	}
	handle, dateStr := args[1], args[2]
	field := strings.ToLower(args[3])
	if field != "ko" && field != "en" {
		return i18n.T(loc, i18n.MsgMeowEditBadField, args[3])
	}
	text := strings.Join(args[4:], " ")
	_, match, errMsg := c.meowChannelPostsForDate(ctx, loc, handle, dateStr)
	if errMsg != "" {
		return errMsg
	}
	d := meowPick(match, store.MpPostDraft)
	if d == nil {
		return i18n.T(loc, i18n.MsgMeowNoDraftToAct, handle, dateStr)
	}
	// Re-upsert the draft in place (idempotent, draft-only) with one field changed.
	upd := *d
	upd.TenantID = c.meowTenantID
	if field == "ko" {
		upd.KoText = text
	} else {
		upd.EnText = text
	}
	if err := c.meowStore.UpsertDraftPost(ctx, &upd); err != nil {
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	return i18n.T(loc, i18n.MsgMeowEdited, field, handle, dateStr)
}

func (c *Channel) meowIngest(ctx context.Context, loc, senderID string, args []string) string {
	if msg, ok := c.meowReady(ctx, loc, senderID); !ok {
		return msg
	}
	if c.meowInboxRoot == "" || c.meowAssetRoot == "" {
		return i18n.T(loc, i18n.MsgMeowOpsUnavailable)
	}
	if len(args) < 2 {
		return i18n.T(loc, i18n.MsgMeowIngestUsage)
	}
	// The owner gives a path relative to the inbox. ValidateImagePath resolves
	// symlinks and asserts containment under the inbox root, so a `..` or symlink
	// escape is rejected before any read.
	jsonPath := filepath.Join(c.meowInboxRoot, args[1])
	resolved, err := meow.ValidateImagePath(jsonPath, []string{c.meowInboxRoot})
	if err != nil {
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	b, err := meow.ParseDraftBundle(data)
	if err != nil {
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	res, err := meow.IngestDraft(ctx, c.meowStore, c.meowTenantID, b, filepath.Dir(resolved), c.meowAssetRoot)
	if err != nil {
		return i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	if res.Held {
		return i18n.T(loc, i18n.MsgMeowIngestHeld, res.Reason)
	}
	return i18n.T(loc, i18n.MsgMeowIngested, b.Handle, b.ScheduledDate)
}

// meowChannelPostsForDate resolves a channel by handle and returns the posts
// scheduled for dateStr. A non-empty errMsg is a localized reply the caller
// should return directly (bad date / unknown channel / store error).
func (c *Channel) meowChannelPostsForDate(ctx context.Context, loc, handle, dateStr string) (*store.MpChannel, []store.MpContentPost, string) {
	date, err := time.Parse(meowDateLayout, dateStr)
	if err != nil {
		return nil, nil, i18n.T(loc, i18n.MsgMeowBadDate, dateStr)
	}
	ch, err := c.meowStore.GetChannelByHandle(ctx, c.meowTenantID, handle)
	if errors.Is(err, store.ErrMeowChannelNotFound) {
		return nil, nil, i18n.T(loc, i18n.MsgMeowChannelUnknown, handle)
	}
	if err != nil {
		return nil, nil, i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	all, err := c.meowStore.ListPostsByChannel(ctx, c.meowTenantID, ch.ID)
	if err != nil {
		return nil, nil, i18n.T(loc, i18n.MsgMeowError, err.Error())
	}
	var match []store.MpContentPost
	for _, p := range all {
		if meowSameDate(p.ScheduledDate, date) {
			match = append(match, p)
		}
	}
	return ch, match, ""
}

// meowPick returns the first post whose status matches one of statuses (in the
// given priority order), or nil.
func meowPick(posts []store.MpContentPost, statuses ...string) *store.MpContentPost {
	for _, want := range statuses {
		for i := range posts {
			if posts[i].Status == want {
				return &posts[i]
			}
		}
	}
	return nil
}

// meowSameDate compares two timestamps by calendar day (scheduled_date is a date).
func meowSameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// meowSnippet is a short one-line preview of a post's caption (EN, else KO).
func meowSnippet(p store.MpContentPost) string {
	s := strings.TrimSpace(p.EnText)
	if s == "" {
		s = strings.TrimSpace(p.KoText)
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > 48 {
		return string(r[:48]) + "…"
	}
	return s
}

// meowButtons decodes a post's stored button set; a malformed set yields nil.
func meowButtons(raw json.RawMessage) []meow.Button {
	if len(raw) == 0 {
		return nil
	}
	var b []meow.Button
	_ = json.Unmarshal(raw, &b)
	return b
}
