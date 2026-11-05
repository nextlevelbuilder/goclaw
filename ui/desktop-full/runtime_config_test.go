//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPaths(t *testing.T) appPaths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	p, err := resolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeSettings(t *testing.T, p appPaths, body string) {
	t.Helper()
	if err := os.WriteFile(p.EnvFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRuntimeNeedsSetupWithoutSettings(t *testing.T) {
	p := testPaths(t)
	if _, err := buildRuntime(p, `C:\app`, nil); err != errNeedsSetup {
		t.Fatalf("err = %v, want errNeedsSetup", err)
	}
}

func TestBuildRuntimeIgnoresAmbientGoclawVariables(t *testing.T) {
	p := testPaths(t)
	t.Setenv("GOCLAW_LOG_FILE", `X:\ambient.log`)
	t.Setenv("GOCLAW_HOST", "0.0.0.0")
	writeSettings(t, p, "GOCLAW_POSTGRES_DSN=postgres://u:p@127.0.0.1:5432/db\n")
	rc, err := buildRuntime(p, `C:\app`, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := childEnv(rc.Server.Env)
	if env.get("GOCLAW_HOST") != "127.0.0.1" || env.get("GOCLAW_LOG_FILE") != p.ServerLog {
		t.Fatalf("ambient variables leaked: host=%q log=%q", env.get("GOCLAW_HOST"), env.get("GOCLAW_LOG_FILE"))
	}
}

func TestBuildRuntimeDefaultsAndOverrides(t *testing.T) {
	p := testPaths(t)
	writeSettings(t, p, "GOCLAW_POSTGRES_DSN=postgres://u:p@127.0.0.1:5432/db\nGOCLAW_ALLOWED_ORIGINS=https://a.example\n")
	if err := os.WriteFile(p.ConfigFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := buildRuntime(p, `C:\app`, map[string]string{"GOCLAW_AUTO_UPGRADE": "true"})
	if err != nil {
		t.Fatal(err)
	}
	env := childEnv(rc.Server.Env)
	check := func(key, want string) {
		t.Helper()
		if got := env.get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if rc.Server.Port != 18800 {
		t.Errorf("port = %d, want the app default 18800", rc.Server.Port)
	}
	check("GOCLAW_HOST", "127.0.0.1")
	check("GOCLAW_PORT", "18800")
	check("GOCLAW_CONFIG", p.ConfigFile)
	check("GOCLAW_LOG_FILE", p.ServerLog)
	check("GOCLAW_AUTO_UPGRADE", "true")
	check("GOCLAW_ALLOWED_ORIGINS", "https://a.example,http://127.0.0.1:18800")
	if !strings.HasPrefix(env.get("PATH"), `C:\app`+string(os.PathListSeparator)) {
		t.Errorf("PATH does not start with the install dir: %q", env.get("PATH"))
	}
	if rc.PostgresA != "127.0.0.1:5432" {
		t.Errorf("postgres addr = %q", rc.PostgresA)
	}
	if rc.Server.Exe != `C:\app\goclaw-server.exe` {
		t.Errorf("exe = %q", rc.Server.Exe)
	}
}

func TestBuildRuntimeKeepsExplicitSettings(t *testing.T) {
	p := testPaths(t)
	writeSettings(t, p, "GOCLAW_POSTGRES_DSN=postgres://u:p@pg:6000/db\nGOCLAW_PORT=19000\nGOCLAW_HOST=0.0.0.0\nGOCLAW_LOG_FILE=D:\\logs\\g.log\nGOCLAW_ALLOWED_ORIGINS=http://127.0.0.1:19000\n")
	rc, err := buildRuntime(p, `C:\app`, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := childEnv(rc.Server.Env)
	if rc.Server.Port != 19000 || env.get("GOCLAW_HOST") != "0.0.0.0" || rc.Server.LogPath != `D:\logs\g.log` {
		t.Fatalf("explicit settings overridden: port=%d host=%q log=%q", rc.Server.Port, env.get("GOCLAW_HOST"), rc.Server.LogPath)
	}
	if got := env.get("GOCLAW_ALLOWED_ORIGINS"); got != "http://127.0.0.1:19000" {
		t.Fatalf("origin duplicated: %q", got)
	}
	if rc.PostgresA != "pg:6000" {
		t.Fatalf("postgres addr = %q", rc.PostgresA)
	}
}

func TestBuildRuntimeRejectsBadSettings(t *testing.T) {
	p := testPaths(t)
	writeSettings(t, p, "FOO=bar\n")
	if _, err := buildRuntime(p, `C:\app`, nil); err == nil {
		t.Fatal("missing DSN must be an error")
	}
	writeSettings(t, p, "GOCLAW_POSTGRES_DSN=postgres://u@h/db\nGOCLAW_PORT=notaport\n")
	if _, err := buildRuntime(p, `C:\app`, nil); err == nil {
		t.Fatal("invalid port must be an error")
	}
}
