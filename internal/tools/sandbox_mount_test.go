package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type recordingSandboxManager struct {
	sandbox   *recordingSandbox
	key       string
	workspace string
	cfg       *sandbox.Config
}

func (m *recordingSandboxManager) Get(ctx context.Context, key string, workspace string, cfg *sandbox.Config) (sandbox.Sandbox, error) {
	m.key = key
	m.workspace = workspace
	m.cfg = cfg
	if m.sandbox == nil {
		m.sandbox = &recordingSandbox{}
	}
	return m.sandbox, nil
}

func (m *recordingSandboxManager) Release(context.Context, string) error { return nil }
func (m *recordingSandboxManager) ReleaseAll(context.Context) error      { return nil }
func (m *recordingSandboxManager) Stop()                                 {}
func (m *recordingSandboxManager) Stats() map[string]any                 { return nil }

type recordingSandbox struct {
	workDir string
	command []string
}

func (s *recordingSandbox) Exec(ctx context.Context, command []string, workDir string, opts ...sandbox.ExecOption) (*sandbox.ExecResult, error) {
	s.command = append([]string(nil), command...)
	s.workDir = workDir
	return &sandbox.ExecResult{Stdout: "ok"}, nil
}

func (s *recordingSandbox) Destroy(context.Context) error { return nil }
func (s *recordingSandbox) ID() string                    { return "recording-sandbox" }

func TestEffectiveSandboxWorkspacePrefersTenantWorkspace(t *testing.T) {
	globalWorkspace := "/srv/goclaw/workspace"
	tenantWorkspace := "/srv/goclaw/workspace/tenants/acme/sessions/direct"

	got, err := effectiveSandboxWorkspace(WithToolWorkspace(context.Background(), tenantWorkspace), globalWorkspace)
	if err != nil {
		t.Fatalf("effectiveSandboxWorkspace returned error: %v", err)
	}
	if got != tenantWorkspace {
		t.Fatalf("effectiveSandboxWorkspace = %q, want tenant workspace %q", got, tenantWorkspace)
	}
}

func TestEffectiveSandboxWorkspaceFailsClosedWithoutTenantWorkspace(t *testing.T) {
	ctx := store.WithTenantID(context.Background(), uuid.New())

	if got, err := effectiveSandboxWorkspace(ctx, "/srv/goclaw/workspace"); err == nil {
		t.Fatalf("effectiveSandboxWorkspace = %q, want fail-closed error for tenant context without workspace", got)
	}
}

func TestEffectiveSandboxWorkspaceAllowsMasterFallback(t *testing.T) {
	globalWorkspace := "/srv/goclaw/workspace"
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)

	got, err := effectiveSandboxWorkspace(ctx, globalWorkspace)
	if err != nil {
		t.Fatalf("effectiveSandboxWorkspace returned error: %v", err)
	}
	if got != globalWorkspace {
		t.Fatalf("effectiveSandboxWorkspace = %q, want global workspace %q", got, globalWorkspace)
	}
}

func TestSandboxCwdForHostPathMapsMountRootToWorkspace(t *testing.T) {
	mountWorkspace := "/srv/goclaw/workspace/tenants/acme"

	got, err := sandboxCwdForHostPath(mountWorkspace, mountWorkspace, sandbox.DefaultContainerWorkdir)
	if err != nil {
		t.Fatalf("sandboxCwdForHostPath returned error: %v", err)
	}
	if got != sandbox.DefaultContainerWorkdir {
		t.Fatalf("sandboxCwdForHostPath = %q, want %q", got, sandbox.DefaultContainerWorkdir)
	}
}

func TestSandboxCwdForHostPathMapsChildPathUnderWorkspace(t *testing.T) {
	mountWorkspace := "/srv/goclaw/workspace/tenants/acme"
	cwd := "/srv/goclaw/workspace/tenants/acme/project"

	got, err := sandboxCwdForHostPath(cwd, mountWorkspace, sandbox.DefaultContainerWorkdir)
	if err != nil {
		t.Fatalf("sandboxCwdForHostPath returned error: %v", err)
	}
	if got != "/workspace/project" {
		t.Fatalf("sandboxCwdForHostPath = %q, want /workspace/project", got)
	}
}

