package telegram

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeSysCfg is an in-memory store.SystemConfigStore.
type fakeSysCfg struct{ m map[string]string }

func (f *fakeSysCfg) Get(_ context.Context, k string) (string, error) { return f.m[k], nil }
func (f *fakeSysCfg) Set(_ context.Context, k, v string) error        { f.m[k] = v; return nil }
func (f *fakeSysCfg) Delete(_ context.Context, k string) error        { delete(f.m, k); return nil }
func (f *fakeSysCfg) List(_ context.Context) (map[string]string, error) {
	return f.m, nil
}

func TestMeowOwnerAllowed_ClosedByDefault(t *testing.T) {
	ctx := context.Background()

	// No config store injected → /meow disabled, everyone denied.
	c := &Channel{}
	if c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("nil meowCfg must deny")
	}

	cs := &fakeSysCfg{m: map[string]string{}}
	c.meowCfg = cs

	// Empty config → denied.
	if c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("empty config must deny")
	}

	// Owner set but unverified → denied (incl. the owner).
	cs.m[meow.CfgOwnerChatID] = "123"
	if c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("unverified owner must be denied")
	}

	// Verified → only the owner id passes.
	cs.m[meow.CfgOwnerVerified] = "true"
	if !c.meowOwnerAllowed(ctx, "123") {
		t.Fatal("verified owner should be allowed")
	}
	if c.meowOwnerAllowed(ctx, "999") {
		t.Fatal("non-owner must be denied")
	}
}

