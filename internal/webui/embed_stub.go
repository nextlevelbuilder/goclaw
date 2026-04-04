//go:build !embedui

// Package webui provides optional embedded web UI serving.
// When built without the embedui tag, no assets are available.
package webui

import "io/fs"

// Assets returns nil when built without the embedui tag.
func Assets() fs.FS { return nil }

// HasAssets returns false when built without the embedui tag.
func HasAssets() bool { return false }
