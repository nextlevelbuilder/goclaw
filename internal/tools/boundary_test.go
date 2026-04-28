package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// helper to create a temp workspace with files
func setupWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Create a normal file
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	// Create a subdirectory
	sub := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestResolvePath_NormalFile(t *testing.T) {
	ws := setupWorkspace(t)
	resolved, err := resolvePath("hello.txt", ws, true)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if filepath.Base(resolved) != "hello.txt" {
		t.Fatalf("expected hello.txt, got: %s", resolved)
	}
}

func TestResolvePath_NestedFile(t *testing.T) {
	ws := setupWorkspace(t)
	resolved, err := resolvePath("subdir/nested.txt", ws, true)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if filepath.Base(resolved) != "nested.txt" {
		t.Fatalf("expected nested.txt, got: %s", resolved)
	}
}

func TestResolvePath_AbsolutePath(t *testing.T) {
	ws := setupWorkspace(t)
	absPath := filepath.Join(ws, "hello.txt")
	resolved, err := resolvePath(absPath, ws, true)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if resolved != absPath {
		// canonical path might differ if ws has symlinks (e.g. /tmp on macOS)
		realAbs, _ := filepath.EvalSymlinks(absPath)
		if resolved != realAbs {
			t.Fatalf("expected %s or %s, got: %s", absPath, realAbs, resolved)
		}
	}
}

func TestResolvePath_TraversalBlocked(t *testing.T) {
	ws := setupWorkspace(t)
	_, err := resolvePath("../../etc/passwd", ws, true)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestResolvePath_AbsoluteEscapeBlocked(t *testing.T) {
	ws := setupWorkspace(t)
	_, err := resolvePath("/etc/passwd", ws, true)
	if err == nil {
		t.Fatal("expected error for absolute path outside workspace, got nil")
	}
}

func TestResolvePath_SymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create a file outside workspace
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink inside workspace pointing outside
	link := filepath.Join(ws, "evil_link")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolvePath("evil_link", ws, true)
	if err == nil {
		t.Fatal("expected error for symlink escaping workspace, got nil")
	}
}

func TestResolvePath_SymlinkInsideWorkspaceAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create symlink pointing to a file within workspace
	target := filepath.Join(ws, "hello.txt")
	link := filepath.Join(ws, "good_link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolvePath("good_link", ws, true)
	if err != nil {
		t.Fatalf("expected success for symlink within workspace, got: %v", err)
	}

	// Should resolve to canonical path of target
	realTarget, _ := filepath.EvalSymlinks(target)
	if resolved != realTarget {
		t.Fatalf("expected %s, got: %s", realTarget, resolved)
	}
}

func TestResolvePath_BrokenSymlinkBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create symlink pointing to non-existent file outside workspace
	link := filepath.Join(ws, "broken_link")
	if err := os.Symlink("/nonexistent/secret", link); err != nil {
		t.Fatal(err)
	}

	_, err := resolvePath("broken_link", ws, true)
	if err == nil {
		t.Fatal("expected error for broken symlink outside workspace, got nil")
	}
}

func TestResolvePath_DirSymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require special privileges on Windows")
	}
	ws := setupWorkspace(t)

	// Create a directory symlink pointing outside workspace
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, "evil_dir")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	_, err := resolvePath("evil_dir/secret.txt", ws, true)
	if err == nil {
		t.Fatal("expected error for directory symlink escape, got nil")
	}
}

func TestResolvePath_NonExistentFileInWorkspace(t *testing.T) {
	ws := setupWorkspace(t)
	resolved, err := resolvePath("new_file.txt", ws, true)
	if err != nil {
		t.Fatalf("expected success for non-existent file in workspace, got: %v", err)
	}
	if filepath.Dir(resolved) == "" {
		t.Fatal("expected resolved path to have directory")
	}
}

func TestResolvePath_UnrestrictedAllowsEscape(t *testing.T) {
	ws := setupWorkspace(t)
	// restrict=false should allow any path
	resolved, err := resolvePath("/etc/hosts", ws, false)
	if err != nil {
		t.Fatalf("expected success with restrict=false, got: %v", err)
	}
	if resolved != "/etc/hosts" {
		t.Fatalf("expected /etc/hosts, got: %s", resolved)
	}
}

