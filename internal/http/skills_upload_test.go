package http

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func captureEventNames(msgBus *bus.MessageBus) *[]string {
	names := []string{}
	msgBus.Subscribe("test", func(event bus.Event) { names = append(names, event.Name) })
	return &names
}

func stubUploadDepFns(
	t *testing.T,
	installFn func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error),
	checkFn func(*skills.SkillManifest) (bool, []string),
) {
	t.Helper()
	prevInstall := installUploadedSkillDeps
	prevCheck := checkUploadedSkillDeps
	installUploadedSkillDeps = installFn
	checkUploadedSkillDeps = checkFn
	t.Cleanup(func() {
		installUploadedSkillDeps = prevInstall
		checkUploadedSkillDeps = prevCheck
	})
}

func TestReconcileUploadedSkillDeps_SkipsAutoInstallOutsideMasterTenant(t *testing.T) {
	msgBus := bus.New()
	handler := &SkillsHandler{msgBus: msgBus}
	events := captureEventNames(msgBus)
	called := false
	stubUploadDepFns(t, func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
		called = true
		return nil, nil
	}, func(*skills.SkillManifest) (bool, []string) { return false, nil })

	state := handler.reconcileUploadedSkillDeps(context.Background(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, false)
	if called {
		t.Fatal("expected auto-install to be skipped")
	}
	if got := state.status; got != "archived" {
		t.Fatalf("status = %v, want archived", got)
	}
	if !reflect.DeepEqual(state.missing, []string{"pip:requests"}) {
		t.Fatalf("missing = %#v", state.missing)
	}
	response := state.response
	state.emit(handler, "demo")
	if got := response["deps_warning"]; got != "missing dependencies: pip:requests" {
		t.Fatalf("deps_warning = %v", got)
	}
	if !reflect.DeepEqual(response["missing_deps"], []string{"pip:requests"}) {
		t.Fatalf("missing_deps = %#v", response["missing_deps"])
	}
	if !reflect.DeepEqual(*events, []string{protocol.EventSkillDepsChecked}) {
		t.Fatalf("events = %v", *events)
	}
}

