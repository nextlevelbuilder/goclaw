package cmd

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/meow/sheets"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// startMeowSheetsSync starts the Meow Google Sheets approval-bridge sync worker
// when enabled. It is a no-op (logged) when disabled, the Meow store is absent
// (Lite/non-PG), or no spreadsheet id is configured. Credentials come from env
// only — never config.json. The worker reads approved rows and writes results
// back; it is sync-only (no Telegram posting) unless meow_sheets.publish is on,
// in which case a row's publish_now triggers a live, exactly-once publish. It
// stops when ctx is cancelled.
func startMeowSheetsSync(ctx context.Context, cfg *config.Config, meowStore store.MeowStore, channelMgr *channels.Manager) {
	if !cfg.MeowSheets.Enabled {
		slog.Info("meow sheets sync disabled")
		return
	}
	if meowStore == nil {
		slog.Warn("meow sheets sync skipped: no Meow store (PG/Standard only)")
		return
	}
	if cfg.MeowSheets.SpreadsheetID == "" {
		slog.Warn("meow sheets sync skipped: GOCLAW_MEOW_SHEETS_SPREADSHEET_ID / meow_sheets.spreadsheet_id not set")
		return
	}

	client, err := sheets.New(ctx, sheets.Config{
		CredentialsJSON: os.Getenv("GOCLAW_MEOW_SHEETS_CREDENTIALS_JSON"),
		CredentialsFile: os.Getenv("GOCLAW_MEOW_SHEETS_CREDENTIALS_FILE"),
		SpreadsheetID:   cfg.MeowSheets.SpreadsheetID,
	})
	if err != nil {
		slog.Error("meow sheets sync disabled: client init failed", "error", err)
		return
	}

	dataDir := config.ResolvedDataDirFromEnv()
	assetRoot := filepath.Join(dataDir, "meow-assets")
	worker := &meow.SyncWorker{
		Store:              meowStore,
		Client:             client,
		TenantID:           store.MasterTenantID,
		InboxRoot:          filepath.Join(dataDir, "meow-inbox"),
		AssetRoot:          assetRoot,
		ApprovedByFallback: cfg.MeowSheets.ApprovedBy(),
		Tabs:               cfg.MeowSheets.Tabs,
	}
	// Live publish (publish_now) requires the master switch AND a wired channel
	// manager. Off → sync-only (ingest + approve), no Telegram posting.
	publish := cfg.MeowSheets.Publish && channelMgr != nil
	if publish {
		worker.Publisher = tools.NewMeowPublisher(meowStore, channelMgr, channels.TypeTelegram, []string{assetRoot})
	}
	interval := cfg.MeowSheets.SyncInterval()
	slog.Info("meow sheets sync enabled", "spreadsheet", cfg.MeowSheets.SpreadsheetID, "interval", interval.String(), "publish", publish)
	go worker.Run(ctx, interval)
}