func TestCheckHardlink_NormalFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "normal.txt")
	if err := os.WriteFile(f, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkHardlink(f); err != nil {
		t.Fatalf("expected no error for normal file, got: %v", err)
	}
}

func TestCheckHardlink_HardlinkedFileBlocked(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(original, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "hardlink.txt")
	if err := os.Link(original, hardlink); err != nil {
		t.Fatal(err)
	}

	// Both original and hardlink should be rejected (nlink=2)
	if err := checkHardlink(original); err == nil {
		t.Fatal("expected error for hardlinked file (original), got nil")
	}
	if err := checkHardlink(hardlink); err == nil {
		t.Fatal("expected error for hardlinked file (link), got nil")
	}
}

func TestCheckHardlink_DirectoryAllowed(t *testing.T) {
	dir := t.TempDir()
	// Directories naturally have nlink > 1, should be exempt
	if err := checkHardlink(dir); err != nil {
		t.Fatalf("expected no error for directory, got: %v", err)
	}
}

func TestCheckHardlink_NonExistent(t *testing.T) {
	if err := checkHardlink("/nonexistent/path"); err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}
}

func TestCheckDeniedPath(t *testing.T) {
	ws := setupWorkspace(t)
	wsReal, _ := filepath.EvalSymlinks(ws)

	denied := filepath.Join(wsReal, ".goclaw", "secrets")
	if err := os.MkdirAll(filepath.Dir(denied), 0755); err != nil {
		t.Fatal(err)
	}

	err := checkDeniedPath(denied, ws, []string{".goclaw"})
	if err == nil {
		t.Fatal("expected error for denied path, got nil")
	}

	// Non-denied path should pass
	err = checkDeniedPath(filepath.Join(wsReal, "hello.txt"), ws, []string{".goclaw"})
	if err != nil {
		t.Fatalf("expected no error for non-denied path, got: %v", err)
	}
}

