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

func TestSetupToolRegistrySandbox(t *testing.T) {
	for _, mode := range []string{"all", "non-main"} {
		for _, failure := range []string{"missing", "daemon"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				binDir := t.TempDir()
				t.Setenv("PATH", binDir)
				if failure == "daemon" {
					if runtime.GOOS == "windows" {
						t.Skip("fake Docker executable requires a POSIX shell")
					}
					if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte("#!/bin/sh\necho 'permission denied connecting to Docker daemon' >&2\nexit 1\n"), 0755); err != nil {
						t.Fatal(err)
					}
				}
				cfg := config.Default()
				cfg.DataDir = filepath.Join(t.TempDir(), "data")
				cfg.Agents.Defaults.Sandbox = &config.SandboxConfig{Mode: mode}
				reg, approvalMgr, mcpMgr, mgr, browserMgr, fetchTool, ttsTool, audioMgr, permPE, toolPE, dataDir, _, err := setupToolRegistry(cfg, t.TempDir(), providers.NewRegistry(nil))
				if mgr != nil {
					t.Cleanup(mgr.Stop)
				}
				if mcpMgr != nil {
					t.Cleanup(mcpMgr.Stop)
				}
				if browserMgr != nil {
					t.Cleanup(func() {
						if err := browserMgr.Close(); err != nil {
							t.Error(err)
						}
					})
				}
				if err == nil || !strings.Contains(err.Error(), mode) || !strings.Contains(err.Error(), "docker not available") {
					t.Fatalf("startup error = %v, want mode and Docker cause", err)
				}
				if failure == "daemon" && !strings.Contains(err.Error(), "permission denied connecting to Docker daemon") {
					t.Fatalf("startup error lost Docker output: %v", err)
				}
				if reg != nil || mgr != nil || approvalMgr != nil || mcpMgr != nil || browserMgr != nil || fetchTool != nil || ttsTool != nil || audioMgr != nil || permPE != nil || toolPE != nil || dataDir != "" {
					t.Fatal("failed setup returned tools or later resources")
				}
				if _, err := os.Stat(cfg.DataDir); !os.IsNotExist(err) {
					t.Fatalf("data directory stat error = %v, want not exist", err)
				}
			})
		}
	}
}

func TestSetupToolRegistrySandboxModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Docker executable requires a POSIX shell")
	}
	for _, mode := range []string{"nil", "", "off", "all", "non-main"} {
		t.Run(mode, func(t *testing.T) {
			binDir := t.TempDir()
			logPath := filepath.Join(binDir, "invocations")
			t.Setenv("PATH", binDir)
			t.Setenv("DOCKER_TEST_LOG", logPath)
			enabled := mode == "all" || mode == "non-main"
			t.Setenv("DOCKER_TEST_READY", "0")
			if enabled {
				t.Setenv("DOCKER_TEST_READY", "1")
			}
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_TEST_LOG"
if [ "$DOCKER_TEST_READY" = 1 ] && [ "$#" = 3 ] && [ "$1" = info ] && [ "$2" = --format ] && [ "$3" = '{{.ServerVersion}}' ]; then
 printf '27.0.0\n'
 exit 0
fi
echo 'container creation unavailable' >&2
exit 1
`
			if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			workspace, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Default()
			cfg.DataDir = t.TempDir()
			if mode == "nil" {
				cfg.Agents.Defaults.Sandbox = nil
			} else {
				cfg.Agents.Defaults.Sandbox = &config.SandboxConfig{Mode: mode}
			}
			reg, _, mcpMgr, mgr, browserMgr, _, _, _, _, _, _, _, err := setupToolRegistry(cfg, workspace, providers.NewRegistry(nil))
			if mcpMgr != nil {
				t.Cleanup(mcpMgr.Stop)
			}
			if browserMgr != nil {
				t.Cleanup(func() {
					if err := browserMgr.Close(); err != nil {
						t.Error(err)
					}
				})
			}
			if mgr != nil {
				t.Cleanup(mgr.Stop)
			}
			if err != nil {
				t.Fatalf("setupToolRegistry() error = %v", err)
			}
			if reg == nil {
				t.Fatal("missing tool registry")
			}
			for _, name := range []string{"exec", "read_file", "write_file", "list_files", "edit"} {
				if _, ok := reg.Get(name); !ok {
					t.Errorf("tool %q not registered", name)
				}
			}
			if (mgr != nil) != enabled {
				t.Fatalf("sandbox manager = %v, enabled = %v", mgr, enabled)
			}
			invocations, err := os.ReadFile(logPath)
			if !enabled {
				if !os.IsNotExist(err) {
					t.Fatalf("disabled sandbox probed Docker: %q, error = %v", invocations, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(invocations) != "info --format {{.ServerVersion}}\n" {
				t.Fatalf("unexpected setup Docker invocations: %q", invocations)
			}
			childReg, _ := buildSubagentToolsRegistry(reg, workspace, cfg.Agents.Defaults.RestrictToWorkspace, mgr, nil)
			for _, entry := range []struct {
				name string
				reg  *tools.Registry
			}{{"primary", reg}, {"child", childReg}} {
				marker := filepath.Join(workspace, entry.name+"-marker")
				ctx := tools.WithToolSandboxKey(tools.WithToolWorkspace(context.Background(), workspace), entry.name)
				result := entry.reg.Execute(ctx, "exec", map[string]any{"command": "printf unsafe > '" + marker + "'"})
				if !result.IsError || !strings.Contains(result.ForLLM, "docker run failed") || !strings.Contains(result.ForLLM, "container creation unavailable") {
					t.Fatalf("%s exec did not fail at sandbox creation: %+v", entry.name, result)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("%s host marker stat error = %v, want not exist", entry.name, err)
				}
			}
		})
	}
}
