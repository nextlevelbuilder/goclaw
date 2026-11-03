//go:build windows

// GoClaw desktop shell: a native window around the full (PostgreSQL) gateway.
//
// The server (goclaw-server.exe) runs detached from this process, so closing the
// window does not stop bots or cron. It stops through "Quit and stop server",
// `GoClawDesktop.exe --quit`, or the installer.
//
//	GoClawDesktop.exe              open the window (starts the server when needed)
//	GoClawDesktop.exe --background start the server only
//	GoClawDesktop.exe --quit       stop the server this app started
package main

import (
	"embed"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed assets
var assets embed.FS

const singleInstanceID = "sh.goclaw.desktop-full"

func main() {
	p, err := resolvePaths()
	if err != nil {
		os.Exit(2)
	}
	if err := p.ensure(); err != nil {
		os.Exit(2)
	}
	logFile, err := os.OpenFile(p.ShellLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		defer logFile.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Writer(logFile), nil)))
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	installDir := filepath.Dir(exe)

	for _, arg := range os.Args[1:] {
		switch arg {
		case "--quit":
			os.Exit(runQuit(p, installDir))
		case "--background":
			os.Exit(runBackground(p, installDir))
		}
	}
	os.Exit(runWindow(p, installDir, exe))
}

func runWindow(p appPaths, installDir, exe string) int {
	app := newApp(p, installDir)
	width, height := windowSize(p.WindowFile)

	appMenu := menu.NewMenu()
	file := appMenu.AddSubmenu("File")
	file.AddText("Quit and stop server", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { app.QuitAndStop() })
	file.AddText("Close window (server keeps running)", keys.CmdOrCtrl("w"), func(*menu.CallbackData) {
		wailsRuntime.Quit(app.ctx)
	})
	file.AddSeparator()
	file.AddCheckbox("Start server when I sign in to Windows", autostartEnabled(), nil, func(d *menu.CallbackData) {
		if err := setAutostart(exe, d.MenuItem.Checked); err != nil {
			slog.Warn("cannot change autostart", "err", err)
			d.MenuItem.Checked = !d.MenuItem.Checked
		}
	})

	err := wails.Run(&options.App{
		Title:     "GoClaw",
		Width:     width,
		Height:    height,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		Menu:      appMenu,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Bind:          []interface{}{app},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					wailsRuntime.WindowUnminimise(app.ctx)
					wailsRuntime.WindowShow(app.ctx)
				}
			},
		},
		Windows: &windows.Options{WebviewUserDataPath: p.WebviewDir},
	})
	if err != nil {
		slog.Error("window", "err", err)
		return 1
	}
	return 0
}
