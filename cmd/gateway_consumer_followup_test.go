package cmd

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateForReminder_TruncatesByRuneAndKeepsUTF8Valid(t *testing.T) {
	input := strings.Repeat("a", 199) + "✌️"

	got := truncateForReminder(input, 200)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateForReminder() produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("truncateForReminder() = %q, want suffix ...", got)
	}
	if strings.Contains(got, "\uFFFD") {
		t.Fatalf("truncateForReminder() introduced replacement rune: %q", got)
	}
}

func TestTruncateForReminder_StripsInvalidUTF8(t *testing.T) {
	invalid := string([]byte{'a', 0xef, 0xb8, '.', 'b'})

	got := truncateForReminder(invalid, 200)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateForReminder() produced invalid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("truncateForReminder() = %q, want no replacement rune", got)
	}
}
