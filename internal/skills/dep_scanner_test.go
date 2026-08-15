package skills

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Regression guard: existing skill without deps:/exclude_deps: fields must
// produce the same scan output as pre-manifest behavior.
func TestScanSkillDeps_NoManifestFields_Unchanged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), `---
name: sample
description: sample skill
---
body`)
	writeFile(t, filepath.Join(dir, "scripts", "run.py"),
		"import requests\nimport json\n")

	got := ScanSkillDeps(dir)
	if got.FromManifest {
		t.Error("FromManifest should be false")
	}
	if len(got.Explicit) != 0 {
		t.Errorf("Explicit should be empty: %v", got.Explicit)
	}
	if !slices.Contains(got.RequiresPython, "requests") {
		t.Errorf("expected requests in RequiresPython, got %v", got.RequiresPython)
	}
	// json is stdlib → must be excluded
	if slices.Contains(got.RequiresPython, "json") {
		t.Errorf("stdlib json leaked into RequiresPython: %v", got.RequiresPython)
	}
}

func TestScanSkillDeps_ExplicitDeps_Authoritative(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), `---
name: sample
deps:
  - pip:psycopg2-binary
  - pip:requests>=2.31
  - system:ffmpeg
---
`)
	// Auto-scan would pick up 'numpy' but manifest overrides.
	writeFile(t, filepath.Join(dir, "scripts", "run.py"),
		"import numpy\nimport psycopg2\n")

	got := ScanSkillDeps(dir)
	if !got.FromManifest {
		t.Fatal("FromManifest should be true")
	}
	wantPy := []string{"psycopg2-binary", "requests"}
	if !slices.Equal(got.RequiresPython, wantPy) {
		t.Errorf("RequiresPython = %v, want %v (auto-scanned numpy should be overridden)", got.RequiresPython, wantPy)
	}
	if !slices.Contains(got.Requires, "ffmpeg") {
		t.Errorf("system ffmpeg missing: %v", got.Requires)
	}
	// Auto-scan numpy must NOT leak through when explicit is set.
	if slices.Contains(got.RequiresPython, "numpy") {
		t.Errorf("numpy leaked despite explicit override: %v", got.RequiresPython)
	}
}

func TestScanSkillDeps_ExcludeDeps_Filters(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), `---
name: sample
exclude_deps:
  - pip:my_local_helper
---
`)
	writeFile(t, filepath.Join(dir, "scripts", "run.py"),
		"import requests\nimport my_local_helper\n")

	got := ScanSkillDeps(dir)
	if got.FromManifest {
		t.Error("FromManifest should be false for exclude-only")
	}
	if !slices.Contains(got.RequiresPython, "requests") {
		t.Errorf("requests missing: %v", got.RequiresPython)
	}
	if slices.Contains(got.RequiresPython, "my_local_helper") {
		t.Errorf("my_local_helper should be filtered: %v", got.RequiresPython)
	}
	if !slices.Equal(got.ExcludeDeps, []string{"pip:my_local_helper"}) {
		t.Errorf("ExcludeDeps = %v", got.ExcludeDeps)
	}
}

func TestScanSkillDeps_NoSKILLmd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "scripts", "run.py"), "import requests\n")

	got := ScanSkillDeps(dir)
	if got.FromManifest {
		t.Error("FromManifest should be false without SKILL.md")
	}
	if !slices.Contains(got.RequiresPython, "requests") {
		t.Errorf("requests missing: %v", got.RequiresPython)
	}
}

func TestMergeDeps_PreservesManifestFields(t *testing.T) {
	a := &SkillManifest{
		RequiresPython: []string{"requests"},
		Explicit:       []string{"pip:requests"},
		FromManifest:   true,
	}
	b := &SkillManifest{
		RequiresPython: []string{"numpy"},
		ExcludeDeps:    []string{"pip:foo"},
	}
	got := MergeDeps(a, b)
	if !got.FromManifest {
		t.Error("FromManifest OR-fold failed")
	}
	if !slices.Equal(got.Explicit, []string{"pip:requests"}) {
		t.Errorf("Explicit = %v", got.Explicit)
	}
	if !slices.Equal(got.ExcludeDeps, []string{"pip:foo"}) {
		t.Errorf("ExcludeDeps = %v", got.ExcludeDeps)
	}
	wantPy := []string{"requests", "numpy"}
	if !slices.Equal(got.RequiresPython, wantPy) {
		t.Errorf("RequiresPython = %v, want %v", got.RequiresPython, wantPy)
	}
}
