package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestHandleWriteFile_UpdatesManagedSkillContent(t *testing.T) {
	baseDir := t.TempDir()
	versionDir := filepath.Join(baseDir, "skills-store", "my-skill", "1")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "SKILL.md"), []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	skillStore := newSkillManageStoreStub(baseDir)
	id := skillStore.seedCustomSkill("my-skill", versionDir, "active", nil)
	handler := NewSkillsHandler(skillStore, baseDir, baseDir, "", bus.New(), nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/skills/"+id.String()+"/files/SKILL.md", bytes.NewBufferString(`{"content":"new content"}`))
	req.SetPathValue("id", id.String())
	req.SetPathValue("path", "SKILL.md")
	req = req.WithContext(store.WithTenantID(req.Context(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleWriteFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// The original version directory remains untouched (historical versions
	// are immutable).
	origGot, err := os.ReadFile(filepath.Join(versionDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(origGot) != "old content" {
		t.Fatalf("original version content = %q, want unchanged %q", origGot, "old content")
	}

	// A new version directory was created with the edited content, and the
	// skill's DB row now points at it with a bumped version number.
	updated, ok := skillStore.skills[id]
	if !ok {
		t.Fatal("skill not found in store after update")
	}
	if updated.Version != 2 {
		t.Fatalf("version = %d, want 2", updated.Version)
	}
	newVersionDir := filepath.Join(baseDir, "skills-store", "my-skill", "2")
	got, err := os.ReadFile(filepath.Join(newVersionDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Fatalf("content = %q, want %q", got, "new content")
	}
}

func TestHandleWriteFile_RejectsSystemSkill(t *testing.T) {
	baseDir := t.TempDir()
	versionDir := filepath.Join(baseDir, "sys-skill")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "SKILL.md"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	skillStore := newSkillManageStoreStub(baseDir)
	id := skillStore.seedSystemSkill("sys-skill", versionDir)
	handler := NewSkillsHandler(skillStore, baseDir, baseDir, "", bus.New(), nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/skills/"+id.String()+"/files/SKILL.md", bytes.NewBufferString(`{"content":"hacked"}`))
	req.SetPathValue("id", id.String())
	req.SetPathValue("path", "SKILL.md")
	req = req.WithContext(store.WithTenantID(req.Context(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleWriteFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(versionDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("system skill content changed: %q", got)
	}
}

func TestHandleWriteFile_RejectsPathTraversal(t *testing.T) {
	baseDir := t.TempDir()
	versionDir := filepath.Join(baseDir, "my-skill", "1")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	skillStore := newSkillManageStoreStub(baseDir)
	id := skillStore.seedCustomSkill("my-skill", versionDir, "active", nil)
	handler := NewSkillsHandler(skillStore, baseDir, baseDir, "", bus.New(), nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/skills/"+id.String()+"/files/../../etc/passwd", bytes.NewBufferString(`{"content":"pwned"}`))
	req.SetPathValue("id", id.String())
	req.SetPathValue("path", "../../etc/passwd")
	req = req.WithContext(store.WithTenantID(req.Context(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleWriteFile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
