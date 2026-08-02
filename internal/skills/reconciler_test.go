package skills

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type reconcilerStore struct {
	registered []store.SkillCreateParams
	exists     map[string]bool
	bumped     int
}

func (s *reconcilerStore) SkillExists(_ context.Context, slug string) (bool, error) {
	return s.exists[slug], nil
}

func (s *reconcilerStore) CreateSkillManaged(_ context.Context, p store.SkillCreateParams) (uuid.UUID, error) {
	s.registered = append(s.registered, p)
	s.exists[p.Slug] = true
	return uuid.New(), nil
}

func (s *reconcilerStore) BumpVersion() { s.bumped++ }

func writeSkillFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func TestReconciler_RegistersMissingOnDiskSkill(t *testing.T) {
	managedDir := filepath.Join(t.TempDir(), "skills-store")
	content := "---\nname: connect-google-account\ndescription: Connect a user's Google account\n---\n\n# Google\n"
	writeSkillFile(t, filepath.Join(managedDir, "connect-google-account", "1", "SKILL.md"), content)

	storeStub := &reconcilerStore{exists: map[string]bool{}}
	r := NewReconciler(storeStub)

	n, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if n != 1 {
		t.Fatalf("registered = %d, want 1", n)
	}
	if storeStub.bumped != 1 {
		t.Fatalf("bump count = %d, want 1", storeStub.bumped)
	}
	if len(storeStub.registered) != 1 {
		t.Fatalf("registered rows = %d, want 1", len(storeStub.registered))
	}
	p := storeStub.registered[0]
	if p.Slug != "connect-google-account" {
		t.Fatalf("slug = %q, want connect-google-account", p.Slug)
	}
	if p.Name != "connect-google-account" {
		t.Fatalf("name = %q, want connect-google-account", p.Name)
	}
	if p.Visibility != VisibilityPublic {
		t.Fatalf("visibility = %q, want public (reconciler cannot grant private/internal)", p.Visibility)
	}
	if p.Status != "active" {
		t.Fatalf("status = %q, want active", p.Status)
	}
	if p.Version != 1 {
		t.Fatalf("version = %d, want on-disk version 1", p.Version)
	}
	if p.FilePath != filepath.Join(managedDir, "connect-google-account", "1") {
		t.Fatalf("file path = %q, want version dir", p.FilePath)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	if p.FileHash == nil || *p.FileHash != wantHash {
		t.Fatalf("file hash = %v, want %s", p.FileHash, wantHash)
	}
	if p.Description == nil || *p.Description != "Connect a user's Google account" {
		t.Fatalf("description = %v, want frontmatter description", p.Description)
	}
}

func TestReconciler_PicksLatestVersion(t *testing.T) {
	managedDir := filepath.Join(t.TempDir(), "skills-store")
	writeSkillFile(t, filepath.Join(managedDir, "doc-helper", "1", "SKILL.md"), "---\nname: doc-helper\n---\n# v1\n")
	writeSkillFile(t, filepath.Join(managedDir, "doc-helper", "2", "SKILL.md"), "---\nname: doc-helper\ndescription: v2\n---\n# v2\n")

	storeStub := &reconcilerStore{exists: map[string]bool{}}
	r := NewReconciler(storeStub)

	if _, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir); err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(storeStub.registered) != 1 {
		t.Fatalf("registered rows = %d, want 1", len(storeStub.registered))
	}
	p := storeStub.registered[0]
	if p.Version != 2 {
		t.Fatalf("version = %d, want latest disk version 2", p.Version)
	}
	if p.Description == nil || *p.Description != "v2" {
		t.Fatalf("description = %v, want latest version metadata", p.Description)
	}
}

func TestReconciler_SkipsExistingSkills(t *testing.T) {
	managedDir := filepath.Join(t.TempDir(), "skills-store")
	writeSkillFile(t, filepath.Join(managedDir, "already-registered", "1", "SKILL.md"), "---\nname: already-registered\n---\n")

	storeStub := &reconcilerStore{exists: map[string]bool{"already-registered": true}}
	r := NewReconciler(storeStub)

	n, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if n != 0 {
		t.Fatalf("registered = %d, want 0", n)
	}
	if len(storeStub.registered) != 0 {
		t.Fatalf("created rows = %d, want 0 (idempotent)", len(storeStub.registered))
	}
	if storeStub.bumped != 0 {
		t.Fatalf("bump count = %d, want 0", storeStub.bumped)
	}
}

