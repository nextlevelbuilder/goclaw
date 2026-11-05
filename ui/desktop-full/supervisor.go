//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type serverConfig struct {
	Exe        string   // goclaw-server.exe
	Dir        string   // working directory of the server
	Env        []string // full child environment (KEY=VALUE)
	Port       int
	LogPath    string // the server's own log (GOCLAW_LOG_FILE)
	StderrPath string // panics and early errors
	PidPath    string
	// StartTimeout bounds how long Ensure waits for /health.
	StartTimeout time.Duration
}

type supervisor struct {
	cfg    serverConfig
	client *http.Client
}

func newSupervisor(cfg serverConfig) *supervisor {
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 60 * time.Second
	}
	return &supervisor{cfg: cfg, client: &http.Client{Timeout: 3 * time.Second}}
}

func (s *supervisor) baseURL() string { return "http://127.0.0.1:" + strconv.Itoa(s.cfg.Port) }

// healthy reports whether a GoClaw gateway answers on the port. Another program
// that happens to hold the port must not count, so the body is checked too.
func (s *supervisor) healthy() bool {
	resp, err := s.client.Get(s.baseURL() + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return strings.Contains(string(body), `"status":"ok"`)
}

func (s *supervisor) readPid() uint32 {
	b, err := os.ReadFile(s.cfg.PidPath)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

// ownedPid returns the recorded server pid when that process is still our server.
func (s *supervisor) ownedPid() uint32 {
	pid := s.readPid()
	if pid != 0 && sameImage(processImage(pid), s.cfg.Exe) {
		return pid
	}
	return 0
}

// errPortHeld means something other than our server uses the port.
type errPortHeld struct {
	Port  int
	PID   uint32
	Image string
}

func (e *errPortHeld) Error() string {
	if e.PID == 0 {
		return fmt.Sprintf("port %d is in use by another program", e.Port)
	}
	return fmt.Sprintf("port %d is in use by %s (pid %d)", e.Port, filepath.Base(e.Image), e.PID)
}

// errStartFailed means the server did not become healthy.
type errStartFailed struct {
	Reason  string
	LogTail string
}

func (e *errStartFailed) Error() string { return e.Reason }

func (s *supervisor) portInUse() bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(s.cfg.Port), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func (s *supervisor) holder() *errPortHeld {
	pid := portOwner(s.cfg.Port)
	return &errPortHeld{Port: s.cfg.Port, PID: pid, Image: processImage(pid)}
}

// Ensure makes sure our server answers on the port. reused is true when a
// server we already own was running. The server outlives the caller.
func (s *supervisor) Ensure(ctx context.Context) (reused bool, err error) {
	if pid := s.ownedPid(); pid != 0 {
		if s.healthy() {
			return true, nil
		}
		return true, s.waitHealthy(ctx, pid, nil)
	}
	if s.healthy() {
		// A gateway without a pid record: adopt it only when it is our binary.
		h := s.holder()
		if h.PID != 0 && sameImage(h.Image, s.cfg.Exe) {
			_ = os.WriteFile(s.cfg.PidPath, []byte(strconv.FormatUint(uint64(h.PID), 10)), 0o600)
			return true, nil
		}
		return false, h
	}
	if s.portInUse() {
		return false, s.holder()
	}
	return false, s.spawn(ctx)
}

func (s *supervisor) spawn(ctx context.Context) error {
	for _, p := range []string{s.cfg.LogPath, s.cfg.StderrPath, s.cfg.PidPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
	}
	stderr, err := os.OpenFile(s.cfg.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	nul, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer nul.Close()

	cmd := exec.Command(s.cfg.Exe)
	cmd.Dir = s.cfg.Dir
	cmd.Env = s.cfg.Env
	cmd.Stdout = nul // the server also writes GOCLAW_LOG_FILE
	cmd.Stderr = stderr
	cmd.SysProcAttr = detachedSysProcAttr()
	if err := cmd.Start(); err != nil {
		return &errStartFailed{Reason: "cannot start server: " + err.Error()}
	}
	pid := uint32(cmd.Process.Pid)
	if err := os.WriteFile(s.cfg.PidPath, []byte(strconv.FormatUint(uint64(pid), 10)), 0o600); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	return s.waitHealthy(ctx, pid, exited)
}

func (s *supervisor) waitHealthy(ctx context.Context, pid uint32, exited <-chan struct{}) error {
	deadline := time.NewTimer(s.cfg.StartTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if s.healthy() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			_ = os.Remove(s.cfg.PidPath)
			return &errStartFailed{Reason: "server exited during startup", LogTail: s.logTail()}
		case <-deadline.C:
			return &errStartFailed{Reason: "server did not become healthy in time", LogTail: s.logTail()}
		case <-tick.C:
			if exited == nil && processImage(pid) == "" {
				_ = os.Remove(s.cfg.PidPath)
				return &errStartFailed{Reason: "server stopped during startup", LogTail: s.logTail()}
			}
		}
	}
}

// logTail returns the last few KB of the server log (and stderr log) for the
// error page. The server never prints secrets at these levels, but values are
// still not echoed from the environment by this code.
func (s *supervisor) logTail() string {
	tail := func(path string) string {
		f, err := os.Open(path)
		if err != nil {
			return ""
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return ""
		}
		const n = 6 * 1024
		off := st.Size() - n
		if off < 0 {
			off = 0
		}
		buf := make([]byte, st.Size()-off)
		if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
			return ""
		}
		return string(buf)
	}
	out := tail(s.cfg.LogPath)
	if e := tail(s.cfg.StderrPath); strings.TrimSpace(e) != "" {
		out += "\n--- stderr ---\n" + e
	}
	return out
}

// Stop asks the server to shut down gracefully and hard-kills it only after
// timeout. graceful reports which path was taken. A server we do not own is left
// alone.
func (s *supervisor) Stop(timeout time.Duration) (graceful bool, err error) {
	pid := s.ownedPid()
	if pid == 0 {
		_ = os.Remove(s.cfg.PidPath)
		return true, nil
	}
	defer os.Remove(s.cfg.PidPath)
	if err := sendCtrlBreak(pid); err == nil && waitExit(pid, timeout) {
		return true, nil
	}
	if waitExit(pid, 0) {
		return true, nil
	}
	if err := killProcess(pid); err != nil {
		return false, err
	}
	waitExit(pid, 5*time.Second)
	return false, nil
}
