//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
	ctrlBreakEvent        = 1
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	procFreeCons   = kernel32.NewProc("FreeConsole")
	procAttachCons = kernel32.NewProc("AttachConsole")
	procSetCtrl    = kernel32.NewProc("SetConsoleCtrlHandler")
	procGenCtrl    = kernel32.NewProc("GenerateConsoleCtrlEvent")

	// Console attachment is process-wide, so ctrl events are sent one at a time.
	consoleMu sync.Mutex
)

// processImage returns the full image path of pid, or "" when it is not running
// or not accessible.
func processImage(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH*4)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func sameImage(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Base(a), filepath.Base(b))
}

// sendCtrlBreak delivers a graceful-stop signal (seen by Go as os.Interrupt) to
// a console process started with CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW.
func sendCtrlBreak(pid uint32) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	procFreeCons.Call()
	defer procFreeCons.Call()
	if r, _, err := procAttachCons.Call(uintptr(pid)); r == 0 {
		return fmt.Errorf("attach console of pid %d: %w", pid, err)
	}
	procSetCtrl.Call(0, 1) // ignore the event in this process
	defer procSetCtrl.Call(0, 0)
	if r, _, err := procGenCtrl.Call(ctrlBreakEvent, uintptr(pid)); r == 0 {
		return fmt.Errorf("send ctrl-break to pid %d: %w", pid, err)
	}
	return nil
}

// waitExit waits for pid to exit; it reports false on timeout.
func waitExit(pid uint32, timeout time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return true // already gone
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
	return ev == windows.WAIT_OBJECT_0
}

func killProcess(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// detachedSysProcAttr starts a child in its own process group with a hidden
// console, which is what makes ctrl-break delivery possible later.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | createNoWindow}
}

// portOwner returns the pid listening on 127.0.0.1:port or the wildcard
// address, 0 when nobody does.
func portOwner(port int) uint32 {
	cmd := exec.Command("netstat", "-ano", "-p", "TCP")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	suffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.EqualFold(f[3], "LISTENING") || !strings.HasSuffix(f[1], suffix) {
			continue
		}
		if local := strings.TrimSuffix(f[1], suffix); local != "127.0.0.1" && local != "0.0.0.0" && local != "[::]" && local != "[::1]" {
			continue
		}
		if pid, err := strconv.ParseUint(f[4], 10, 32); err == nil {
			return uint32(pid)
		}
	}
	return 0
}
