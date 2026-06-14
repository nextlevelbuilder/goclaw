package sheets

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/sheets/v4"

	"github.com/nextlevelbuilder/goclaw/internal/meow"
)

// checkboxMaxRows is how far down the manager_approved / publish_now columns get a
// checkbox data-validation rule applied.
const checkboxMaxRows = 1000

// Provision creates a brand-new approval-console spreadsheet, lays out its tabs,
// and shares it. NOTE: a service account can only do this on Google Workspace
// (Shared Drive) or via domain-wide delegation — a service account on a consumer
// project has zero Drive storage quota and Create returns 403. For a personal
// Google account, create the spreadsheet as a human, share it (Editor) with the
// service-account email, then call EnsureLayout instead.
func (c *Client) Provision(ctx context.Context, title string, tabs, shareWith []string) (id, url string, err error) {
	spec := make([]*sheets.Sheet, len(tabs))
	for i, t := range tabs {
		spec[i] = &sheets.Sheet{Properties: &sheets.SheetProperties{Title: t}}
	}
	ss, err := c.sheets.Spreadsheets.Create(&sheets.Spreadsheet{
		Properties: &sheets.SpreadsheetProperties{Title: title},
		Sheets:     spec,
	}).Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("create spreadsheet: %w", err)
	}
	c.spreadsheetID = ss.SpreadsheetId
	if err := c.EnsureLayout(ctx, tabs); err != nil {
		return ss.SpreadsheetId, ss.SpreadsheetUrl, err
	}
	if err := c.share(ctx, ss.SpreadsheetId, shareWith); err != nil {
		return ss.SpreadsheetId, ss.SpreadsheetUrl, err
	}
	return ss.SpreadsheetId, ss.SpreadsheetUrl, nil
}

// EnsureLayout makes the bound spreadsheet match the approval-console layout: it
// adds any missing tabs, (re)writes the canonical header row on each, and applies
// checkbox data-validation to the manager_approved / publish_now columns. It is
// idempotent and works on an existing spreadsheet the service account can edit
// (no Drive storage needed) — the path used for a human-created, SA-shared sheet.
func (c *Client) EnsureLayout(ctx context.Context, tabs []string) error {
	if c.spreadsheetID == "" {
		return fmt.Errorf("no spreadsheet bound")
	}
	existing, err := c.tabIDs(ctx)
	if err != nil {
		return err
	}
	var addReqs []*sheets.Request
	for _, t := range tabs {
		if _, ok := existing[t]; !ok {
			addReqs = append(addReqs, &sheets.Request{AddSheet: &sheets.AddSheetRequest{
				Properties: &sheets.SheetProperties{Title: t},
			}})
		}
	}
	if len(addReqs) > 0 {
		if _, err := c.sheets.Spreadsheets.BatchUpdate(c.spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
			Requests: addReqs,
		}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("add tabs: %w", err)
		}
		if existing, err = c.tabIDs(ctx); err != nil {
			return err
		}
	}
	if err := c.writeHeaders(ctx, tabs); err != nil {
		return err
	}
	return c.applyCheckboxes(ctx, tabs, existing)
}

// tabIDs returns the bound spreadsheet's tab title → numeric sheetId map.
func (c *Client) tabIDs(ctx context.Context) (map[string]int64, error) {
	ss, err := c.sheets.Spreadsheets.Get(c.spreadsheetID).Fields("sheets.properties(sheetId,title)").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get spreadsheet: %w", err)
	}
	out := make(map[string]int64, len(ss.Sheets))
	for _, sh := range ss.Sheets {
		out[sh.Properties.Title] = sh.Properties.SheetId
	}
	return out, nil
}

func (c *Client) writeHeaders(ctx context.Context, tabs []string) error {
	headers := meow.SheetHeaders()
	hdr := make([]any, len(headers))
	for i, h := range headers {
		hdr[i] = h
	}
	data := make([]*sheets.ValueRange, 0, len(tabs))
	for _, t := range tabs {
		data = append(data, &sheets.ValueRange{Range: a1Cell(t, "A1"), Values: [][]any{hdr}})
	}
	if _, err := c.sheets.Spreadsheets.Values.BatchUpdate(c.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             data,
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("write headers: %w", err)
	}
	return nil
}

// applyCheckboxes turns the manager_approved / publish_now columns into checkbox
// cells (rows 2..checkboxMaxRows) so a manager approves by ticking a box.
func (c *Client) applyCheckboxes(ctx context.Context, tabs []string, tabID map[string]int64) error {
	headers := meow.SheetHeaders()
	cols := []string{meow.ColManagerApproved, meow.ColPublishNow}
	var reqs []*sheets.Request
	for _, t := range tabs {
		sid, ok := tabID[t]
		if !ok {
			continue
		}
		for _, col := range cols {
			idx, ok := headerIndex(headers, col)
			if !ok {
				continue
			}
			reqs = append(reqs, &sheets.Request{SetDataValidation: &sheets.SetDataValidationRequest{
				Range: &sheets.GridRange{
					SheetId:          sid,
					StartRowIndex:    1, // skip header
					EndRowIndex:      checkboxMaxRows,
					StartColumnIndex: int64(idx),
					EndColumnIndex:   int64(idx + 1),
				},
				Rule: &sheets.DataValidationRule{Condition: &sheets.BooleanCondition{Type: "BOOLEAN"}},
			}})
		}
	}
	if len(reqs) == 0 {
		return nil
	}
	if _, err := c.sheets.Spreadsheets.BatchUpdate(c.spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: reqs,
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("apply checkbox validation: %w", err)
	}
	return nil
}

// DeleteTab removes a tab from the bound spreadsheet (a no-op if absent). Used to
// clean up a transient canary tab so the real console is left pristine.
func (c *Client) DeleteTab(ctx context.Context, tab string) error {
	if c.spreadsheetID == "" {
		return fmt.Errorf("no spreadsheet bound")
	}
	ids, err := c.tabIDs(ctx)
	if err != nil {
		return err
	}
	sid, ok := ids[tab]
	if !ok {
		return nil
	}
	if _, err := c.sheets.Spreadsheets.BatchUpdate(c.spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{{DeleteSheet: &sheets.DeleteSheetRequest{SheetId: sid}}},
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("delete tab %q: %w", tab, err)
	}
	return nil
}

// share grants writer access to each email (no notification email).
func (c *Client) share(ctx context.Context, fileID string, emails []string) error {
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		if _, err := c.drive.Permissions.Create(fileID, &drive.Permission{
			Type:         "user",
			Role:         "writer",
			EmailAddress: email,
		}).SendNotificationEmail(false).Context(ctx).Do(); err != nil {
			return fmt.Errorf("share with %q: %w", email, err)
		}
	}
	return nil
}
