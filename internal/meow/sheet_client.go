package meow

import "context"

// RowResult is the publish/sync outcome written back to a sheet row so the sheet
// reflects Postgres (the execution ledger) without becoming the source of truth
// for results. Empty fields are left unchanged by the implementation.
type RowResult struct {
	Status      string // draft|approved|skipped|published|error
	TgMessageID string
	TgLink      string
	Error       string
}

// SheetClient is the minimal surface the Meow sheet bridge needs. The live
// implementation (deferred) wraps the Google Sheets API; tests use an in-memory
// fake. Tabs are addressed by name (= brand_key); rows by 1-based RowIndex.
type SheetClient interface {
	// ReadTab returns the parsed rows of one tab.
	ReadTab(ctx context.Context, tab string) ([]SheetRow, error)
	// WriteBack updates the result columns of the row at rowIndex (1-based).
	WriteBack(ctx context.Context, tab string, rowIndex int, result RowResult) error
}