func TestHandleMeowCommand_OwnerGated(t *testing.T) {
	ctx := context.Background()
	cs := &fakeSysCfg{m: map[string]string{meow.CfgOwnerChatID: "123"}} // configured, NOT verified
	published := 0
	c := &Channel{meowCfg: cs}
	c.meowPublish = func(_ context.Context, handle, date string, force bool) (string, error) {
		published++
		return "published " + handle + " " + date, nil
	}

	// Unverified owner: post rejected, publisher NOT called.
	if r := c.handleMeowCommand(ctx, "123", []string{"post", "@K", "2026-06-16"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("expected rejection before verify, got %q", r)
	}
	if published != 0 {
		t.Fatal("publisher must not run before verification")
	}

	// Verify as owner → success.
	if r := c.handleMeowCommand(ctx, "123", []string{"verify"}); !strings.Contains(r, "verified") {
		t.Fatalf("verify should succeed for owner, got %q", r)
	}

	// Owner post now works → publisher called once.
	if r := c.handleMeowCommand(ctx, "123", []string{"post", "@K", "2026-06-16"}); !strings.Contains(r, "published @K") {
		t.Fatalf("owner post should publish, got %q", r)
	}
	if published != 1 {
		t.Fatalf("publisher should be called once, got %d", published)
	}

	// Non-owner still rejected after owner verification.
	if r := c.handleMeowCommand(ctx, "999", []string{"post", "@K", "2026-06-16"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("non-owner must be rejected, got %q", r)
	}
	if published != 1 {
		t.Fatal("non-owner must not trigger publish")
	}

	// Usage on missing args.
	if r := c.handleMeowCommand(ctx, "123", []string{"post"}); !strings.Contains(r, "Usage") {
		t.Fatalf("expected usage, got %q", r)
	}
}

// fakeMeowStore implements the MeowStore methods the content commands call.
type fakeMeowStore struct {
	store.MeowStore
	chans    map[string]*store.MpChannel
	posts    []store.MpContentPost
	approved []uuid.UUID
	skipped  []uuid.UUID
	upserts  []store.MpContentPost
}

func (f *fakeMeowStore) GetChannelByHandle(_ context.Context, _ uuid.UUID, handle string) (*store.MpChannel, error) {
	if ch, ok := f.chans[handle]; ok {
		return ch, nil
	}
	return nil, store.ErrMeowChannelNotFound
}

func (f *fakeMeowStore) ListPostsByChannel(_ context.Context, _, channelID uuid.UUID) ([]store.MpContentPost, error) {
	var out []store.MpContentPost
	for _, p := range f.posts {
		if p.ChannelID == channelID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeMeowStore) ApprovePost(_ context.Context, _, id uuid.UUID, _ string) error {
	for i := range f.posts {
		if f.posts[i].ID == id && f.posts[i].Status == store.MpPostDraft {
			f.posts[i].Status = store.MpPostApproved
			f.approved = append(f.approved, id)
			return nil
		}
	}
	return store.ErrMeowPostNotFound
}

func (f *fakeMeowStore) SkipPost(_ context.Context, _, id uuid.UUID) error {
	for i := range f.posts {
		if f.posts[i].ID == id && (f.posts[i].Status == store.MpPostDraft || f.posts[i].Status == store.MpPostApproved) {
			f.posts[i].Status = store.MpPostSkipped
			f.skipped = append(f.skipped, id)
			return nil
		}
	}
	return store.ErrMeowPostNotFound
}

func (f *fakeMeowStore) UpsertDraftPost(_ context.Context, p *store.MpContentPost) error {
	f.upserts = append(f.upserts, *p)
	for i := range f.posts {
		if f.posts[i].ChannelID == p.ChannelID && meowSameDate(f.posts[i].ScheduledDate, p.ScheduledDate) && f.posts[i].Status == store.MpPostDraft {
			f.posts[i].KoText = p.KoText
			f.posts[i].EnText = p.EnText
			p.ID = f.posts[i].ID
			return nil
		}
	}
	p.ID = uuid.New()
	p.Status = store.MpPostDraft
	f.posts = append(f.posts, *p)
	return nil
}

// ownerChannel returns a Channel whose owner gate is verified for id "123".
func ownerChannel(ms store.MeowStore, assetRoot, inboxRoot string) *Channel {
	cs := &fakeSysCfg{m: map[string]string{meow.CfgOwnerChatID: "123", meow.CfgOwnerVerified: "true"}}
	c := &Channel{meowCfg: cs}
	c.SetMeowOps(ms, uuid.New(), assetRoot, inboxRoot)
	return c
}

var meowDay = time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)

func TestMeowQueue(t *testing.T) {
	ctx := context.Background()
	chID := uuid.New()
	ms := &fakeMeowStore{
		chans: map[string]*store.MpChannel{"@K": {ID: chID, Handle: "@K"}},
		posts: []store.MpContentPost{
			{ID: uuid.New(), ChannelID: chID, ScheduledDate: meowDay, Status: store.MpPostDraft, EnText: "draft one"},
			{ID: uuid.New(), ChannelID: chID, ScheduledDate: meowDay.AddDate(0, 0, 1), Status: store.MpPostApproved, EnText: "approved two"},
			{ID: uuid.New(), ChannelID: chID, ScheduledDate: meowDay.AddDate(0, 0, 2), Status: store.MpPostPublished, EnText: "published three"},
		},
	}
	c := ownerChannel(ms, "", "")

	// Non-owner denied.
	if r := c.handleMeowCommand(ctx, "999", []string{"queue", "@K"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("non-owner: %q", r)
	}
	// Owner: lists draft+approved only.
	r := c.handleMeowCommand(ctx, "123", []string{"queue", "@K"})
	if !strings.Contains(r, "draft") || !strings.Contains(r, "approved") {
		t.Fatalf("queue missing rows: %q", r)
	}
	if strings.Contains(r, "published three") {
		t.Fatalf("queue must not list published: %q", r)
	}
	// Usage on missing handle.
	if r := c.handleMeowCommand(ctx, "123", []string{"queue"}); !strings.Contains(r, "Usage") {
		t.Fatalf("queue usage: %q", r)
	}
}

func TestMeowApproveSkip(t *testing.T) {
	ctx := context.Background()
	chID := uuid.New()
	mk := func() *fakeMeowStore {
		return &fakeMeowStore{
			chans: map[string]*store.MpChannel{"@K": {ID: chID, Handle: "@K"}},
			posts: []store.MpContentPost{{ID: uuid.New(), ChannelID: chID, ScheduledDate: meowDay, Status: store.MpPostDraft, EnText: "hi"}},
		}
	}

	ms := mk()
	c := ownerChannel(ms, "", "")

	// Non-owner cannot approve.
	if r := c.handleMeowCommand(ctx, "999", []string{"approve", "@K", "2026-06-16"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("non-owner approve: %q", r)
	}
	if len(ms.approved) != 0 {
		t.Fatal("non-owner must not approve")
	}
	// Owner approves the draft once.
	if r := c.handleMeowCommand(ctx, "123", []string{"approve", "@K", "2026-06-16"}); !strings.Contains(r, "Approved") {
		t.Fatalf("approve: %q", r)
	}
	if len(ms.approved) != 1 {
		t.Fatalf("expected one approve, got %d", len(ms.approved))
	}
	// No draft remains → no-draft reply.
	if r := c.handleMeowCommand(ctx, "123", []string{"approve", "@K", "2026-06-16"}); !strings.Contains(r, "No draft") {
		t.Fatalf("re-approve: %q", r)
	}
	// Unknown channel / bad date.
	if r := c.handleMeowCommand(ctx, "123", []string{"approve", "@X", "2026-06-16"}); !strings.Contains(r, "Unknown channel") {
		t.Fatalf("unknown channel: %q", r)
	}
	if r := c.handleMeowCommand(ctx, "123", []string{"approve", "@K", "nope"}); !strings.Contains(r, "Bad date") {
		t.Fatalf("bad date: %q", r)
	}

	// Skip: a fresh draft can be skipped; non-owner cannot.
	ms2 := mk()
	c2 := ownerChannel(ms2, "", "")
	if r := c2.handleMeowCommand(ctx, "999", []string{"skip", "@K", "2026-06-16"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("non-owner skip: %q", r)
	}
	if r := c2.handleMeowCommand(ctx, "123", []string{"skip", "@K", "2026-06-16"}); !strings.Contains(r, "Skipped") {
		t.Fatalf("skip: %q", r)
	}
	if len(ms2.skipped) != 1 {
		t.Fatalf("expected one skip, got %d", len(ms2.skipped))
	}
}

func TestMeowPreviewAndEdit(t *testing.T) {
	ctx := context.Background()
	chID := uuid.New()
	bs, _ := json.Marshal([]meow.Button{{Label: "Play", URL: "https://t.me/TestBot?startapp=x"}})
	ms := &fakeMeowStore{
		chans: map[string]*store.MpChannel{"@K": {ID: chID, Handle: "@K"}},
		posts: []store.MpContentPost{{ID: uuid.New(), ChannelID: chID, ScheduledDate: meowDay, Status: store.MpPostDraft, KoText: "안녕", EnText: "hi", Buttons: bs}},
	}
	c := ownerChannel(ms, "", "")

	// Preview shows the fields.
	r := c.handleMeowCommand(ctx, "123", []string{"preview", "@K", "2026-06-16"})
	if !strings.Contains(r, "KO: 안녕") || !strings.Contains(r, "EN: hi") || !strings.Contains(r, "Play") {
		t.Fatalf("preview missing fields: %q", r)
	}
	// Preview with no post for the date.
	if r := c.handleMeowCommand(ctx, "123", []string{"preview", "@K", "2026-12-31"}); !strings.Contains(r, "No post") {
		t.Fatalf("preview none: %q", r)
	}

	// Edit the EN caption of the draft.
	if r := c.handleMeowCommand(ctx, "123", []string{"edit", "@K", "2026-06-16", "en", "new", "english", "text"}); !strings.Contains(r, "Updated") {
		t.Fatalf("edit: %q", r)
	}
	if len(ms.upserts) != 1 || ms.upserts[0].EnText != "new english text" {
		t.Fatalf("edit did not upsert new text: %+v", ms.upserts)
	}
	// Bad field rejected.
	if r := c.handleMeowCommand(ctx, "123", []string{"edit", "@K", "2026-06-16", "fr", "x"}); !strings.Contains(r, "Field must be") {
		t.Fatalf("edit bad field: %q", r)
	}
}

func TestMeowIngest(t *testing.T) {
	ctx := context.Background()
	inbox, asset := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(inbox, "post.webp"), []byte("RIFF\x00\x00\x00\x00WEBP"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle := `{"handle":"@K","scheduled_date":"2026-06-16","ko_text":"안녕","en_text":"hi","image":"post.webp","buttons":[{"label":"Play","url":"https://t.me/TestBot?startapp=x"}]}`
	if err := os.WriteFile(filepath.Join(inbox, "bundle.json"), []byte(bundle), 0o600); err != nil {
		t.Fatal(err)
	}
	chID := uuid.New()
	bs, _ := json.Marshal([]meow.Button{{Label: "Play", URL: "https://t.me/TestBot?startapp=x"}})
	ms := &fakeMeowStore{chans: map[string]*store.MpChannel{"@K": {ID: chID, Handle: "@K", ButtonSet: bs}}}
	c := ownerChannel(ms, asset, inbox)

	// Non-owner denied; usage on missing arg.
	if r := c.handleMeowCommand(ctx, "999", []string{"ingest", "bundle.json"}); !strings.Contains(r, "Not authorized") {
		t.Fatalf("non-owner ingest: %q", r)
	}
	if r := c.handleMeowCommand(ctx, "123", []string{"ingest"}); !strings.Contains(r, "Usage") {
		t.Fatalf("ingest usage: %q", r)
	}
	// Owner ingest succeeds and creates a draft.
	if r := c.handleMeowCommand(ctx, "123", []string{"ingest", "bundle.json"}); !strings.Contains(r, "Ingested") {
		t.Fatalf("ingest: %q", r)
	}
	if len(ms.upserts) != 1 {
		t.Fatalf("expected one draft upsert, got %d", len(ms.upserts))
	}
	// Path escape is rejected (never ingests).
	if r := c.handleMeowCommand(ctx, "123", []string{"ingest", "../bundle.json"}); strings.Contains(r, "Ingested") {
		t.Fatalf("path escape must not ingest: %q", r)
	}
}
