//go:build sqliteonly

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/cmd"
	"github.com/nextlevelbuilder/goclaw/internal/updater"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App holds Wails application state and embedded gateway lifecycle.
type App struct {
	ctx          context.Context
	cancelGw     context.CancelFunc
	gatewayToken string
	gatewayPort  int
	lastUpdate   *updater.UpdateInfo // cached update info from last check
}

// NewApp creates a new App instance with default port.
func NewApp() *App {
	return &App{
		gatewayPort: 18790,
	}
}

// startup is called by Wails when the application starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Resolve port from env or use default.
	if p := os.Getenv("GOCLAW_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &a.gatewayPort)
	}

	// Ensure secrets (encryption key + gateway token) via OS keyring or file fallback.
	encKey, gwToken, err := EnsureSecrets()
	if err != nil {
		slog.Error("failed to setup secrets", "error", err)
		return
	}
	a.gatewayToken = gwToken

	// Set env vars consumed by the embedded gateway.
	os.Setenv("GOCLAW_ENCRYPTION_KEY", encKey)
	os.Setenv("GOCLAW_GATEWAY_TOKEN", gwToken)
	os.Setenv("GOCLAW_STORAGE_BACKEND", "sqlite")
	os.Setenv("GOCLAW_DESKTOP", "1")
	// Bind to localhost only — desktop has no reason to expose on LAN.
	if os.Getenv("GOCLAW_HOST") == "" {
		os.Setenv("GOCLAW_HOST", "127.0.0.1")
	}
	slog.Info("desktop secrets configured", "token_len", len(gwToken), "token_prefix", gwToken[:min(8, len(gwToken))])

	// Ensure data directory exists.
	dataDir := os.Getenv("GOCLAW_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = home + "/.goclaw/data"
		os.Setenv("GOCLAW_DATA_DIR", dataDir)
	}
	os.MkdirAll(dataDir, 0755)

	// Start gateway in background.
	a.startGateway()

	a.waitForGateway()

	// Check for updates after gateway is ready, then every 6 hours.
	go a.updateLoop()
}

// waitForGateway polls the health endpoint until the gateway is ready or times out.
func (a *App) waitForGateway() {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", a.gatewayPort)
	for i := 0; i < 30; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			slog.Info("gateway ready", "port", a.gatewayPort)
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	slog.Warn("gateway not responding after 15s")
}

// shutdown is called by Wails when the application is closing.
func (a *App) shutdown(ctx context.Context) {
	if a.cancelGw != nil {
		a.cancelGw()
	}
}

// GetGatewayURL returns the base URL of the embedded gateway.
func (a *App) GetGatewayURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", a.gatewayPort)
}

// GetGatewayToken returns the token for WebSocket authentication.
func (a *App) GetGatewayToken() string {
	return a.gatewayToken
}

// GetGatewayPort returns the gateway port number.
func (a *App) GetGatewayPort() int {
	return a.gatewayPort
}

// IsGatewayReady checks if the gateway health endpoint is responding.
func (a *App) IsGatewayReady() bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", a.gatewayPort)
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// GetVersion returns the embedded gateway version string.
func (a *App) GetVersion() string {
	return cmd.Version
}

// OpenFile opens a local file with the system's default application.
func (a *App) OpenFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "linux":
		return exec.Command("xdg-open", path).Start()
	default: // windows
		return exec.Command("cmd", "/c", "start", "", path).Start()
	}
}

// SaveFile opens a Save As dialog and copies the source file to the chosen location.
func (a *App) SaveFile(srcPath string) error {
	dest, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		DefaultFilename: filepath.Base(srcPath),
		Title:           "Save File",
	})
	if err != nil || dest == "" {
		return err
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

// CheckForUpdate queries GitHub for a newer desktop release.
func (a *App) CheckForUpdate() (*updater.UpdateInfo, error) {
	info, err := updater.CheckForUpdate(cmd.Version)
	if err != nil {
		return nil, err
	}
	if info.Available {
		a.lastUpdate = info // cache for ApplyUpdate
	}
	return info, nil
}

// ApplyUpdate downloads and installs the cached update.
// Uses server-side cached UpdateInfo — does NOT accept URL from frontend (security).
func (a *App) ApplyUpdate() error {
	if a.lastUpdate == nil || !a.lastUpdate.Available {
		return fmt.Errorf("no update available")
	}
	appPath, err := updater.ResolveAppPath()
	if err != nil {
		return fmt.Errorf("resolve app path: %w", err)
	}
	if err := updater.DownloadAndApply(a.lastUpdate, appPath); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}
	return nil
}

// RestartApp gracefully shuts down the gateway, relaunches the app, and exits.
func (a *App) RestartApp() error {
	appPath, err := updater.ResolveAppPath()
	if err != nil {
		return err
	}
	// Graceful gateway shutdown before exit
	if a.cancelGw != nil {
		a.cancelGw()
		time.Sleep(500 * time.Millisecond) // brief wait for WAL checkpoint
	}
	switch runtime.GOOS {
	case "darwin":
		exec.Command("open", "-n", appPath).Start()
	case "windows":
		exec.Command(appPath).Start()
	}
	os.Exit(0)
	return nil
}

// updateLoop checks for updates on startup (5s delay) then every 6 hours.
// Respects app context for clean shutdown.
func (a *App) updateLoop() {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
		a.checkAndEmitUpdate()
	case <-a.ctx.Done():
		return
	}

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.checkAndEmitUpdate()
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *App) checkAndEmitUpdate() {
	info, err := updater.CheckForUpdate(cmd.Version)
	if err != nil {
		slog.Debug("update check failed", "error", err)
		return
	}
	if info.Available {
		a.lastUpdate = info
		slog.Info("update available", "version", info.Version)
		wailsRuntime.EventsEmit(a.ctx, "update:available", info)
	}
}

// GetDataDir returns the path to the data directory containing the SQLite database.
func (a *App) GetDataDir() string {
	dir := os.Getenv("GOCLAW_DATA_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = home + "/.goclaw/data"
	}
	return dir
}

// startGateway launches the embedded gateway in a background goroutine.
func (a *App) startGateway() {
	gwCtx, cancel := context.WithCancel(context.Background())
	a.cancelGw = cancel

	go func() {
		go func() {
			<-gwCtx.Done()
			p, _ := os.FindProcess(os.Getpid())
			p.Signal(os.Interrupt)
		}()
		slog.Info("starting embedded gateway", "port", a.gatewayPort)
		cmd.RunGateway()
	}()
}

// ResetDatabase deletes the SQLite DB files and restarts the entire app.
// On macOS/Linux, os.Remove works on open files (unlinks inode).
// The gateway holds the DB open and cancelGw sends SIGINT to the process,
// so we delete first, then restart the app cleanly.
func (a *App) ResetDatabase() error {
	dataDir := a.GetDataDir()
	dbPath := filepath.Join(dataDir, "goclaw.db")

	// Delete DB + WAL/SHM (works on Unix even while file is open).
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := dbPath + suffix
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			slog.Warn("database reset: failed to remove", "path", p, "error", err)
		}
	}
	slog.Info("database reset: files deleted, restarting app", "path", dbPath)

	return a.RestartApp()
}

// DownloadURL fetches a URL with Bearer auth and opens a Save As dialog.
func (a *App) DownloadURL(url, defaultFilename string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	// Use gateway token for local API calls
	if strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") {
		req.Header.Set("Authorization", "Bearer "+a.gatewayToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	dest, err := wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save File",
	})
	if err != nil || dest == "" {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
