package http

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Export archives the whole skill directory, but import used to recognise only
// metadata.json, SKILL.md and grants.jsonl and silently discard everything else — the
// switch had no default. A skill whose SKILL.md cites references/*.md arrived without
// them, the import reported success, and the skill failed at the first read.
func TestWriteImportedSkillAuxFiles_PreservesTree(t *testing.T) {
	dir := t.TempDir()

	aux := map[string][]byte{
		"references/errors.md":      []byte("# Errors"),
		"references/nested/deep.md": []byte("# Deep"),
		"scripts/run.sh":            []byte("#!/bin/sh\n"),
		"assets/logo.svg":           []byte("<svg/>"),
	}
	writeImportedSkillAuxFiles(dir, "demo", aux)

	for rel, want := range aux {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s: content = %q, want %q", rel, got, want)
		}
	}
}

// Archive entry names are attacker-controlled. Each segment is sanitised, so a traversal
// attempt collapses to a relative path and the write stays inside the skill directory.
func TestWriteImportedSkillAuxFiles_ContainsTraversal(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")

	writeImportedSkillAuxFiles(skillDir, "demo", map[string][]byte{
		"../outside.txt":        []byte("escaped"),
		"../../outside.txt":     []byte("escaped"),
		"references/../../x.md": []byte("escaped"),
		"./references/ok.md":    []byte("fine"),
	})

	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("traversal escaped the skill directory: %v", err)
	}

	// The sanitised forms land inside skillDir instead.
	if _, err := os.Stat(filepath.Join(skillDir, "outside.txt")); err != nil {
		t.Errorf("expected the sanitised path inside the skill dir: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(skillDir, "references", "ok.md")); err != nil || string(got) != "fine" {
		t.Errorf("ordinary path broke: got %q err %v", got, err)
	}

	// Nothing may sit above skillDir.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "skill" {
			t.Errorf("unexpected entry written above the skill dir: %s", e.Name())
		}
	}
}

// A path that sanitises to nothing is skipped rather than written as the directory itself.
func TestWriteImportedSkillAuxFiles_SkipsEmptyPaths(t *testing.T) {
	dir := t.TempDir()
	writeImportedSkillAuxFiles(dir, "demo", map[string][]byte{
		"..":  []byte("x"),
		".":   []byte("x"),
		"/":   []byte("x"),
		"///": []byte("x"),
	})
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected nothing written, got %s", strings.Join(names, ", "))
	}
}

// A nil or empty map is a no-op, not a panic — most skills have no extra files.
func TestWriteImportedSkillAuxFiles_NoFiles(t *testing.T) {
	dir := t.TempDir()
	writeImportedSkillAuxFiles(dir, "demo", nil)
	writeImportedSkillAuxFiles(dir, "demo", map[string][]byte{})
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected an empty directory, got %d entries", len(entries))
	}
}

// SKILL.md is scanned by GuardSkillContent before anything is written. The switch that
// routes archive entries compares the raw path, so "./SKILL.md" does not match it and
// lands in the auxiliary set — where sanitizeRelPath collapses it back to "SKILL.md".
// Writing that would replace the scanned file with an unscanned one.
func TestWriteImportedSkillAuxFiles_RefusesReservedNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("scanned"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	writeImportedSkillAuxFiles(dir, "demo", map[string][]byte{
		"./SKILL.md":       []byte("unscanned"),
		"SKILL.md/":        []byte("unscanned"),
		".//SKILL.md":      []byte("unscanned"),
		"./metadata.json":  []byte(`{"ok":false}`),
		"./grants.jsonl":   []byte("forged"),
		"references/ok.md": []byte("fine"),
	})

	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(got) != "scanned" {
		t.Errorf("SKILL.md was overwritten from the auxiliary set: got %q err %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil || string(got) != `{"ok":true}` {
		t.Errorf("metadata.json was overwritten: got %q err %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "grants.jsonl")); !os.IsNotExist(err) {
		t.Errorf("grants.jsonl was written from the auxiliary set: %v", err)
	}
	// An ordinary path in the same batch must still land.
	if got, err := os.ReadFile(filepath.Join(dir, "references", "ok.md")); err != nil || string(got) != "fine" {
		t.Errorf("ordinary path broke: got %q err %v", got, err)
	}
}

// A tar directory entry has a trailing separator and no content. Written as a file it
// would take the name a real directory needs, and whether that happens depends on map
// iteration order — so the failure would be intermittent.
func TestWriteImportedSkillAuxFiles_IgnoresDirectoryEntries(t *testing.T) {
	for range 20 {
		dir := t.TempDir()
		writeImportedSkillAuxFiles(dir, "demo", map[string][]byte{
			"references/":          {},
			"references/errors.md": []byte("# Errors"),
			"assets/":              {},
			"assets/logo.svg":      []byte("<svg/>"),
		})

		got, err := os.ReadFile(filepath.Join(dir, "references", "errors.md"))
		if err != nil || string(got) != "# Errors" {
			t.Fatalf("directory entry blocked a real file: got %q err %v", got, err)
		}
		info, err := os.Stat(filepath.Join(dir, "references"))
		if err != nil || !info.IsDir() {
			t.Fatalf("references should be a directory: %v", err)
		}
	}
}

// An archive with an implausible number of extra files is capped, and the cap is logged
// rather than applied silently.
func TestWriteImportedSkillAuxFiles_CapsFileCount(t *testing.T) {
	dir := t.TempDir()
	aux := make(map[string][]byte, maxImportedAuxFiles+50)
	for i := range maxImportedAuxFiles + 50 {
		aux[fmt.Sprintf("references/f%d.md", i)] = []byte("x")
	}
	writeImportedSkillAuxFiles(dir, "demo", aux)

	entries, err := os.ReadDir(filepath.Join(dir, "references"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > maxImportedAuxFiles {
		t.Errorf("wrote %d files, cap is %d", len(entries), maxImportedAuxFiles)
	}
	if len(entries) != maxImportedAuxFiles {
		t.Errorf("expected exactly the cap to be written, got %d", len(entries))
	}
}
