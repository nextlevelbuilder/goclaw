package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// kitFixture builds an isolated kit manifest plus skills root inside a temp
// directory so tests never depend on the real repository layout.
//
// Layout produced:
//
//	<tmp>/kit/kit.yaml
//	<tmp>/skills/<slug>/SKILL.md   (one per slug in contentBySlug)
func kitFixture(t *testing.T, contentBySlug map[string]string, checksum string) (manifestPath, skillsRoot string) {
	t.Helper()
	root := t.TempDir()
	kitDir := filepath.Join(root, "kit")
	if err := os.MkdirAll(kitDir, 0o755); err != nil {
		t.Fatalf("kitFixture mkdir kit: %v", err)
	}
	skillsRoot = filepath.Join(root, "skills")

	var b strings.Builder
	b.WriteString("name: go-claw-engineer\n")
	b.WriteString("version: 1.0.0\n")
	b.WriteString("description: Test kit\n")
	b.WriteString("skills:\n")
	for slug := range contentBySlug {
		b.WriteString("  - " + slug + "\n")
	}
	if checksum != "" {
		b.WriteString("checksum: " + checksum + "\n")
	}
	manifestPath = filepath.Join(kitDir, "kit.yaml")
	if err := os.WriteFile(manifestPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("kitFixture write manifest: %v", err)
	}

	for slug, content := range contentBySlug {
		dir := filepath.Join(skillsRoot, slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("kitFixture mkdir skill %s: %v", slug, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("kitFixture write skill %s: %v", slug, err)
		}
	}
	return manifestPath, skillsRoot
}

func TestKitManager_Load_ParsesManifest(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{
		"plan": "---\nname: plan\n---\n# Plan\n",
		"fix":  "---\nname: fix\n---\n# Fix\n",
	}, "")

	mgr := NewKitManager(manifestPath, skillsRoot)
	m, err := mgr.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Name != "go-claw-engineer" {
		t.Errorf("Name: got %q", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version: got %q", m.Version)
	}
	if len(m.Skills) != 2 {
		t.Errorf("Skills: got %v", m.Skills)
	}
}

func TestKitManager_Load_MissingFile(t *testing.T) {
	root := t.TempDir()
	mgr := NewKitManager(filepath.Join(root, "nope", "kit.yaml"), "")
	if _, err := mgr.Load(context.Background()); err == nil {
		t.Fatal("Load: expected error for missing manifest")
	}
}

func TestKitManager_ListSkills_FiltersByDisk(t *testing.T) {
	// plan + fix exist on disk; review is listed in the manifest but missing.
	manifestPath, skillsRoot := kitFixture(t, map[string]string{
		"plan": "# Plan\n",
		"fix":  "# Fix\n",
	}, "")
	manifestPath = rewriteManifestSkills(t, manifestPath, []string{"plan", "fix", "review"})

	mgr := NewKitManager(manifestPath, skillsRoot)
	got := mgr.ListSkills(context.Background())
	if len(got) != 2 || got[0] != "fix" || got[1] != "plan" {
		t.Errorf("ListSkills: got %v, want [fix plan] (sorted, review filtered)", got)
	}
}

func TestKitManager_ListSkills_EmptySkillsRoot(t *testing.T) {
	manifestPath, _ := kitFixture(t, map[string]string{
		"plan": "# Plan\n",
	}, "")
	mgr := NewKitManager(manifestPath, "")
	if got := mgr.ListSkills(context.Background()); len(got) != 0 {
		t.Errorf("ListSkills with empty root: got %v, want none", got)
	}
}

func TestKitManager_Version(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{"plan": "# Plan\n"}, "")
	mgr := NewKitManager(manifestPath, skillsRoot)
	if got := mgr.Version(); got != "1.0.0" {
		t.Errorf("Version: got %q", got)
	}
}

func TestKitManager_ComputeChecksum_Deterministic(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{
		"plan": "# Plan\n",
		"fix":  "# Fix\n",
	}, "")

	mgr := NewKitManager(manifestPath, skillsRoot)
	ctx := context.Background()
	c1, err := mgr.ComputeChecksum(ctx)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}
	c2, err := mgr.ComputeChecksum(ctx)
	if err != nil {
		t.Fatalf("ComputeChecksum second call: %v", err)
	}
	if c1 != c2 {
		t.Errorf("checksum not deterministic: %q != %q", c1, c2)
	}
	if len(c1) != 64 {
		t.Errorf("checksum: expected 64 hex chars, got %q", c1)
	}
}