func TestSandboxCwdForHostPathRejectsWorkspaceEscape(t *testing.T) {
	mountWorkspace := "/srv/goclaw/workspace/tenants/acme"
	cwd := "/srv/goclaw/workspace/tenants/other"

	if got, err := sandboxCwdForHostPath(cwd, mountWorkspace, sandbox.DefaultContainerWorkdir); err == nil {
		t.Fatalf("sandboxCwdForHostPath = %q, want escape error", got)
	}
}

func TestExecSandboxUsesEffectiveWorkspaceMountAndContainerCwd(t *testing.T) {
	globalWorkspace := "/srv/goclaw/workspace"
	tenantWorkspace := "/srv/goclaw/workspace/tenants/acme"
	mgr := &recordingSandboxManager{}
	tool := NewSandboxedExecTool(globalWorkspace, true, mgr)

	result := tool.executeInSandbox(WithToolWorkspace(context.Background(), tenantWorkspace), "pwd", tenantWorkspace, "session-1")
	if result.IsError {
		t.Fatalf("executeInSandbox returned error: %s", result.ForLLM)
	}
	if mgr.workspace != tenantWorkspace {
		t.Fatalf("sandbox manager workspace = %q, want tenant workspace %q", mgr.workspace, tenantWorkspace)
	}
	if mgr.sandbox.workDir != sandbox.DefaultContainerWorkdir {
		t.Fatalf("sandbox exec workDir = %q, want %q", mgr.sandbox.workDir, sandbox.DefaultContainerWorkdir)
	}
}

func TestCredentialedExecSandboxUsesEffectiveWorkspaceMountAndContainerCwd(t *testing.T) {
	globalWorkspace := "/srv/goclaw/workspace"
	tenantWorkspace := "/srv/goclaw/workspace/tenants/acme"
	mgr := &recordingSandboxManager{}
	tool := NewSandboxedExecTool(globalWorkspace, true, mgr)

	result := tool.executeCredentialedSandbox(WithToolWorkspace(context.Background(), tenantWorkspace), "/usr/bin/gh", []string{"api", "user"}, tenantWorkspace, "session-1", map[string]string{"GOCLAW_TEST_ENV": "value"}, 30*time.Second)
	if result.IsError {
		t.Fatalf("executeCredentialedSandbox returned error: %s", result.ForLLM)
	}
	if mgr.workspace != tenantWorkspace {
		t.Fatalf("sandbox manager workspace = %q, want tenant workspace %q", mgr.workspace, tenantWorkspace)
	}
	if mgr.sandbox.workDir != sandbox.DefaultContainerWorkdir {
		t.Fatalf("sandbox exec workDir = %q, want %q", mgr.sandbox.workDir, sandbox.DefaultContainerWorkdir)
	}
}

func TestCredentialedExecSandboxWorkingDirResolvesInsideTenantWorkspace(t *testing.T) {
	globalWorkspace := t.TempDir()
	tenantWorkspace := filepath.Join(globalWorkspace, "tenants", "acme")
	subdir := filepath.Join(tenantWorkspace, "repo")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	binaryPath := filepath.Join(tenantWorkspace, "fake-cli")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	wantMountWorkspace, err := filepath.EvalSymlinks(tenantWorkspace)
	if err != nil {
		t.Fatalf("canonicalize tenant workspace: %v", err)
	}

	mgr := &recordingSandboxManager{}
	tool := NewSandboxedExecTool(globalWorkspace, true, mgr)
	tool.SetSecureCLIStore(&sandboxMountSecureCLIStore{binary: &store.SecureCLIBinary{
		BinaryName:     "fake-cli",
		BinaryPath:     &binaryPath,
		TimeoutSeconds: 30,
		Enabled:        true,
		IsGlobal:       true,
	}})

	ctx := WithToolWorkspace(context.Background(), tenantWorkspace)
	ctx = WithToolSandboxKey(ctx, "session-1")
	result := tool.Execute(ctx, map[string]any{
		"command":     "fake-cli status",
		"working_dir": "repo",
	})
	if result.IsError {
		t.Fatalf("Execute returned error: %s", result.ForLLM)
	}
	if mgr.workspace != wantMountWorkspace {
		t.Fatalf("sandbox manager workspace = %q, want tenant workspace %q", mgr.workspace, wantMountWorkspace)
	}
	if mgr.sandbox.workDir != "/workspace/repo" {
		t.Fatalf("sandbox exec workDir = %q, want /workspace/repo", mgr.sandbox.workDir)
	}
}

