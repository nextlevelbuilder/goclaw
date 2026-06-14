// Package sheets is the live Google Sheets/Drive implementation of the Meow
// meow.SheetClient seam. It is deliberately isolated from the pure meow core so
// the core stays dependency-free and unit-testable; only this package imports
// google.golang.org/api. The Meow Publishing Queue spreadsheet is the human
// approval console: Meow writes draft rows, a manager flips manager_approved, and
// the sync reads approved rows + writes publish results back.
package sheets

import (
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"github.com/nextlevelbuilder/goclaw/internal/meow"
)

// scopes: spreadsheets (read/write cells) + drive.file (create/share only files
// this service account itself creates — least privilege; it cannot read or modify
// any other Drive file).
var scopes = []string{sheets.SpreadsheetsScope, drive.DriveFileScope}

// Config holds credentials + target for the sheet bridge. Credential precedence
// mirrors the Vertex provider: CredentialsJSON > CredentialsFile > ADC.
type Config struct {
	CredentialsJSON string // inline service-account JSON (from env/DB)
	CredentialsFile string // path to service-account JSON. OPERATOR-ONLY: read directly from disk
	SpreadsheetID   string // existing spreadsheet to sync; empty until Provision creates one
}

// Client implements meow.SheetClient against live Google APIs.
type Client struct {
	sheets        *sheets.Service
	drive         *drive.Service
	spreadsheetID string
}

var _ meow.SheetClient = (*Client)(nil)

// New builds a Client from cfg. It resolves credentials, then constructs the
// Sheets + Drive services.
func New(ctx context.Context, cfg Config) (*Client, error) {
	creds, err := resolveCredentials(ctx, cfg)
	if err != nil {
		return nil, err
	}
	ss, err := sheets.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("sheets service: %w", err)
	}
	dr, err := drive.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("drive service: %w", err)
	}
	return &Client{sheets: ss, drive: dr, spreadsheetID: strings.TrimSpace(cfg.SpreadsheetID)}, nil
}

// SpreadsheetID returns the id the client is bound to (set via Config or Provision).
func (c *Client) SpreadsheetID() string { return c.spreadsheetID }

func resolveCredentials(ctx context.Context, cfg Config) (*google.Credentials, error) {
	if data := strings.TrimSpace(cfg.CredentialsJSON); data != "" {
		creds, err := google.CredentialsFromJSON(ctx, []byte(data), scopes...)
		if err != nil {
			return nil, fmt.Errorf("parse inline credentials: %w", err)
		}
		return creds, nil
	}
	if path := strings.TrimSpace(cfg.CredentialsFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read credentials file %q: %w", path, err)
		}
		creds, err := google.CredentialsFromJSON(ctx, data, scopes...)
		if err != nil {
			return nil, fmt.Errorf("parse credentials file %q: %w", path, err)
		}
		return creds, nil
	}
	creds, err := google.FindDefaultCredentials(ctx, scopes...)
	if err != nil {
		return nil, fmt.Errorf("application default credentials not found (set credentials): %w", err)
	}
	return creds, nil
}

// ReadTab reads a tab's grid, treats row 1 as headers, and returns parsed rows.
// RowIndex is the 1-based sheet row (header = 1, data starts at 2) so write-back
// targets the right cell.
func (c *Client) ReadTab(ctx context.Context, tab string) ([]meow.SheetRow, error) {
	if c.spreadsheetID == "" {
		return nil, fmt.Errorf("no spreadsheet bound")
	}
	resp, err := c.sheets.Spreadsheets.Values.Get(c.spreadsheetID, a1Range(tab)).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read tab %q: %w", tab, err)
	}
	if len(resp.Values) == 0 {
		return nil, nil
	}
	headers := toStrings(resp.Values[0])
	rows := make([]meow.SheetRow, 0, len(resp.Values)-1)
	for i := 1; i < len(resp.Values); i++ {
		rows = append(rows, meow.ParseSheetRow(tab, i+1, headers, toStrings(resp.Values[i])))
	}
	return rows, nil
}

// WriteBack updates the result columns (status/tg_message_id/tg_link/error) of the
// row at rowIndex. It maps columns by header name (so a reordered sheet still
// targets the right cell) and writes only the non-empty RowResult fields, never
// clobbering a cell with a blank.
func (c *Client) WriteBack(ctx context.Context, tab string, rowIndex int, r meow.RowResult) error {
	if c.spreadsheetID == "" {
		return fmt.Errorf("no spreadsheet bound")
	}
	headers, err := c.headerRow(ctx, tab)
	if err != nil {
		return err
	}
	var data []*sheets.ValueRange
	put := func(col, val string) {
		if val == "" {
			return
		}
		idx, ok := headerIndex(headers, col)
		if !ok {
			return
		}
		data = append(data, &sheets.ValueRange{
			Range:  a1Cell(tab, fmt.Sprintf("%s%d", colLetter(idx), rowIndex)),
			Values: [][]any{{val}},
		})
	}
	put(meow.ColStatus, r.Status)
	put(meow.ColTgMessageID, r.TgMessageID)
	put(meow.ColTgLink, r.TgLink)
	put(meow.ColError, r.Error)
	if len(data) == 0 {
		return nil
	}
	_, err = c.sheets.Spreadsheets.Values.BatchUpdate(c.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             data,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("write-back tab %q row %d: %w", tab, rowIndex, err)
	}
	return nil
}

// WriteRow writes a full canonical row (all columns in meow.SheetHeaders order)
// at the 1-based rowIndex. This is the outbound half of the bridge: Meow publishes
// a prepared calendar row into the sheet for human approval. Checkbox columns are
// written as booleans so they render as ticked/unticked boxes.
func (c *Client) WriteRow(ctx context.Context, tab string, rowIndex int, row meow.SheetRow) error {
	if c.spreadsheetID == "" {
		return fmt.Errorf("no spreadsheet bound")
	}
	rng := a1Cell(tab, fmt.Sprintf("A%d", rowIndex))
	_, err := c.sheets.Spreadsheets.Values.Update(c.spreadsheetID, rng, &sheets.ValueRange{
		Values: [][]any{rowValues(row)},
	}).ValueInputOption("USER_ENTERED").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("write row tab %q row %d: %w", tab, rowIndex, err)
	}
	return nil
}

// rowValues lays out a SheetRow's fields in the canonical header order.
func rowValues(r meow.SheetRow) []any {
	return []any{
		r.Date, r.Status, r.KoText, r.EnText, r.ImagePrompt, r.ImageFile,
		r.Buttons, r.ManagerApproved, r.ApprovedBy, r.ApprovedAt,
		r.PublishNow, r.TgMessageID, r.TgLink, r.Error,
	}
}

// headerRow returns the first row of a tab as strings.
func (c *Client) headerRow(ctx context.Context, tab string) ([]string, error) {
	resp, err := c.sheets.Spreadsheets.Values.Get(c.spreadsheetID, a1Cell(tab, "1:1")).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read header of tab %q: %w", tab, err)
	}
	if len(resp.Values) == 0 {
		return nil, fmt.Errorf("tab %q has no header row", tab)
	}
	return toStrings(resp.Values[0]), nil
}
