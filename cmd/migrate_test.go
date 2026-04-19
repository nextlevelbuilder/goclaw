package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAbsoluteToFileURI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"posix absolute", "/app/migrations", "file:///app/migrations"},
		// Windows drive-letter path with backslashes: the exact shape
		// golang-migrate needs. Before the fix, "file://F:\\..." was parsed
		// with "F" as host and ":\\..." as port → "invalid port" error.
		{"windows backslash", `F:\project\goclaw\migrations`, "file:///F:/project/goclaw/migrations"},
		{"windows mixed separators", `C:/already/forward`, "file:///C:/already/forward"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := absoluteToFileURI(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestMigrationsSourceURLRelative verifies the helper resolves a relative
// input via filepath.Abs before formatting — exact output depends on CWD,
// so we only assert invariants that must hold on every runner.
func TestMigrationsSourceURLRelative(t *testing.T) {
	got := migrationsSourceURL("migrations")
	if !strings.HasPrefix(got, "file://") {
		t.Fatalf("missing file:// prefix: %q", got)
	}
	if !strings.Contains(filepath.ToSlash(got), "/migrations") {
		t.Errorf("missing /migrations segment: %q", got)
	}
}
