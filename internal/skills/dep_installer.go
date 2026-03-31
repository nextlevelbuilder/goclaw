package skills

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const installTimeout = 5 * time.Minute

// pkgHelperSocket is the Unix socket path for the root-privileged pkg-helper.
const pkgHelperSocket = "/tmp/pkg.sock"

// validPkgName matches safe package names: alphanumeric start, then alphanumeric/dot/hyphen/underscore.
var validPkgName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// stdlibBlocklist contains pip package names that shadow Python stdlib modules.
// Installing these via pip can silently hijack imports used by other skills.
var stdlibBlocklist = map[string]bool{
	"pathlib": true, "os": true, "sys": true, "json": true,
	"collections": true, "re": true, "io": true, "time": true,
	"datetime": true, "subprocess": true, "socket": true,
}

// validatePackageName checks that a package spec (possibly with version constraint)
// has a safe name and is not in the stdlib blocklist. Returns the original raw string
// on success so it can be passed to pip/npm unchanged.
func validatePackageName(raw string) (string, error) {
	// Strip version specifiers to validate the name portion only.
	name := raw
	for _, sep := range []string{">=", "<=", "==", "~=", "!=", "<", ">"} {
		if idx := strings.Index(name, sep); idx > 0 {
			name = name[:idx]
			break
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty package name")
	}
	if !validPkgName.MatchString(name) {
		return "", fmt.Errorf("invalid package name: %q", raw)
	}
	if stdlibBlocklist[strings.ToLower(name)] {
		return "", fmt.Errorf("blocked: %q shadows Python stdlib", name)
	}
	return raw, nil
}

// InstallResult holds per-category install outcomes.
type InstallResult struct {
	System []string `json:"system,omitempty"`
	Pip    []string `json:"pip,omitempty"`
	Npm    []string `json:"npm,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// AggregateMissingDeps scans all provided skill directories, merges their manifests,
// then checks which dependencies are missing.
// skillDirs is map[slug]->dir.
func AggregateMissingDeps(skillDirs map[string]string) (*SkillManifest, []string) {
	var merged *SkillManifest
	for _, dir := range skillDirs {
		m := ScanSkillDeps(dir)
		if m != nil {
			merged = MergeDeps(merged, m)
		}
	}
	if merged == nil || merged.IsEmpty() {
		return nil, nil
	}
	_, missing := CheckSkillDeps(merged)
	return merged, missing
}

// InstallSingleDep installs one dependency (format: "pip:pkg", "npm:pkg", or plain binary name).
// Returns (ok, errorMessage). Logs progress via slog so the Log page can show install status.
func InstallSingleDep(ctx context.Context, dep string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	slog.Info("skills: installing dep", "dep", dep)

	switch {
	case strings.HasPrefix(dep, "pip:"):
		pkg := strings.TrimPrefix(dep, "pip:")
		if _, err := validatePackageName(pkg); err != nil {
			slog.Warn("security.dep_install: rejected package", "dep", dep, "error", err)
			return false, err.Error()
		}
		cmd := exec.CommandContext(ctx, "pip3", "install", "--no-cache-dir", "--break-system-packages", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: dep install failed", "dep", dep, "error", msg)
			return false, msg
		}
	case strings.HasPrefix(dep, "npm:"):
		pkg := strings.TrimPrefix(dep, "npm:")
		if _, err := validatePackageName(pkg); err != nil {
			slog.Warn("security.dep_install: rejected package", "dep", dep, "error", err)
			return false, err.Error()
		}
		cmd := exec.CommandContext(ctx, "npm", "install", "-g", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: dep install failed", "dep", dep, "error", msg)
			return false, msg
		}
	default:
		// System package via pkg-helper — validate name before sending.
		if _, err := validatePackageName(dep); err != nil {
			slog.Warn("security.dep_install: rejected package", "dep", dep, "error", err)
			return false, err.Error()
		}
		ok, errMsg := apkViaHelper(ctx, "install", dep)
		if !ok {
			return false, errMsg
		}
	}

	slog.Info("skills: dep installed", "dep", dep)
	cleanCaches(ctx)
	return true, ""
}

// InstallDeps installs missing packages by category.
// Uses PIP_TARGET and NPM_CONFIG_PREFIX from env (set by docker-entrypoint.sh).
func InstallDeps(ctx context.Context, manifest *SkillManifest, missing []string) (*InstallResult, error) {
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	result := &InstallResult{}

	var sysPkgs, pipPkgs, npmPkgs []string
	for _, dep := range missing {
		switch {
		case strings.HasPrefix(dep, "pip:"):
			pkg := strings.TrimPrefix(dep, "pip:")
			if _, err := validatePackageName(pkg); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("pip %s: %s", pkg, err))
				continue
			}
			pipPkgs = append(pipPkgs, pkg)
		case strings.HasPrefix(dep, "npm:"):
			pkg := strings.TrimPrefix(dep, "npm:")
			if _, err := validatePackageName(pkg); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("npm %s: %s", pkg, err))
				continue
			}
			npmPkgs = append(npmPkgs, pkg)
		default:
			if _, err := validatePackageName(dep); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("apk %s: %s", dep, err))
				continue
			}
			sysPkgs = append(sysPkgs, dep)
		}
	}

	// System packages: install one by one via pkg-helper.
	if len(sysPkgs) > 0 {
		slog.Info("skills: installing system packages", "pkgs", sysPkgs)
		var successful []string
		for _, pkg := range sysPkgs {
			ok, errMsg := apkViaHelper(ctx, "install", pkg)
			if !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("apk %s: %s", pkg, errMsg))
			} else {
				successful = append(successful, pkg)
			}
		}
		result.System = successful
	}

	if len(pipPkgs) > 0 {
		slog.Info("skills: installing pip packages", "pkgs", pipPkgs)
		args := append([]string{"install", "--no-cache-dir", "--break-system-packages"}, pipPkgs...)
		cmd := exec.CommandContext(ctx, "pip3", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("pip: %s (%v)", strings.TrimSpace(string(out)), err))
		} else {
			result.Pip = pipPkgs
		}
	}

	if len(npmPkgs) > 0 {
		slog.Info("skills: installing npm packages", "pkgs", npmPkgs)
		args := append([]string{"install", "-g"}, npmPkgs...)
		cmd := exec.CommandContext(ctx, "npm", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("npm: %s (%v)", strings.TrimSpace(string(out)), err))
		} else {
			result.Npm = npmPkgs
		}
	}

	cleanCaches(ctx)
	return result, nil
}

// UninstallPackage removes one package (format: "pip:pkg", "npm:pkg", or plain apk name).
// Returns (ok, errorMessage).
func UninstallPackage(ctx context.Context, dep string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	slog.Info("skills: uninstalling package", "dep", dep)

	switch {
	case strings.HasPrefix(dep, "pip:"):
		pkg := strings.TrimPrefix(dep, "pip:")
		if _, err := validatePackageName(pkg); err != nil {
			return false, err.Error()
		}
		cmd := exec.CommandContext(ctx, "pip3", "uninstall", "-y", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: uninstall failed", "dep", dep, "error", msg)
			return false, msg
		}
	case strings.HasPrefix(dep, "npm:"):
		pkg := strings.TrimPrefix(dep, "npm:")
		if _, err := validatePackageName(pkg); err != nil {
			return false, err.Error()
		}
		cmd := exec.CommandContext(ctx, "npm", "uninstall", "-g", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: uninstall failed", "dep", dep, "error", msg)
			return false, msg
		}
	default:
		if _, err := validatePackageName(dep); err != nil {
			return false, err.Error()
		}
		ok, errMsg := apkViaHelper(ctx, "uninstall", dep)
		if !ok {
			return false, errMsg
		}
	}

	slog.Info("skills: package uninstalled", "dep", dep)
	return true, ""
}

// apkViaHelper sends an install/uninstall request to the root-privileged pkg-helper
// via Unix socket. The helper runs apk add/del as root and manages the persist file.
func apkViaHelper(ctx context.Context, action, pkg string) (bool, string) {
	conn, err := net.DialTimeout("unix", pkgHelperSocket, 5*time.Second)
	if err != nil {
		return false, fmt.Sprintf("pkg-helper unavailable: %v", err)
	}
	defer conn.Close()

	// Set deadline from context.
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline) //nolint:errcheck
	}

	// Send request as JSON line.
	req := map[string]string{"action": action, "package": pkg}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return false, fmt.Sprintf("pkg-helper send failed: %v", err)
	}

	// Read response.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return false, "pkg-helper: no response"
	}

	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return false, fmt.Sprintf("pkg-helper: invalid response: %v", err)
	}

	return resp.OK, resp.Error
}

// cleanCaches removes pip and npm caches to save disk space.
func cleanCaches(ctx context.Context) {
	exec.CommandContext(ctx, "pip3", "cache", "purge").Run()          //nolint:errcheck
	exec.CommandContext(ctx, "sh", "-c", "rm -rf /tmp/npm-*").Run()  //nolint:errcheck
}
