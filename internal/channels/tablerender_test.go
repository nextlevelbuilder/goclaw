package channels

import (
	"strings"
	"testing"
)

// bucketListLines mimics the exact table from the user's screenshot — 5 columns, mobile-unfriendly.
var bucketListLines = []string{
	"| Destination | Priority | Planned Visit | Status | Notes |",
	"|---|---|---|---|---|",
	"| Kananaskis Nordic Spa | High | Before Aug 2026 | Planned | Spa and relaxation trip |",
	"| Radium Hot Springs | Medium | Summer 2026 | Planned |  |",
	"| Fairmont Hot Springs | Medium | Summer 2026 | Planned |  |",
}

var narrowLines = []string{
	"| Name | Value |",
	"|---|---|",
	"| Foo | Bar |",
	"| Baz | Qux |",
}

func parsedBucketList() *TableRow { return ParseMarkdownTableRows(bucketListLines) }
func parsedNarrow() *TableRow     { return ParseMarkdownTableRows(narrowLines) }

// --- ParseTableMode ---

func TestParseTableMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"auto", "auto"},
		{"ascii", "ascii"},
		{"cards", "cards"},
		{"list", "list"},
		{"off", "off"},
		{"", "auto"},
		{"unknown", "auto"},
		{"AUTO", "auto"},
	}
	for _, tc := range cases {
		got := ParseTableMode(tc.in)
		if got != tc.want {
			t.Errorf("ParseTableMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- ParseMarkdownTableRows ---

func TestParseMarkdownTableRows_NilOnTooShort(t *testing.T) {
	if ParseMarkdownTableRows(nil) != nil {
		t.Error("expected nil for nil input")
	}
	if ParseMarkdownTableRows([]string{"| A |"}) != nil {
		t.Error("expected nil for single-line input (no separator)")
	}
}

func TestParseMarkdownTableRows_Header(t *testing.T) {
	p := parsedBucketList()
	if p == nil {
		t.Fatal("expected non-nil")
	}
	if len(p.Header) != 5 {
		t.Fatalf("header cols = %d, want 5", len(p.Header))
	}
	if p.Header[0] != "Destination" {
		t.Errorf("header[0] = %q, want %q", p.Header[0], "Destination")
	}
}

func TestParseMarkdownTableRows_DataRows(t *testing.T) {
	p := parsedBucketList()
	if len(p.Rows) != 3 {
		t.Fatalf("data rows = %d, want 3", len(p.Rows))
	}
	if p.Rows[0][0] != "Kananaskis Nordic Spa" {
		t.Errorf("row[0][0] = %q, want %q", p.Rows[0][0], "Kananaskis Nordic Spa")
	}
}

// --- isWideTable ---

func TestIsWideTable_Wide(t *testing.T) {
	if !isWideTable(parsedBucketList()) {
		t.Error("5-column bucket list should be wide")
	}
}

func TestIsWideTable_Narrow(t *testing.T) {
	if isWideTable(parsedNarrow()) {
		t.Error("2-column narrow table should not be wide")
	}
}

// --- RenderTable: auto mode ---

func TestRenderTable_AutoWideUsesCards(t *testing.T) {
	out := RenderTable(TableModeAuto, parsedBucketList())
	// Cards mode: first data row title appears unindented, fields indented with ":"
	if !strings.Contains(out, "Kananaskis Nordic Spa") {
		t.Error("expected destination name in output")
	}
	if !strings.Contains(out, " : ") {
		t.Error("expected card-style \" : \" separator")
	}
	// Should NOT have pipe-aligned table columns
	if strings.HasPrefix(out, "|") {
		t.Error("auto wide mode should not produce pipe-prefix ASCII table")
	}
}

func TestRenderTable_AutoNarrowUsesASCII(t *testing.T) {
	out := RenderTable(TableModeAuto, parsedNarrow())
	if !strings.HasPrefix(out, "|") {
		t.Errorf("auto narrow mode should produce ASCII table, got: %q", out)
	}
	if !strings.Contains(out, "---") {
		t.Error("auto narrow mode should have separator row")
	}
}

// --- RenderTable: ascii mode ---

func TestRenderTable_ASCII(t *testing.T) {
	out := RenderTable(TableModeASCII, parsedBucketList())
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("ascii mode: expected ≥3 lines, got %d", len(lines))
	}
	// Every line must start and end with "|"
	for i, line := range lines {
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			t.Errorf("ascii line %d does not start/end with pipe: %q", i, line)
		}
	}
	// Header and data rows must be same width
	headerWidth := len([]rune(lines[0]))
	for i := 2; i < len(lines); i++ {
		if w := len([]rune(lines[i])); w != headerWidth {
			t.Errorf("ascii line %d width %d != header width %d", i, w, headerWidth)
		}
	}
}

// --- RenderTable: cards mode ---

func TestRenderTable_Cards_Structure(t *testing.T) {
	out := RenderTable(TableModeCards, parsedBucketList())
	// Each row's first column value should appear as an unindented title
	if !strings.Contains(out, "Kananaskis Nordic Spa\n") {
		t.Error("expected first row title on its own line")
	}
	// Fields should use " : " separator
	if !strings.Contains(out, "Priority") {
		t.Error("expected header label in output")
	}
	if !strings.Contains(out, " : High") {
		t.Error("expected priority value")
	}
	// Divider between rows
	if !strings.Contains(out, "─") {
		t.Error("expected divider between card rows")
	}
}

func TestRenderTable_Cards_NoDividerAfterLastRow(t *testing.T) {
	out := RenderTable(TableModeCards, parsedBucketList())
	lines := strings.Split(out, "\n")
	last := lines[len(lines)-1]
	if strings.Contains(last, "─") {
		t.Error("last line should not be a divider")
	}
}

func TestRenderTable_Cards_NoWideColumns(t *testing.T) {
	out := RenderTable(TableModeCards, parsedBucketList())
	// Each line must be shorter than a safe mobile width (no wide alignment needed)
	for i, line := range strings.Split(out, "\n") {
		if len(line) > 80 {
			t.Errorf("cards line %d is %d chars — may be too wide for mobile: %q", i, len(line), line)
		}
	}
}

// --- RenderTable: list mode ---

func TestRenderTable_List(t *testing.T) {
	out := RenderTable(TableModeList, parsedBucketList())
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("list mode: expected 3 lines (one per row), got %d", len(lines))
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "• ") {
			t.Errorf("list line %d missing bullet prefix: %q", i, line)
		}
	}
	if !strings.Contains(lines[0], "Kananaskis Nordic Spa") {
		t.Error("first list item should contain destination name")
	}
}

// --- RenderTable: off mode ---

func TestRenderTable_Off(t *testing.T) {
	out := RenderTable(TableModeOff, parsedBucketList())
	if out != "" {
		t.Errorf("off mode should return empty string, got %q", out)
	}
}

// --- RenderTable: nil input ---

func TestRenderTable_NilParsed(t *testing.T) {
	for _, mode := range []string{TableModeAuto, TableModeASCII, TableModeCards, TableModeList, TableModeOff} {
		out := RenderTable(mode, nil)
		if out != "" {
			t.Errorf("mode %q with nil parsed: expected empty, got %q", mode, out)
		}
	}
}

// --- splitTableRow ---

func TestSplitTableRow(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"| A | B | C |", []string{"A", "B", "C"}},
		{"|A|B|", []string{"A", "B"}},
		{"  | foo | bar |  ", []string{"foo", "bar"}},
	}
	for _, tc := range cases {
		got := splitTableRow(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitTableRow(%q): len %d, want %d", tc.in, len(got), len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitTableRow(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
