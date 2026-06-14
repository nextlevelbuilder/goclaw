package meow

import (
	"context"
	"testing"
)

// fakeSheetClient is an in-memory SheetClient for testing the sync planner without
// any Google API. ReadTab serves seeded rows; WriteBack records calls.
type fakeSheetClient struct {
	tabs   map[string][]SheetRow
	writes []fakeWrite
}

type fakeWrite struct {
	tab      string
	rowIndex int
	result   RowResult
}

func (f *fakeSheetClient) ReadTab(_ context.Context, tab string) ([]SheetRow, error) {
	return f.tabs[tab], nil
}

func (f *fakeSheetClient) WriteBack(_ context.Context, tab string, rowIndex int, result RowResult) error {
	f.writes = append(f.writes, fakeWrite{tab, rowIndex, result})
	return nil
}

// handles maps the two tabs used in these tests; an unseeded tab is "unknown".
func testResolveHandle(tab string) (string, bool) {
	m := map[string]string{
		"one-jackpot": "@OneJackpotOfficial",
		"ton-slot":    "@TonSlotOfficial",
	}
	h, ok := m[tab]
	return h, ok
}

// validRow is a fully valid, approved row for one-jackpot.
func validRow(rowIndex int) SheetRow {
	return SheetRow{
		Tab:             "one-jackpot",
		RowIndex:        rowIndex,
		Date:            "2026-06-16",
		Status:          "ready",
		KoText:          "안녕",
		ImageFile:       "2026-06-16.webp",
		Buttons:         "Play | https://t.me/x",
		ManagerApproved: true,
		ApprovedBy:      "manager-1",
	}
}

func TestPlanSync_Classification(t *testing.T) {
	notApproved := validRow(2)
	notApproved.ManagerApproved = false

	terminal := validRow(3)
	terminal.Status = "published"

	unknownTab := validRow(4)
	unknownTab.Tab = "no-such-brand"

	badDate := validRow(5)
	badDate.Date = "16/06/2026"

	badButtons := validRow(6)
	badButtons.Buttons = "broken-no-separator"

	noText := validRow(7)
	noText.KoText = ""
	noText.EnText = ""

	rows := []SheetRow{
		validRow(1), notApproved, terminal, unknownTab, badDate, badButtons, noText,
	}
	actions := PlanSync(rows, testResolveHandle)
	if len(actions) != len(rows) {
		t.Fatalf("got %d actions, want %d", len(actions), len(rows))
	}

	want := []struct {
		kind    SyncActionKind
		approve bool
	}{
		{SyncIngest, true}, // valid approved row
		{SyncSkip, false},  // not approved
		{SyncSkip, false},  // terminal status
		{SyncError, false}, // unknown tab
		{SyncError, false}, // bad date
		{SyncError, false}, // bad buttons
		{SyncError, false}, // no text
	}
	for i, w := range want {
		if actions[i].Kind != w.kind || actions[i].Approve != w.approve {
			t.Errorf("action[%d] = {kind:%d approve:%v reason:%q}, want {kind:%d approve:%v}",
				i, actions[i].Kind, actions[i].Approve, actions[i].Reason, w.kind, w.approve)
		}
	}

	// The valid action must carry a usable bundle + approver, and skip/error must not.
	a0 := actions[0]
	if a0.Bundle == nil || a0.Bundle.Handle != "@OneJackpotOfficial" || a0.ApprovedBy != "manager-1" {
		t.Fatalf("ingest action missing bundle/approver: %+v", a0)
	}
	for i := 1; i < len(actions); i++ {
		if actions[i].Bundle != nil {
			t.Errorf("non-ingest action[%d] should not carry a bundle", i)
		}
		if actions[i].Reason == "" {
			t.Errorf("non-ingest action[%d] should explain itself", i)
		}
	}
}

func TestPlanSync_DrivenThroughFakeClient(t *testing.T) {
	fake := &fakeSheetClient{tabs: map[string][]SheetRow{
		"ton-slot": {
			func() SheetRow { r := validRow(2); r.Tab = "ton-slot"; return r }(),
		},
	}}

	rows, err := fake.ReadTab(context.Background(), "ton-slot")
	if err != nil {
		t.Fatalf("ReadTab: %v", err)
	}
	actions := PlanSync(rows, testResolveHandle)
	if len(actions) != 1 || actions[0].Kind != SyncIngest {
		t.Fatalf("expected one SyncIngest, got %+v", actions)
	}
	if actions[0].Bundle.Handle != "@TonSlotOfficial" {
		t.Fatalf("tab→handle resolution wrong: %+v", actions[0].Bundle)
	}

	// WriteBack is part of the contract even though the planner doesn't drive it.
	if err := fake.WriteBack(context.Background(), "ton-slot", 2, RowResult{Status: "published"}); err != nil {
		t.Fatalf("WriteBack: %v", err)
	}
	if len(fake.writes) != 1 || fake.writes[0].result.Status != "published" {
		t.Fatalf("write-back not recorded: %+v", fake.writes)
	}
}
