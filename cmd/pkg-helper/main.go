// pkg-helper is a root-privileged helper that listens on a Unix socket
// and executes apk add/del commands on behalf of the non-root app process.
// It is started by docker-entrypoint.sh before dropping privileges.
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
	"sync"
	"syscall"
	"time"
)

const socketPath = "/tmp/pkg.sock"

// goclawGID is the group ID of the goclaw process.
// Persist files and sockets are chowned to root:goclaw so the
// unprivileged app process (uid 1000, gid 1000) can read them.
const goclawGID = 1000

// validPkgName allows alphanumeric, hyphens, underscores, dots, @, / (scoped npm).
// Rejects names starting with - to prevent argument injection.
// Used by install/uninstall for pip/npm cross-runtime compatibility (historical).
var validPkgName = regexp.MustCompile(`^[a-zA-Z0-9@][a-zA-Z0-9._+\-/@]*$`)

// validApkName enforces the stricter Alpine package name grammar applied
// only to the `upgrade` action. install/uninstall keep validPkgName for
// pip/npm cross-runtime compat (historical).
// Valid: curl, libstdc++, gtk+3.0, ca-certificates, py3-pip.
// Invalid: CURL (uppercase), @scope/pkg (@), curl/extra (/), -pkg (leading hyphen).
var validApkName = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*$`)

// apkMutex serializes all apk CLI invocations within the helper process.
// Alpine apk uses a file lock at /var/lib/apk/db.lock; parallel calls would
// return "unable to lock database" with poor UX. Serializing in-process
// avoids the retry loop.
var apkMutex sync.Mutex

type request struct {
	Action  string `json:"action"`
	Package string `json:"package"`
}

type response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Code  string `json:"code,omitempty"`
	Data  string `json:"data,omitempty"`
}

func main() {
	slog.Info("pkg-helper: starting", "socket", socketPath, "protocol", "v2")

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

	// Socket permissions: owner root, group goclaw (gid 1000), mode 0660.
	// Chown requires CAP_CHOWN; if missing (misconfigured container), warn but continue
	// since umask already set restrictive permissions.
	if os.Getuid() == 0 {
		if err := os.Chown(socketPath, 0, goclawGID); err != nil {
			slog.Warn("pkg-helper: chown socket failed (missing CAP_CHOWN?)", "error", err)
		}
	}
	if err := os.Chmod(socketPath, 0660); err != nil {
		slog.Warn("pkg-helper: chmod socket failed", "error", err)
	}

	// Ensure persist directory is writable by root (self-healing for upgrades).
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
				// Safety ceiling: 10-minute deadline to evict dead clients.
				// This is NOT a per-operation timeout — clients set conn.SetDeadline
				// from ctx.Deadline() for that. This ceiling prevents maxConns=3
				// semaphore starvation (DoS) if a client stops reading/writing.
				// Renewed after each successful scanner.Scan() in handleConn.
				c.SetDeadline(time.Now().Add(10 * time.Minute)) //nolint:errcheck
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

	// scanner.Buffer: 64KB initial / 1MB max.
	// 1MB ceiling is a CONTRACT: any action returning >1MB of output must either
	// raise this ceiling (both here and in the client) or split into multiple JSON
	// lines. Violating this silently truncates at scanner boundary → helper_error.
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	encoder := json.NewEncoder(conn)

	for scanner.Scan() {
		// Renew the 10-min safety deadline after each successfully received line.
		// Rationale: a slow-mirror apk upgrade that took 9m59s to complete is
		// legitimate; the next request should get a fresh 10 minutes.
		conn.SetDeadline(time.Now().Add(10 * time.Minute)) //nolint:errcheck

		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			encoder.Encode(response{Error: "invalid json", Code: "validation"}) //nolint:errcheck
			continue
		}

		resp := handleRequest(req)
		encoder.Encode(resp) //nolint:errcheck
	}
}

// validatePkg checks that pkg is non-empty and matches the given regex.
// Returns (true, zero) on success; (false, error response) on failure.
func validatePkg(pkg string, re *regexp.Regexp) (bool, response) {
	if pkg == "" {
		return false, response{Error: "package required", Code: "validation"}
	}
	if !re.MatchString(pkg) {
		return false, response{Error: "invalid package name", Code: "validation"}
	}
	return true, response{}
}

func handleRequest(req request) response {
	switch req.Action {
	case "install":
		ok, errResp := validatePkg(req.Package, validPkgName)
		if !ok {
			return errResp
		}
		return doInstall(req.Package)
	case "uninstall":
		ok, errResp := validatePkg(req.Package, validPkgName)
		if !ok {
			return errResp
		}
		return doUninstall(req.Package)
	case "upgrade":
		// upgrade uses stricter validApkName (no @, no /, lowercase-only).
		ok, errResp := validatePkg(req.Package, validApkName)
		if !ok {
			return errResp
		}
		return doUpgrade(req.Package)
	case "update-index":
		// Read-only action: no package argument expected.
		if req.Package != "" {
			return response{Error: "update-index takes no package", Code: "validation"}
		}
		return doUpdateIndex()
	case "list-outdated":
		// Read-only action: no package argument expected.
		if req.Package != "" {
			return response{Error: "list-outdated takes no package", Code: "validation"}
		}
		return doListOutdated()
	default:
		return response{Error: fmt.Sprintf("unknown action: %s", req.Action), Code: "validation"}
	}
}

func doInstall(pkg string) response {
	apkMutex.Lock()
	defer apkMutex.Unlock()

	slog.Info("pkg-helper: installing", "package", pkg)

	cmd := exec.Command("apk", "add", "--no-cache", pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg, code := classifyApkOutput(string(out), err)
		slog.Error("pkg-helper: install failed", "package", pkg, "error", msg, "code", code)
		return response{Error: msg, Code: code}
	}

	persistAdd(pkg)
	slog.Info("pkg-helper: installed", "package", pkg)
	return response{OK: true}
}

func doUninstall(pkg string) response {
	apkMutex.Lock()
	defer apkMutex.Unlock()

	slog.Info("pkg-helper: uninstalling", "package", pkg)

	cmd := exec.Command("apk", "del", pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg, code := classifyApkOutput(string(out), err)
		slog.Error("pkg-helper: uninstall failed", "package", pkg, "error", msg, "code", code)
		return response{Error: msg, Code: code}
	}

	persistRemove(pkg)
	slog.Info("pkg-helper: uninstalled", "package", pkg)
	return response{OK: true}
}

// doUpgrade runs `apk add -u <pkg>` to upgrade an existing package.
// Intentionally does NOT call persistAdd — upgrade does not change the installed set.
// The apk-packages file tracks what was explicitly installed, not version pinning.
func doUpgrade(pkg string) response {
	apkMutex.Lock()
	defer apkMutex.Unlock()

	slog.Info("pkg-helper: upgrading", "package", pkg)

	cmd := exec.Command("apk", "add", "-u", pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg, code := classifyApkOutput(string(out), err)
		slog.Error("pkg-helper: upgrade failed", "package", pkg, "error", msg, "code", code)
		return response{Error: msg, Code: code}
	}

	slog.Info("pkg-helper: upgraded", "package", pkg)
	return response{OK: true}
}

// doUpdateIndex runs `apk update` to refresh the package index.
func doUpdateIndex() response {
	apkMutex.Lock()
	defer apkMutex.Unlock()

	slog.Info("pkg-helper: updating index")

	cmd := exec.Command("apk", "update")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg, code := classifyApkOutput(string(out), err)
		slog.Warn("pkg-helper: update-index failed", "error", msg, "code", code)
		return response{Error: msg, Code: code}
	}

	slog.Info("pkg-helper: index updated")
	return response{OK: true, Data: string(out)}
}

// doListOutdated runs `apk version -l '<'` to list packages with available upgrades.
// Returns stdout verbatim in the Data field.
func doListOutdated() response {
	apkMutex.Lock()
	defer apkMutex.Unlock()

	cmd := exec.Command("apk", "version", "-l", "<")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg, code := classifyApkOutput(string(out), err)
		return response{Error: msg, Code: code}
	}

	return response{OK: true, Data: string(out)}
}

// classifyApkOutput inspects combined apk output + exit error and returns
// (truncated message, error code). This mirrors gateway-side ClassifyApkStderr
// but returns the code string directly (helper binary is separate from internal/skills).
//
// Code strings (authoritative for pkg-helper protocol):
// "locked", "permission", "disk_full", "not_found", "conflict", "network", "system_error".
//
// Note: "helper_unavailable" and "helper_error" are client-only codes; never emitted here.
func classifyApkOutput(out string, err error) (string, string) {
	msg := strings.TrimSpace(out)
	if msg == "" {
		msg = err.Error()
	}
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(out, "unable to lock"):
		return msg, "locked"
	case strings.Contains(out, "Permission denied"):
		return msg, "permission"
	case strings.Contains(out, "No space left on device"):
		return msg, "disk_full"
	case strings.Contains(out, "unsatisfiable constraints"):
		if strings.Contains(out, "breaks: world") || strings.Contains(out, "required by") {
			return msg, "conflict"
		}
		return msg, "not_found"
	case strings.Contains(out, "breaks: world"):
		return msg, "conflict"
	case strings.Contains(lower, "network") ||
		strings.Contains(out, "unable to fetch") ||
		strings.Contains(out, "connection") ||
		strings.Contains(out, "timed out") ||
		strings.Contains(out, "hostname resolution failed"):
		return msg, "network"
	default:
		return msg, "system_error"
	}
}

// persistAdd appends a package name to the apk persist file (dedup check).
func persistAdd(pkg string) {
	listFile := apkListFile()

	// Check if already persisted (avoid duplicates).
	if data, err := os.ReadFile(listFile); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			if strings.TrimSpace(line) == pkg {
				return // already persisted
			}
		}
	}

	created := false
	if _, err := os.Stat(listFile); os.IsNotExist(err) {
		created = true
	}
	f, err := os.OpenFile(listFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		slog.Warn("pkg-helper: persist add failed", "error", err)
		return
	}
	defer f.Close()
	fmt.Fprintln(f, pkg)
	// Ensure group ownership allows the goclaw process to read the file.
	if created {
		if err := os.Chown(listFile, 0, goclawGID); err != nil {
			slog.Warn("pkg-helper: chown persist file failed", "file", listFile, "error", err)
		}
	}
}

// persistRemove removes a package name from the apk persist file.
// Uses write-to-temp-then-rename for atomic update (avoids truncation on disk-full).
func persistRemove(pkg string) {
	listFile := apkListFile()
	data, err := os.ReadFile(listFile)
	if err != nil {
		return
	}

	var kept []string
	for line := range strings.SplitSeq(string(data), "\n") {
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
		return
	}
	// Restore group ownership so the goclaw process (gid 1000) can read the file.
	// Without this, the renamed file inherits root:root from the temp file,
	// causing ListInstalledPackages to return nil for system packages.
	if err := os.Chown(listFile, 0, goclawGID); err != nil {
		slog.Warn("pkg-helper: chown persist file failed", "file", listFile, "error", err)
	}
}

func apkListFile() string {
	runtimeDir := os.Getenv("RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/app/data/.runtime"
	}
	return runtimeDir + "/apk-packages"
}

// ensurePersistDir ensures the apk persist file's parent directory is writable by root.
// On existing volumes the directory may be goclaw-owned (from older images); fix ownership
// using CAP_CHOWN so pkg-helper can create/write the persist file.
func ensurePersistDir() {
	dir := filepath.Dir(apkListFile())
	fi, err := os.Stat(dir)
	if err != nil {
		// Directory doesn't exist — entrypoint should have created it.
		return
	}
	if !fi.IsDir() {
		return
	}

	// Try to fix ownership to root:goclaw (gid 1000) if not already root-owned.
	// CAP_CHOWN is available even when CAP_DAC_OVERRIDE is dropped.
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok && stat.Uid != 0 {
		if err := os.Chown(dir, 0, goclawGID); err != nil {
			slog.Warn("pkg-helper: cannot fix persist dir ownership", "dir", dir, "error", err)
		} else {
			os.Chmod(dir, 0750) //nolint:errcheck
			slog.Info("pkg-helper: fixed persist dir ownership", "dir", dir)
		}
	}
}
