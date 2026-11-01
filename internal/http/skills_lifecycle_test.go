package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestHandleSkillDependenciesStatus_ReturnsStructuredMissingBySource(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	skillDir := filepath.Join(root, "skills-store", "dep-skill", "1")
	skillID := skillStore.seedCustomSkill("dep-skill", skillDir, "archived", []string{"ffmpeg", "pip:requests"})

	prevScan := scanSkillDeps
	prevCheck := checkSkillDepsContext
	prevGitHubInstalled := githubSkillDependencyInstalled
	scanSkillDeps = func(string) *skills.SkillManifest {
		return &skills.SkillManifest{
			Requires:       []string{"ffmpeg"},
			RequiresPython: []string{"requests"},
			RequiresNode:   []string{"tsx"},
			Explicit:       []string{"github:cli/cli@v2.40.0"},
		}
	}
	checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
		return skills.DepCheckResult{Status: skills.DepCheckMissing, Missing: []string{"ffmpeg", "pip:requests"}}
	}
	githubSkillDependencyInstalled = func(string) bool { return false }
	t.Cleanup(func() {
		scanSkillDeps = prevScan
		checkSkillDepsContext = prevCheck
		githubSkillDependencyInstalled = prevGitHubInstalled
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/skills/"+skillID.String()+"/dependencies", http.NoBody).WithContext(ctx)
	req.SetPathValue("id", skillID.String())
	w := httptest.NewRecorder()

	handler.handleSkillDependenciesStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		OK           bool `json:"ok"`
		MissingCount int  `json:"missing_count"`
		Dependencies []struct {
			Source string `json:"source"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"dependencies"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OK {
		t.Fatal("ok = true, want false")
	}
	if resp.MissingCount != 3 {
		t.Fatalf("missing_count = %d, want 3", resp.MissingCount)
	}
	got := map[string]string{}
	for _, dep := range resp.Dependencies {
		got[dep.Source+":"+dep.Name] = dep.Status
	}
	want := map[string]string{
		"system:ffmpeg":          "missing",
		"pip:requests":           "missing",
		"npm:tsx":                "installed",
		"github:cli/cli@v2.40.0": "missing",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %#v, want %#v", got, want)
	}
}

func TestSkillDependencyRoutesRequireAdmin(t *testing.T) {
	handler, skillStore, _, root := newTestUploadHandler(t)
	skillID := skillStore.seedCustomSkill("dep-skill", filepath.Join(root, "skills-store", "dep-skill", "1"), "active", nil)
	tenantID := uuid.New()
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey("read-token"): {ID: uuid.New(), Scopes: []string{"operator.read"}},
		crypto.HashAPIKey("tenant-admin-token"): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.admin"},
			TenantID: tenantID,
			OwnerID:  "tenant-admin",
		},
	})

	prevScan := scanSkillDeps
	scanCalled := false
	scanSkillDeps = func(string) *skills.SkillManifest {
		scanCalled = true
		return &skills.SkillManifest{}
	}
	t.Cleanup(func() { scanSkillDeps = prevScan })

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/skills/"+skillID.String()+"/dependencies", http.NoBody)
	req.Header.Set("Authorization", "Bearer read-token")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	if scanCalled {
		t.Fatal("dependency scan ran for non-admin caller")
	}

	scanCalled = false
	req = httptest.NewRequest(http.MethodGet, "/v1/skills/"+skillID.String()+"/dependencies", http.NoBody)
	req.Header.Set("Authorization", "Bearer tenant-admin-token")
	w = httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("tenant admin status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	if scanCalled {
		t.Fatal("dependency scan ran for non-master tenant admin")
	}
}

func TestHandleSkillDependenciesInstall_SplitsGitHubDepsToSingleInstaller(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	skillDir := filepath.Join(root, "skills-store", "dep-skill", "1")
	skillID := skillStore.seedCustomSkill("dep-skill", skillDir, "archived", []string{"pip:requests", "github:cli/cli@v2.40.0"})

	prevScan := scanSkillDeps
	prevCheck := checkSkillDepsContext
	prevInstallManaged := installManagedDeps
	prevInstallSingle := installSingleDep
	prevGitHubInstalled := githubSkillDependencyInstalled
	githubInstalled := false
	scanSkillDeps = func(string) *skills.SkillManifest {
		return &skills.SkillManifest{
			RequiresPython: []string{"requests"},
			Explicit:       []string{"github:cli/cli@v2.40.0"},
		}
	}
	checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
		if githubInstalled {
			return skills.DepCheckResult{Status: skills.DepCheckPresent}
		}
		return skills.DepCheckResult{Status: skills.DepCheckMissing, Missing: []string{"pip:requests"}}
	}
	githubSkillDependencyInstalled = func(raw string) bool {
		if raw != "github:cli/cli@v2.40.0" {
			t.Fatalf("github status checked for %q", raw)
		}
		return githubInstalled
	}
	installManagedDeps = func(_ context.Context, _ *skills.SkillManifest, missing []string) (*skills.InstallResult, error) {
		if !reflect.DeepEqual(missing, []string{"pip:requests"}) {
			t.Fatalf("managed missing = %#v, want pip only", missing)
		}
		return &skills.InstallResult{Pip: []string{"requests"}}, nil
	}
	installSingleDep = func(_ context.Context, dep string) (bool, string) {
		if dep != "github:cli/cli@v2.40.0" {
			t.Fatalf("single dep = %q, want github spec", dep)
		}
		githubInstalled = true
		return true, ""
	}
	t.Cleanup(func() {
		scanSkillDeps = prevScan
		checkSkillDepsContext = prevCheck
		installManagedDeps = prevInstallManaged
		installSingleDep = prevInstallSingle
		githubSkillDependencyInstalled = prevGitHubInstalled
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+skillID.String()+"/dependencies/install", http.NoBody).WithContext(ctx)
	req.SetPathValue("id", skillID.String())
	w := httptest.NewRecorder()

	handler.handleSkillDependenciesInstall(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Result skills.InstallResult `json:"result"`
		Deps   struct {
			OK           bool `json:"ok"`
			MissingCount int  `json:"missing_count"`
		} `json:"dependencies"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(resp.Result.Pip, []string{"requests"}) || !reflect.DeepEqual(resp.Result.GitHub, []string{"cli/cli@v2.40.0"}) {
		t.Fatalf("install result = %+v", resp.Result)
	}
	if !resp.Deps.OK || resp.Deps.MissingCount != 0 {
		t.Fatalf("dependencies = %+v", resp.Deps)
	}
}

func TestHandleSkillDependenciesInstall_RejectsNonMasterTenant(t *testing.T) {
	handler, skillStore, _, root := newTestUploadHandler(t)
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	skillDir := filepath.Join(root, "tenants", tenantID.String(), "skills-store", "dep-skill", "1")
	skillID := skillStore.seedCustomSkillForTenant(tenantID, "dep-skill", skillDir, "archived", []string{"pip:requests"})

	installCalled := false
	prevInstall := installManagedDeps
	installManagedDeps = func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
		installCalled = true
		return &skills.InstallResult{}, nil
	}
	t.Cleanup(func() { installManagedDeps = prevInstall })

	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+skillID.String()+"/dependencies/install", http.NoBody).WithContext(ctx)
	req.SetPathValue("id", skillID.String())
	w := httptest.NewRecorder()

	handler.handleSkillDependenciesInstall(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	if installCalled {
		t.Fatal("installManagedDeps was called for non-master tenant")
	}
}

func TestSkillGrantUserRouteRejectsNonTenantMember(t *testing.T) {
	handler, skillStore, _, root := newTestUploadHandler(t)
	tenantID := uuid.New()
	skillID := skillStore.seedCustomSkillForTenant(tenantID, "access-skill", filepath.Join(root, "tenants", tenantID.String(), "skills-store", "access-skill", "1"), "active", nil)
	tenants := newMockTenantStore()
	tenants.addTenant(tenantID, "tenant-a")
	tenants.setUserRole(tenantID, "admin-user", store.TenantRoleAdmin)
	handler.tenantStore = tenants
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey("admin-token"): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.admin"},
			TenantID: tenantID,
			OwnerID:  "admin-user",
		},
	})

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+skillID.String()+"/grants/users", strings.NewReader(`{"user_id":"outside-user"}`))
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	if len(skillStore.userGrantCalls) != 0 {
		t.Fatalf("user grant calls = %+v, want none", skillStore.userGrantCalls)
	}
}

