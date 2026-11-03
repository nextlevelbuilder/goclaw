//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake gateway: when GO_HELPER_MODE is set it
// serves /health and records how it was stopped instead of running tests.
func TestMain(m *testing.M) {
	switch os.Getenv("GO_HELPER_MODE") {
	case "server":
		runHelperServer()
		return
	case "exit":
		fmt.Fprintln(os.Stderr, "boom: helper refused to start")
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func runHelperServer() {
	ln, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("GO_HELPER_PORT"))
	if err != nil {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"ok","protocol":3}`)
	})
	go http.Serve(ln, mux)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	_ = os.WriteFile(os.Getenv("GO_HELPER_MARK"), []byte("graceful"), 0o600)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func helperConfig(t *testing.T, mode string) serverConfig {
	t.Helper()
	dir := t.TempDir()
	port := freePort(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return serverConfig{
		Exe:  exe,
		Dir:  dir,
		Port: port,
		Env: append(os.Environ(),
			"GO_HELPER_MODE="+mode,
			"GO_HELPER_PORT="+strconv.Itoa(port),
			"GO_HELPER_MARK="+filepath.Join(dir, "mark"),
		),
		LogPath:      filepath.Join(dir, "server.log"),
		StderrPath:   filepath.Join(dir, "stderr.log"),
		PidPath:      filepath.Join(dir, "server.pid"),
		StartTimeout: 20 * time.Second,
	}
}

func TestSupervisorSpawnsReusesAndStopsGracefully(t *testing.T) {
	cfg := helperConfig(t, "server")
	s := newSupervisor(cfg)
	ctx := context.Background()

	reused, err := s.Ensure(ctx)
	if err != nil || reused {
		t.Fatalf("first Ensure: reused=%v err=%v", reused, err)
	}
	pid := s.ownedPid()
	if pid == 0 {
		t.Fatal("pid file does not point at the spawned server")
	}

	reused, err = newSupervisor(cfg).Ensure(ctx)
	if err != nil || !reused {
		t.Fatalf("second Ensure from a fresh supervisor: reused=%v err=%v", reused, err)
	}
	if got := s.ownedPid(); got != pid {
		t.Fatalf("reuse changed the pid: %d -> %d", pid, got)
	}

	graceful, err := s.Stop(15 * time.Second)
	if err != nil || !graceful {
		t.Fatalf("Stop: graceful=%v err=%v", graceful, err)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg.Dir, "mark")); string(b) != "graceful" {
		t.Fatalf("server did not run its shutdown path, mark=%q", b)
	}
	if _, err := os.Stat(cfg.PidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file left behind: %v", err)
	}
	if s.portInUse() {
		t.Fatal("port still in use after Stop")
	}
}

func TestSupervisorRefusesForeignHolder(t *testing.T) {
	cfg := helperConfig(t, "server")
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(cfg.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.NotFoundHandler()) // answers like another program would

	_, err = newSupervisor(cfg).Ensure(context.Background())
	var held *errPortHeld
	if !errors.As(err, &held) {
		t.Fatalf("Ensure error = %v, want errPortHeld", err)
	}
	if held.PID != uint32(os.Getpid()) {
		t.Fatalf("holder pid = %d, want %d", held.PID, os.Getpid())
	}
	if _, err := os.Stat(cfg.PidPath); !os.IsNotExist(err) {
		t.Fatalf("a foreign holder must not get a pid file: %v", err)
	}
}

func TestSupervisorReportsStartFailureWithLogTail(t *testing.T) {
	cfg := helperConfig(t, "exit")
	_, err := newSupervisor(cfg).Ensure(context.Background())
	var failed *errStartFailed
	if !errors.As(err, &failed) {
		t.Fatalf("Ensure error = %v, want errStartFailed", err)
	}
	if !strings.Contains(failed.LogTail, "boom") {
		t.Fatalf("log tail missing stderr output: %q", failed.LogTail)
	}
	if _, err := os.Stat(cfg.PidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file left behind after failed start: %v", err)
	}
}

func TestSupervisorStopIgnoresUnownedPid(t *testing.T) {
	cfg := helperConfig(t, "server")
	// A pid file that points at this test process, which is not the server image
	// in the sense of cfg.Exe? It is the same binary, so use a different exe name.
	cfg.Exe = filepath.Join(cfg.Dir, "some-other-server.exe")
	if err := os.WriteFile(cfg.PidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	graceful, err := newSupervisor(cfg).Stop(time.Second)
	if err != nil || !graceful {
		t.Fatalf("Stop on an unowned pid: graceful=%v err=%v", graceful, err)
	}
	if processImage(uint32(os.Getpid())) == "" {
		t.Fatal("test process disappeared")
	}
}