func TestResolvePathWithAllowed_TenantScoping(t *testing.T) {
	// Simulate: tenant workspace is a subdirectory of global workspace.
	// Paths outside tenant workspace but inside global should be BLOCKED.
	globalWs := t.TempDir()
	tenantWs := filepath.Join(globalWs, "tenants", "acme")
	otherTenantWs := filepath.Join(globalWs, "tenants", "evil")
	if err := os.MkdirAll(tenantWs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherTenantWs, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherTenantWs, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tenantWs, "ok.txt"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	// Using tenant workspace as base: should allow files inside tenant workspace
	_, err := resolvePathWithAllowed("ok.txt", tenantWs, true, nil)
	if err != nil {
		t.Fatalf("expected success for file in tenant workspace, got: %v", err)
	}

	// Using tenant workspace as base: should BLOCK files in other tenant's workspace
	_, err = resolvePathWithAllowed(filepath.Join(otherTenantWs, "secret.txt"), tenantWs, true, nil)
	if err == nil {
		t.Fatal("expected error for path in another tenant's workspace, got nil")
	}

	// Using GLOBAL workspace as base (the bug): would wrongly allow cross-tenant access
	_, err = resolvePathWithAllowed(filepath.Join(otherTenantWs, "secret.txt"), globalWs, true, nil)
	if err != nil {
		t.Fatal("global workspace allows all children (demonstrates why tenant scoping matters)")
	}
}

func TestResolvePathWithAllowed_TeamWorkspaceAccess(t *testing.T) {
	// Agent workspace and team workspace are separate directories.
	// Team workspace should be accessible via allowed prefixes.
	agentWs := t.TempDir()
	teamWs := t.TempDir()
	if err := os.WriteFile(filepath.Join(teamWs, "shared.txt"), []byte("shared"), 0644); err != nil {
		t.Fatal(err)
	}

	// Without team workspace in allowed: should BLOCK
	_, err := resolvePathWithAllowed(filepath.Join(teamWs, "shared.txt"), agentWs, true, nil)
	if err == nil {
		t.Fatal("expected error without team workspace in allowed prefixes, got nil")
	}

	// With team workspace in allowed: should ALLOW
	_, err = resolvePathWithAllowed(filepath.Join(teamWs, "shared.txt"), agentWs, true, []string{teamWs})
	if err != nil {
		t.Fatalf("expected success with team workspace in allowed prefixes, got: %v", err)
	}
}

func TestIsPathInside(t *testing.T) {
	tests := []struct {
		child, parent string
		want          bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/bc", "/a/b", false}, // not a child, just prefix match
		{"/a", "/a/b", false},
		{"/x/y", "/a/b", false},
	}
	for _, tt := range tests {
		got := isPathInside(tt.child, tt.parent)
		if got != tt.want {
			t.Errorf("isPathInside(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
		}
	}
}

func TestIsPathInside_WindowsCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	tests := []struct {
		child, parent string
		want          bool
	}{
		{`C:\Workspace\file.txt`, `c:\workspace`, true},       // case mismatch
		{`c:\workspace\file.txt`, `C:\Workspace`, true},       // reverse case
		{`C:\WORKSPACE\SUB\FILE`, `c:\workspace`, true},       // all caps child
		{`D:\other`, `C:\workspace`, false},                   // different drive
		{`C:\workspaceX\file.txt`, `C:\workspace`, false},     // prefix but not child
	}
	for _, tt := range tests {
		got := isPathInside(tt.child, tt.parent)
		if got != tt.want {
			t.Errorf("isPathInside(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
		}
	}
}

func TestResolvePathWithAllowed_CrossDriveAccess(t *testing.T) {
	// Simulates cross-drive access on Windows using separate temp directories
	// (on Unix these are just separate paths, but the test logic is the same).
	workspace := t.TempDir()
	crossDrive := t.TempDir() // simulates a different drive (e.g., F:\ vs E:\)

	// Create files in both locations
	if err := os.WriteFile(filepath.Join(workspace, "local.txt"), []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crossDrive, "remote.txt"), []byte("remote"), 0644); err != nil {
		t.Fatal(err)
	}

	// Without allowed paths: cross-drive should be BLOCKED
	_, err := resolvePathWithAllowed(filepath.Join(crossDrive, "remote.txt"), workspace, true, nil)
	if err == nil {
		t.Fatal("expected error for cross-drive access without allowed paths")
	}

	// With allowed paths: cross-drive should be ALLOWED
	_, err = resolvePathWithAllowed(filepath.Join(crossDrive, "remote.txt"), workspace, true, []string{crossDrive})
	if err != nil {
		t.Fatalf("expected success with cross-drive in allowed paths, got: %v", err)
	}

	// Allowed paths should not allow escaping to arbitrary locations
	outsideAll := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideAll, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = resolvePathWithAllowed(filepath.Join(outsideAll, "secret.txt"), workspace, true, []string{crossDrive})
	if err == nil {
		t.Fatal("expected error for path outside both workspace and allowed paths")
	}
}

func TestResolvePathWithAllowed_TenantIsolation(t *testing.T) {
	// Ensure that allowed paths cannot be used to escape tenant boundaries.
	// Scenario: tenant A's workspace, tenant B's workspace, and a shared allowed path.
	tenantA := t.TempDir()
	tenantB := t.TempDir()
	sharedSkills := t.TempDir()

	// Create files
	if err := os.WriteFile(filepath.Join(tenantA, "a.txt"), []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tenantB, "b.txt"), []byte("B"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedSkills, "skill.md"), []byte("skill"), 0644); err != nil {
		t.Fatal(err)
	}

	// Tenant A can access their workspace
	_, err := resolvePathWithAllowed(filepath.Join(tenantA, "a.txt"), tenantA, true, []string{sharedSkills})
	if err != nil {
		t.Fatalf("tenant A should access own workspace: %v", err)
	}

	// Tenant A can access shared skills
	_, err = resolvePathWithAllowed(filepath.Join(sharedSkills, "skill.md"), tenantA, true, []string{sharedSkills})
	if err != nil {
		t.Fatalf("tenant A should access shared skills: %v", err)
	}

	// Tenant A CANNOT access tenant B's workspace (even with shared skills allowed)
	_, err = resolvePathWithAllowed(filepath.Join(tenantB, "b.txt"), tenantA, true, []string{sharedSkills})
	if err == nil {
		t.Fatal("tenant A should NOT access tenant B's workspace")
	}

	// Verify the error message is about access denied
	if !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected 'access denied' error, got: %v", err)
	}
}

