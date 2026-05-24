package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNpmCommandEnvUsesRuntimePrefix(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtimeDir)
	t.Setenv("NPM_CONFIG_PREFIX", "")
	t.Setenv("NODE_PATH", "")
	t.Setenv("PATH", "/usr/bin")

	env := npmCommandEnv()
	wantPrefix := filepath.Join(runtimeDir, "npm-global")
	wantBin := npmGlobalBinDir()
	wantNodePath := filepath.Join(wantPrefix, "lib", "node_modules")

	if !envContainsExact(env, "NPM_CONFIG_PREFIX="+wantPrefix) {
		t.Fatalf("npmCommandEnv missing NPM_CONFIG_PREFIX=%q", wantPrefix)
	}
	if !envContainsPrefixValue(env, "PATH=", wantBin) {
		t.Fatalf("npmCommandEnv PATH does not start with %q", wantBin)
	}
	if !envContainsPrefixValue(env, "NODE_PATH=", wantNodePath) {
		t.Fatalf("npmCommandEnv NODE_PATH does not start with %q", wantNodePath)
	}
}

func TestEnsureNpmGlobalEnvPrependsProcessPath(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtimeDir)
	t.Setenv("NPM_CONFIG_PREFIX", "")
	t.Setenv("PATH", "/usr/bin")

	ensureNpmGlobalEnv()

	wantBin := npmGlobalBinDir()
	if got := os.Getenv("PATH"); !strings.HasPrefix(got, wantBin+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want prefix %q", got, wantBin)
	}
}

func TestFindRuntimeExecutableFindsNpmGlobalBinary(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtimeDir)
	t.Setenv("NPM_CONFIG_PREFIX", "")

	binDir := npmGlobalBinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(binDir, "openrouter")
	if err := os.WriteFile(wantPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindRuntimeExecutable("openrouter")
	if !ok {
		t.Fatalf("FindRuntimeExecutable did not find npm global binary")
	}
	if got != wantPath {
		t.Fatalf("FindRuntimeExecutable path = %q, want %q", got, wantPath)
	}
}

func TestFindRuntimeExecutableFindsNpmPackageCliAlias(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("RUNTIME_DIR", runtimeDir)
	t.Setenv("NPM_CONFIG_PREFIX", "")

	pkgDir := filepath.Join(runtimeDir, "npm-global", "lib", "node_modules", "openrouter-cli")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"name":"openrouter-cli","bin":{"orc":"dist/index.js"}}`)
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := npmGlobalBinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(binDir, "orc")
	if err := os.WriteFile(wantPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := FindRuntimeExecutable("openrouter")
	if !ok {
		t.Fatalf("FindRuntimeExecutable did not find npm package CLI alias")
	}
	if got != wantPath {
		t.Fatalf("FindRuntimeExecutable alias path = %q, want %q", got, wantPath)
	}
}

func TestFindRuntimeExecutableRejectsPathLikeNames(t *testing.T) {
	if got, ok := FindRuntimeExecutable("../openrouter"); ok || got != "" {
		t.Fatalf("FindRuntimeExecutable path-like name = %q, %v; want empty false", got, ok)
	}
}

func envContainsExact(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func envContainsPrefixValue(env []string, key, wantPrefix string) bool {
	for _, item := range env {
		if strings.HasPrefix(item, key) {
			return strings.HasPrefix(strings.TrimPrefix(item, key), wantPrefix)
		}
	}
	return false
}
