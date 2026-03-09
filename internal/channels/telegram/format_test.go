package telegram

import (
	"strings"
	"testing"
)

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"Khởi động", 9},           // Vietnamese diacritics = single-width
		{"Hardware tối thiểu", 18}, // Vietnamese diacritics = single-width
		{"Ngôn ngữ", 8},
		{"đ", 1},   // Vietnamese d-stroke = single-width
		{"中文", 4},  // CJK = double-width
		{"日本語", 6}, // CJK = double-width
	}

	for _, tt := range tests {
		got := displayWidth(tt.input)
		if got != tt.want {
			t.Errorf("displayWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestRenderTableAsCode_Vietnamese(t *testing.T) {
	lines := []string{
		"| Metric | OpenClaw | ZeroClaw |",
		"|--------|----------|----------|",
		"| Ngôn ngữ | TypeScript/Node.js | Rust |",
		"| Khởi động | > 500s | < 10ms |",
		"| Hardware tối thiểu | Mac mini $599 | $10 (bao gồm cả Raspberry Pi) |",
	}

	result := renderTableAsCode(lines)

	// Every non-separator line should have the same number of pipes
	resultLines := strings.Split(result, "\n")
	if len(resultLines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(resultLines))
	}

	// Check separator line width matches header line width
	headerWidth := displayWidth(resultLines[0])
	sepWidth := displayWidth(resultLines[1])
	if headerWidth != sepWidth {
		t.Errorf("header width (%d) != separator width (%d)\nheader: %s\nsep:    %s",
			headerWidth, sepWidth, resultLines[0], resultLines[1])
	}

	// Check all data rows match header width
	for i := 2; i < len(resultLines); i++ {
		rowWidth := displayWidth(resultLines[i])
		if rowWidth != headerWidth {
			t.Errorf("row %d width (%d) != header width (%d)\nrow:    %s\nheader: %s",
				i, rowWidth, headerWidth, resultLines[i], resultLines[0])
		}
	}
}
