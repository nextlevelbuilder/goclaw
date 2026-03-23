// pkg-helper is a root-privileged helper that listens on a Unix socket
// and executes apt-get install/remove commands on behalf of the non-root app process.
// It is started by a systemd service before the main goclaw process.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const socketPath = "/tmp/pkg.sock"

// validPkgName allows alphanumeric, hyphens, underscores, dots, @, / (scoped npm).
// Rejects names starting with - to prevent argument injection.
var validPkgName = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9._+\-/@]*$`)

type request struct {
	Action  string `json:"action"`
	Package string `json:"package"`
}

type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func main() {
	slog.Info("pkg-helper: starting", "socket", socketPath)

	// Remove stale socket.
	os.Remove(socketPath)

	// Restrictive umask: socket created as 0660 (not default 0777).
	oldMask := syscall.Umask(0117)
	listener, err := net.Listen("unix", socketPath)
	syscall.Umask(oldMask)
	if err != nil {
		slog.Error("pkg-helper: listen failed", "error", err)
		os.Exit(1)
	}
	defer listener.Close()

	// Socket permissions: owner root, group ubuntu (gid 1000), mode 0660.
	if os.Getuid() == 0 {
		if err := os.Chown(socketPath, 0, 1000); err != nil {
			slog.Warn("pkg-helper: chown socket failed", "error", err)
		}
	}
	if err := os.Chmod(socketPath, 0660); err != nil {
		slog.Warn("pkg-helper: chmod socket failed", "error", err)
	}

	// Ensure persist directory exists.
	ensurePersistDir()

	// Graceful shutdown on SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		slog.Info("pkg-helper: shutting down")
		listener.Close()
		os.Remove(socketPath)
		os.Exit(0)
	}()

	const maxConns = 3
	sem := make(chan struct{}, maxConns)

	slog.Info("pkg-helper: ready")

	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		select {
		case sem <- struct{}{}:
			go func(c net.Conn) {
				defer func() { <-sem }()
				c.SetDeadline(time.Now().Add(30 * time.Second)) //nolint:errcheck
				handleConn(c)
			}(conn)
		default:
			slog.Warn("pkg-helper: connection limit reached, rejecting")
			conn.Close()
		}
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			encoder.Encode(response{Error: "invalid json"}) //nolint:errcheck
			continue
		}

		resp := handleRequest(req)
		encoder.Encode(resp) //nolint:errcheck
	}
}

func handleRequest(req request) response {
	pkg := req.Package
	if pkg == "" {
		return response{Error: "package required"}
	}
	if !validPkgName.MatchString(pkg) {
		return response{Error: "invalid package name"}
	}

	switch req.Action {
	case "install":
		return doInstall(pkg)
	case "uninstall":
		return doUninstall(pkg)
	default:
		return response{Error: fmt.Sprintf("unknown action: %s", req.Action)}
	}
}

func doInstall(pkg string) response {
	slog.Info("pkg-helper: installing", "package", pkg)

	cmd := exec.Command("apt-get", "install", "-y", "--no-install-recommends", pkg)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
		slog.Error("pkg-helper: install failed", "package", pkg, "error", msg)
		return response{Error: msg}
	}

	persistAdd(pkg)
	slog.Info("pkg-helper: installed", "package", pkg)
	return response{OK: true}
}

func doUninstall(pkg string) response {
	slog.Info("pkg-helper: uninstalling", "package", pkg)

	cmd := exec.Command("apt-get", "remove", "-y", pkg)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
		slog.Error("pkg-helper: uninstall failed", "package", pkg, "error", msg)
		return response{Error: msg}
	}

	persistRemove(pkg)
	slog.Info("pkg-helper: uninstalled", "package", pkg)
	return response{OK: true}
}

// persistAdd appends a package name to the persist file (dedup check).
func persistAdd(pkg string) {
	listFile := pkgListFile()

	if data, err := os.ReadFile(listFile); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == pkg {
				return
			}
		}
	}

	f, err := os.OpenFile(listFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		slog.Warn("pkg-helper: persist add failed", "error", err)
		return
	}
	defer f.Close()
	fmt.Fprintln(f, pkg)
}

// persistRemove removes a package name from the persist file.
func persistRemove(pkg string) {
	listFile := pkgListFile()
	data, err := os.ReadFile(listFile)
	if err != nil {
		return
	}

	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != pkg {
			kept = append(kept, line)
		}
	}

	tmpFile := listFile + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(strings.Join(kept, "\n")+"\n"), 0640); err != nil {
		slog.Warn("pkg-helper: persist remove write failed", "error", err)
		return
	}
	if err := os.Rename(tmpFile, listFile); err != nil {
		slog.Warn("pkg-helper: persist remove rename failed", "error", err)
		os.Remove(tmpFile) //nolint:errcheck
	}
}

func pkgListFile() string {
	runtimeDir := os.Getenv("RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/var/lib/goclaw/.runtime"
	}
	return runtimeDir + "/apt-packages"
}

func ensurePersistDir() {
	dir := filepath.Dir(pkgListFile())
	if err := os.MkdirAll(dir, 0750); err != nil {
		slog.Warn("pkg-helper: cannot create persist dir", "dir", dir, "error", err)
	}
}
