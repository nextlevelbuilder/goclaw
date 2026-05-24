package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSkillsHandlerUpdateAcceptsInternalVisibility(t *testing.T) {
	baseDir := t.TempDir()
	skillStore := newSkillManageStoreStub(baseDir)
	id := skillStore.seedCustomSkill("access-mode-skill", filepath.Join(baseDir, "access-mode-skill", "1"), "active", nil)
	handler := NewSkillsHandler(skillStore, baseDir, baseDir, "", bus.New(), nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/skills/"+id.String(), bytes.NewBufferString(`{"visibility":"INTERNAL"}`))
	req.SetPathValue("id", id.String())
	req = req.WithContext(store.WithTenantID(req.Context(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := skillStore.lastUpdates[id]["visibility"]; got != "internal" {
		t.Fatalf("updated visibility = %#v, want internal", got)
	}
	info, ok := skillStore.GetSkillByID(req.Context(), id)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if info.Visibility != "internal" {
		t.Fatalf("stored visibility = %q, want internal", info.Visibility)
	}
}

func TestSkillsHandlerUpdateNormalizesEmptyVisibilityToPrivate(t *testing.T) {
	baseDir := t.TempDir()
	skillStore := newSkillManageStoreStub(baseDir)
	id := skillStore.seedCustomSkill("empty-access-mode-skill", filepath.Join(baseDir, "empty-access-mode-skill", "1"), "active", nil)
	handler := NewSkillsHandler(skillStore, baseDir, baseDir, "", bus.New(), nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/skills/"+id.String(), bytes.NewBufferString(`{"visibility":""}`))
	req.SetPathValue("id", id.String())
	req = req.WithContext(store.WithTenantID(req.Context(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := skillStore.lastUpdates[id]["visibility"]; got != "private" {
		t.Fatalf("updated visibility = %#v, want private", got)
	}
}

func TestSkillsHandlerUpdateRejectsInvalidVisibility(t *testing.T) {
	baseDir := t.TempDir()
	skillStore := newSkillManageStoreStub(baseDir)
	id := skillStore.seedCustomSkill("bad-access-mode-skill", filepath.Join(baseDir, "bad-access-mode-skill", "1"), "active", nil)
	handler := NewSkillsHandler(skillStore, baseDir, baseDir, "", bus.New(), nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/skills/"+id.String(), bytes.NewBufferString(`{"visibility":"team"}`))
	req.SetPathValue("id", id.String())
	req = req.WithContext(store.WithTenantID(req.Context(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleUpdate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, ok := skillStore.lastUpdates[id]; ok {
		t.Fatalf("invalid visibility reached UpdateSkill: %+v", skillStore.lastUpdates[id])
	}
}

func TestSkillsHandlerUpdateRejectsNonStringVisibility(t *testing.T) {
	baseDir := t.TempDir()
	skillStore := newSkillManageStoreStub(baseDir)
	id := skillStore.seedCustomSkill("typed-access-mode-skill", filepath.Join(baseDir, "typed-access-mode-skill", "1"), "active", nil)
	handler := NewSkillsHandler(skillStore, baseDir, baseDir, "", bus.New(), nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/v1/skills/"+id.String(), bytes.NewBufferString(`{"visibility":true}`))
	req.SetPathValue("id", id.String())
	req = req.WithContext(store.WithTenantID(req.Context(), store.MasterTenantID))
	rec := httptest.NewRecorder()

	handler.handleUpdate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, ok := skillStore.lastUpdates[id]; ok {
		t.Fatalf("non-string visibility reached UpdateSkill: %+v", skillStore.lastUpdates[id])
	}
}
