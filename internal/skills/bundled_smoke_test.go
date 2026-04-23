package skills

import (
	"os"
	"path/filepath"
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
