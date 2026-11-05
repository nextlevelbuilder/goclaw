//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
)

// appPaths holds everything the app writes. The install directory is read-only
// at runtime, so all state lives under the current user's profile.
type appPaths struct {
	ConfigDir  string // %APPDATA%\GoClaw
	EnvFile    string
	ConfigFile string
	WindowFile string
	StateDir   string // %LOCALAPPDATA%\GoClaw
	LogDir     string
	ServerLog  string
	StderrLog  string
	ShellLog   string
	PidFile    string
	WebviewDir string
	BackupDir  string
}

func resolvePaths() (appPaths, error) {
	roaming, local := os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")
	if roaming == "" || local == "" {
		return appPaths{}, errors.New("APPDATA/LOCALAPPDATA are not set")
	}
	cfg, state := filepath.Join(roaming, "GoClaw"), filepath.Join(local, "GoClaw")
	logs := filepath.Join(state, "logs")
	return appPaths{
		ConfigDir:  cfg,
		EnvFile:    filepath.Join(cfg, "goclaw.env"),
		ConfigFile: filepath.Join(cfg, "config.json"),
		WindowFile: filepath.Join(cfg, "window.json"),
		StateDir:   state,
		LogDir:     logs,
		ServerLog:  filepath.Join(logs, "server.log"),
		StderrLog:  filepath.Join(logs, "server-stderr.log"),
		ShellLog:   filepath.Join(logs, "shell.log"),
		PidFile:    filepath.Join(state, "server.pid"),
		WebviewDir: filepath.Join(state, "webview"),
		BackupDir:  filepath.Join(state, "backups"),
	}, nil
}

func (p appPaths) ensure() error {
	for _, d := range []string{p.ConfigDir, p.StateDir, p.LogDir, p.WebviewDir, p.BackupDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
