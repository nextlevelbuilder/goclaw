package cmd

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func testExecToolFromGatewaySetup(t *testing.T, workspace, dataDir string) *tools.ExecTool {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Tools.Browser.Enabled = false

	providerRegistry := providers.NewRegistry(nil)
	toolsReg, _, _, _, _, _, _, _, _, _, _ := setupToolRegistry(cfg, workspace, providerRegistry)

	execToolAny, ok := toolsReg.Get("exec")
	if !ok {
		t.Fatal("exec tool not registered")
	}
	execTool, ok := execToolAny.(*tools.ExecTool)
	if !ok {
		t.Fatalf("exec tool type = %T, want *tools.ExecTool", execToolAny)
	}
	return execTool
}

func TestSetupToolRegistryExecWorkspacePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires elevated privileges on Windows")
	}
	dataDir := t.TempDir()
	workspace := filepath.Join(dataDir, "personal")
	teamWorkspace := filepath.Join(dataDir, "teams", "team-123")
	for _, dir := range []string{workspace, teamWorkspace} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", dir, err)
		}
	}

	execTool := testExecToolFromGatewaySetup(t, workspace, dataDir)

	uploadPath := filepath.Join(workspace, ".uploads", "Quarterly Report.png")
	if err := os.MkdirAll(filepath.Dir(uploadPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(uploadPath, []byte("ok"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	teamFilePath := filepath.Join(teamWorkspace, "Quarterly Report.png")
	if err := os.WriteFile(teamFilePath, []byte("ok"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	protectedPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(protectedPath, []byte("secret"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	symlinkPath := filepath.Join(teamWorkspace, "leak.txt")
	if err := os.Symlink(protectedPath, symlinkPath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	legacyWorkspace := filepath.Join(dataDir, ".goclaw", "goclaw-workspace", "ws", "system")
	legacyUploadPath := filepath.Join(legacyWorkspace, "uploads", "Quarterly Report.png")
	legacyCopyTarget := filepath.Join(t.TempDir(), "partner.png")
	if err := os.MkdirAll(filepath.Dir(legacyUploadPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(legacyUploadPath, []byte("ok"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tests := []struct {
		name       string
		ctx        context.Context
		command    string
		wantDenied bool
		wantPath   string
	}{
		{
			name:    "personal_workspace_prefixed_uploads_allowed",
			ctx:     tools.WithToolWorkspace(context.Background(), workspace),
			command: "printf '%s' file=@\"" + uploadPath + "\"",
		},
		{
			name:    "team_workspace_quoted_input_allowed",
			ctx:     tools.WithToolTeamWorkspace(tools.WithToolWorkspace(context.Background(), workspace), teamWorkspace),
			command: "printf '%s' --input=\"" + teamFilePath + "\"",
		},
		{
			name:     "legacy_dotgoclaw_uploads_layout_allowed",
			ctx:      tools.WithToolWorkspace(context.Background(), legacyWorkspace),
			command:  "cp \"" + legacyUploadPath + "\" \"" + legacyCopyTarget + "\"",
			wantPath: legacyCopyTarget,
		},
		{
			name:       "team_workspace_symlink_escape_denied",
			ctx:        tools.WithToolTeamWorkspace(tools.WithToolWorkspace(context.Background(), workspace), teamWorkspace),
			command:    "printf '%s' " + symlinkPath,
			wantDenied: true,
		},
		{
			name:       "unrelated_data_dir_path_denied",
			ctx:        tools.WithToolWorkspace(context.Background(), workspace),
			command:    "printf '%s' " + protectedPath,
			wantDenied: true,
		},
		{
			name:       "workspace_local_dotgoclaw_denied",
			ctx:        tools.WithToolWorkspace(context.Background(), workspace),
			command:    "printf '%s' " + filepath.Join(workspace, ".goclaw", "secrets.json"),
			wantDenied: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := execTool.Execute(tc.ctx, map[string]any{
				"command": tc.command,
			})

			denied := strings.Contains(result.ForLLM, "command denied by safety policy")
			if denied != tc.wantDenied {
				t.Fatalf("denied = %v, want %v; output = %s", denied, tc.wantDenied, result.ForLLM)
			}
			if tc.wantPath != "" {
				if _, err := os.Stat(tc.wantPath); err != nil {
					t.Fatalf("expected copied file at %q, got stat error: %v", tc.wantPath, err)
				}
			} else if !tc.wantDenied && !strings.Contains(result.ForLLM, "Quarterly Report.png") {
				t.Fatalf("expected output to contain quoted file path, got: %s", result.ForLLM)
			}
		})
	}
}
