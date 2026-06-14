//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/meow/sheets"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/testutil"
)

// TestMeowSheetsCanary is the one-tab live dry-run: real PGMeowStore + the real
// Google Sheet. It seeds one channel, stages one image, writes one approved row to
// the king-board-games tab, runs the sync worker, and asserts exactly one approved
// DB post + sheet status=approved — then proves a second pass is idempotent. Skips
// unless the Sheets credentials + spreadsheet id are in the env. Cleans up the
// sheet row + DB posts it created.
func TestMeowSheetsCanary(t *testing.T) {
	credFile := os.Getenv("GOCLAW_MEOW_SHEETS_CREDENTIALS_FILE")
	ssID := os.Getenv("GOCLAW_MEOW_SHEETS_SPREADSHEET_ID")
	if credFile == "" || ssID == "" {
		t.Skip("set GOCLAW_MEOW_SHEETS_CREDENTIALS_FILE + GOCLAW_MEOW_SHEETS_SPREADSHEET_ID to run the live canary")
	}

	db := testutil.TestDB(t, "../../migrations")
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'canary', 'canary', 'active')
		 ON CONFLICT (id) DO NOTHING`, store.MasterTenantID); err != nil {
		t.Fatalf("seed master tenant: %v", err)
	}

	pg.InitSqlx(db) // sqlx-based store methods (GetChannelByHandle, ListPostsByChannel) need this
	st := pg.NewPGMeowStore(db)
	const (
		tab        = "king-board-games"
		handle     = "@kingboardgamesofficial"
		canaryDate = "2030-01-02" // far-future so it never collides with real posts
		btnURL     = "https://t.me/holdemblitz_bot"
	)
	buttons, _ := json.Marshal([]meow.Button{{Label: "Play now", URL: btnURL}})

	if err := st.UpsertChannel(ctx, &store.MpChannel{
		TenantID: store.MasterTenantID, Handle: handle, BrandKey: tab,
		TZ: "Asia/Seoul", Launched: true, Enabled: true, ButtonSet: buttons,
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	ch, err := st.GetChannelByHandle(ctx, store.MasterTenantID, handle)
	if err != nil {
		t.Fatalf("get channel: %v", err)
	}

	// Pre-clean any canary-day posts from a prior run so the first pass ingests.
	cleanPosts := func() {
		db.ExecContext(context.Background(),
			`DELETE FROM mp_content_posts WHERE tenant_id=$1 AND channel_id=$2 AND scheduled_date=$3::date`,
			store.MasterTenantID, ch.ID, canaryDate)
	}
	cleanPosts()

	// Stage the source image in a temp inbox (mirrors <inbox>/<brand>/<date>.webp).
	inbox := t.TempDir()
	asset := t.TempDir()
	if err := os.MkdirAll(filepath.Join(inbox, tab), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, tab, canaryDate+".webp"), []byte("RIFF....WEBP"), 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := sheets.New(ctx, sheets.Config{CredentialsFile: credFile, SpreadsheetID: ssID})
	if err != nil {
		t.Fatalf("sheets client: %v", err)
	}

	// Write one approved row, and register cleanup of the sheet row + DB posts.
	const rowIdx = 2
	if err := client.WriteRow(ctx, tab, rowIdx, meow.SheetRow{
		Date: canaryDate, Status: "ready", KoText: "canary dry-run",
		ImageFile: canaryDate + ".webp", Buttons: "Play now | " + btnURL,
		ManagerApproved: true, ApprovedBy: "canary",
	}); err != nil {
		t.Fatalf("write row: %v", err)
	}
	t.Cleanup(func() {
		client.WriteRow(context.Background(), tab, rowIdx, meow.SheetRow{}) // blank the row
		cleanPosts()
	})

	worker := &meow.SyncWorker{
		Store: st, Client: client, TenantID: store.MasterTenantID,
		InboxRoot: inbox, AssetRoot: asset, ApprovedByFallback: "sheets-sync",
		Tabs: []string{tab},
	}

	// Pass 1: ingest + approve.
	rep, err := worker.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("SyncOnce pass 1: %v", err)
	}
	if rep.Ingested != 1 || rep.Approved != 1 {
		t.Fatalf("pass 1 want 1 ingest + 1 approve, got %+v", rep)
	}

	approvedCount := func() (int, store.MpContentPost) {
		posts, e := st.ListPostsByChannel(ctx, store.MasterTenantID, ch.ID)
		if e != nil {
			t.Fatalf("list posts: %v", e)
		}
		n := 0
		var found store.MpContentPost
		for _, p := range posts {
			if p.ScheduledDate.Format("2006-01-02") == canaryDate {
				n++
				found = p
			}
		}
		return n, found
	}
	n, post := approvedCount()
	if n != 1 || post.Status != store.MpPostApproved || post.ApprovedBy != "canary" {
		t.Fatalf("want exactly one approved post (approved_by=canary), got n=%d post=%+v", n, post)
	}

	// Sheet reflects DB: row status written back as approved.
	rows, err := client.ReadTab(ctx, tab)
	if err != nil {
		t.Fatalf("read tab: %v", err)
	}
	var sheetStatus string
	for _, r := range rows {
		if r.Date == canaryDate {
			sheetStatus = r.Status
		}
	}
	if sheetStatus != "approved" {
		t.Fatalf("sheet row should be written back approved, got %q", sheetStatus)
	}

	// Pass 2: idempotent — no new ingest/approve, still exactly one post.
	rep2, err := worker.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("SyncOnce pass 2: %v", err)
	}
	if rep2.Ingested != 0 || rep2.Approved != 0 {
		t.Fatalf("pass 2 should be idempotent, got %+v", rep2)
	}
	if n2, _ := approvedCount(); n2 != 1 {
		t.Fatalf("pass 2 duplicated posts: %d", n2)
	}

	t.Log("CANARY OK: 1 approved DB post + sheet status=approved; idempotent on re-run")
}
