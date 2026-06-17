package http

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var (
	scanSkillDeps                  = skills.ScanSkillDeps
	checkSkillDeps                 = skills.CheckSkillDeps
	githubSkillDependencyInstalled = skillGitHubDependencyInstalled
)

type skillRef struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version int    `json:"version"`
}

type skillDependencyItem struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type skillDependencyStatusResponse struct {
	Skill        skillRef              `json:"skill"`
	Dependencies []skillDependencyItem `json:"dependencies"`
	OK           bool                  `json:"ok"`
	Status       string                `json:"status"`
	Missing      []string              `json:"missing,omitempty"`
	MissingCount int                   `json:"missing_count"`
}

func (h *SkillsHandler) handleSkillDependenciesStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireMasterTenant(w, r) {
		return
	}
	sk, ok := h.lifecycleSkill(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.buildSkillDependencyStatus(sk))
}

func (h *SkillsHandler) handleSkillDependenciesInstall(w http.ResponseWriter, r *http.Request) {
	if !h.requireMasterTenant(w, r) {
		return
	}
	sk, ok := h.lifecycleSkill(w, r, true)
	if !ok {
		return
	}
	manifest := h.scanWithFallback(sk)
	if manifest == nil || manifest.IsEmpty() {
		h.persistSkillDependencyState(r.Context(), sk, true, nil)
		writeJSON(w, http.StatusOK, h.buildSkillDependencyStatus(sk))
		return
	}
	_, missing := lifecycleCheckSkillDeps(manifest)
	if len(missing) == 0 {
		h.persistSkillDependencyState(r.Context(), sk, true, nil)
		writeJSON(w, http.StatusOK, h.buildSkillDependencyStatus(sk))
		return
	}
	result, err := installLifecycleDeps(r.Context(), manifest, missing)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	okAfter, missingAfter := lifecycleCheckSkillDeps(manifest)
	h.persistSkillDependencyState(r.Context(), sk, okAfter, missingAfter)
	resp := h.buildSkillDependencyStatus(sk)
	resp.OK = okAfter
	resp.Missing = missingAfter
	resp.MissingCount = len(missingAfter)
	resp.Status = dependencyStatus(okAfter)
	writeJSON(w, http.StatusOK, map[string]any{"result": result, "dependencies": resp})
}

func (h *SkillsHandler) lifecycleSkill(w http.ResponseWriter, r *http.Request, crossTenant bool) (store.SkillInfo, bool) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "skill")})
		return store.SkillInfo{}, false
	}
	ctx := r.Context()
	if crossTenant {
		ctx = store.WithCrossTenant(store.WithTenantID(ctx, store.MasterTenantID))
	}
	sk, ok := h.skills.GetSkillByID(ctx, id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "skill", id.String())})
		return store.SkillInfo{}, false
	}
	return sk, true
}

func (h *SkillsHandler) buildSkillDependencyStatus(sk store.SkillInfo) skillDependencyStatusResponse {
	manifest := h.scanWithFallback(sk)
	if manifest == nil || manifest.IsEmpty() {
		return skillDependencyStatusResponse{Skill: refForSkill(sk), OK: true, Status: "ok"}
	}
	ok, missing := lifecycleCheckSkillDeps(manifest)
	return skillDependencyStatusResponse{
		Skill:        refForSkill(sk),
		Dependencies: dependencyItems(manifest, missing),
		OK:           ok,
		Status:       dependencyStatus(ok),
		Missing:      missing,
		MissingCount: len(missing),
	}
}

func (h *SkillsHandler) persistSkillDependencyState(ctx context.Context, sk store.SkillInfo, ok bool, missing []string) {
	id, err := uuid.Parse(sk.ID)
	if err != nil {
		return
	}
	updateCtx := skillTenantContext(ctx, sk)
	_ = h.skills.StoreMissingDeps(updateCtx, id, missing)
	wantStatus := "archived"
	if ok {
		wantStatus = "active"
	}
	if sk.Status != wantStatus {
		_ = h.skills.UpdateSkill(updateCtx, id, map[string]any{"status": wantStatus})
		h.emitCacheInvalidate(bus.CacheKindSkills, sk.ID, uuid.Nil)
	}
}

func refForSkill(sk store.SkillInfo) skillRef {
	return skillRef{ID: sk.ID, Slug: sk.Slug, Name: sk.Name, Status: sk.Status, Version: sk.Version}
}