func TestResolvePathWithAllowed_SymlinkEscapeBlocked(t *testing.T) {
	// Ensure symlinks in allowed paths cannot be used to escape boundaries.
	workspace := t.TempDir()
	allowedDir := t.TempDir()
	secretDir := t.TempDir()

	// Create a secret file outside both workspace and allowed
	if err := os.WriteFile(filepath.Join(secretDir, "secret.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink in allowed dir pointing to secret dir
	evilLink := filepath.Join(allowedDir, "escape")
	if err := os.Symlink(secretDir, evilLink); err != nil {
		t.Skip("cannot create symlinks (permissions or OS limitation)")
	}

	// Attempt to access secret file via symlink escape
	_, err := resolvePathWithAllowed(filepath.Join(evilLink, "secret.txt"), workspace, true, []string{allowedDir})
	if err == nil {
		t.Fatal("expected error for symlink escape attempt")
	}
}

func TestAllowedWithTeamWorkspace_TenantPathsMerge(t *testing.T) {
	// Test that allowedWithTeamWorkspace correctly merges:
	// base (global) + tenant paths (from context) + team workspace (from context)
	ctx := context.Background()

	base := []string{"/global/skills", "/global/builtin"}
	tenantPaths := []string{"/tenant/allowed1", "/tenant/allowed2"}
	teamWs := "/team/workspace"

	// Inject tenant paths and team workspace into context
	ctx = WithTenantAllowedPaths(ctx, tenantPaths)
	ctx = WithToolTeamWorkspace(ctx, teamWs)

	result := allowedWithTeamWorkspace(ctx, base)

	// Verify all paths are present in correct order
	expected := []string{"/global/skills", "/global/builtin", "/tenant/allowed1", "/tenant/allowed2", "/team/workspace"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d paths, got %d", len(expected), len(result))
	}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("path[%d]: expected %q, got %q", i, exp, result[i])
		}
	}
}

func TestAllowedWithTeamWorkspace_TenantPathsOnly(t *testing.T) {
	// Test tenant paths without team workspace
	ctx := context.Background()
	base := []string{"/global/skills"}
	tenantPaths := []string{"/tenant/data"}

	ctx = WithTenantAllowedPaths(ctx, tenantPaths)

	result := allowedWithTeamWorkspace(ctx, base)

	if len(result) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(result))
	}
	if result[0] != "/global/skills" || result[1] != "/tenant/data" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestAllowedWithTeamWorkspace_TeamRootMerged(t *testing.T) {
	// Team root should be appended after team workspace so leader/member agents
	// can read peer-scoped files in the same team without enabling shared mode.
	ctx := context.Background()
	base := []string{"/global/skills"}
	teamWs := "/data/teams/abc/chatA"
	teamRoot := "/data/teams/abc"

	ctx = WithToolTeamWorkspace(ctx, teamWs)
	ctx = WithToolTeamRoot(ctx, teamRoot)

	result := allowedWithTeamWorkspace(ctx, base)

	expected := []string{"/global/skills", teamWs, teamRoot}
	if len(result) != len(expected) {
		t.Fatalf("expected %d paths, got %d: %v", len(expected), len(result), result)
	}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("path[%d]: expected %q, got %q", i, exp, result[i])
		}
	}
}

