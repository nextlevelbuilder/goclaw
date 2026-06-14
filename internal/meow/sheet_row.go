package meow

import (
	"fmt"
	"strings"
)

// Sheet column headers — the canonical header names in the Meow Publishing Queue
// spreadsheet. Rows are mapped to fields by header name (case-insensitive,
// trimmed), so a human reordering columns in the sheet never corrupts the
// mapping. One sheet tab = one channel (tab name = brand_key); one row = one
// planned post for a channel-day.
const (
	ColDate            = "date"
	ColStatus          = "status"
	ColKoText          = "ko_text"
	ColEnText          = "en_text"
	ColImagePrompt     = "image_prompt"
	ColImageFile       = "image_file"
	ColButtons         = "buttons"
	ColManagerApproved = "manager_approved"
	ColApprovedBy      = "approved_by"
	ColApprovedAt      = "approved_at"
	ColPublishNow      = "publish_now"
	ColTgMessageID     = "tg_message_id"
	ColTgLink          = "tg_link"
	ColError           = "error"
)

// SheetRow is one parsed row of a channel tab. Tab is the tab name (brand_key);
// RowIndex is the 1-based sheet row used to target write-back.
type SheetRow struct {
	Tab      string
	RowIndex int

	Date            string
	Status          string
	KoText          string
	EnText          string
	ImagePrompt     string
	ImageFile       string
	Buttons         string // raw cell: one "Label | https://url" per line
	ManagerApproved bool
	ApprovedBy      string
	ApprovedAt      string
	PublishNow      bool
	TgMessageID     string
	TgLink          string
	Error           string
}

// ParseSheetRow maps cells onto a SheetRow using headers (matched
// case-insensitively, trimmed). A missing column is empty; extra cells are
// ignored. Status is lowercased so comparisons against the post-status constants
// are stable. tab is the tab name and rowIndex the 1-based sheet row.
func ParseSheetRow(tab string, rowIndex int, headers, cells []string) SheetRow {
	get := func(name string) string {
		for i, h := range headers {
			if i < len(cells) && strings.EqualFold(strings.TrimSpace(h), name) {
				return strings.TrimSpace(cells[i])
			}
		}
		return ""
	}
	return SheetRow{
		Tab:             tab,
		RowIndex:        rowIndex,
		Date:            get(ColDate),
		Status:          strings.ToLower(get(ColStatus)),
		KoText:          get(ColKoText),
		EnText:          get(ColEnText),
		ImagePrompt:     get(ColImagePrompt),
		ImageFile:       get(ColImageFile),
		Buttons:         get(ColButtons),
		ManagerApproved: parseCheckbox(get(ColManagerApproved)),
		ApprovedBy:      get(ColApprovedBy),
		ApprovedAt:      get(ColApprovedAt),
		PublishNow:      parseCheckbox(get(ColPublishNow)),
		TgMessageID:     get(ColTgMessageID),
		TgLink:          get(ColTgLink),
		Error:           get(ColError),
	}
}

// parseCheckbox interprets a Sheets checkbox/boolean cell. Google returns checkbox
// cells as "TRUE"/"FALSE"; accept common truthy spellings, everything else false.
func parseCheckbox(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "y", "✓", "checked":
		return true
	}
	return false
}

// ParseButtons parses the buttons cell: one button per line, "Label | URL". Blank
// lines are skipped. A line without a "|" separator or with an empty label/url is
// an error, so a malformed cell holds the row rather than silently dropping a
// button (the URL is allowlist-checked later, at ingest).
func ParseButtons(cell string) ([]Button, error) {
	var out []Button
	for _, line := range strings.Split(cell, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		label, url, ok := strings.Cut(line, "|")
		if !ok {
			return nil, fmt.Errorf("button line %q must be \"Label | https://url\"", line)
		}
		label, url = strings.TrimSpace(label), strings.TrimSpace(url)
		if label == "" || url == "" {
			return nil, fmt.Errorf("button line %q has empty label or url", line)
		}
		out = append(out, Button{Label: label, URL: url})
	}
	return out, nil
}

// ToDraftBundle converts the row to a DraftBundle for the given channel handle so
// the existing ingest path applies all content validation (channel, button
// allowlist, image presence/WebP). It returns an error only for a malformed
// buttons cell; the bundle's own Validate (run by IngestDraft) covers the rest.
func (r SheetRow) ToDraftBundle(handle string) (*DraftBundle, error) {
	buttons, err := ParseButtons(r.Buttons)
	if err != nil {
		return nil, err
	}
	return &DraftBundle{
		Handle:        handle,
		ScheduledDate: r.Date,
		KoText:        r.KoText,
		EnText:        r.EnText,
		Image:         r.ImageFile,
		Buttons:       buttons,
	}, nil
}
