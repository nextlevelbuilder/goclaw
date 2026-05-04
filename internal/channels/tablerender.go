package channels

import "strings"

// TableMode controls how markdown tables are rendered before delivery to a channel.
//
//   - "auto"  (default) — narrow tables (≤3 cols AND ≤36 display chars) render as ASCII;
//     wide tables render as cards. Works well on both desktop and mobile.
//   - "ascii" — always aligned ASCII columns inside a code block. Desktop-only.
//   - "cards" — each row as a vertical block (one field per line). Mobile-friendly.
//   - "list"  — each row as a single bullet line. Widest compatibility.
//   - "off"   — pass raw markdown table through unchanged.
const (
	TableModeAuto  = "auto"
	TableModeASCII = "ascii"
	TableModeCards = "cards"
	TableModeList  = "list"
	TableModeOff   = "off"
)

// ParseTableMode validates and normalises a table mode string.
// Unknown values fall back to "auto".
func ParseTableMode(s string) string {
	switch s {
	case TableModeASCII, TableModeCards, TableModeList, TableModeOff:
		return s
	default:
		return TableModeAuto
	}
}

// TableRow holds a parsed header + data rows from a markdown table.
type TableRow struct {
	Header []string
	Rows   [][]string
}

// ParseMarkdownTableRows parses raw markdown table lines (including separator)
// into a TableRow. Returns nil when fewer than two lines are provided.
func ParseMarkdownTableRows(lines []string) *TableRow {
	if len(lines) < 2 {
		return nil
	}
	header := splitTableRow(lines[0])
	var rows [][]string
	for _, line := range lines[2:] { // skip separator at [1]
		rows = append(rows, splitTableRow(line))
	}
	return &TableRow{Header: header, Rows: rows}
}

// splitTableRow splits a markdown table row on "|", trimming leading/trailing
// pipes and whitespace from each cell.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// autoWideModeThreshold is the maximum total ASCII width for a table to stay
// in ascii mode under "auto". Tables wider than this switch to cards.
// 36 chars fits comfortably in Telegram's mobile <pre> block without wrapping.
const autoWideModeThreshold = 36

// RenderTable renders parsed table rows according to the given mode.
// The caller is responsible for wrapping the result in a code block if needed.
func RenderTable(mode string, parsed *TableRow) string {
	if parsed == nil {
		return ""
	}
	switch mode {
	case TableModeCards:
		return renderCards(parsed)
	case TableModeList:
		return renderList(parsed)
	case TableModeOff:
		return "" // caller falls back to raw markdown
	case TableModeASCII:
		return renderASCII(parsed)
	default: // "auto"
		if isWideTable(parsed) {
			return renderCards(parsed)
		}
		return renderASCII(parsed)
	}
}

// isWideTable returns true when the table has more than 3 columns or
// the widest row exceeds autoWideModeThreshold chars.
func isWideTable(t *TableRow) bool {
	if len(t.Header) > 3 {
		return true
	}
	// Estimate row width: sum of header cell lengths + separators
	total := 0
	for _, h := range t.Header {
		total += len(h) + 3 // " cell " + "|"
	}
	return total > autoWideModeThreshold
}

// renderASCII produces a column-aligned ASCII table (current default behaviour).
func renderASCII(t *TableRow) string {
	numCols := len(t.Header)
	for _, row := range t.Rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}

	colWidths := make([]int, numCols)
	allRows := append([][]string{t.Header}, t.Rows...)
	for _, row := range allRows {
		for j := 0; j < numCols && j < len(row); j++ {
			if w := len([]rune(row[j])); w > colWidths[j] {
				colWidths[j] = w
			}
		}
	}

	var out []string
	out = append(out, asciiRow(t.Header, colWidths))

	var sep []string
	for _, w := range colWidths {
		sep = append(sep, strings.Repeat("-", w+2))
	}
	out = append(out, "|"+strings.Join(sep, "|")+"|")

	for _, row := range t.Rows {
		out = append(out, asciiRow(row, colWidths))
	}
	return strings.Join(out, "\n")
}

func asciiRow(cells []string, colWidths []int) string {
	parts := make([]string, len(colWidths))
	for j, w := range colWidths {
		cell := ""
		if j < len(cells) {
			cell = cells[j]
		}
		pad := w - len([]rune(cell))
		if pad < 0 {
			pad = 0
		}
		parts[j] = " " + cell + strings.Repeat(" ", pad) + " "
	}
	return "|" + strings.Join(parts, "|") + "|"
}

// renderCards renders each data row as a vertical block — one field per line.
// Fits any screen width.
//
// Example output:
//
//	Kananaskis Nordic Spa
//	  Priority    : High
//	  Planned     : Before Aug 2026
//	  Status      : Planned
//	  ────────────────────
func renderCards(t *TableRow) string {
	if len(t.Header) == 0 {
		return ""
	}
	// Compute header label column width for alignment.
	maxLabel := 0
	for _, h := range t.Header[1:] {
		if len(h) > maxLabel {
			maxLabel = len(h)
		}
	}

	divider := "  " + strings.Repeat("─", 20)
	var out []string

	for i, row := range t.Rows {
		// First column is the "title" of the card.
		title := ""
		if len(row) > 0 {
			title = row[0]
		}
		out = append(out, title)

		for j, header := range t.Header[1:] {
			col := j + 1
			value := ""
			if col < len(row) {
				value = row[col]
			}
			pad := maxLabel - len(header)
			if pad < 0 {
				pad = 0
			}
			out = append(out, "  "+header+strings.Repeat(" ", pad)+" : "+value)
		}

		if i < len(t.Rows)-1 {
			out = append(out, divider)
		}
	}
	return strings.Join(out, "\n")
}

// renderList renders each data row as a single bullet summary line.
//
// Example output:
//
//	• Kananaskis Nordic Spa | High | Before Aug 2026 | Planned
func renderList(t *TableRow) string {
	var out []string
	for _, row := range t.Rows {
		line := "• " + strings.Join(row, " | ")
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
