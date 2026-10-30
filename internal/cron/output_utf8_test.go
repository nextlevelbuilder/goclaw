package cron

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateOutputPreservesUTF8(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("x", maxOutputBytes+1),
		strings.Repeat("界", maxOutputBytes/3+1),
		strings.Repeat("x", maxOutputBytes-1) + "\U0001F642",
	} {
		output := TruncateOutput(input)
		if !utf8.ValidString(output) {
			t.Error("truncated run-log output splits a UTF-8 rune")
		}
		if !strings.HasSuffix(output, "...[truncated]") {
			t.Error("truncation marker is missing")
		}
		if len(output) > maxOutputBytes+len("...[truncated]") {
			t.Errorf("output is too long: %d bytes", len(output))
		}
	}
}