func TestSandboxFileToolsUseEffectiveWorkspaceMount(t *testing.T) {
	globalWorkspace := "/srv/goclaw/workspace"
	tenantWorkspace := "/srv/goclaw/workspace/tenants/acme"
	ctx := WithToolWorkspace(context.Background(), tenantWorkspace)

	tests := []struct {
		name string
		run  func(*recordingSandboxManager) error
	}{
		{
			name: "read_file",
			run: func(mgr *recordingSandboxManager) error {
				tool := NewSandboxedReadFileTool(globalWorkspace, true, mgr)
				_, err := tool.getFsBridge(ctx, "session-1", tenantWorkspace, sandbox.DefaultContainerWorkdir)
				return err
			},
		},
		{
			name: "write_file",
			run: func(mgr *recordingSandboxManager) error {
				tool := NewSandboxedWriteFileTool(globalWorkspace, true, mgr)
				_, err := tool.getFsBridge(ctx, "session-1", tenantWorkspace, sandbox.DefaultContainerWorkdir)
				return err
			},
		},
		{
			name: "list_files",
			run: func(mgr *recordingSandboxManager) error {
				tool := NewSandboxedListFilesTool(globalWorkspace, true, mgr)
				_, err := tool.getFsBridge(ctx, "session-1", tenantWorkspace, sandbox.DefaultContainerWorkdir)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &recordingSandboxManager{}
			if err := tt.run(mgr); err != nil {
				t.Fatalf("getFsBridge returned error: %v", err)
			}
			if mgr.workspace != tenantWorkspace {
				t.Fatalf("sandbox manager workspace = %q, want tenant workspace %q", mgr.workspace, tenantWorkspace)
			}
		})
	}
}

type sandboxMountSecureCLIStore struct {
	binary *store.SecureCLIBinary
}

func (s *sandboxMountSecureCLIStore) Create(context.Context, *store.SecureCLIBinary) error {
	return nil
}
func (s *sandboxMountSecureCLIStore) Get(context.Context, uuid.UUID) (*store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *sandboxMountSecureCLIStore) Update(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (s *sandboxMountSecureCLIStore) Delete(context.Context, uuid.UUID) error { return nil }
func (s *sandboxMountSecureCLIStore) List(context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *sandboxMountSecureCLIStore) LookupByBinary(context.Context, string, *uuid.UUID, string) (*store.SecureCLIBinary, error) {
	return s.binary, nil
}
func (s *sandboxMountSecureCLIStore) ListEnabled(context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *sandboxMountSecureCLIStore) ListForAgent(context.Context, uuid.UUID) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *sandboxMountSecureCLIStore) IsRegisteredBinary(context.Context, string) (bool, error) {
	return false, nil
}
func (s *sandboxMountSecureCLIStore) GetUserCredentials(context.Context, uuid.UUID, string) (*store.SecureCLIUserCredential, error) {
	return nil, nil
}
func (s *sandboxMountSecureCLIStore) SetUserCredentials(context.Context, uuid.UUID, string, []byte) error {
	return nil
}
func (s *sandboxMountSecureCLIStore) SetUserCredentialsTyped(context.Context, uuid.UUID, string, []byte, *string, *string) error {
	return nil
}
func (s *sandboxMountSecureCLIStore) DeleteUserCredentials(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *sandboxMountSecureCLIStore) ListUserCredentials(context.Context, uuid.UUID) ([]store.SecureCLIUserCredential, error) {
	return nil, nil
}

func TestExecSandboxFailsClosedWhenTenantWorkspaceMissing(t *testing.T) {
	globalWorkspace := "/srv/goclaw/workspace"
	mgr := &recordingSandboxManager{}
	tool := NewSandboxedExecTool(globalWorkspace, true, mgr)
	ctx := store.WithTenantID(context.Background(), uuid.New())

	result := tool.executeInSandbox(ctx, "pwd", globalWorkspace, "session-1")
	if !result.IsError {
		t.Fatalf("executeInSandbox succeeded, want fail-closed error")
	}
	if result.ForLLM == "" {
		t.Fatalf("executeInSandbox returned empty error result")
	}
	if mgr.workspace != "" {
		t.Fatalf("sandbox manager should not be called, got workspace %q", mgr.workspace)
	}
}
