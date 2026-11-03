//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Phases the start page renders.
const (
	phaseStarting = "starting"
	phaseReady    = "ready"
	phaseSetup    = "setup"
	phaseUpgrade  = "upgrade"
	phasePortHeld = "portheld"
	phaseError    = "error"
)

type uiState struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	URL     string `json:"url"`
	Detail  string `json:"detail"`
}

// App is bound to the start page. Its exported methods are what the page calls.
type App struct {
	ctx        context.Context
	paths      appPaths
	installDir string

	mu     sync.Mutex // guards st and sup
	st     uiState
	sup    *supervisor
	booted sync.Mutex // serializes boot attempts
}

func newApp(p appPaths, installDir string) *App {
	return &App{paths: p, installDir: installDir, st: uiState{Phase: phaseStarting, Message: "Starting GoClaw..."}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if x, y, ok := loadWindowPosition(a.paths.WindowFile); ok {
		wailsRuntime.WindowSetPosition(ctx, x, y)
	}
	go a.boot(nil)
}

func (a *App) set(s uiState) {
	a.mu.Lock()
	a.st = s
	a.mu.Unlock()
}

// State is polled by the start page.
func (a *App) State() uiState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.st
}

// Retry runs the startup sequence again.
func (a *App) Retry() { go a.boot(nil) }

// ImportEnv lets the user pick the folder holding .env/.env.local (and
// config.json) of the existing installation and imports it.
func (a *App) ImportEnv() {
	dir, err := wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select the folder with your existing GoClaw .env / .env.local",
	})
	if err != nil || dir == "" {
		return
	}
	res, err := importFromDir(dir, a.paths.EnvFile, a.paths.ConfigFile)
	if err != nil {
		a.set(uiState{Phase: phaseSetup, Message: "Import failed: " + err.Error()})
		return
	}
	slog.Info("settings imported", "keys", res.Keys, "config_copied", res.ConfigCopied)
	go a.boot(nil)
}

// BackupAndUpgrade backs up the database and files, then starts the server once
// with automatic schema upgrade enabled. Without a successful backup nothing
// is upgraded.
func (a *App) BackupAndUpgrade() {
	go func() {
		a.set(uiState{Phase: phaseStarting, Message: "Backing up before upgrading..."})
		rc, err := buildRuntime(a.paths, a.installDir, nil)
		if err != nil {
			a.fail(err)
			return
		}
		out := filepath.Join(a.paths.BackupDir, "pre-upgrade-"+time.Now().Format("20060102-150405")+".tar.gz")
		cmd := exec.Command(rc.Server.Exe, "backup", "--output", out)
		cmd.Dir, cmd.Env = rc.Server.Dir, rc.Server.Env
		cmd.SysProcAttr = hiddenSysProcAttr()
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		if err := cmd.Run(); err != nil {
			a.set(uiState{
				Phase:   phaseError,
				Message: "The backup failed, so the database was not upgraded. Make sure Docker and the PostgreSQL container are running.",
				Detail:  tailString(buf.String(), 4000),
			})
			return
		}
		slog.Info("pre-upgrade backup written", "path", out)
		a.boot(map[string]string{"GOCLAW_AUTO_UPGRADE": "true"})
	}()
}

func (a *App) fail(err error) {
	a.set(uiState{Phase: phaseError, Message: err.Error()})
}

// boot runs the whole startup sequence and publishes progress through State.
func (a *App) boot(extra map[string]string) {
	a.booted.Lock()
	defer a.booted.Unlock()
	a.set(uiState{Phase: phaseStarting, Message: "Starting GoClaw..."})

	rc, err := buildRuntime(a.paths, a.installDir, extra)
	if errors.Is(err, errNeedsSetup) {
		a.set(uiState{Phase: phaseSetup, Message: "Import the settings of your existing GoClaw installation to get started."})
		return
	}
	if err != nil {
		a.fail(err)
		return
	}
	sup := newSupervisor(rc.Server)
	a.mu.Lock()
	a.sup = sup
	a.mu.Unlock()

	a.set(uiState{Phase: phaseStarting, Message: "Waiting for PostgreSQL at " + rc.PostgresA + "..."})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := waitForTCP(ctx, rc.PostgresA, 60*time.Second); err != nil {
		a.set(uiState{
			Phase:   phaseError,
			Message: "PostgreSQL at " + rc.PostgresA + " is not reachable. Start Docker Desktop and the database container, then retry.",
		})
		return
	}

	a.set(uiState{Phase: phaseStarting, Message: "Starting the GoClaw server..."})
	if _, err := sup.Ensure(ctx); err != nil {
		a.set(classifyStartError(err, rc.Server.Port))
		return
	}
	a.set(uiState{Phase: phaseReady, URL: sup.baseURL() + "/"})
}

