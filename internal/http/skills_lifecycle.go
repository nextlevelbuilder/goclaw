package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var (
	scanSkillDeps                  = skills.ScanSkillDeps
	checkSkillDepsContext          = skills.CheckSkillDepsContext
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
	writeJSON(w, http.StatusOK, h.buildSkillDependencyStatus(r.Context(), sk))
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
	checked := lifecycleCheckSkillDeps(r.Context(), manifest)
	if checked.Incomplete || checked.Status == skills.DepCheckUnknown {
		writeJSON(w, http.StatusOK, dependencyResponse(sk, manifest, checked))
		return
	}
	var installed *skills.InstallResult
	if checked.Status == skills.DepCheckMissing {
		installedResult, err := installLifecycleDeps(r.Context(), manifest, checked.Missing)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		installed = installedResult
		checked = lifecycleCheckSkillDeps(r.Context(), manifest)
	}
	persisted, changed, err := h.persistSkillDependencyState(r.Context(), sk, checked)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if changed {
		h.emitCacheInvalidate(bus.CacheKindSkills, sk.ID, uuid.Nil)
	}
	resp := dependencyResponse(persisted, manifest, checked)
	if installed != nil {
		writeJSON(w, http.StatusOK, map[string]any{"result": installed, "dependencies": resp})
	} else {
		writeJSON(w, http.StatusOK, resp)
	}

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

func (h *SkillsHandler) buildSkillDependencyStatus(ctx context.Context, sk store.SkillInfo) skillDependencyStatusResponse {
	manifest := h.scanWithFallback(sk)
	return dependencyResponse(sk, manifest, lifecycleCheckSkillDeps(ctx, manifest))
}

func dependencyResponse(sk store.SkillInfo, manifest *skills.SkillManifest, checked skills.DepCheckResult) skillDependencyStatusResponse {
	incomplete := checked.Incomplete || checked.Status == skills.DepCheckUnknown
	status := "unknown"
	if checked.Status == skills.DepCheckMissing {
		status = "missing"
	} else if !incomplete {
		status = "ok"
	}
	resp := skillDependencyStatusResponse{
		Skill: refForSkill(sk), OK: checked.Status == skills.DepCheckPresent && !incomplete,
		Status: status, Missing: checked.Missing, MissingCount: len(checked.Missing),
	}
	if manifest != nil {
		resp.Dependencies = dependencyItems(manifest, checked.Missing, incomplete)
	}
	return resp
}

// persistSkillDependencyState writes status and dependency data together. A
// failed or cancelled SQL statement leaves both old values intact.
func (h *SkillsHandler) persistSkillDependencyState(ctx context.Context, sk store.SkillInfo, checked skills.DepCheckResult) (store.SkillInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return sk, false, err
	}
	if checked.Incomplete || checked.Status == skills.DepCheckUnknown {
		return sk, false, nil
	}
	id, err := uuid.Parse(sk.ID)
	if err != nil {
		return sk, false, err
	}
	wantStatus := "archived"
	if checked.Status == skills.DepCheckPresent {
		wantStatus = "active"
	}
	missing := checked.Missing
	if missing == nil {
		missing = []string{}
	}
	if sk.Status == wantStatus && stringSlicesEqual(sk.MissingDeps, missing) {
		return sk, false, nil
	}
	encoded, err := json.Marshal(map[string]any{"missing": missing})
	if err != nil {
		return sk, false, err
	}
	if err := h.skills.UpdateSkill(skillTenantContext(ctx, sk), id, map[string]any{"status": wantStatus, "deps": string(encoded)}); err != nil {
		return sk, false, err
	}
	sk.Status = wantStatus
	sk.MissingDeps = append([]string(nil), missing...)
	return sk, true, nil
}

func refForSkill(sk store.SkillInfo) skillRef {
	return skillRef{ID: sk.ID, Slug: sk.Slug, Name: sk.Name, Status: sk.Status, Version: sk.Version}
}
