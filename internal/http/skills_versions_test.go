package http

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadableSkillRootsFallsBackToBundledSystemSkill(t *testing.T) {
	tmp := t.TempDir()
	missingManaged := filepath.Join(tmp, "managed", "demo", "1")
	bundled := filepath.Join(tmp, "bundled", "demo")
	if err := os.MkdirAll(bundled, 0755); err != nil {
		t.Fatal(err)
	}

	roots := readableSkillRoots(missingManaged, "demo", true, filepath.Join(tmp, "bundled"))
	if len(roots) != 1 || roots[0] != bundled {
		t.Fatalf("roots = %#v, want bundled fallback", roots)
	}
}

func TestReadableSkillRootsDoesNotFallbackForCustomSkill(t *testing.T) {
	tmp := t.TempDir()
	bundled := filepath.Join(tmp, "bundled", "demo")
	if err := os.MkdirAll(bundled, 0755); err != nil {
		t.Fatal(err)
	}

	roots := readableSkillRoots(filepath.Join(tmp, "missing"), "demo", false, filepath.Join(tmp, "bundled"))
	if len(roots) != 0 {
		t.Fatalf("roots = %#v, want no custom fallback", roots)
	}
}

func TestSkillVersionReadbackListsAndReadsCompanionReferenceFiles(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	versionDir := filepath.Join(tmp, "managed", "demo", "2")
	referencePath := filepath.Join(versionDir, "references", "ship-workflow.md")
	if err := os.MkdirAll(filepath.Dir(referencePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "SKILL.md"), []byte("---\nname: Demo\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(referencePath, []byte("# Ship\n"), 0644); err != nil {
		t.Fatal(err)
	}

	files := walkSkillFiles(versionDir)
	if !slices.ContainsFunc(files, func(entry fileEntry) bool {
		return entry.Path == filepath.Join("references", "ship-workflow.md") && !entry.IsDir && entry.Size == int64(len("# Ship\n"))
	}) {
		t.Fatalf("files = %#v, want references/ship-workflow.md", files)
	}

	data, info, err := readSkillFile(referencePath)
	if err != nil {
		t.Fatalf("readSkillFile: %v", err)
	}
	if string(data) != "# Ship\n" {
		t.Fatalf("content = %q", data)
	}
	if info.Size() != int64(len("# Ship\n")) {
		t.Fatalf("size = %d", info.Size())
	}
}