func TestAllowedWriteWithTeamWorkspace_ExcludesTeamRoot(t *testing.T) {
	// Write variant must NOT include team root — cross-chat writes are blocked
	// even when reads across the same team are allowed. Shared-mode parity:
	// when teamWs == teamRoot (shared workspace), writing to teamWs is still
	// permitted because teamWs is the leaf scope.
	ctx := context.Background()
	base := []string{"/global/skills"}
	teamWs := "/data/teams/abc/chatA"
	teamRoot := "/data/teams/abc"

	ctx = WithToolTeamWorkspace(ctx, teamWs)
	ctx = WithToolTeamRoot(ctx, teamRoot)

	writeAllowed := allowedWriteWithTeamWorkspace(ctx, base)
	readAllowed := allowedWithTeamWorkspace(ctx, base)

	// Read allowed should include team root.
	if len(readAllowed) != 3 {
		t.Fatalf("read: expected 3 prefixes, got %d: %v", len(readAllowed), readAllowed)
	}
	// Write allowed must NOT include team root.
	if len(writeAllowed) != 2 {
		t.Fatalf("write: expected 2 prefixes (base + teamWs), got %d: %v", len(writeAllowed), writeAllowed)
	}
	for _, p := range writeAllowed {
		if p == teamRoot {
			t.Errorf("write allowed must not include team root %q", teamRoot)
		}
	}
}

func TestAllowedWithTeamWorkspace_TeamRootDeduped(t *testing.T) {
	// When team root == team workspace (shared-workspace mode), avoid duplicate entry.
	ctx := context.Background()
	base := []string{"/global/skills"}
	same := "/data/teams/abc"

	ctx = WithToolTeamWorkspace(ctx, same)
	ctx = WithToolTeamRoot(ctx, same)

	result := allowedWithTeamWorkspace(ctx, base)

	if len(result) != 2 {
		t.Fatalf("expected 2 paths (base + one team path), got %d: %v", len(result), result)
	}
	if result[0] != "/global/skills" || result[1] != same {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestResolvePathWithAllowed_TeamRootCrossChatAccess(t *testing.T) {
	// Reproduces trace 019db4df-c2e2: leader agent in chat scope "chatA" tries to
	// read a file generated by a teammate under chat scope "chatB" within the
	// same team. Team root as allowed prefix must permit this cross-chat read.
	teamRoot := t.TempDir()
	chatA := filepath.Join(teamRoot, "chatA")
	chatB := filepath.Join(teamRoot, "chatB", "generated")
	if err := os.MkdirAll(chatA, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(chatB, 0755); err != nil {
		t.Fatal(err)
	}
	peerFile := filepath.Join(chatB, "v3-02-prompt-mode.png")
	if err := os.WriteFile(peerFile, []byte("img"), 0644); err != nil {
		t.Fatal(err)
	}

	// Without team root: leader scoped to chatA cannot reach chatB.
	_, err := resolvePathWithAllowed(peerFile, chatA, true, nil)
	if err == nil {
		t.Fatal("expected error without team root in allowed prefixes, got nil")
	}

	// With team root as allowed prefix: access granted.
	_, err = resolvePathWithAllowed(peerFile, chatA, true, []string{teamRoot})
	if err != nil {
		t.Fatalf("expected success with team root in allowed prefixes, got: %v", err)
	}
}

func TestAllowedWithTeamWorkspace_EmptyContext(t *testing.T) {
	// Test with no tenant paths or team workspace in context
	ctx := context.Background()
	base := []string{"/global/skills"}

	result := allowedWithTeamWorkspace(ctx, base)

	// Should return base unchanged
	if len(result) != 1 || result[0] != "/global/skills" {
		t.Errorf("expected base unchanged, got: %v", result)
	}
}

func TestTenantAllowedPathsFromCtx_Inheritance(t *testing.T) {
	// Test that TenantAllowedPathsFromCtx correctly reads from RunContext fallback
	ctx := context.Background()
	tenantPaths := []string{"/tenant/path1", "/tenant/path2"}

	// Set via direct context
	ctx = WithTenantAllowedPaths(ctx, tenantPaths)
	result := TenantAllowedPathsFromCtx(ctx)

	if len(result) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(result))
	}
	if result[0] != "/tenant/path1" || result[1] != "/tenant/path2" {
		t.Errorf("unexpected result: %v", result)
	}
}
