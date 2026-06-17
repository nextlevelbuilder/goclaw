package tools

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type skillManageFilesStore struct {
	nextBySlug            map[string]int
	skills                map[uuid.UUID]store.SkillInfo
	owners                map[string]string
	lastUpdates           map[uuid.UUID]map[string]any
	beforeVersionLockHook func(slug string)
}

func newSkillManageFilesStore() *skillManageFilesStore {
	return &skillManageFilesStore{
		nextBySlug:  map[string]int{},
		skills:      map[uuid.UUID]store.SkillInfo{},
		owners:      map[string]string{},
		lastUpdates: map[uuid.UUID]map[string]any{},
	}
}

func skillManageFilesContext() context.Context {
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctx = store.WithUserID(ctx, "owner")
	ctx = store.WithSenderID(ctx, "owner")
	ctx = store.WithAgentID(ctx, uuid.New())
	return ctx
}

func writeManagedSkillVersion(t *testing.T, root, slug string, version int, content string) string {
	t.Helper()
	dir := filepath.Join(root, "skills-store", slug, "1")
	if version != 1 {
		dir = filepath.Join(root, "skills-store", slug, "2")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return dir
}

func seedManagedSkill(t *testing.T, st *skillManageFilesStore, root, slug string, content string) (uuid.UUID, string) {
	t.Helper()
	dir := writeManagedSkillVersion(t, root, slug, 1, content)
	id := uuid.New()
	st.nextBySlug[slug] = 1
	st.owners[slug] = "owner"
	st.skills[id] = store.SkillInfo{
		ID:         id.String(),
		TenantID:   store.MasterTenantID.String(),
		Name:       "Managed Skill",
		Slug:       slug,
		Path:       filepath.Join(dir, "SKILL.md"),
		BaseDir:    dir,
		Version:    1,
		Status:     "active",
		Enabled:    true,
		Visibility: skills.VisibilityPrivate,
		OwnerID:    "owner",
	}
	return id, dir
}

func validManagedSkillMarkdown(slug string) string {
	return "---\nname: Managed Skill\nslug: " + slug + "\n---\nOriginal body\n"
}

func newSkillManageFilesTool(root string, st *skillManageFilesStore) *SkillManageTool {
	return NewSkillManageTool(st, filepath.Join(root, "skills-store"), root, nil)
}

func TestSkillManagePatchFilesOnlyCreatesNewVersionWithReferenceFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action": "patch",
		"slug":   "managed-skill",
		"files": map[string]any{
			"references/ship-workflow.md": "# Ship\n",
		},
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/2/references/ship-workflow.md"); got != "# Ship\n" {
		t.Fatalf("reference content = %q", got)
	}
	if got := st.latestBySlug("managed-skill").Version; got != 2 {
		t.Fatalf("version = %d, want 2", got)
	}
}

func TestSkillManagePatchFindReplaceAndFilesCopiesExistingCompanions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	_, v1Dir := seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))
	if err := os.MkdirAll(filepath.Join(v1Dir, "assets"), 0755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(v1Dir, "assets/logo.txt"), []byte("logo"), 0644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action":  "patch",
		"slug":    "managed-skill",
		"find":    "Original body",
		"replace": "Updated body",
		"files": map[string]any{
			"references/ship-workflow.md": "# Ship\n",
		},
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/2/SKILL.md"); !strings.Contains(got, "Updated body") {
		t.Fatalf("patched SKILL.md missing update: %q", got)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/2/assets/logo.txt"); got != "logo" {
		t.Fatalf("copied asset = %q", got)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/2/references/ship-workflow.md"); got != "# Ship\n" {
		t.Fatalf("reference content = %q", got)
	}
}

