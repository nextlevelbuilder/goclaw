//go:build windows

package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procEnumWindows   = user32.NewProc("EnumWindows")
	procGetWndPid     = user32.NewProc("GetWindowThreadProcessId")
	procIsWndVisible  = user32.NewProc("IsWindowVisible")
	procLoadImage     = user32.NewProc("LoadImageW")
	procSendMessage   = user32.NewProc("SendMessageW")
	procGetSystemMetr = user32.NewProc("GetSystemMetrics")
)

const (
	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	smCxIcon       = 11
	smCxSmIcon     = 49
)

// applyWindowIcon sets the title-bar and taskbar icon of this process's main
// window from an .ico file. The executable carries no icon resource, so the
// shell loads goclaw.ico (installed next to it) at runtime. Failure is not
// fatal: the window just keeps the default icon.
func applyWindowIcon(icoPath string) {
	if _, err := os.Stat(icoPath); err != nil {
		return
	}
	path, err := syscall.UTF16PtrFromString(icoPath)
	if err != nil {
		return
	}
	load := func(size uintptr) uintptr {
		h, _, _ := procLoadImage.Call(0, uintptr(unsafe.Pointer(path)), imageIcon, size, size, lrLoadFromFile)
		return h
	}
	big, small := metric(smCxIcon, 32), metric(smCxSmIcon, 16)
	hBig, hSmall := load(big), load(small)
	if hBig == 0 && hSmall == 0 {
		return
	}
	pid := uint32(os.Getpid())
	for i := 0; i < 50; i++ { // the window appears shortly after startup
		if hwnd := findOwnWindow(pid); hwnd != 0 {
			if hBig != 0 {
				procSendMessage.Call(hwnd, wmSetIcon, iconBig, hBig)
			}
			if hSmall != 0 {
				procSendMessage.Call(hwnd, wmSetIcon, iconSmall, hSmall)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func metric(index, fallback uintptr) uintptr {
	if v, _, _ := procGetSystemMetr.Call(index); v != 0 {
		return v
	}
	return fallback
}

// findOwnWindow returns the first visible top-level window of pid, or 0.
func findOwnWindow(pid uint32) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wp uint32
		procGetWndPid.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
		if wp != pid {
			return 1
		}
		if vis, _, _ := procIsWndVisible.Call(hwnd); vis == 0 {
			return 1
		}
		found = hwnd
		return 0 // stop
	})
	procEnumWindows.Call(cb, 0)
	return found
}
