package meow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeWorkerStore is a stateful in-memory MeowStore for the sync worker: it tracks
// created posts so a second pass sees the first pass's approved post (idempotency).
type fakeWorkerStore struct {
	store.MeowStore
	ch    store.MpChannel
	posts []*store.MpContentPost
}

func (f *fakeWorkerStore) ListChannels(context.Context, uuid.UUID) ([]store.MpChannel, error) {
	return []store.MpChannel{f.ch}, nil
}
func (f *fakeWorkerStore) GetChannelByHandle(_ context.Context, _ uuid.UUID, h string) (*store.MpChannel, error) {
	if h == f.ch.Handle {
		return &f.ch, nil
	}
	return nil, store.ErrMeowChannelNotFound
}
func (f *fakeWorkerStore) ListPostsByChannel(_ context.Context, _, _ uuid.UUID) ([]store.MpContentPost, error) {
	out := make([]store.MpContentPost, 0, len(f.posts))
	for _, p := range f.posts {
		out = append(out, *p)
	}
	return out, nil
}
func (f *fakeWorkerStore) UpsertDraftPost(_ context.Context, p *store.MpContentPost) error {
	for _, ex := range f.posts { // update existing draft for the channel-day in place
		if ex.Status == store.MpPostDraft && ex.ScheduledDate.Equal(p.ScheduledDate) {
			ex.KoText, ex.EnText, ex.ImagePath, ex.Buttons = p.KoText, p.EnText, p.ImagePath, p.Buttons
			p.ID = ex.ID
			return nil
		}
	}
	p.ID = uuid.New()
	p.Status = store.MpPostDraft
	cp := *p
	f.posts = append(f.posts, &cp)
	return nil
}
func (f *fakeWorkerStore) ApprovePost(_ context.Context, _, id uuid.UUID, by string) error {
	for _, p := range f.posts {
		if p.ID == id && p.Status == store.MpPostDraft {
			p.Status = store.MpPostApproved
			p.ApprovedBy = by
			return nil
		}
	}
	return store.ErrMeowPostNotFound
}

// fakeRWClient serves seeded rows and records write-backs by row index.
type fakeRWClient struct {
	rows   map[string][]SheetRow
	writes map[int]RowResult
}

func (c *fakeRWClient) ReadTab(_ context.Context, tab string) ([]SheetRow, error) {
	return c.rows[tab], nil
}
func (c *fakeRWClient) WriteBack(_ context.Context, _ string, rowIndex int, r RowResult) error {
	if c.writes == nil {
		c.writes = map[int]RowResult{}
	}
	c.writes[rowIndex] = r
	return nil
}

func workerFixture(t *testing.T) (*fakeWorkerStore, *fakeRWClient, *SyncWorker) {
	t.Helper()
	inboxRoot := t.TempDir()
	assetRoot := t.TempDir()
	tabDir := filepath.Join(inboxRoot, "king-board-games")
	if err := os.MkdirAll(tabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tabDir, "2026-06-16.webp"), []byte("RIFF....WEBP"), 0o600); err != nil {
		t.Fatal(err)
	}
	bs, _ := json.Marshal([]Button{{Label: "Play now", URL: "https://t.me/holdemblitz_bot"}})
	st := &fakeWorkerStore{ch: store.MpChannel{
		ID: uuid.New(), TenantID: uuid.New(), Handle: "@kingboardgamesofficial",
		BrandKey: "king-board-games", ButtonSet: bs, Launched: true, Enabled: true,
	}}
	client := &fakeRWClient{rows: map[string][]SheetRow{
		"king-board-games": {{
			Tab: "king-board-games", RowIndex: 2, Date: "2026-06-16", Status: "ready",
			KoText: "안녕", ImageFile: "2026-06-16.webp",
			Buttons: "Play now | https://t.me/holdemblitz_bot", ManagerApproved: true, ApprovedBy: "boss",
		}},
	}}
	w := &SyncWorker{
		Store: st, Client: client, TenantID: st.ch.TenantID,
		InboxRoot: inboxRoot, AssetRoot: assetRoot, ApprovedByFallback: "sheets-sync",
	}
	return st, client, w
}

func TestSyncWorker_IngestsApprovesAndWritesBack(t *testing.T) {
	st, client, w := workerFixture(t)
	rep, err := w.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if rep.Ingested != 1 || rep.Approved != 1 {
		t.Fatalf("want 1 ingest + 1 approve, got %+v", rep)
	}
	if len(st.posts) != 1 || st.posts[0].Status != store.MpPostApproved {
		t.Fatalf("expected one approved post, got %+v", st.posts)
	}
	if got := client.writes[2]; got.Status != "approved" {
		t.Fatalf("row 2 should be written back approved, got %+v", got)
	}
}

func TestSyncWorker_Idempotent(t *testing.T) {
	st, client, w := workerFixture(t)
	if _, err := w.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.writes = nil // forget first pass's write-backs

	rep, err := w.SyncOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Second pass must NOT create a duplicate post, and must not re-ingest/approve.
	if len(st.posts) != 1 {
		t.Fatalf("second pass duplicated posts: %d", len(st.posts))
	}
	if rep.Ingested != 0 || rep.Approved != 0 {
		t.Fatalf("second pass should not ingest/approve again: %+v", rep)
	}
	// It should still reconcile the sheet (write back the DB status).
	if got := client.writes[2]; got.Status != store.MpPostApproved {
		t.Fatalf("reconcile write-back should report approved, got %+v", got)
	}
}

func TestSyncWorker_BadRowWritesErrorNoCrash(t *testing.T) {
	st, client, w := workerFixture(t)
	bad := w.Client.(*fakeRWClient).rows["king-board-games"][0]
	bad.Date = "16/06/2026" // invalid → SyncError
	w.Client.(*fakeRWClient).rows["king-board-games"] = []SheetRow{bad}

	rep, err := w.SyncOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Errored != 1 || len(st.posts) != 0 {
		t.Fatalf("bad row must error with no post: %+v", rep)
	}
	if got := client.writes[2]; got.Status != "error" || got.Error == "" {
		t.Fatalf("bad row must write back an error: %+v", got)
	}
}

func TestSyncWorker_RunStopsOnContextCancel(t *testing.T) {
	_, _, w := workerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx, time.Hour); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on context cancel")
	}
}
