package meow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeIngestStore struct {
	store.MeowStore
	ch       *store.MpChannel
	notFound bool
	upserts  []*store.MpContentPost
}

func (f *fakeIngestStore) GetChannelByHandle(_ context.Context, _ uuid.UUID, _ string) (*store.MpChannel, error) {
	if f.notFound {
		return nil, store.ErrMeowChannelNotFound
	}
	return f.ch, nil
}
func (f *fakeIngestStore) UpsertDraftPost(_ context.Context, p *store.MpContentPost) error {
	p.ID = uuid.New()
	f.upserts = append(f.upserts, p)
	return nil
}

func ingestFixture(t *testing.T) (*fakeIngestStore, string, string) {
	t.Helper()
	bundleDir := t.TempDir()
	assetRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "2026-06-16.webp"), []byte("RIFF....WEBP"), 0o600); err != nil {
		t.Fatal(err)
	}
	bs, _ := json.Marshal([]Button{{Label: "Play now", URL: "https://t.me/holdemblitz_bot"}})
	st := &fakeIngestStore{ch: &store.MpChannel{
		ID: uuid.New(), TenantID: uuid.New(), Handle: "@kingboardgamesofficial",
		BrandKey: "king-board-games", ButtonSet: bs,
	}}
	return st, bundleDir, assetRoot
}

func goodBundle() *DraftBundle {
	return &DraftBundle{
		Handle: "@kingboardgamesofficial", ScheduledDate: "2026-06-16",
		KoText: "안녕", EnText: "hi", Image: "2026-06-16.webp",
		Buttons: []Button{{Label: "Play now", URL: "https://t.me/holdemblitz_bot"}},
	}
}

func TestIngestDraft_Valid(t *testing.T) {
	st, bundleDir, assetRoot := ingestFixture(t)
	res, err := IngestDraft(context.Background(), st, st.ch.TenantID, goodBundle(), bundleDir, assetRoot)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Held {
		t.Fatalf("valid bundle should not hold: %s", res.Reason)
	}
	// image_path validates inside the asset root (the production publish check)…
	if _, err := ValidateImagePath(res.ImagePath, []string{assetRoot}); err != nil {
		t.Fatalf("image_path must validate inside the asset root: %v", err)
	}
	// …and is NOT the source/bundle path.
	if _, err := ValidateImagePath(res.ImagePath, []string{bundleDir}); err == nil {
		t.Fatal("image_path must not resolve inside the bundle/source dir")
	}
	if _, err := os.Stat(res.ImagePath); err != nil {
		t.Fatalf("image not transported: %v", err)
	}
	if len(st.upserts) != 1 || st.upserts[0].Status != store.MpPostDraft {
		t.Fatalf("expected one draft upsert, got %d", len(st.upserts))
	}
}

func TestIngestDraft_MissingImageHolds(t *testing.T) {
	st, bundleDir, assetRoot := ingestFixture(t)
	b := goodBundle()
	b.Image = "nope.webp"
	res, err := IngestDraft(context.Background(), st, st.ch.TenantID, b, bundleDir, assetRoot)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Held || len(st.upserts) != 0 {
		t.Fatalf("missing image must hold with no draft (held=%v upserts=%d)", res.Held, len(st.upserts))
	}
}

func TestIngestDraft_TraversalHolds(t *testing.T) {
	st, bundleDir, assetRoot := ingestFixture(t)
	b := goodBundle()
	b.Image = "../escape.webp" // rejected structurally
	res, _ := IngestDraft(context.Background(), st, st.ch.TenantID, b, bundleDir, assetRoot)
	if !res.Held || len(st.upserts) != 0 {
		t.Fatal("traversal image must hold with no draft")
	}
}

func TestIngestDraft_BadButtonHolds(t *testing.T) {
	st, bundleDir, assetRoot := ingestFixture(t)
	b := goodBundle()
	b.Buttons = []Button{{Label: "Phish", URL: "https://t.me/attacker_bot"}} // host ok, NOT registered
	res, _ := IngestDraft(context.Background(), st, st.ch.TenantID, b, bundleDir, assetRoot)
	if !res.Held || len(st.upserts) != 0 {
		t.Fatal("off-allowlist button must hold with no draft")
	}
}

func TestIngestDraft_UnknownChannelHolds(t *testing.T) {
	st, bundleDir, assetRoot := ingestFixture(t)
	st.notFound = true
	res, _ := IngestDraft(context.Background(), st, st.ch.TenantID, goodBundle(), bundleDir, assetRoot)
	if !res.Held || len(st.upserts) != 0 {
		t.Fatal("unknown channel must hold with no draft")
	}
}

func TestIngestDraft_NonWebPHolds(t *testing.T) {
	st, bundleDir, assetRoot := ingestFixture(t)
	// Overwrite the fixture image with non-WebP content (still named .webp).
	if err := os.WriteFile(filepath.Join(bundleDir, "2026-06-16.webp"), []byte("totally-not-webp"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := IngestDraft(context.Background(), st, st.ch.TenantID, goodBundle(), bundleDir, assetRoot)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Held || len(st.upserts) != 0 {
		t.Fatalf("non-webp image must hold with no draft (held=%v upserts=%d)", res.Held, len(st.upserts))
	}
}
