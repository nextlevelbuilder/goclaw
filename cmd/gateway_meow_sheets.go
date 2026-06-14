package cmd

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/meow/sheets"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// startMeowSheetsSync starts the Meow Google Sheets approval-bridge sync worker
// when enabled. It is a no-op (logged) when disabled, the Meow store is absent
// (Lite/non-PG), or no spreadsheet id is configured. Credentials come from env
// only — never config.json. The worker reads approved rows and writes results
// back; it never publishes to Telegram. It stops when ctx is cancelled.
func startMeowSheetsSync(ctx context.Context, cfg *config.Config, meowStore store.MeowStore) {
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
	worker := &meow.SyncWorker{
		Store:              meowStore,
		Client:             client,
		TenantID:           store.MasterTenantID,
		InboxRoot:          filepath.Join(dataDir, "meow-inbox"),
		AssetRoot:          filepath.Join(dataDir, "meow-assets"),
		ApprovedByFallback: cfg.MeowSheets.ApprovedBy(),
		Tabs:               cfg.MeowSheets.Tabs,
	}
	interval := cfg.MeowSheets.SyncInterval()
	slog.Info("meow sheets sync enabled", "spreadsheet", cfg.MeowSheets.SpreadsheetID, "interval", interval.String())
	go worker.Run(ctx, interval)
}