func TestReconcileUploadedSkillDeps_AutoInstallSuccessClearsMissingDeps(t *testing.T) {
	msgBus := bus.New()
	handler := &SkillsHandler{msgBus: msgBus}
	events := captureEventNames(msgBus)
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			return &skills.InstallResult{Pip: []string{"requests"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) { return true, nil },
	)

	state := handler.reconcileUploadedSkillDeps(context.Background(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, true)
	if got := state.status; got != "active" {
		t.Fatalf("status = %v, want active", got)
	}
	if len(state.missing) != 0 {
		t.Fatalf("missing = %v, want none", state.missing)
	}
	response := state.response
	state.emit(handler, "demo")
	if got := response["deps_installed"]; got != true {
		t.Fatalf("deps_installed = %v, want true", got)
	}
	wantEvents := []string{
		protocol.EventSkillDepsInstalling,
		protocol.EventSkillDepsInstalled,
		protocol.EventSkillDepsChecked,
	}
	if !reflect.DeepEqual(*events, wantEvents) {
		t.Fatalf("events = %v, want %v", *events, wantEvents)
	}
}

func TestReconcileUploadedSkillDeps_AutoInstallFailureArchivesSkill(t *testing.T) {
	msgBus := bus.New()
	handler := &SkillsHandler{msgBus: msgBus}
	events := captureEventNames(msgBus)
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			return &skills.InstallResult{Errors: []string{"pip failed"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) { return false, []string{"pip:requests"} },
	)

	state := handler.reconcileUploadedSkillDeps(context.Background(), "demo", &skills.SkillManifest{}, []string{"pip:requests"}, true)
	if got := state.status; got != "archived" {
		t.Fatalf("status = %v, want archived", got)
	}
	if !reflect.DeepEqual(state.missing, []string{"pip:requests"}) {
		t.Fatalf("missing = %#v", state.missing)
	}
	response := state.response
	state.emit(handler, "demo")
	if got := response["deps_warning"]; got != "auto-install failed for: pip:requests" {
		t.Fatalf("deps_warning = %v", got)
	}
	if !reflect.DeepEqual(response["deps_errors"], []string{"pip failed"}) {
		t.Fatalf("deps_errors = %#v", response["deps_errors"])
	}
	wantEvents := []string{
		protocol.EventSkillDepsInstalling,
		protocol.EventSkillDepsInstalled,
		protocol.EventSkillDepsChecked,
	}
	if !reflect.DeepEqual(*events, wantEvents) {
		t.Fatalf("events = %v, want %v", *events, wantEvents)
	}
}

func TestHandleUpload_AutoInstallsMissingDepsAndKeepsSkillActive(t *testing.T) {
	handler, skillStore, ctx, _ := newTestUploadHandler(t)
	installCalls := 0
	checkCalls := 0
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			installCalls++
			return &skills.InstallResult{Pip: []string{"requests"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) {
			checkCalls++
			if checkCalls == 1 {
				return false, []string{"pip:requests"}
			}
			return true, nil
		},
	)

	req := newZipUploadRequest(t, ctx, map[string]string{
		"SKILL.md":       skillMarkdown("Pip Skill", "pip-skill"),
		"scripts/run.py": "import requests\n",
	})
	w := httptest.NewRecorder()
	handler.handleUpload(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if installCalls != 1 {
		t.Fatalf("install calls = %d, want 1", installCalls)
	}

	var resp struct {
		ID            string   `json:"id"`
		Status        string   `json:"status"`
		DepsInstalled bool     `json:"deps_installed"`
		DepsErrors    []string `json:"deps_errors"`
		MissingDeps   []string `json:"missing_deps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "active" {
		t.Fatalf("response status = %q, want active", resp.Status)
	}
	if !resp.DepsInstalled {
		t.Fatal("expected deps_installed=true")
	}
	if len(resp.DepsErrors) != 0 {
		t.Fatalf("deps_errors = %v, want none", resp.DepsErrors)
	}
	if len(resp.MissingDeps) != 0 {
		t.Fatalf("missing_deps = %v, want none", resp.MissingDeps)
	}

	id := uuid.MustParse(resp.ID)
	info, ok := skillStore.GetSkillByID(ctx, id)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if info.Status != "active" {
		t.Fatalf("stored status = %q, want active", info.Status)
	}
	if len(info.MissingDeps) != 0 {
		t.Fatalf("stored missing_deps = %v, want none", info.MissingDeps)
	}
}

func TestHandleUpload_UninstallableDepArchivesSkillWithErrors(t *testing.T) {
	handler, skillStore, ctx, _ := newTestUploadHandler(t)
	installCalls := 0
	stubUploadDepFns(t,
		func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
			installCalls++
			return &skills.InstallResult{Errors: []string{"pip failed"}}, nil
		},
		func(*skills.SkillManifest) (bool, []string) { return false, []string{"pip:requests"} },
	)

	req := newZipUploadRequest(t, ctx, map[string]string{
		"SKILL.md":       skillMarkdown("Broken Pip Skill", "broken-pip-skill"),
		"scripts/run.py": "import requests\n",
	})
	w := httptest.NewRecorder()
	handler.handleUpload(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if installCalls != 1 {
		t.Fatalf("install calls = %d, want 1", installCalls)
	}

	var resp struct {
		ID          string   `json:"id"`
		Status      string   `json:"status"`
		DepsErrors  []string `json:"deps_errors"`
		MissingDeps []string `json:"missing_deps"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "archived" {
		t.Fatalf("response status = %q, want archived", resp.Status)
	}
	if !reflect.DeepEqual(resp.DepsErrors, []string{"pip failed"}) {
		t.Fatalf("deps_errors = %v", resp.DepsErrors)
	}
	if !reflect.DeepEqual(resp.MissingDeps, []string{"pip:requests"}) {
		t.Fatalf("missing_deps = %v", resp.MissingDeps)
	}

	id := uuid.MustParse(resp.ID)
	info, ok := skillStore.GetSkillByID(ctx, id)
	if !ok {
		t.Fatal("GetSkillByID returned !ok")
	}
	if info.Status != "archived" {
		t.Fatalf("stored status = %q, want archived", info.Status)
	}
	if !reflect.DeepEqual(info.MissingDeps, []string{"pip:requests"}) {
		t.Fatalf("stored missing_deps = %v", info.MissingDeps)
	}
}

func TestHandleInstallDeps_ExistingEndpointStillReturnsInstallResult(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	systemDir := filepath.Join(root, "skills-store", "system-skill", "1")
	skillStore.seedSystemSkill("system-skill", systemDir)

	prevAggregate := aggregateInstallDeps
	prevInstall := installManagedDeps
	aggregateInstallDeps = func(dirs map[string]string) (*skills.SkillManifest, []string) {
		if got := dirs["system-skill"]; got != systemDir {
			t.Fatalf("system dir = %q, want %q", got, systemDir)
		}
		return &skills.SkillManifest{RequiresPython: []string{"requests"}}, []string{"pip:requests"}
	}
	installManagedDeps = func(context.Context, *skills.SkillManifest, []string) (*skills.InstallResult, error) {
		return &skills.InstallResult{Pip: []string{"requests"}}, nil
	}
	t.Cleanup(func() {
		aggregateInstallDeps = prevAggregate
		installManagedDeps = prevInstall
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/skills/install-deps", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.handleInstallDeps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp skills.InstallResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(resp.Pip, []string{"requests"}) {
		t.Fatalf("pip installs = %v, want [requests]", resp.Pip)
	}
}

func newTestUploadHandler(t *testing.T) (*SkillsHandler, *skillManageStoreStub, context.Context, string) {
	t.Helper()

	root := t.TempDir()
	baseDir := filepath.Join(root, "skills-store")
	skillStore := newSkillManageStoreStub(baseDir)
	handler := NewSkillsHandler(skillStore, baseDir, root, "", bus.New(), nil, nil)
	ctx := store.WithLocale(
		store.WithTenantID(
			store.WithUserID(context.Background(), "user-1"),
			store.MasterTenantID,
		),
		"en",
	)
	return handler, skillStore, ctx, root
}

func newZipUploadRequest(t *testing.T, ctx context.Context, files map[string]string) *http.Request {
	t.Helper()

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "skill.zip")
	if err != nil {
		t.Fatalf("multipart file: %v", err)
	}
	if _, err := part.Write(zipBuf.Bytes()); err != nil {
		t.Fatalf("multipart write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/skills/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req.WithContext(ctx)
}

func skillMarkdown(name, slug string) string {
	return "---\nname: " + name + "\nslug: " + slug + "\n---\nSkill body\n"
}

type skillManageStoreStub struct {
	baseDir    string
	version    int64
	nextBySlug map[string]int
	skills     map[uuid.UUID]store.SkillInfo
	systemDirs map[string]string
}

func newSkillManageStoreStub(baseDir string) *skillManageStoreStub {
	return &skillManageStoreStub{
		baseDir:    baseDir,
		nextBySlug: map[string]int{},
		skills:     map[uuid.UUID]store.SkillInfo{},
		systemDirs: map[string]string{},
	}
}

func (s *skillManageStoreStub) seedSystemSkill(slug, dir string) {
	id := uuid.New()
	s.skills[id] = store.SkillInfo{
		ID:       id.String(),
		Name:     "System Skill",
		Slug:     slug,
		Path:     filepath.Join(dir, "SKILL.md"),
		BaseDir:  dir,
		Version:  1,
		Status:   "active",
		Enabled:  true,
		IsSystem: true,
	}
	s.systemDirs[slug] = dir
}

func (s *skillManageStoreStub) ListSkills(context.Context) []store.SkillInfo {
	return s.ListAllSkills(context.Background())
}

func (s *skillManageStoreStub) LoadSkill(context.Context, string) (string, bool) { return "", false }
func (s *skillManageStoreStub) LoadForContext(context.Context, []string) string  { return "" }
func (s *skillManageStoreStub) BuildSummary(context.Context, []string) string    { return "" }
func (s *skillManageStoreStub) GetSkill(_ context.Context, name string) (*store.SkillInfo, bool) {
	for _, skill := range s.skills {
		if skill.Slug == name {
			copy := skill
			return &copy, true
		}
	}
	return nil, false
}
func (s *skillManageStoreStub) FilterSkills(context.Context, []string) []store.SkillInfo {
	return s.ListAllSkills(context.Background())
}
func (s *skillManageStoreStub) Version() int64 { return s.version }
func (s *skillManageStoreStub) BumpVersion()   { s.version++ }
func (s *skillManageStoreStub) Dirs() []string { return []string{s.baseDir} }

func (s *skillManageStoreStub) CreateSkillManaged(_ context.Context, p store.SkillCreateParams) (uuid.UUID, error) {
	id := uuid.New()
	status := p.Status
	if status == "" {
		status = "active"
	}
	version := p.Version
	if version == 0 {
		version = s.nextBySlug[p.Slug] + 1
	}
	if version > s.nextBySlug[p.Slug] {
		s.nextBySlug[p.Slug] = version
	}
	s.skills[id] = store.SkillInfo{
		ID:          id.String(),
		Name:        p.Name,
		Slug:        p.Slug,
		Path:        filepath.Join(p.FilePath, "SKILL.md"),
		BaseDir:     p.FilePath,
		Version:     version,
		Status:      status,
		Enabled:     true,
		MissingDeps: append([]string(nil), p.MissingDeps...),
	}
	return id, nil
}

func (s *skillManageStoreStub) UpdateSkill(_ context.Context, id uuid.UUID, updates map[string]any) error {
	skill, ok := s.skills[id]
	if !ok {
		return nil
	}
	if status, ok := updates["status"].(string); ok {
		skill.Status = status
	}
	s.skills[id] = skill
	return nil
}

func (s *skillManageStoreStub) DeleteSkill(context.Context, uuid.UUID) error       { return nil }
func (s *skillManageStoreStub) ToggleSkill(context.Context, uuid.UUID, bool) error { return nil }
func (s *skillManageStoreStub) GetSkillByID(_ context.Context, id uuid.UUID) (store.SkillInfo, bool) {
	info, ok := s.skills[id]
	return info, ok
}
func (s *skillManageStoreStub) GetSkillOwnerID(context.Context, uuid.UUID) (string, bool) {
	return "", false
}
func (s *skillManageStoreStub) GetSkillOwnerIDBySlug(context.Context, string) (string, bool) {
	return "", false
}
func (s *skillManageStoreStub) GetNextVersion(_ context.Context, slug string) int {
	return s.nextBySlug[slug] + 1
}
func (s *skillManageStoreStub) GetNextVersionLocked(_ context.Context, slug string) (int, func() error, error) {
	return s.GetNextVersion(context.Background(), slug), func() error { return nil }, nil
}
func (s *skillManageStoreStub) IsSystemSkill(slug string) bool {
	_, ok := s.systemDirs[slug]
	return ok
}
func (s *skillManageStoreStub) ListAllSkills(context.Context) []store.SkillInfo {
	out := make([]store.SkillInfo, 0, len(s.skills))
	for _, skill := range s.skills {
		out = append(out, skill)
	}
	return out
}
func (s *skillManageStoreStub) ListAllSystemSkills(context.Context) []store.SkillInfo {
	var out []store.SkillInfo
	for _, skill := range s.skills {
		if skill.IsSystem {
			out = append(out, skill)
		}
	}
	return out
}
func (s *skillManageStoreStub) ListSystemSkillDirs(context.Context) map[string]string {
	out := make(map[string]string, len(s.systemDirs))
	for slug, dir := range s.systemDirs {
		out[slug] = dir
	}
	return out
}
func (s *skillManageStoreStub) StoreMissingDeps(_ context.Context, id uuid.UUID, missing []string) error {
	skill, ok := s.skills[id]
	if !ok {
		return nil
	}
	skill.MissingDeps = append([]string(nil), missing...)
	s.skills[id] = skill
	return nil
}
func (s *skillManageStoreStub) GrantToAgent(context.Context, uuid.UUID, uuid.UUID, int, string) error {
	return nil
}
func (s *skillManageStoreStub) RevokeFromAgent(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *skillManageStoreStub) GrantToUser(context.Context, uuid.UUID, string, string) error {
	return nil
}
func (s *skillManageStoreStub) RevokeFromUser(context.Context, uuid.UUID, string) error { return nil }
func (s *skillManageStoreStub) ListWithGrantStatus(context.Context, uuid.UUID) ([]store.SkillWithGrantStatus, error) {
	return nil, nil
}
func (s *skillManageStoreStub) GetSkillFilePath(context.Context, uuid.UUID) (string, string, int, bool, bool) {
	return "", "", 0, false, false
}
