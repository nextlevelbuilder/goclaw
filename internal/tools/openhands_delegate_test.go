package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeGitRemoteURL(t *testing.T) {
	tests := map[string]string{
		"https://github.com/nextlevelbuilder/goclaw": "https://github.com/nextlevelbuilder/goclaw",
		"git@github.com:nextlevelbuilder/goclaw.git": "https://github.com/nextlevelbuilder/goclaw.git",
		"ssh://git@gitlab.com/org/repo.git":          "https://gitlab.com/org/repo.git",
	}
	for in, want := range tests {
		if got := normalizeGitRemoteURL(in); got != want {
			t.Fatalf("normalizeGitRemoteURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeSubdir(t *testing.T) {
	if got := sanitizeSubdir("pkg/tools"); got != "pkg/tools" {
		t.Fatalf("expected pkg/tools, got %q", got)
	}
	if got := sanitizeSubdir("../etc"); got != "__invalid__" {
		t.Fatalf("expected invalid marker, got %q", got)
	}
	if got := sanitizeSubdir("."); got != "" {
		t.Fatalf("expected empty for dot, got %q", got)
	}
}

func TestShouldSkipSnapshotPath(t *testing.T) {
	cases := map[string]bool{
		".git/config":        true,
		"node_modules/react": true,
		"src/main.go":        false,
		"dist/app.js":        true,
	}
	for rel, want := range cases {
		got := shouldSkipSnapshotPath(rel, filepath.Base(rel) == "dist")
		if got != want {
			t.Fatalf("shouldSkipSnapshotPath(%q) = %v, want %v", rel, got, want)
		}
	}
}

func TestBuildUntrackedTarball(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "new.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := buildUntrackedTarball(root, "new.txt\x00")
	if err != nil {
		t.Fatalf("buildUntrackedTarball returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected tarball bytes")
	}
}

func TestResolveGitWorkspaceFallsBackToUpstreamForUnpublishedHead(t *testing.T) {
	ctx := context.Background()
	origin, local := initGitOriginAndClone(t)

	writeFile(t, filepath.Join(local, "README.md"), "base\n")
	gitRun(t, local, "add", "README.md")
	gitRun(t, local, "commit", "-m", "base")
	gitRun(t, local, "branch", "-M", "main")
	gitRun(t, local, "push", "-u", "origin", "main")

	writeFile(t, filepath.Join(local, "README.md"), "base\nlocal change\n")
	gitRun(t, local, "commit", "-am", "local only")

	head := strings.TrimSpace(gitRun(t, local, "rev-parse", "HEAD"))
	if remoteContainsCommit(ctx, local, head) {
		t.Fatal("expected local-only HEAD to be absent from origin refs")
	}
	ref, err := resolveFallbackRemoteRef(ctx, local)
	if err != nil {
		t.Fatalf("resolveFallbackRemoteRef returned error: %v", err)
	}
	if ref != "origin/main" {
		t.Fatalf("expected fallback ref origin/main, got %q", ref)
	}

	tool := NewOpenHandsDelegateTool(local, OpenHandsDelegateConfig{
		MaxUploadMB:         8,
		AllowedRepoPrefixes: []string{normalizeGitRemoteURL(origin)},
	})
	gitCtx, err := tool.resolveGitWorkspace(ctx, local, true, "", "", "")
	if err != nil {
		t.Fatalf("resolveGitWorkspace returned error: %v", err)
	}
	if gitCtx.ref != "origin/main" {
		t.Fatalf("expected ref origin/main, got %q", gitCtx.ref)
	}
	if gitCtx.patchBase != "origin/main" {
		t.Fatalf("expected patchBase origin/main, got %q", gitCtx.patchBase)
	}
	if gitCtx.overlayPatchB64 == "" {
		t.Fatal("expected overlay patch for unpublished local commit")
	}
}

func TestBuildCreateRequestUsesDetachedGitRefOutsideRepo(t *testing.T) {
	ctx := context.Background()
	tool := NewOpenHandsDelegateTool(t.TempDir(), OpenHandsDelegateConfig{
		MaxUploadMB:         8,
		AllowedRepoPrefixes: []string{"https://github.com/"},
	})
	req, summary, err := tool.buildCreateRequest(ctx, "git_ref", true, "https://github.com/nextlevelbuilder/goclaw", "main", "", "Inspect repo", 300, 12)
	if err != nil {
		t.Fatalf("buildCreateRequest returned error: %v", err)
	}
	if req.Mode != "git_ref" {
		t.Fatalf("expected git_ref mode, got %q", req.Mode)
	}
	if req.RepoURL != "https://github.com/nextlevelbuilder/goclaw" {
		t.Fatalf("unexpected repo_url %q", req.RepoURL)
	}
	if req.Ref != "main" {
		t.Fatalf("unexpected ref %q", req.Ref)
	}
	if req.OverlayPatchB64 != "" || req.OverlayUntrackedTarB64 != "" {
		t.Fatal("detached git request should not include local overlays")
	}
	if !strings.Contains(summary, "default branch") && !strings.Contains(summary, "@ main") {
		t.Fatalf("unexpected summary %q", summary)
	}
}

func initGitOriginAndClone(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	local := filepath.Join(root, "local")
	gitRun(t, root, "init", "--bare", origin)
	gitRun(t, root, "clone", origin, local)
	gitRun(t, local, "config", "user.name", "Codex")
	gitRun(t, local, "config", "user.email", "codex@example.com")
	return origin, local
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
