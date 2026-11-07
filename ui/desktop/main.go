//go:build sqliteonly

package main

import (
	"embed"
	"log/slog"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "GoClaw",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 640,
		// The frontend draws its own title bar (drag region + window controls).
		Frameless: true,
		// Dark surface-window token (oklch 0.14 0.012 45) so no white flash before first paint.
		BackgroundColour: &options.RGBA{R: 13, G: 8, B: 6, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
	})
	if err != nil {
		slog.Error("desktop app error", "error", err)
	}
}
