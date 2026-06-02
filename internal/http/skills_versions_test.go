package http

import (
	"os"
	"path/filepath"
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
