package sheets

import (
	"fmt"
	"strings"
)

// a1Range quotes a tab name for A1 notation (covers hyphens/spaces); an internal
// single quote is doubled, e.g. ton-slot → 'ton-slot'.
func a1Range(tab string) string {
	return "'" + strings.ReplaceAll(tab, "'", "''") + "'"
}

// a1Cell builds a quoted-tab A1 range like 'ton-slot'!B2 or 'ton-slot'!1:1.
func a1Cell(tab, cell string) string {
	return a1Range(tab) + "!" + cell
}

// colLetter converts a 0-based column index to a spreadsheet column label
// (0→A, 25→Z, 26→AA).
func colLetter(idx int) string {
	idx++
	var b []byte
	for idx > 0 {
		idx--
		b = append([]byte{byte('A' + idx%26)}, b...)
		idx /= 26
	}
	return string(b)
}

// headerIndex returns the 0-based column index of a header (case-insensitive,
// trimmed), matching how the core parser maps cells.
func headerIndex(headers []string, name string) (int, bool) {
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return i, true
		}
	}
	return -1, false
}

// toStrings flattens a sheet row's cell values (any) to trimmed strings.
func toStrings(cells []any) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.TrimSpace(fmt.Sprint(c))
	}
	return out
}
