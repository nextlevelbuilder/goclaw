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

// fakeExecStore implements the MeowStore methods IngestDraft + ApprovePost call.
type fakeExecStore struct {
	store.MeowStore
	ch       *store.MpChannel
	upserts  []*store.MpContentPost
	approved []uuid.UUID
}

func (f *fakeExecStore) GetChannelByHandle(_ context.Context, _ uuid.UUID, _ string) (*store.MpChannel, error) {
	return f.ch, nil
}
func (f *fakeExecStore) UpsertDraftPost(_ context.Context, p *store.MpContentPost) error {
	p.ID = uuid.New()
	f.upserts = append(f.upserts, p)
	return nil
}
func (f *fakeExecStore) ApprovePost(_ context.Context, _, id uuid.UUID, _ string) error {
	f.approved = append(f.approved, id)
	return nil
}

// execFixture wires a fake store for @kingboardgamesofficial plus an inbox root
// holding the per-tab source image the sheet row references.
func execFixture(t *testing.T) (*fakeExecStore, string, string) {
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
	st := &fakeExecStore{ch: &store.MpChannel{
		ID: uuid.New(), TenantID: uuid.New(), Handle: "@kingboardgamesofficial",
		BrandKey: "king-board-games", ButtonSet: bs,
	}}
	return st, inboxRoot, assetRoot
}

func execRow() SheetRow {
	return SheetRow{
		Tab: "king-board-games", RowIndex: 2, Date: "2026-06-16", Status: "ready",
		KoText: "안녕", ImageFile: "2026-06-16.webp",
		Buttons: "Play now | https://t.me/holdemblitz_bot", ManagerApproved: true, ApprovedBy: "boss",
	}
}

func resolveKBG(tab string) (string, bool) {
	if tab == "king-board-games" {
		return "@kingboardgamesofficial", true
	}
	return "", false
}

func TestApplySync_IngestsAndApproves(t *testing.T) {
	st, inboxRoot, assetRoot := execFixture(t)
	actions := PlanSync([]SheetRow{execRow()}, resolveKBG)
	rep := ApplySync(context.Background(), st, st.ch.TenantID, actions, inboxRoot, assetRoot, "sheets-sync")

	if rep.Ingested != 1 || rep.Approved != 1 {
		t.Fatalf("want 1 ingest + 1 approve, got %+v", rep)
	}
	if len(st.upserts) != 1 || st.upserts[0].Status != store.MpPostDraft {
		t.Fatalf("expected one draft upsert: %+v", st.upserts)
	}
	if len(st.approved) != 1 || st.approved[0] != st.upserts[0].ID {
		t.Fatalf("approved id must match the upserted draft: %+v / %+v", st.approved, st.upserts)
	}
	if len(rep.Outcomes) != 1 || rep.Outcomes[0].Result.Status != "approved" || rep.Outcomes[0].RowIndex != 2 {
		t.Fatalf("outcome should be approved on row 2: %+v", rep.Outcomes)
	}
}

func TestApplySync_HeldOnMissingImage(t *testing.T) {
	st, inboxRoot, assetRoot := execFixture(t)
	r := execRow()
	r.ImageFile = "nope.webp" // source not present in the inbox tab dir
	actions := PlanSync([]SheetRow{r}, resolveKBG)
	rep := ApplySync(context.Background(), st, st.ch.TenantID, actions, inboxRoot, assetRoot, "sheets-sync")

	if rep.Held != 1 || rep.Ingested != 0 || len(st.upserts) != 0 || len(st.approved) != 0 {
		t.Fatalf("missing image must hold with no draft/approve: %+v", rep)
	}
	if rep.Outcomes[0].Result.Status != "error" || rep.Outcomes[0].Result.Error == "" {
		t.Fatalf("held row must write back an error: %+v", rep.Outcomes[0])
	}
}

func TestApplySync_SkipAndErrorClassification(t *testing.T) {
	st, inboxRoot, assetRoot := execFixture(t)
	notApproved := execRow()
	notApproved.ManagerApproved = false
	unknownTab := execRow()
	unknownTab.Tab = "no-such"

	actions := PlanSync([]SheetRow{notApproved, unknownTab}, resolveKBG)
	rep := ApplySync(context.Background(), st, st.ch.TenantID, actions, inboxRoot, assetRoot, "sheets-sync")

	if rep.Skipped != 1 || rep.Errored != 1 {
		t.Fatalf("want 1 skip + 1 error, got %+v", rep)
	}
	// Skip writes nothing back; only the error row produces an outcome.
	if len(rep.Outcomes) != 1 || rep.Outcomes[0].Result.Status != "error" {
		t.Fatalf("only the error row should have an outcome: %+v", rep.Outcomes)
	}
}
