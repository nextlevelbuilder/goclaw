package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundledSkills_NoRegression verifies every bundled skill scans successfully
// after Phase 02 changes. No skill currently uses deps:/exclude_deps: (verified
// via grep), so FromManifest must be false across the board.
func TestBundledSkills_NoRegression(t *testing.T) {
	bundled := "../../skills"
	entries, err := os.ReadDir(bundled)
	if err != nil {
		t.Skip("bundled skills dir not found:", err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_shared" {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			skillDir := filepath.Join(bundled, name)
			m := ScanSkillDeps(skillDir)
			if m == nil {
				t.Fatal("ScanSkillDeps returned nil")
			}
			if m.FromManifest {
				t.Errorf("%s: FromManifest=true (unexpected — bundled skills don't use deps: yet)", name)
			}
			if len(m.Explicit) != 0 {
				t.Errorf("%s: Explicit non-empty: %v", name, m.Explicit)
			}
			if len(m.ExcludeDeps) != 0 {
				t.Errorf("%s: ExcludeDeps non-empty: %v", name, m.ExcludeDeps)
			}
			t.Logf("%s: py=%d node=%d sys=%d python_deps=%v",
				name, len(m.RequiresPython), len(m.RequiresNode), len(m.Requires), m.RequiresPython)
		})
	}
}

func TestBundledSkills_ExpectedCoreSkillSlugs(t *testing.T) {
	bundled := "../../skills"
	expected := map[string]bool{
		"docx":                 false,
		"goclaw":               false,
		"pdf":                  false,
		"pptx":                 false,
		"skill-creator":        false,
		"workspace-organizing": false,
		"xlsx":                 false,
	}

	entries, err := os.ReadDir(bundled)
	if err != nil {
		t.Skip("bundled skills dir not found:", err)
		return
	}

	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_shared" {
			continue
		}
		name := e.Name()
		if _, ok := expected[name]; !ok {
			continue
		}
		expected[name] = true

		meta := parseMetadata(filepath.Join(bundled, name, "SKILL.md"))
		if meta == nil {
			t.Fatalf("%s: missing SKILL.md metadata", name)
		}
		if meta.Name == "" {
			t.Errorf("%s: metadata name is empty", name)
		}
		if meta.Description == "" {
			t.Errorf("%s: metadata description is empty", name)
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected bundled skill %q to exist", name)
		}
	}

	loader := NewLoader("", "", bundled)
	var foundGoclaw bool
	for _, info := range loader.ListSkills(context.Background()) {
		if info.Slug == "goclaw" {
			foundGoclaw = true
			if info.Source != "builtin" {
				t.Errorf("goclaw source = %q, want builtin", info.Source)
			}
			if info.Description == "" {
				t.Error("goclaw description is empty in loader metadata")
			}
		}
	}
	if !foundGoclaw {
		t.Fatal("goclaw was not discoverable by the bundled skills loader")
	}
	content, ok := loader.LoadSkill(context.Background(), "goclaw")
	if !ok {
		t.Fatal("goclaw was not loadable by the bundled skills loader")
	}
	if !strings.Contains(content, "GoClaw Gateway CLI Administration") {
		t.Error("goclaw loaded content does not include the expected guide heading")
	}
}
