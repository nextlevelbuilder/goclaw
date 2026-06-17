package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecureCLICheckBinaryFindsRuntimeNpmBinary(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtimeDir)
	t.Setenv("NPM_CONFIG_PREFIX", "")
	t.Setenv("PATH", "/usr/bin")

	binDir := filepath.Join(runtimeDir, "npm-global", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(binDir, "openrouter")
	if err := os.WriteFile(wantPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/cli-credentials/check-binary", strings.NewReader(`{"binary_name":"openrouter"}`))
	rec := httptest.NewRecorder()

	NewSecureCLIHandler(nil, nil).handleCheckBinary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Found bool   `json:"found"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Found {
		t.Fatalf("found = false, error = %q", got.Error)
	}
	if got.Path != wantPath {
		t.Fatalf("path = %q, want %q", got.Path, wantPath)
	}
}

func TestSecureCLICheckBinaryFindsGoogleWorkspaceRuntimeBinary(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtimeDir)
	t.Setenv("NPM_CONFIG_PREFIX", "")
	t.Setenv("PATH", "/usr/bin")

	binDir := filepath.Join(runtimeDir, "npm-global", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(binDir, "gws")
	if err := os.WriteFile(wantPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/cli-credentials/check-binary", strings.NewReader(`{"binary_name":"gws"}`))
	rec := httptest.NewRecorder()

	NewSecureCLIHandler(nil, nil).handleCheckBinary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Found bool   `json:"found"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Found {
		t.Fatalf("found = false, error = %q", got.Error)
	}
	if got.Path != wantPath {
		t.Fatalf("path = %q, want %q", got.Path, wantPath)
	}
}

func TestSecureCLIPresetsIncludesGoogleWorkspace(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/cli-credentials/presets", nil)
	rec := httptest.NewRecorder()

	NewSecureCLIHandler(nil, nil).handlePresets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Presets map[string]struct {
			BinaryName string `json:"binary_name"`
		} `json:"presets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Presets["gws"].BinaryName != "gws" {
		t.Fatalf("gws preset = %#v, want binary_name gws", got.Presets["gws"])
	}
}

func TestSecureCLIPresetsReturnStableArraysForGit(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/cli-credentials/presets", nil)
	rec := httptest.NewRecorder()

	NewSecureCLIHandler(nil, nil).handlePresets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Presets map[string]struct {
			EnvVars     []struct{} `json:"env_vars"`
			DenyVerbose []string   `json:"deny_verbose"`
			AdapterName string     `json:"adapter_name"`
		} `json:"presets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	git := got.Presets["git"]
	if git.EnvVars == nil {
		t.Fatalf("git env_vars = nil; response must use [] for frontend compatibility")
	}
	if git.DenyVerbose == nil {
		t.Fatalf("git deny_verbose = nil; response must use [] for frontend compatibility")
	}
	if git.AdapterName != "git" {
		t.Fatalf("git adapter_name = %q, want git", git.AdapterName)
	}
}

func TestSecureCLICreateAllowsGitPresetWithoutLegacyEnv(t *testing.T) {
	st := &fakeSecureCLIStore{}
	req := httptest.NewRequest(http.MethodPost, "/v1/cli-credentials", strings.NewReader(`{"preset":"git"}`))
	rec := httptest.NewRecorder()

	NewSecureCLIHandler(st, nil).handleCreate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if st.created == nil {
		t.Fatal("expected store.Create to be called")
	}
	if st.created.BinaryName != "git" {
		t.Fatalf("binary_name = %q, want git", st.created.BinaryName)
	}
	if st.created.AdapterName == nil || *st.created.AdapterName != "git" {
		t.Fatalf("adapter_name = %#v, want git", st.created.AdapterName)
	}
	if string(st.created.EncryptedEnv) != "{}" {
		t.Fatalf("encrypted env = %q, want {}", st.created.EncryptedEnv)
	}
}

func TestSecureCLICheckBinaryFindsNpmPackageCliAlias(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtimeDir)
	t.Setenv("NPM_CONFIG_PREFIX", "")
	t.Setenv("PATH", "/usr/bin")

	pkgDir := filepath.Join(runtimeDir, "npm-global", "lib", "node_modules", "openrouter-cli")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"name":"openrouter-cli","bin":{"orc":"dist/index.js"}}`)
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(runtimeDir, "npm-global", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(binDir, "orc")
	if err := os.WriteFile(wantPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/cli-credentials/check-binary", strings.NewReader(`{"binary_name":"openrouter"}`))
	rec := httptest.NewRecorder()

	NewSecureCLIHandler(nil, nil).handleCheckBinary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Found bool   `json:"found"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Found {
		t.Fatalf("found = false, error = %q", got.Error)
	}
	if got.Path != wantPath {
		t.Fatalf("path = %q, want %q", got.Path, wantPath)
	}
}