func TestKitManager_ComputeChecksum_ChangesWithContent(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{
		"plan": "# Plan\n",
	}, "")

	mgr := NewKitManager(manifestPath, skillsRoot)
	ctx := context.Background()
	before, err := mgr.ComputeChecksum(ctx)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	// Mutate one SKILL.md and confirm the checksum changes.
	if err := os.WriteFile(filepath.Join(skillsRoot, "plan", "SKILL.md"), []byte("# Plan V2\n"), 0o644); err != nil {
		t.Fatalf("rewrite skill: %v", err)
	}
	after, err := mgr.ComputeChecksum(ctx)
	if err != nil {
		t.Fatalf("ComputeChecksum after change: %v", err)
	}
	if before == after {
		t.Errorf("checksum should change when skill content changes")
	}
}

func TestKitManager_VerifyChecksum_NoChecksumPinned(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{"plan": "# Plan\n"}, "")
	mgr := NewKitManager(manifestPath, skillsRoot)
	ok, err := mgr.VerifyChecksum(context.Background())
	if err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
	if !ok {
		t.Errorf("manifest without checksum should verify as true")
	}
}

func TestKitManager_VerifyChecksum_PinnedMatchAndMismatch(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{"plan": "# Plan\n"}, "")
	mgr := NewKitManager(manifestPath, skillsRoot)
	ctx := context.Background()

	computed, err := mgr.ComputeChecksum(ctx)
	if err != nil {
		t.Fatalf("ComputeChecksum: %v", err)
	}

	// Pin the correct checksum → verified.
	writeChecksum(t, manifestPath, computed)
	ok, err := mgr.VerifyChecksum(ctx)
	if err != nil {
		t.Fatalf("VerifyChecksum (match): %v", err)
	}
	if !ok {
		t.Errorf("matching checksum should verify as true")
	}

	// Pin a wrong checksum → not verified.
	writeChecksum(t, manifestPath, strings.Repeat("0", 64))
	ok, err = mgr.VerifyChecksum(ctx)
	if err != nil {
		t.Fatalf("VerifyChecksum (mismatch): %v", err)
	}
	if ok {
		t.Errorf("mismatched checksum should verify as false")
	}
}

func TestKitManager_RenderedManifest(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{
		"plan": "# Plan\n",
		"fix":  "# Fix\n",
	}, "")

	mgr := NewKitManager(manifestPath, skillsRoot)
	rendered := mgr.RenderedManifest()
	for _, want := range []string{"name: go-claw-engineer", "version: 1.0.0", "checksum: ", "  - plan"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("RenderedManifest missing %q in:\n%s", want, rendered)
		}
	}
}

func TestKitManager_RenderedManifest_MissingSkillAnnotated(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{"plan": "# Plan\n"}, "")
	manifestPath = rewriteManifestSkills(t, manifestPath, []string{"plan", "ghost"})

	mgr := NewKitManager(manifestPath, skillsRoot)
	rendered := mgr.RenderedManifest()
	if !strings.Contains(rendered, "# missing on disk") {
		t.Errorf("RenderedManifest should annotate missing skills, got:\n%s", rendered)
	}
}

func TestKitManager_Inspect(t *testing.T) {
	manifestPath, skillsRoot := kitFixture(t, map[string]string{
		"plan": "# Plan\n",
		"fix":  "# Fix\n",
	}, "")

	mgr := NewKitManager(manifestPath, skillsRoot)
	info, err := mgr.Inspect(context.Background())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.Name != "go-claw-engineer" {
		t.Errorf("Name: got %q", info.Name)
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version: got %q", info.Version)
	}
	if info.SkillCount != 2 {
		t.Errorf("SkillCount: got %d", info.SkillCount)
	}
	if len(info.Skills) != 2 {
		t.Errorf("Skills: got %v", info.Skills)
	}
	if len(info.Checksum) != 64 {
		t.Errorf("Checksum: got %q", info.Checksum)
	}
	// No checksum pinned → verified is true.
	if !info.Verified {
		t.Errorf("Verified: expected true when no checksum pinned")
	}
}

// rewriteManifestSkills rewrites a manifest's skills list in place using a
// yaml round-trip (unmarshal → replace → marshal) so the resulting file always
// parses cleanly with no duplicate keys. Returns the same manifest path.
func rewriteManifestSkills(t *testing.T, manifestPath string, slugs []string) string {
	t.Helper()
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m KitManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	m.Skills = slugs
	writeManifest(t, manifestPath, &m)
	return manifestPath
}

// writeChecksum pins a checksum value into the manifest via a yaml round-trip.
func writeChecksum(t *testing.T, manifestPath, checksum string) {
	t.Helper()
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m KitManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	m.Checksum = checksum
	writeManifest(t, manifestPath, &m)
}

// writeManifest serializes a manifest struct to YAML and writes it to disk.
func writeManifest(t *testing.T, manifestPath string, m *KitManifest) {
	t.Helper()
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}
