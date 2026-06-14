package meow

// SyncActionKind classifies what the inbound (Sheets → Postgres) sync does with
// one sheet row.
type SyncActionKind int

const (
	// SyncSkip: the row is not eligible (not approved) or is already managed by
	// Postgres (terminal status). No DB change.
	SyncSkip SyncActionKind = iota
	// SyncIngest: an eligible, structurally valid row — ingest it as a draft, then
	// approve. The carried Bundle is still validated by IngestDraft when applied.
	SyncIngest
	// SyncError: an approved row that is structurally invalid; Reason explains. No
	// DB change — the reason is the write-back error for the operator.
	SyncError
)

// SyncAction is the planned inbound action for one sheet row. It is intent only:
// PlanSync performs no IO. An executor applies SyncIngest via IngestDraft +
// ApprovePost, and may write a SyncError Reason back to the sheet.
type SyncAction struct {
	Tab        string
	Date       string
	RowIndex   int
	Kind       SyncActionKind
	Bundle     *DraftBundle // set for SyncIngest
	Approve    bool         // SyncIngest: approve the draft after upsert
	ApprovedBy string       // who approved (sheet approved_by; may be empty)
	Reason     string       // SyncSkip/SyncError explanation
}

// terminalSheetStatus reports whether a row's status means Postgres already owns
// its lifecycle, so inbound sync leaves it alone (avoids re-ingesting a published
// or skipped row).
func terminalSheetStatus(status string) bool {
	switch status {
	case "published", "publishing", "skipped", "error":
		return true
	}
	return false
}

// PlanSync classifies each row into an inbound SyncAction. It is pure (no IO).
// resolveHandle maps a tab (brand_key) to its channel handle; an unknown tab on
// an approved row yields SyncError. Only manager_approved rows are eligible, and
// eligibility never bypasses validation — the bundle built here is validated both
// now (structurally) and again by IngestDraft when the action is applied.
func PlanSync(rows []SheetRow, resolveHandle func(tab string) (string, bool)) []SyncAction {
	actions := make([]SyncAction, 0, len(rows))
	for _, r := range rows {
		a := SyncAction{Tab: r.Tab, Date: r.Date, RowIndex: r.RowIndex}
		switch {
		case !r.ManagerApproved:
			a.Kind, a.Reason = SyncSkip, "not approved"
		case terminalSheetStatus(r.Status):
			a.Kind, a.Reason = SyncSkip, "terminal status "+r.Status
		default:
			a = planApprovedRow(r, resolveHandle)
		}
		actions = append(actions, a)
	}
	return actions
}

// planApprovedRow builds the action for an eligible (manager_approved) row:
// resolve the channel, convert to a bundle, and structurally validate. Any
// failure becomes SyncError with an operator-facing reason.
func planApprovedRow(r SheetRow, resolveHandle func(tab string) (string, bool)) SyncAction {
	a := SyncAction{Tab: r.Tab, Date: r.Date, RowIndex: r.RowIndex, Kind: SyncError}
	handle, ok := resolveHandle(r.Tab)
	if !ok {
		a.Reason = "unknown channel tab " + r.Tab
		return a
	}
	b, err := r.ToDraftBundle(handle)
	if err != nil {
		a.Reason = err.Error()
		return a
	}
	if err := b.Validate(); err != nil {
		a.Reason = err.Error()
		return a
	}
	a.Kind = SyncIngest
	a.Bundle = b
	a.Approve = true
	a.ApprovedBy = r.ApprovedBy
	return a
}
