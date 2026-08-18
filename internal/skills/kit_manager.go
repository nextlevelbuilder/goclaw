package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// KitManifest is the on-disk kit descriptor (kit.yaml). The checksum field is
// optional: when present, VerifyChecksum compares the computed checksum against
// it; when absent, verification is skipped and the manifest is reported as
// verified. Checksums are never authored by hand — they change whenever a skill
// is added or edited, so the value is computed at runtime.
type KitManifest struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Skills      []string `yaml:"skills"`
	Checksum    string   `yaml:"checksum,omitempty"`
}

// KitInfo is a point-in-time snapshot of a kit used for inspect/report output.
type KitInfo struct {
	Name       string
	Version    string
	Skills     []string
	Checksum   string
	Verified   bool
	SkillCount int
}

// KitManager reads a kit manifest from disk and computes its checksum from the
// underlying skill files. It never writes to the kit directory.
//
// Layout expectations:
//   - manifestPath points at <kitDir>/kit.yaml.
//   - skillsRoot points at the directory that contains one folder per skill,
//     each holding a SKILL.md (e.g. <repo>/skills/). Slugs listed in the
//     manifest resolve to <skillsRoot>/<slug>/SKILL.md.
//
// An empty skillsRoot yields an empty skill list (ListSkills returns nothing
// and ComputeChecksum hashes an empty set), so a manifest-only kit is still
// representable.
type KitManager struct {
	manifestPath string
	skillsRoot   string
}

// NewKitManager creates a KitManager.
// manifestPath: absolute path to the kit's kit.yaml file.
// skillsRoot: absolute path to the directory containing per-skill folders.
func NewKitManager(manifestPath, skillsRoot string) *KitManager {
	return &KitManager{
		manifestPath: manifestPath,
		skillsRoot:   skillsRoot,
	}
}

// Load reads and parses the kit manifest. Returns an error when the file is
// missing or malformed.
func (k *KitManager) Load(ctx context.Context) (*KitManifest, error) {
	raw, err := os.ReadFile(k.manifestPath)
	if err != nil {
		return nil, fmt.Errorf("kit: read manifest %s: %w", k.manifestPath, err)
	}
	var m KitManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("kit: parse manifest %s: %w", k.manifestPath, err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return nil, fmt.Errorf("kit: manifest %s missing required field name", k.manifestPath)
	}
	if strings.TrimSpace(m.Version) == "" {
		return nil, fmt.Errorf("kit: manifest %s missing required field version", k.manifestPath)
	}
	return &m, nil
}

// ListSkills returns the slugs from the manifest whose SKILL.md actually exists
// under the skills root. Slugs listed in the manifest but missing on disk are
// skipped; a missing skills root yields an empty list. Results are sorted for
// deterministic output.
func (k *KitManager) ListSkills(ctx context.Context) []string {
	m, err := k.Load(ctx)
	if err != nil || k.skillsRoot == "" {
		return nil
	}
	var found []string
	for _, slug := range m.Skills {
		if _, err := os.Stat(filepath.Join(k.skillsRoot, slug, "SKILL.md")); err == nil {
			found = append(found, slug)
		}
	}
	sort.Strings(found)
	return found
}

// Version returns the kit version from the manifest, or "" when the manifest
// cannot be loaded.
func (k *KitManager) Version() string {
	m, err := k.Load(context.Background())
	if err != nil {
		return ""
	}
	return m.Version
}

// ComputeChecksum returns a SHA-256 over the kit's skill content. The input is
// deterministic: for each skill slug (sorted) the hash covers the slug name and
// the raw bytes of its SKILL.md. Skill slugs without a SKILL.md on disk are
// ignored, so the checksum only changes when actually available content
// changes.
func (k *KitManager) ComputeChecksum(ctx context.Context) (string, error) {
	slugs := k.ListSkills(ctx)
	h := sha256.New()
	for _, slug := range slugs {
		content, err := os.ReadFile(filepath.Join(k.skillsRoot, slug, "SKILL.md"))
		if err != nil {
			return "", fmt.Errorf("kit: read skill %s: %w", slug, err)
		}
		// Length-prefix framing keeps slug/content boundaries unambiguous even
		// if one skill's content happens to be byte-identical to another's.
		if _, err := fmt.Fprintf(h, "%d:%s\n", len(slug), slug); err != nil {
			return "", fmt.Errorf("kit: hash slug %s: %w", slug, err)
		}
		if _, err := h.Write(content); err != nil {
			return "", fmt.Errorf("kit: hash skill %s: %w", slug, err)
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("kit: hash skill %s: %w", slug, err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChecksum compares the runtime-computed checksum against the value
// pinned in the manifest. When the manifest has no checksum field the
// verification is skipped: it returns (true, nil) so a manifest without a pin
// is reported as verified rather than failed. A mismatch returns (false, nil).
func (k *KitManager) VerifyChecksum(ctx context.Context) (bool, error) {
	m, err := k.Load(ctx)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(m.Checksum) == "" {
		return true, nil
	}
	actual, err := k.ComputeChecksum(ctx)
	if err != nil {
		return false, err
	}
	return actual == m.Checksum, nil
}

// RenderedManifest returns a YAML-ish text rendering of the current manifest
// and checksum state, used for inspect and dry-run output. It does not modify
// the on-disk manifest.
func (k *KitManager) RenderedManifest() string {
	m, err := k.Load(context.Background())
	if err != nil {
		return fmt.Sprintf("name: %s\nstatus: unreadable (%v)\n", filepath.Base(k.manifestPath), err)
	}
	checksum, csErr := k.ComputeChecksum(context.Background())
	csLine := "checksum: <unavailable>"
	if csErr == nil {
		csLine = "checksum: " + checksum
	}
	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n", m.Name)
	fmt.Fprintf(&b, "version: %s\n", m.Version)
	fmt.Fprintf(&b, "description: %s\n", m.Description)
	fmt.Fprintf(&b, "%s\n", csLine)
	fmt.Fprintf(&b, "skills:\n")
	for _, slug := range m.Skills {
		exists := ""
		if k.skillsRoot != "" {
			if _, err := os.Stat(filepath.Join(k.skillsRoot, slug, "SKILL.md")); err != nil {
				exists = " # missing on disk"
			}
		}
		fmt.Fprintf(&b, "  - %s%s\n", slug, exists)
	}
	return b.String()
}

// Inspect assembles a KitInfo snapshot: manifest metadata, the skills that
// actually exist on disk, the computed checksum, and whether it verifies.
func (k *KitManager) Inspect(ctx context.Context) (*KitInfo, error) {
	m, err := k.Load(ctx)
	if err != nil {
		return nil, err
	}
	checksum, err := k.ComputeChecksum(ctx)
	if err != nil {
		return nil, err
	}
	verified, err := k.VerifyChecksum(ctx)
	if err != nil {
		return nil, err
	}
	skills := k.ListSkills(ctx)
	return &KitInfo{
		Name:       m.Name,
		Version:    m.Version,
		Skills:     skills,
		Checksum:   checksum,
		Verified:   verified,
		SkillCount: len(skills),
	}, nil
}