func TestHandleSkillAccessGetListsAgentAndUserGrants(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	skillID := skillStore.seedCustomSkill("access-skill", filepath.Join(root, "skills-store", "access-skill", "1"), "active", nil)
	agentID := uuid.New()
	skillStore.agentGrants[skillID] = []store.SkillAgentGrantInfo{{
		AgentID:       agentID,
		PinnedVersion: 3,
		GrantedBy:     "owner-user",
		CanManage:     true,
	}}
	skillStore.userGrants[skillID] = []store.SkillUserGrantInfo{{
		UserID:    "target-user",
		GrantedBy: "owner-user",
	}}

	req := httptest.NewRequest(http.MethodGet, "/v1/skills/"+skillID.String()+"/access", http.NoBody).WithContext(ctx)
	req.SetPathValue("id", skillID.String())
	w := httptest.NewRecorder()

	handler.handleGetSkillAccess(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Visibility  string                      `json:"visibility"`
		AgentGrants []store.SkillAgentGrantInfo `json:"agent_grants"`
		UserGrants  []store.SkillUserGrantInfo  `json:"user_grants"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.AgentGrants) != 1 || !resp.AgentGrants[0].CanManage || resp.AgentGrants[0].PinnedVersion != 3 {
		t.Fatalf("agent grants = %+v", resp.AgentGrants)
	}
	if len(resp.UserGrants) != 1 || resp.UserGrants[0].UserID != "target-user" {
		t.Fatalf("user grants = %+v", resp.UserGrants)
	}
}

func TestHandleSkillEffectiveAccessReportsAgentGrant(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	skillID := skillStore.seedCustomSkill("effective-skill", filepath.Join(root, "skills-store", "effective-skill", "1"), "active", nil)
	agentID := uuid.New()
	tenants := newMockTenantStore()
	tenants.addTenant(store.MasterTenantID, "master")
	tenants.setUserRole(store.MasterTenantID, "target-user", store.TenantRoleMember)
	handler.tenantStore = tenants
	skillStore.skills[skillID] = store.SkillInfo{
		ID:         skillID.String(),
		TenantID:   store.MasterTenantID.String(),
		Name:       "Effective Skill",
		Slug:       "effective-skill",
		Path:       filepath.Join(root, "skills-store", "effective-skill", "1", "SKILL.md"),
		BaseDir:    filepath.Join(root, "skills-store", "effective-skill", "1"),
		Visibility: "internal",
		Status:     "active",
		Enabled:    true,
	}
	skillStore.agentGrants[skillID] = []store.SkillAgentGrantInfo{{
		AgentID:       agentID,
		PinnedVersion: 2,
		GrantedBy:     "owner-user",
		CanManage:     true,
	}}

	req := httptest.NewRequest(http.MethodGet, "/v1/skills/"+skillID.String()+"/access/effective?agent_id="+agentID.String()+"&user_id=target-user", http.NoBody).WithContext(ctx)
	req.SetPathValue("id", skillID.String())
	w := httptest.NewRecorder()

	handler.handleGetSkillEffectiveAccess(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Accessible    bool   `json:"accessible"`
		Reason        string `json:"reason"`
		CanManage     bool   `json:"can_manage"`
		PinnedVersion *int   `json:"pinned_version"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Accessible || resp.Reason != "agent_grant" || !resp.CanManage || resp.PinnedVersion == nil || *resp.PinnedVersion != 2 {
		t.Fatalf("effective access = %+v", resp)
	}
}

func TestEffectiveAccessTreatsAccessibleIndexAsSourceOfTruthForSystemSkills(t *testing.T) {
	ctx := store.WithTenantID(context.Background(), uuid.New())
	sk := store.SkillInfo{
		ID:       uuid.NewString(),
		Name:     "System Skill",
		Slug:     "system-skill",
		Status:   "active",
		Enabled:  true,
		IsSystem: true,
	}
	resp := effectiveAccessForSkill(ctx, sk, effectiveAccessIndex{accessibleBySlug: map[string]bool{}}, "target-user")
	if resp.Accessible || resp.Reason != "none" {
		t.Fatalf("effective access = %+v, want inaccessible none", resp)
	}
	resp = effectiveAccessForSkill(ctx, sk, effectiveAccessIndex{accessibleBySlug: map[string]bool{"system-skill": true}}, "target-user")
	if !resp.Accessible || resp.Reason != "system" {
		t.Fatalf("effective access = %+v, want system access", resp)
	}
}

func TestHandlePatchSkillAccessRejectsNonMasterSystemSkillUpdate(t *testing.T) {
	handler, skillStore, _, root := newTestUploadHandler(t)
	tenantID := uuid.New()
	skillID := skillStore.seedSystemSkill("system-skill", filepath.Join(root, "bundled", "system-skill"))
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey("tenant-admin-token"): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.admin"},
			TenantID: tenantID,
			OwnerID:  "tenant-admin",
		},
	})

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPatch, "/v1/skills/"+skillID.String()+"/access", strings.NewReader(`{"mode":"public"}`))
	req.Header.Set("Authorization", "Bearer tenant-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
	if updates := skillStore.lastUpdates[skillID]; updates != nil {
		t.Fatalf("system skill updated by non-master tenant: %+v", updates)
	}
}

