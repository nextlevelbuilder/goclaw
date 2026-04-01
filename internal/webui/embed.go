//go:build embedui

package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// Assets returns the embedded web UI filesystem rooted at "dist/".
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil
	}
	return sub
}

// HasAssets returns true when built with the embedui tag.
func HasAssets() bool { return true }
