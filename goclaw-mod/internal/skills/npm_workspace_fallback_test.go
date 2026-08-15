package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNpmOutputHasWorkspaceProtocolError(t *testing.T) {
	out := `npm error code EUNSUPPORTEDPROTOCOL
npm error Unsupported URL Type "workspace:": workspace:*`
	if !npmOutputHasWorkspaceProtocolError(out) {
		t.Fatal("expected workspace protocol error to be detected")
	}
	if npmOutputHasWorkspaceProtocolError("npm error code ERESOLVE") {
		t.Fatal("non-workspace npm error should not trigger fallback")
	}
}

func TestResolveWorkspaceDependencySpec(t *testing.T) {
	orig := npmPackageVersionResolver
	t.Cleanup(func() { npmPackageVersionResolver = orig })
	npmPackageVersionResolver = func(context.Context, string) (string, error) {
		return "1.2.3", nil
	}

	cases := []struct {
		spec string
		want string
	}{
		{"workspace:*", "1.2.3"},
		{"workspace:^", "^1.2.3"},
		{"workspace:~", "~1.2.3"},
		{"workspace:^1.0.0", "^1.0.0"},
	}
	for _, tc := range cases {
		got, err := resolveWorkspaceDependencySpec(context.Background(), "@scope/core", tc.spec)
		if err != nil {
			t.Fatalf("resolveWorkspaceDependencySpec(%q) error: %v", tc.spec, err)
		}
		if got != tc.want {
			t.Fatalf("resolveWorkspaceDependencySpec(%q) = %q, want %q", tc.spec, got, tc.want)
		}
	}
}

func TestRewriteWorkspacePackageJSON(t *testing.T) {
	orig := npmPackageVersionResolver
	t.Cleanup(func() { npmPackageVersionResolver = orig })
	npmPackageVersionResolver = func(_ context.Context, name string) (string, error) {
		switch name {
		case "@agenttasks/core":
			return "0.1.0", nil
		case "react":
			return "0.1.0", nil
		default:
			t.Fatalf("unexpected version lookup for %s", name)
			return "", nil
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "package.json")
	raw := []byte(`{
  "name": "@agenttasks/cli",
  "dependencies": {
    "@agenttasks/core": "workspace:*",
    "ws": "^8.18.3"
  },
  "peerDependencies": {
    "react": "workspace:^"
  }
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	rewrites, err := rewriteWorkspacePackageJSON(context.Background(), path)
	if err != nil {
		t.Fatalf("rewriteWorkspacePackageJSON error: %v", err)
	}
	if rewrites != 2 {
		t.Fatalf("rewrites = %d, want 2", rewrites)
	}

	var pkg struct {
		Dependencies     map[string]string `json:"dependencies"`
		PeerDependencies map[string]string `json:"peerDependencies"`
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(updated, &pkg); err != nil {
		t.Fatal(err)
	}
	if got := pkg.Dependencies["@agenttasks/core"]; got != "0.1.0" {
		t.Fatalf("@agenttasks/core = %q, want 0.1.0", got)
	}
	if got := pkg.PeerDependencies["react"]; got != "^0.1.0" {
		t.Fatalf("react = %q, want ^0.1.0", got)
	}
	if got := pkg.Dependencies["ws"]; got != "^8.18.3" {
		t.Fatalf("ws = %q, want unchanged", got)
	}
}

func TestPackNpmPackageDirDoesNotNeedNpmScripts(t *testing.T) {
	dir := t.TempDir()
	packageDir := filepath.Join(dir, "package")
	if err := os.MkdirAll(packageDir, 0o750); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{
  "name": "@agenttasks/cli",
  "version": "0.1.0",
  "scripts": {
    "prepack": "exit 127"
  }
}`)
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "index.js"), []byte("console.log('ok')\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tarball, err := packNpmPackageDir(packageDir, dir)
	if err != nil {
		t.Fatalf("packNpmPackageDir error: %v", err)
	}
	if filepath.Base(tarball) != "agenttasks-cli-0.1.0.tgz" {
		t.Fatalf("tarball name = %q", filepath.Base(tarball))
	}

	files, err := ExtractArchiveAs(tarball, filepath.Base(tarball), 1024*1024)
	if err != nil {
		t.Fatalf("extract sanitized tarball: %v", err)
	}
	seenPackageJSON := false
	for _, file := range files {
		if file.Name == "package/package.json" {
			seenPackageJSON = true
			break
		}
	}
	if !seenPackageJSON {
		t.Fatal("sanitized tarball missing package/package.json")
	}
}
