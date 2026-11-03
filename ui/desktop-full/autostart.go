//go:build windows

package main

import (
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "GoClaw"

	flagBackground = "--background"
	flagMinimized  = "--minimized"
)

// hiddenSysProcAttr runs a helper command without a console window.
func hiddenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// autostartCommand is the per-user Run entry: with minimized the window opens
// minimized to the taskbar, otherwise only the server starts (no window).
func autostartCommand(exePath string, minimized bool) string {
	flag := flagBackground
	if minimized {
		flag = flagMinimized
	}
	return `"` + exePath + `" ` + flag
}

// parseAutostart reads a Run entry written by autostartCommand.
func parseAutostart(value string) (enabled, minimized bool) {
	if value == "" {
		return false, false
	}
	return true, strings.HasSuffix(strings.TrimSpace(value), flagMinimized)
}

// autostartState reports this user's Run entry.
func autostartState() (enabled, minimized bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return false, false
	}
	return parseAutostart(v)
}

// setAutostart writes or removes the per-user Run entry.
func setAutostart(exePath string, enabled, minimized bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	return k.SetStringValue(runValueName, autostartCommand(exePath, minimized))
}