func classifyStartError(err error, port int) uiState {
	var held *errPortHeld
	if errors.As(err, &held) {
		return uiState{Phase: phasePortHeld, Message: held.Error() + ". Close that program or set a different GOCLAW_PORT in your settings, then retry."}
	}
	var failed *errStartFailed
	if errors.As(err, &failed) {
		switch {
		case strings.Contains(failed.LogTail, "Database schema is outdated"):
			return uiState{
				Phase:   phaseUpgrade,
				Message: "The database schema is older than this version of GoClaw. A backup is taken first, then the schema is upgraded.",
				Detail:  tailString(failed.LogTail, 2000),
			}
		case strings.Contains(failed.LogTail, "is newer than this binary"):
			return uiState{
				Phase:   phaseError,
				Message: "The database was upgraded by a newer GoClaw than this one. Install a newer version; the database was not changed.",
				Detail:  tailString(failed.LogTail, 2000),
			}
		}
		return uiState{Phase: phaseError, Message: failed.Reason + ".", Detail: tailString(failed.LogTail, 4000)}
	}
	return uiState{Phase: phaseError, Message: err.Error()}
}

// QuitAndStop stops the server gracefully and exits the app.
func (a *App) QuitAndStop() {
	a.set(uiState{Phase: phaseStarting, Message: "Stopping the GoClaw server..."})
	a.mu.Lock()
	sup := a.sup
	a.mu.Unlock()
	if sup == nil {
		sup = newSupervisor(serverConfig{Exe: serverExePath(a.installDir), PidPath: a.paths.PidFile})
	}
	graceful, err := sup.Stop(30 * time.Second)
	slog.Info("server stop requested", "graceful", graceful, "err", err)
	wailsRuntime.Quit(a.ctx)
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	w, h := wailsRuntime.WindowGetSize(ctx)
	x, y := wailsRuntime.WindowGetPosition(ctx)
	if err := saveWindowState(a.paths.WindowFile, windowState{Width: w, Height: h, X: x, Y: y}); err != nil {
		slog.Warn("cannot save window state", "err", err)
	}
	return false
}

func serverExePath(installDir string) string {
	if v := os.Getenv("GOCLAW_SERVER_EXE"); v != "" {
		return v
	}
	return filepath.Join(installDir, "goclaw-server.exe")
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// runBackground starts the server without opening a window.
func runBackground(p appPaths, installDir string) int {
	rc, err := buildRuntime(p, installDir, nil)
	if err != nil {
		slog.Error("background start: settings", "err", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if err := waitForTCP(ctx, rc.PostgresA, 5*time.Minute); err != nil {
		slog.Error("background start: postgres", "err", err)
		return 1
	}
	reused, err := newSupervisor(rc.Server).Ensure(ctx)
	if err != nil {
		slog.Error("background start: server", "err", err)
		return 1
	}
	slog.Info("background start ok", "reused", reused, "port", strconv.Itoa(rc.Server.Port))
	return 0
}

// runQuit stops the server this app started.
func runQuit(p appPaths, installDir string) int {
	sup := newSupervisor(serverConfig{Exe: serverExePath(installDir), PidPath: p.PidFile})
	graceful, err := sup.Stop(30 * time.Second)
	slog.Info("quit", "graceful", graceful, "err", err)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not stop the server:", err)
		return 1
	}
	return 0
}