func TestHandlePatchSkillAccessAcceptsModeAlias(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	skillID := skillStore.seedCustomSkill("access-skill", filepath.Join(root, "skills-store", "access-skill", "1"), "active", nil)

	req := httptest.NewRequest(http.MethodPatch, "/v1/skills/"+skillID.String()+"/access", bytes.NewBufferString(`{"mode":"PUBLIC"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", skillID.String())
	w := httptest.NewRecorder()

	handler.handlePatchSkillAccess(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	info, _ := skillStore.GetSkillByID(ctx, skillID)
	if info.Visibility != "public" {
		t.Fatalf("visibility = %q, want public", info.Visibility)
	}
}

func TestRescanAndUpdate_CancelledPassLeavesSkillsUntouched(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	skillID := skillStore.seedCustomSkill("slow-skill", filepath.Join(root, "skills-store", "slow-skill", "1"), "active", []string{"pip:anthropic"})

	prevScan := scanSkillDeps
	prevCheck := checkSkillDepsContext
	scanSkillDeps = func(string) *skills.SkillManifest {
		return &skills.SkillManifest{RequiresPython: []string{"anthropic"}}
	}
	checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
		t.Fatal("dependency probe ran after the pass deadline")
		return skills.DepCheckResult{}
	}
	t.Cleanup(func() {
		scanSkillDeps = prevScan
		checkSkillDepsContext = prevCheck
	})

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	updated, results := handler.rescanAndUpdate(cancelled)
	if updated != 0 {
		t.Fatalf("updated = %d, want 0 after cancellation", updated)
	}
	if len(skillStore.lastUpdates) != 0 {
		t.Fatalf("cancelled pass wrote updates: %v", skillStore.lastUpdates)
	}
	if got := skillStore.skills[skillID].MissingDeps; !reflect.DeepEqual(got, []string{"pip:anthropic"}) {
		t.Fatalf("stored missing deps changed on cancellation: %v", got)
	}
	if got := skillStore.skills[skillID].Status; got != "active" {
		t.Fatalf("status changed on cancellation: %q", got)
	}
	if len(results) != 1 || results[0].Status != "active" {
		t.Fatalf("results = %#v, want the stored active status", results)
	}
}

func TestRescanAndUpdate_RecoversArchivedSkillsAfterKnownSuccess(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	customID := skillStore.seedCustomSkill("custom-archived", filepath.Join(root, "skills-store", "custom-archived", "1"), "archived", nil)
	systemID := skillStore.seedSystemSkill("system-archived", filepath.Join(root, "skills-store", "system-archived", "1"))
	system := skillStore.skills[systemID]
	system.Status = "archived"
	skillStore.skills[systemID] = system

	prevScan := scanSkillDeps
	scanSkillDeps = func(string) *skills.SkillManifest {
		return &skills.SkillManifest{}
	}
	t.Cleanup(func() { scanSkillDeps = prevScan })

	updated, _ := handler.rescanAndUpdate(ctx)
	if updated != 2 {
		t.Fatalf("updated = %d, want both skills recovered", updated)
	}
	if got := skillStore.skills[customID].Status; got != "active" {
		t.Fatalf("custom dependency archive = %q, want active", got)
	}
	if got := skillStore.skills[systemID].Status; got != "active" {
		t.Fatalf("system archived skill = %q, want active", got)
	}
}

func TestRescanAndUpdate_CancellationDuringProbeStopsRemainingChecks(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	ids := []uuid.UUID{
		skillStore.seedCustomSkill("first", filepath.Join(root, "first"), "active", []string{"pip:anthropic"}),
		skillStore.seedCustomSkill("second", filepath.Join(root, "second"), "archived", []string{"pip:anthropic"}),
	}
	parent, cancel := context.WithCancel(ctx)
	defer cancel()
	prevScan, prevCheck := scanSkillDeps, checkSkillDepsContext
	scanSkillDeps = func(string) *skills.SkillManifest {
		return &skills.SkillManifest{RequiresPython: []string{"anthropic"}}
	}
	calls := 0
	checkSkillDepsContext = func(pass context.Context, _ *skills.SkillManifest) skills.DepCheckResult {
		calls++
		deadline, ok := pass.Deadline()
		if !ok || time.Until(deadline) > depCheckPassTimeout {
			t.Fatal("probe lacks bounded pass deadline")
		}
		cancel()
		<-pass.Done()
		return skills.DepCheckResult{Status: skills.DepCheckUnknown}
	}
	t.Cleanup(func() { scanSkillDeps, checkSkillDepsContext = prevScan, prevCheck })
	updated, results := handler.rescanAndUpdate(parent)
	if calls != 1 || updated != 0 || len(results) != 2 {
		t.Fatalf("calls=%d updated=%d results=%v", calls, updated, results)
	}
	if len(skillStore.lastUpdates) != 0 {
		t.Fatalf("cancelled pass updated skills: %v", skillStore.lastUpdates)
	}
	for _, id := range ids {
		if !reflect.DeepEqual(skillStore.skills[id].MissingDeps, []string{"pip:anthropic"}) {
			t.Fatalf("stored deps changed for %s", id)
		}
	}
}

func TestSingleSkillLifecycle_UnknownPreservesStateAndDoesNotInstall(t *testing.T) {
	for _, action := range []string{"status", "install"} {
		t.Run(action, func(t *testing.T) {
			handler, skillStore, ctx, root := newTestUploadHandler(t)
			id := skillStore.seedCustomSkill("unverified", filepath.Join(root, "unverified"), "archived", []string{"pip:requests"})
			prevScan, prevCheck, prevInstall := scanSkillDeps, checkSkillDepsContext, installManagedDeps
			scanSkillDeps = func(string) *skills.SkillManifest { return &skills.SkillManifest{RequiresPython: []string{"requests"}} }
			calls := 0
			checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
				calls++
				return skills.DepCheckResult{Status: skills.DepCheckUnknown, Incomplete: true}
			}
			installManagedDeps = func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
				t.Fatal("install ran without a verified missing dependency")
				return nil, nil
			}
			t.Cleanup(func() { scanSkillDeps, checkSkillDepsContext, installManagedDeps = prevScan, prevCheck, prevInstall })
			method := http.MethodPost
			if action == "status" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, "/v1/skills/"+id.String()+"/dependencies/"+action, http.NoBody).WithContext(ctx)
			req.SetPathValue("id", id.String())
			w := httptest.NewRecorder()
			if action == "install" {
				handler.handleSkillDependenciesInstall(w, req)
			} else {
				handler.handleSkillDependenciesStatus(w, req)
			}
			var resp skillDependencyStatusResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || resp.OK || resp.Status != "unknown" || calls != 1 {
				t.Fatalf("code=%d response=%+v calls=%d", w.Code, resp, calls)
			}
			if len(resp.Dependencies) != 1 || resp.Dependencies[0].Status != "unknown" {
				t.Fatalf("unverified dependency marked installed: %+v", resp.Dependencies)
			}
			if len(skillStore.lastUpdates) != 0 || skillStore.skills[id].Status != "archived" || !reflect.DeepEqual(skillStore.skills[id].MissingDeps, []string{"pip:requests"}) {
				t.Fatalf("unknown check mutated state: %+v", skillStore.skills[id])
			}
		})
	}
}

func TestSingleSkillLifecycle_CancellationReachesProbeWithoutRepeatedChecks(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	id := skillStore.seedCustomSkill("cancelled", filepath.Join(root, "cancelled"), "archived", []string{"pip:requests"})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	prevScan, prevCheck := scanSkillDeps, checkSkillDepsContext
	scanSkillDeps = func(string) *skills.SkillManifest { return &skills.SkillManifest{RequiresPython: []string{"requests"}} }
	calls := 0
	checkSkillDepsContext = func(probe context.Context, _ *skills.SkillManifest) skills.DepCheckResult {
		calls++
		if !errors.Is(probe.Err(), context.Canceled) {
			t.Fatal("request cancellation lost")
		}
		return skills.DepCheckResult{Status: skills.DepCheckUnknown, Incomplete: true}
	}
	t.Cleanup(func() { scanSkillDeps, checkSkillDepsContext = prevScan, prevCheck })
	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+id.String()+"/dependencies/install", http.NoBody).WithContext(cancelled)
	req.SetPathValue("id", id.String())
	w := httptest.NewRecorder()
	handler.handleSkillDependenciesInstall(w, req)
	var resp skillDependencyStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || resp.OK || resp.Status != "unknown" || len(skillStore.lastUpdates) != 0 {
		t.Fatalf("calls=%d response=%+v writes=%v", calls, resp, skillStore.lastUpdates)
	}
}

func TestRescanAndUpdate_KnownResultAfterCancellationDoesNotWrite(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	id := skillStore.seedCustomSkill("partial", filepath.Join(root, "partial"), "active", []string{"pip:old"})
	parent, cancel := context.WithCancel(ctx)
	defer cancel()
	prevScan, prevCheck := scanSkillDeps, checkSkillDepsContext
	scanSkillDeps = func(string) *skills.SkillManifest { return &skills.SkillManifest{Requires: []string{"missing-bin"}} }
	checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
		cancel()
		return skills.DepCheckResult{Status: skills.DepCheckMissing, Missing: []string{"missing-bin"}}
	}
	t.Cleanup(func() { scanSkillDeps, checkSkillDepsContext = prevScan, prevCheck })
	updated, results := handler.rescanAndUpdate(parent)
	if updated != 0 || len(skillStore.lastUpdates) != 0 || results[0].Status != "active" || !reflect.DeepEqual(results[0].Missing, []string{"pip:old"}) {
		t.Fatalf("updated=%d results=%+v writes=%v", updated, results, skillStore.lastUpdates)
	}
	if skillStore.skills[id].Status != "active" {
		t.Fatal("cancelled write changed status")
	}
}

func TestRescanAndUpdate_PersistenceFailurePreservesBothValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status skills.DepCheckStatus
	}{{"missing", skills.DepCheckMissing}, {"present", skills.DepCheckPresent}} {
		t.Run(tc.name, func(t *testing.T) {
			handler, skillStore, ctx, root := newTestUploadHandler(t)
			id := skillStore.seedCustomSkill("write-failure", filepath.Join(root, "write-failure"), "archived", []string{"pip:old"})
			skillStore.updateError = errors.New("database unavailable")
			prevScan, prevCheck := scanSkillDeps, checkSkillDepsContext
			scanSkillDeps = func(string) *skills.SkillManifest { return &skills.SkillManifest{Requires: []string{"dep"}} }
			checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
				if tc.status == skills.DepCheckPresent {
					return skills.DepCheckResult{Status: tc.status}
				}
				return skills.DepCheckResult{Status: tc.status, Missing: []string{"dep"}}
			}
			t.Cleanup(func() { scanSkillDeps, checkSkillDepsContext = prevScan, prevCheck })
			updated, results := handler.rescanAndUpdate(ctx)
			if updated != 0 || len(skillStore.lastUpdates) != 0 || results[0].Status != "archived" || !reflect.DeepEqual(results[0].Missing, []string{"pip:old"}) {
				t.Fatalf("updated=%d results=%+v writes=%v", updated, results, skillStore.lastUpdates)
			}
			if !reflect.DeepEqual(skillStore.skills[id].MissingDeps, []string{"pip:old"}) {
				t.Fatal("failed atomic write cleared old missing list")
			}
		})
	}
}

func TestSingleSkillInstall_UnknownAfterInstallPreservesArchivedState(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	id := skillStore.seedCustomSkill("post-install", filepath.Join(root, "post-install"), "archived", []string{"pip:requests"})
	prevScan, prevCheck, prevInstall := scanSkillDeps, checkSkillDepsContext, installManagedDeps
	scanSkillDeps = func(string) *skills.SkillManifest { return &skills.SkillManifest{RequiresPython: []string{"requests"}} }
	calls := 0
	checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
		calls++
		if calls == 1 {
			return skills.DepCheckResult{Status: skills.DepCheckMissing, Missing: []string{"pip:requests"}}
		}
		return skills.DepCheckResult{Status: skills.DepCheckUnknown, Incomplete: true}
	}
	installManagedDeps = func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
		return &skills.InstallResult{Pip: []string{"requests"}}, nil
	}
	t.Cleanup(func() { scanSkillDeps, checkSkillDepsContext, installManagedDeps = prevScan, prevCheck, prevInstall })
	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+id.String()+"/dependencies/install", http.NoBody).WithContext(ctx)
	req.SetPathValue("id", id.String())
	w := httptest.NewRecorder()
	handler.handleSkillDependenciesInstall(w, req)
	var resp struct {
		Dependencies skillDependencyStatusResponse `json:"dependencies"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || calls != 2 || resp.Dependencies.OK || resp.Dependencies.Status != "unknown" {
		t.Fatalf("code=%d calls=%d response=%+v", w.Code, calls, resp)
	}
	if len(skillStore.lastUpdates) != 0 || skillStore.skills[id].Status != "archived" || !reflect.DeepEqual(skillStore.skills[id].MissingDeps, []string{"pip:requests"}) {
		t.Fatal("unverified installation cleared persisted dependency state")
	}
}

func TestSingleSkillInstall_PartialMissingResultDoesNotLabelUncheckedPackagesInstalled(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	id := skillStore.seedCustomSkill("partial-known", filepath.Join(root, "partial-known"), "active", []string{"pip:old"})
	prevScan, prevCheck := scanSkillDeps, checkSkillDepsContext
	scanSkillDeps = func(string) *skills.SkillManifest {
		return &skills.SkillManifest{Requires: []string{"missing-bin"}, RequiresPython: []string{"anthropic"}}
	}
	checkSkillDepsContext = func(context.Context, *skills.SkillManifest) skills.DepCheckResult {
		return skills.DepCheckResult{Status: skills.DepCheckMissing, Missing: []string{"missing-bin"}, Incomplete: true}
	}
	t.Cleanup(func() { scanSkillDeps, checkSkillDepsContext = prevScan, prevCheck })
	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+id.String()+"/dependencies/install", http.NoBody).WithContext(ctx)
	req.SetPathValue("id", id.String())
	w := httptest.NewRecorder()
	handler.handleSkillDependenciesInstall(w, req)
	var resp skillDependencyStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Status != "missing" || len(resp.Dependencies) != 2 || resp.Dependencies[0].Status != "missing" || resp.Dependencies[1].Status != "unknown" {
		t.Fatalf("partial result=%+v", resp)
	}
	if len(skillStore.lastUpdates) != 0 || !reflect.DeepEqual(skillStore.skills[id].MissingDeps, []string{"pip:old"}) {
		t.Fatal("partial result replaced old dependency evidence")
	}
}
