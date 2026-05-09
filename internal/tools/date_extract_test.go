package tools

import (
	"testing"
	"time"
)

func TestExtractDateRange(t *testing.T) {
	tests := []struct {
		query     string
		wantDay   int
		wantMonth time.Month
		wantYear  int
		wantNil   bool
	}{
		// English dates
		{"status delivery 8th May", 8, time.May, 0, false},
		{"handover for May 7th", 7, time.May, 0, false},
		{"delivery 8 May 2026", 8, time.May, 2026, false},
		{"status for 7th may", 7, time.May, 0, false},

		// Indonesian dates
		{"handover 08 Mei 2026", 8, time.May, 2026, false},
		{"delivery 7 Mei", 7, time.May, 0, false},
		{"status 02 Juni 2026", 2, time.June, 2026, false},

		// Numeric dates
		{"status 2026-05-08", 8, time.May, 2026, false},
		{"delivery 08/05/2026", 8, time.May, 2026, false},
		{"report 2026-12-25", 25, time.December, 2026, false},

		// No date
		{"delivery status sovereign", 0, 0, 0, true},
		{"what is handover", 0, 0, 0, true},

		// Indonesian months
		{"laporan 15 Januari 2026", 15, time.January, 2026, false},
		{"data 28 Desember", 28, time.December, 0, false},
		{"status 1 Agustus 2026", 1, time.August, 2026, false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := ExtractDateRange(tt.query)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ExtractDateRange(%q) = %+v, want nil", tt.query, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ExtractDateRange(%q) = nil, want date", tt.query)
			}
			if got.From.Day() != tt.wantDay {
				t.Errorf("day = %d, want %d", got.From.Day(), tt.wantDay)
			}
			if got.From.Month() != tt.wantMonth {
				t.Errorf("month = %v, want %v", got.From.Month(), tt.wantMonth)
			}
			if tt.wantYear != 0 && got.From.Year() != tt.wantYear {
				t.Errorf("year = %d, want %d", got.From.Year(), tt.wantYear)
			}
			// To should be From + 2 days (buffer)
			expectedTo := got.From.AddDate(0, 0, 2)
			if !got.To.Equal(expectedTo) {
				t.Errorf("To = %v, want %v", got.To, expectedTo)
			}
		})
	}
}

func TestStripDateTokens(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{"status delivery 8th May", "status delivery"},
		{"handover 08 Mei 2026 sovereign", "handover  sovereign"},
		{"delivery status", "delivery status"},
		{"report for 2026-05-08 please", "report for  please"},
		{"May 7th handover info", "handover info"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := StripDateTokens(tt.query)
			if got != tt.want {
				t.Errorf("StripDateTokens(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}
