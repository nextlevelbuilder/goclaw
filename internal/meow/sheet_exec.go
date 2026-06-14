package meow

import (
	"context"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SyncOutcome is the per-row result of applying a SyncAction: the RowResult to
// write back to the sheet so it mirrors Postgres (the execution ledger).
type SyncOutcome struct {
	Tab      string
	Date     string
	RowIndex int
	Result   RowResult
}

// SyncReport aggregates the outcomes of applying a batch of actions.
type SyncReport struct {
	Ingested  int
	Approved  int
	Published int // live-published via the publish_now trigger
	Skipped   int
	Held      int // ingest found a content problem at apply time (no draft row)
	Errored   int // planner error or apply failure
	Outcomes  []SyncOutcome
}

// ApplySync executes inbound SyncActions against the store using the existing
// IngestDraft + ApprovePost path — so every synced row passes the SAME validation
// as /meow ingest. It performs no Google IO: it returns write-back outcomes the
// caller hands to a SheetClient. The source image for a row lives at
// <inboxRoot>/<tab>/<image_file> (mirrors the manual inbox convention); images
// are transported into assetRoot by IngestDraft. approvedByFallback is used when a
// row's approved_by cell is empty.
func ApplySync(ctx context.Context, st store.MeowStore, tenantID uuid.UUID, actions []SyncAction, inboxRoot, assetRoot, approvedByFallback string) SyncReport {
	var rep SyncReport
	for _, a := range actions {
		out := SyncOutcome{Tab: a.Tab, Date: a.Date, RowIndex: a.RowIndex}
		switch a.Kind {
		case SyncSkip:
			rep.Skipped++
			// No write-back: a skipped (not-approved / terminal) row is left as-is.
			continue
		case SyncError:
			rep.Errored++
			out.Result = RowResult{Status: "error", Error: a.Reason}
		case SyncIngest:
			out.Result = applyIngest(ctx, st, tenantID, a, inboxRoot, assetRoot, approvedByFallback, &rep)
		}
		rep.Outcomes = append(rep.Outcomes, out)
	}
	return rep
}

// applyIngest runs IngestDraft for one action and, on success, approves the draft.
// It updates rep counters and returns the RowResult to write back.
func applyIngest(ctx context.Context, st store.MeowStore, tenantID uuid.UUID, a SyncAction, inboxRoot, assetRoot, approvedByFallback string, rep *SyncReport) RowResult {
	bundleDir := filepath.Join(inboxRoot, a.Tab)
	res, err := IngestDraft(ctx, st, tenantID, a.Bundle, bundleDir, assetRoot)
	if err != nil {
		rep.Errored++
		return RowResult{Status: "error", Error: err.Error()}
	}
	if res.Held {
		rep.Held++
		return RowResult{Status: "error", Error: res.Reason}
	}
	rep.Ingested++
	if !a.Approve {
		return RowResult{Status: "draft"}
	}
	approvedBy := a.ApprovedBy
	if approvedBy == "" {
		approvedBy = approvedByFallback
	}
	if err := st.ApprovePost(ctx, tenantID, res.PostID, approvedBy); err != nil {
		rep.Errored++
		return RowResult{Status: "error", Error: err.Error()}
	}
	rep.Approved++
	return RowResult{Status: "approved"}
}
