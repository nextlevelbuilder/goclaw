//go:build !sqliteonly

package backup

import "testing"

func TestParsePgDumpMajor(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"pg_dump (PostgreSQL) 17.9 (Debian 17.9-1.pgdg12+1)", 17},
		{"pg_dump (PostgreSQL) 18.3", 18},
		{"pg_dump (PostgreSQL) 18.3 (Homebrew)", 18},
		{"pg_dump (PostgreSQL) 10.21", 10},
		{"pg_dump (PostgreSQL) 9.6.24", 9},
		{"pg_dump (PostgreSQL) 18", 18},
		// Parse failures → 0
		{"", 0},
		{"pg_dump unknown", 0},
		{"pg_dump (PostgreSQL) vNext", 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ParsePgDumpMajor(tc.in)
			if got != tc.want {
				t.Errorf("ParsePgDumpMajor(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
