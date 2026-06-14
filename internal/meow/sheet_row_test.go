package meow

import "testing"

func TestParseSheetRow_MapsByHeaderNameNotPosition(t *testing.T) {
	// Headers deliberately reordered + mixed-case + padded vs the canonical order.
	headers := []string{"En_Text", " date ", "MANAGER_APPROVED", "image_file", "ko_text", "buttons", "status"}
	cells := []string{"hello", "2026-06-16", "TRUE", "2026-06-16.webp", "안녕", "Play | https://t.me/x", "Draft"}
	r := ParseSheetRow("one-jackpot", 7, headers, cells)

	if r.Tab != "one-jackpot" || r.RowIndex != 7 {
		t.Fatalf("tab/rowIndex = %q/%d", r.Tab, r.RowIndex)
	}
	if r.Date != "2026-06-16" || r.KoText != "안녕" || r.EnText != "hello" {
		t.Fatalf("text fields mismatch: %+v", r)
	}
	if r.ImageFile != "2026-06-16.webp" || r.Buttons != "Play | https://t.me/x" {
		t.Fatalf("image/buttons mismatch: %+v", r)
	}
	if r.Status != "draft" { // lowercased
		t.Fatalf("status = %q, want draft", r.Status)
	}
	if !r.ManagerApproved {
		t.Fatalf("manager_approved should be true")
	}
}

func TestParseSheetRow_MissingColumnsAreEmpty(t *testing.T) {
	r := ParseSheetRow("ton-slot", 2, []string{"date", "ko_text"}, []string{"2026-06-16", "안녕"})
	if r.EnText != "" || r.ImageFile != "" || r.ManagerApproved {
		t.Fatalf("absent columns should be zero values: %+v", r)
	}
}

func TestParseCheckbox(t *testing.T) {
	truthy := []string{"TRUE", "true", "1", "yes", "Y", "✓", "checked", " true "}
	for _, s := range truthy {
		if !parseCheckbox(s) {
			t.Errorf("parseCheckbox(%q) = false, want true", s)
		}
	}
	falsy := []string{"", "FALSE", "false", "0", "no", "maybe", "x"}
	for _, s := range falsy {
		if parseCheckbox(s) {
			t.Errorf("parseCheckbox(%q) = true, want false", s)
		}
	}
}

func TestParseButtons(t *testing.T) {
	got, err := ParseButtons("Play | https://t.me/x\n\n  Website  |  https://onewallet.store ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Button{
		{Label: "Play", URL: "https://t.me/x"},
		{Label: "Website", URL: "https://onewallet.store"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d buttons, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("button[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	if _, err := ParseButtons("no separator here"); err == nil {
		t.Error("missing separator should error")
	}
	if _, err := ParseButtons("  | https://t.me/x"); err == nil {
		t.Error("empty label should error")
	}
	if _, err := ParseButtons("Label |   "); err == nil {
		t.Error("empty url should error")
	}
	if got, err := ParseButtons("   \n  "); err != nil || got != nil {
		t.Errorf("blank cell = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestSheetRow_ToDraftBundle(t *testing.T) {
	r := SheetRow{
		Date:     "2026-06-16",
		KoText:   "안녕",
		EnText:   "hello",
		ImageFile: "2026-06-16.webp",
		Buttons:  "Play | https://t.me/x",
	}
	b, err := r.ToDraftBundle("@OneJackpotOfficial")
	if err != nil {
		t.Fatalf("ToDraftBundle: %v", err)
	}
	if b.Handle != "@OneJackpotOfficial" || b.ScheduledDate != "2026-06-16" || b.Image != "2026-06-16.webp" {
		t.Fatalf("bundle fields mismatch: %+v", b)
	}
	// The bundle must pass the SAME structural validation the ingest path uses.
	if err := b.Validate(); err != nil {
		t.Fatalf("converted bundle should validate: %v", err)
	}

	// A malformed buttons cell surfaces as a conversion error.
	bad := SheetRow{Date: "2026-06-16", KoText: "x", ImageFile: "a.webp", Buttons: "broken"}
	if _, err := bad.ToDraftBundle("@OneJackpotOfficial"); err == nil {
		t.Error("malformed buttons cell should error")
	}
}