func TestSkillManagePatchCopiesExistingHiddenCompanions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	_, v1Dir := seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))
	paths := map[string]string{
		".env.example":                       "TOKEN=\n",
		".github/workflows/check.yml":        "name: check\n",
		"references/ship-workflow.md":        "# Ship\n",
		"references/nested/.keep-example.md": "keep\n",
	}
	for relPath, content := range paths {
		fullPath := filepath.Join(v1Dir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action": "patch",
		"slug":   "managed-skill",
		"files": map[string]any{
			"references/new.md": "# New\n",
		},
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	for relPath, want := range paths {
		got := readTestFile(t, root, filepath.Join("skills-store/managed-skill/2", filepath.ToSlash(relPath)))
		if got != want {
			t.Fatalf("copied %s = %q, want %q", relPath, got, want)
		}
	}
}

func TestSkillManagePatchCopiesExistingLargeCompanionUnderTotalLimit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	_, v1Dir := seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))
	largeContent := strings.Repeat("x", maxManagedSkillFileSize+1)
	largePath := filepath.Join(v1Dir, "assets", "large.txt")
	if err := os.MkdirAll(filepath.Dir(largePath), 0755); err != nil {
		t.Fatalf("mkdir large asset: %v", err)
	}
	if err := os.WriteFile(largePath, []byte(largeContent), 0644); err != nil {
		t.Fatalf("write large asset: %v", err)
	}

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action": "patch",
		"slug":   "managed-skill",
		"files": map[string]any{
			"references/new.md": "# New\n",
		},
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/2/assets/large.txt"); got != largeContent {
		t.Fatalf("large asset length = %d, want %d", len(got), len(largeContent))
	}
}

func TestSkillManagePatchReloadsLatestVersionAfterLock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	id, _ := seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))
	st.beforeVersionLockHook = func(slug string) {
		if slug != "managed-skill" {
			return
		}
		v2Dir := writeManagedSkillVersion(t, root, slug, 2, validManagedSkillMarkdown(slug))
		firstPath := filepath.Join(v2Dir, "references", "first.md")
		if err := os.MkdirAll(filepath.Dir(firstPath), 0755); err != nil {
			t.Fatalf("mkdir concurrent reference: %v", err)
		}
		if err := os.WriteFile(firstPath, []byte("# First\n"), 0644); err != nil {
			t.Fatalf("write concurrent reference: %v", err)
		}
		skill := st.skills[id]
		skill.Version = 2
		skill.BaseDir = v2Dir
		skill.Path = filepath.Join(v2Dir, "SKILL.md")
		st.skills[id] = skill
		st.nextBySlug[slug] = 2
		st.beforeVersionLockHook = nil
	}

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action": "patch",
		"slug":   "managed-skill",
		"files": map[string]any{
			"references/second.md": "# Second\n",
		},
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/3/references/first.md"); got != "# First\n" {
		t.Fatalf("first concurrent reference = %q", got)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/3/references/second.md"); got != "# Second\n" {
		t.Fatalf("second reference = %q", got)
	}
}

func TestSkillManagePatchFilesOverlayExistingCompanion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	_, v1Dir := seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))
	if err := os.MkdirAll(filepath.Join(v1Dir, "references"), 0755); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(filepath.Join(v1Dir, "references/guide.md"), []byte("old"), 0644); err != nil {
		t.Fatalf("write reference: %v", err)
	}

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action": "patch",
		"slug":   "managed-skill",
		"files": map[string]any{
			"references/guide.md": "new",
		},
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	if got := readTestFile(t, root, "skills-store/managed-skill/2/references/guide.md"); got != "new" {
		t.Fatalf("overlaid reference content = %q", got)
	}
}

func TestSkillManageCreateWritesCompanionFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action":  "create",
		"content": validManagedSkillMarkdown("new-skill"),
		"files": map[string]any{
			"references/guide.md": "# Guide\n",
		},
	})
	if res.IsError {
		t.Fatalf("create returned error: %s", res.ForLLM)
	}
	if got := readTestFile(t, root, "skills-store/new-skill/1/references/guide.md"); got != "# Guide\n" {
		t.Fatalf("reference content = %q", got)
	}
}

func TestSkillManageVisibilityOnlyPatchDoesNotCreateNewVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	id, _ := seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action":     "patch",
		"slug":       "managed-skill",
		"visibility": skills.VisibilityPublic,
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	if got := st.latestBySlug("managed-skill").Version; got != 1 {
		t.Fatalf("version = %d, want unchanged v1", got)
	}
	if _, err := os.Stat(filepath.Join(root, "skills-store/managed-skill/2")); !os.IsNotExist(err) {
		t.Fatalf("version 2 dir exists after visibility-only patch: err=%v", err)
	}
	if got := st.lastUpdates[id]["visibility"]; got != skills.VisibilityPublic {
		t.Fatalf("visibility update = %v, want public", got)
	}
}

