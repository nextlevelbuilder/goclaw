//go:build windows

package main

import (
	"encoding/json"
	"os"
)

type windowState struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	X      int `json:"x"`
	Y      int `json:"y"`
}

const (
	defaultWidth  = 1280
	defaultHeight = 800
	minWidth      = 900
	minHeight     = 600
)

func saveWindowState(path string, s windowState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func readWindowState(path string) (windowState, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return windowState{}, false
	}
	var s windowState
	if json.Unmarshal(b, &s) != nil {
		return windowState{}, false
	}
	return s, true
}

// windowSize returns the saved size when it is plausible, else the default.
func windowSize(path string) (int, int) {
	s, ok := readWindowState(path)
	if !ok || s.Width < minWidth || s.Height < minHeight || s.Width > 10000 || s.Height > 10000 {
		return defaultWidth, defaultHeight
	}
	return s.Width, s.Height
}

// loadWindowPosition returns a saved position; implausible values (a monitor
// that is gone shows up as a far-off or minimized-window coordinate) are ignored.
func loadWindowPosition(path string) (x, y int, ok bool) {
	s, ok := readWindowState(path)
	if !ok || s.X < -5000 || s.X > 10000 || s.Y < -5000 || s.Y > 10000 || s.X <= -30000 {
		return 0, 0, false
	}
	return s.X, s.Y, true
}
