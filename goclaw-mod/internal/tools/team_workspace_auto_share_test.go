package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractFilePaths(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  []string
	}{
		{"quoted_double", `see "report.md" for details`, []string{"report.md"}},
		{"quoted_single", `check 'docs/notes.txt' please`, []string{"docs/notes.txt"}},
		{"path_param", `path="docs/a.md"`, []string{"docs/a.md"}},
		{"markdown_link", `[report](analysis.md)`, []string{"analysis.md"}},
		{"backtick", "check `config.yaml` file", []string{"config.yaml"}},
		{"multiple", `see "a.md" and "b.txt"`, []string{"a.md", "b.txt"}},
		{"nested", `"docs/sub/report.md"`, []string{"docs/sub/report.md"}},
		{"skip_url", `see https://example.com/file.md`, nil},
		{"skip_abs", `see "/etc/passwd"`, nil},
		{"skip_traversal", `see "../secret.md"`, nil},
		{"skip_unknown_ext", `see "binary.exe"`, nil},
		{"no_matches", "do some research on AI", nil},
		{"dedup", `"report.md" and path="report.md"`, []string{"report.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFilePaths(tt.text)
			if len(got) != len(tt.want) {
				t.Fatalf("extractFilePaths(%q) = %v, want %v", tt.text, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractFilePaths(%q)[%d] = %q, want %q", tt.text, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestAutoShareFiles_CopiesFromPersonalToTeam(t *testing.T) {
	personal := t.TempDir()
	team := t.TempDir()

	// Create file in personal workspace.
	os.MkdirAll(filepath.Join(personal, "docs"), 0755)
	os.WriteFile(filepath.Join(personal, "report.md"), []byte("# Report"), 0644)
	os.WriteFile(filepath.Join(personal, "docs", "notes.txt"), []byte("notes"), 0644)

	n := autoShareFiles(`see "report.md" and "docs/notes.txt"`, personal, team)
	if n != 2 {
		t.Fatalf("autoShareFiles copied %d files, want 2", n)
	}

	// Verify files exist in team workspace.
	if _, err := os.Stat(filepath.Join(team, "report.md")); err != nil {
		t.Error("report.md not copied to team workspace")
	}
	if _, err := os.Stat(filepath.Join(team, "docs", "notes.txt")); err != nil {
		t.Error("docs/notes.txt not copied to team workspace")
	}

	// Verify content.
	data, _ := os.ReadFile(filepath.Join(team, "report.md"))
	if string(data) != "# Report" {
		t.Errorf("copied content = %q, want %q", data, "# Report")
	}
}

func TestAutoShareFiles_SkipAlreadyInTeam(t *testing.T) {
	personal := t.TempDir()
	team := t.TempDir()

	os.WriteFile(filepath.Join(personal, "report.md"), []byte("personal"), 0644)
	os.WriteFile(filepath.Join(team, "report.md"), []byte("team version"), 0644)

	n := autoShareFiles(`see "report.md"`, personal, team)
	if n != 0 {
		t.Fatalf("autoShareFiles copied %d files, want 0 (already in team)", n)
	}

	// Team file should be unchanged.
	data, _ := os.ReadFile(filepath.Join(team, "report.md"))
	if string(data) != "team version" {
		t.Errorf("team file was overwritten: %q", data)
	}
}

func TestAutoShareFiles_SkipNonExistent(t *testing.T) {
	personal := t.TempDir()
	team := t.TempDir()

	n := autoShareFiles(`see "nonexistent.md"`, personal, team)
	if n != 0 {
		t.Fatalf("autoShareFiles copied %d files, want 0", n)
	}
}

func TestAutoShareFiles_SameWorkspace(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("data"), 0644)

	n := autoShareFiles(`see "a.md"`, dir, dir)
	if n != 0 {
		t.Fatalf("autoShareFiles should skip when personal==team, got %d", n)
	}
}

func TestAutoShareFiles_EmptyWorkspace(t *testing.T) {
	n := autoShareFiles(`see "a.md"`, "", "/tmp/team")
	if n != 0 {
		t.Fatalf("autoShareFiles should skip empty personal ws, got %d", n)
	}
	n = autoShareFiles(`see "a.md"`, "/tmp/personal", "")
	if n != 0 {
		t.Fatalf("autoShareFiles should skip empty team ws, got %d", n)
	}
}