func TestSkillManageFilesRejectUnsafePathsBeforeCreatingVersion(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/abs.md",
		"../escape.md",
		`C:/escape.md`,
		"references/ok\x00.md",
		".git/config",
		".env",
		"references/.secret",
		"__MACOSX/x",
		".DS_Store",
		"SKILL.md",
	}
	for _, relPath := range cases {
		t.Run(relPath, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			st := newSkillManageFilesStore()
			ctx := skillManageFilesContext()
			seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))

			res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
				"action": "patch",
				"slug":   "managed-skill",
				"files": map[string]any{
					relPath: "bad",
				},
			})
			if !res.IsError {
				t.Fatalf("patch succeeded for unsafe path %q: %s", relPath, res.ForLLM)
			}
			if !strings.Contains(res.ForLLM, "invalid file path") {
				t.Fatalf("error = %q, want invalid file path", res.ForLLM)
			}
			if _, err := os.Stat(filepath.Join(root, "skills-store/managed-skill/2")); !os.IsNotExist(err) {
				t.Fatalf("version 2 dir exists after rejected path: err=%v", err)
			}
		})
	}
}

func TestSkillManageFilesRejectNonStringPayload(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action": "patch",
		"slug":   "managed-skill",
		"files": map[string]any{
			"references/guide.md": map[string]any{"nested": "no"},
		},
	})
	if !res.IsError {
		t.Fatalf("patch succeeded with non-string payload: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "must be a string") {
		t.Fatalf("error = %q, want string validation", res.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(root, "skills-store/managed-skill/2")); !os.IsNotExist(err) {
		t.Fatalf("version 2 dir exists after rejected payload: err=%v", err)
	}
}

func TestSkillManageFilesRejectOversizePayloadBeforeCreatingVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action": "patch",
		"slug":   "managed-skill",
		"files": map[string]any{
			"references/too-large.md": strings.Repeat("x", maxManagedSkillFileSize+1),
		},
	})
	if !res.IsError {
		t.Fatalf("patch succeeded with oversize payload: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "too large") {
		t.Fatalf("error = %q, want size validation", res.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(root, "skills-store/managed-skill/2")); !os.IsNotExist(err) {
		t.Fatalf("version 2 dir exists after rejected payload: err=%v", err)
	}
}

func TestSkillManagePatchFindMissWithFilesDoesNotCreateVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := newSkillManageFilesStore()
	ctx := skillManageFilesContext()
	seedManagedSkill(t, st, root, "managed-skill", validManagedSkillMarkdown("managed-skill"))

	res := newSkillManageFilesTool(root, st).Execute(ctx, map[string]any{
		"action":  "patch",
		"slug":    "managed-skill",
		"find":    "missing text",
		"replace": "replacement",
		"files": map[string]any{
			"references/guide.md": "# Guide\n",
		},
	})
	if res.IsError {
		t.Fatalf("patch returned error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "no change") {
		t.Fatalf("result = %q, want no-change message", res.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(root, "skills-store/managed-skill/2")); !os.IsNotExist(err) {
		t.Fatalf("version 2 dir exists after missing find: err=%v", err)
	}
}

func readTestFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func (s *skillManageFilesStore) latestBySlug(slug string) store.SkillInfo {
	var latest store.SkillInfo
	for _, skill := range s.skills {
		if skill.Slug == slug && skill.Version > latest.Version {
			latest = skill
		}
	}
	return latest
}

func (s *skillManageFilesStore) ListSkills(context.Context) []store.SkillInfo { return nil }
func (s *skillManageFilesStore) LoadSkill(context.Context, string) (string, bool) {
	return "", false
}
func (s *skillManageFilesStore) LoadForContext(context.Context, []string) string { return "" }
func (s *skillManageFilesStore) BuildSummary(context.Context, []string) string   { return "" }
func (s *skillManageFilesStore) GetSkill(_ context.Context, slug string) (*store.SkillInfo, bool) {
	for _, skill := range s.skills {
		if skill.Slug == slug && skill.Status != "deleted" {
			copy := skill
			return &copy, true
		}
	}
	return nil, false
}
func (s *skillManageFilesStore) FilterSkills(context.Context, []string) []store.SkillInfo {
	return nil
}
func (s *skillManageFilesStore) Version() int64 { return 0 }
func (s *skillManageFilesStore) BumpVersion()   {}
func (s *skillManageFilesStore) Dirs() []string { return nil }
func (s *skillManageFilesStore) CreateSkillManaged(ctx context.Context, p store.SkillCreateParams) (uuid.UUID, error) {
	id := uuid.New()
	version := p.Version
	if version == 0 {
		version = s.nextBySlug[p.Slug] + 1
	}
	if version > s.nextBySlug[p.Slug] {
		s.nextBySlug[p.Slug] = version
	}
	s.owners[p.Slug] = p.OwnerID
	s.skills[id] = store.SkillInfo{
		ID:          id.String(),
		TenantID:    store.MasterTenantID.String(),
		Name:        p.Name,
		Slug:        p.Slug,
		Description: derefString(p.Description),
		Path:        filepath.Join(p.FilePath, "SKILL.md"),
		BaseDir:     p.FilePath,
		Version:     version,
		Status:      "active",
		Enabled:     true,
		Visibility:  p.Visibility,
		OwnerID:     p.OwnerID,
	}
	return id, nil
}
func (s *skillManageFilesStore) UpdateSkill(_ context.Context, id uuid.UUID, updates map[string]any) error {
	skill, ok := s.skills[id]
	if !ok {
		return nil
	}
	if version, ok := updates["version"].(int); ok {
		skill.Version = version
		if version > s.nextBySlug[skill.Slug] {
			s.nextBySlug[skill.Slug] = version
		}
	}
	if filePath, ok := updates["file_path"].(string); ok {
		skill.BaseDir = filePath
		skill.Path = filepath.Join(filePath, "SKILL.md")
	}
	if visibility, ok := updates["visibility"].(string); ok {
		skill.Visibility = visibility
	}
	s.lastUpdates[id] = maps.Clone(updates)
	s.skills[id] = skill
	return nil
}
func (s *skillManageFilesStore) DeleteSkill(context.Context, uuid.UUID) error       { return nil }
func (s *skillManageFilesStore) ToggleSkill(context.Context, uuid.UUID, bool) error { return nil }
func (s *skillManageFilesStore) GetSkillByID(_ context.Context, id uuid.UUID) (store.SkillInfo, bool) {
	info, ok := s.skills[id]
	return info, ok
}
func (s *skillManageFilesStore) GetSkillOwnerID(context.Context, uuid.UUID) (string, bool) {
	return "", false
}
func (s *skillManageFilesStore) GetSkillOwnerIDBySlug(_ context.Context, slug string) (string, bool) {
	owner, ok := s.owners[slug]
	return owner, ok
}
func (s *skillManageFilesStore) GetNextVersion(_ context.Context, slug string) int {
	return s.nextBySlug[slug] + 1
}
func (s *skillManageFilesStore) GetNextVersionLocked(_ context.Context, slug string) (int, func() error, error) {
	if s.beforeVersionLockHook != nil {
		s.beforeVersionLockHook(slug)
	}
	return s.GetNextVersion(context.Background(), slug), func() error { return nil }, nil
}
func (s *skillManageFilesStore) GetSkillHashBySlug(context.Context, string) (string, int, bool) {
	return "", 0, false
}
func (s *skillManageFilesStore) IsSystemSkill(string) bool                       { return false }
func (s *skillManageFilesStore) ListAllSkills(context.Context) []store.SkillInfo { return nil }
func (s *skillManageFilesStore) ListAllSystemSkills(context.Context) []store.SkillInfo {
	return nil
}
func (s *skillManageFilesStore) ListSystemSkillDirs(context.Context) map[string]string {
	return nil
}
func (s *skillManageFilesStore) StoreMissingDeps(context.Context, uuid.UUID, []string) error {
	return nil
}
func (s *skillManageFilesStore) GrantToAgent(context.Context, uuid.UUID, uuid.UUID, int, string, ...bool) error {
	return nil
}
func (s *skillManageFilesStore) RevokeFromAgent(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *skillManageFilesStore) GrantToUser(context.Context, uuid.UUID, string, string) error {
	return nil
}
func (s *skillManageFilesStore) RevokeFromUser(context.Context, uuid.UUID, string) error { return nil }
func (s *skillManageFilesStore) ListWithGrantStatus(context.Context, uuid.UUID) ([]store.SkillWithGrantStatus, error) {
	return nil, nil
}
func (s *skillManageFilesStore) ListAgentGrantsForSkill(context.Context, uuid.UUID) ([]store.SkillAgentGrantInfo, error) {
	return nil, nil
}
func (s *skillManageFilesStore) ListUserGrantsForSkill(context.Context, uuid.UUID) ([]store.SkillUserGrantInfo, error) {
	return nil, nil
}
func (s *skillManageFilesStore) AgentCanManageSkill(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *skillManageFilesStore) GetSkillFilePath(context.Context, uuid.UUID) (string, string, int, bool, bool) {
	return "", "", 0, false, false
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