func TestReconciler_DoesNotResurrectDeletedSkill(t *testing.T) {
	// A deleted skill keeps its directory (DeleteSkill is a soft delete); the
	// reconciler must not re-register it.
	managedDir := filepath.Join(t.TempDir(), "skills-store")
	writeSkillFile(t, filepath.Join(managedDir, "removed", "1", "SKILL.md"), "---\nname: removed\n---\n")

	storeStub := &reconcilerStore{exists: map[string]bool{"removed": true}}
	r := NewReconciler(storeStub)

	if _, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir); err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(storeStub.registered) != 0 {
		t.Fatalf("re-registered deleted skill = %d rows, want 0", len(storeStub.registered))
	}
}

func TestReconciler_SkipsSharedAndVersionlessDirs(t *testing.T) {
	managedDir := filepath.Join(t.TempDir(), "skills-store")
	writeSkillFile(t, filepath.Join(managedDir, "_shared", "SKILL.md"), "shared helper, not a skill")
	writeSkillFile(t, filepath.Join(managedDir, "no-version", "SKILL.md"), "not versioned")

	storeStub := &reconcilerStore{exists: map[string]bool{}}
	r := NewReconciler(storeStub)

	n, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if n != 0 {
		t.Fatalf("registered = %d, want 0", n)
	}
	if len(storeStub.registered) != 0 {
		t.Fatalf("registered rows = %d, want 0", len(storeStub.registered))
	}
}

func TestReconciler_RegistersAgainWhenRowDeletedByOtherWriter(t *testing.T) {
	// Sanity check for the idempotency guarantee after a successful run:
	// a second run over the same dir is a no-op.
	managedDir := filepath.Join(t.TempDir(), "skills-store")
	writeSkillFile(t, filepath.Join(managedDir, "foo", "1", "SKILL.md"), "---\nname: foo\n---\n")

	storeStub := &reconcilerStore{exists: map[string]bool{}}
	r := NewReconciler(storeStub)

	first, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir)
	if err != nil {
		t.Fatalf("first Reconcile error: %v", err)
	}
	second, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir)
	if err != nil {
		t.Fatalf("second Reconcile error: %v", err)
	}
	if first != 1 || second != 0 {
		t.Fatalf("runs = (%d, %d), want (1, 0)", first, second)
	}
	if len(storeStub.registered) != 1 {
		t.Fatalf("registered rows = %d, want 1", len(storeStub.registered))
	}
}

func TestReconciler_MissingManagedDirIsNoop(t *testing.T) {
	storeStub := &reconcilerStore{exists: map[string]bool{}}
	r := NewReconciler(storeStub)

	n, err := r.Reconcile(context.Background(), store.MasterTenantID, filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if n != 0 || len(storeStub.registered) != 0 {
		t.Fatalf("registered = %d rows=%d, want no-op", n, len(storeStub.registered))
	}
}

func TestReconciler_FrontmatterVisibilityHonoredOnlyWhenPublic(t *testing.T) {
	managedDir := filepath.Join(t.TempDir(), "skills-store")
	writeSkillFile(t, filepath.Join(managedDir, "pub", "1", "SKILL.md"),
		"---\nname: pub\nvisibility: public\n---\n")
	writeSkillFile(t, filepath.Join(managedDir, "priv", "1", "SKILL.md"),
		"---\nname: priv\nvisibility: private\n---\n")

	storeStub := &reconcilerStore{exists: map[string]bool{}}
	r := NewReconciler(storeStub)

	if _, err := r.Reconcile(context.Background(), store.MasterTenantID, managedDir); err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if len(storeStub.registered) != 2 {
		t.Fatalf("registered rows = %d, want 2", len(storeStub.registered))
	}
	vis := map[string]string{}
	for _, p := range storeStub.registered {
		vis[p.Slug] = p.Visibility
	}
	if vis["pub"] != VisibilityPublic {
		t.Fatalf("pub visibility = %q, want public", vis["pub"])
	}
	if vis["priv"] != VisibilityPublic {
		t.Fatalf("priv visibility = %q, want public (reconciler cannot grant private)", vis["priv"])
	}
}
