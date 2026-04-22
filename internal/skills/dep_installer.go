package skills

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"
)

// InstallTimeout is the wall-clock cap applied to a single package install.
// Exported so HTTP handlers that bypass InstallSingleDep (e.g. the github:
// fast path) can wrap their context with the same deadline.
const InstallTimeout = 5 * time.Minute

// pkgHelperSocket is the Unix socket path for the root-privileged pkg-helper.
const pkgHelperSocket = "/tmp/pkg.sock"

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
	ctx, cancel := context.WithTimeout(ctx, InstallTimeout)
	defer cancel()

	slog.Info("skills: installing dep", "dep", dep)

	switch {
	case strings.HasPrefix(dep, "github:"):
		gh := DefaultGitHubInstaller()
		if gh == nil {
			return false, "github installer not configured"
		}
		if _, err := gh.Install(ctx, dep); err != nil {
			slog.Error("skills: github install failed", "dep", dep, "error", err)
			return false, err.Error()
		}
		slog.Info("skills: dep installed", "dep", dep)
		return true, ""
	case strings.HasPrefix(dep, "pip:"):
		pkg := strings.TrimPrefix(dep, "pip:")
		cmd := exec.CommandContext(ctx, "pip3", "install", "--no-cache-dir", "--break-system-packages", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: dep install failed", "dep", dep, "error", msg)
			return false, msg
		}
	case strings.HasPrefix(dep, "npm:"):
		pkg := strings.TrimPrefix(dep, "npm:")
		cmd := exec.CommandContext(ctx, "npm", "install", "-g", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: dep install failed", "dep", dep, "error", msg)
			return false, msg
		}
	default:
		// System package via pkg-helper (root-privileged Unix socket).
		// pkg-helper handles persist to apk-packages file.
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
	ctx, cancel := context.WithTimeout(ctx, InstallTimeout)
	defer cancel()

	result := &InstallResult{}

	var sysPkgs, pipPkgs, npmPkgs []string
	for _, dep := range missing {
		switch {
		case strings.HasPrefix(dep, "pip:"):
			pipPkgs = append(pipPkgs, strings.TrimPrefix(dep, "pip:"))
		case strings.HasPrefix(dep, "npm:"):
			npmPkgs = append(npmPkgs, strings.TrimPrefix(dep, "npm:"))
		default:
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

	// Pip packages: install one by one for partial-success resilience.
	if len(pipPkgs) > 0 {
		slog.Info("skills: installing pip packages", "pkgs", pipPkgs)
		var successful []string
		for _, pkg := range pipPkgs {
			cmd := exec.CommandContext(ctx, "pip3", "install", "--no-cache-dir", "--break-system-packages", pkg)
			if out, err := cmd.CombinedOutput(); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("pip %s: %s (%v)", pkg, strings.TrimSpace(string(out)), err))
			} else {
				successful = append(successful, pkg)
			}
		}
		result.Pip = successful
	}

	// Npm packages: install one by one for partial-success resilience.
	if len(npmPkgs) > 0 {
		slog.Info("skills: installing npm packages", "pkgs", npmPkgs)
		var successful []string
		for _, pkg := range npmPkgs {
			cmd := exec.CommandContext(ctx, "npm", "install", "-g", pkg)
			if out, err := cmd.CombinedOutput(); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("npm %s: %s (%v)", pkg, strings.TrimSpace(string(out)), err))
			} else {
				successful = append(successful, pkg)
			}
		}
		result.Npm = successful
	}

	cleanCaches(ctx)
	return result, nil
}

// UninstallPackage removes one package (format: "pip:pkg", "npm:pkg", or plain apk name).
// Returns (ok, errorMessage).
func UninstallPackage(ctx context.Context, dep string) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, InstallTimeout)
	defer cancel()

	slog.Info("skills: uninstalling package", "dep", dep)

	switch {
	case strings.HasPrefix(dep, "github:"):
		gh := DefaultGitHubInstaller()
		if gh == nil {
			return false, "github installer not configured"
		}
		// Accept either "github:name" (manifest name only) or the full
		// "github:owner/repo[@tag]". For the full form we look up the manifest
		// entry by owner/repo so packages whose binary name differs from the
		// repo name (e.g. cli/cli → gh) can still be uninstalled via spec.
		name := strings.TrimPrefix(dep, "github:")
		if spec, err := ParseGitHubSpec(dep); err == nil {
			name = spec.Repo
			if entries, lerr := gh.List(); lerr == nil {
				want := spec.Owner + "/" + spec.Repo
				for _, e := range entries {
					if strings.EqualFold(e.Repo, want) {
						name = e.Name
						break
					}
				}
			}
		} else if slash := strings.Index(name, "/"); slash >= 0 {
			// Tolerate bare "owner/repo" without the scheme prefix.
			name = name[slash+1:]
			if at := strings.IndexByte(name, '@'); at >= 0 {
				name = name[:at]
			}
		}
		if err := gh.Uninstall(ctx, name); err != nil {
			slog.Error("skills: github uninstall failed", "dep", dep, "error", err)
			return false, err.Error()
		}
		slog.Info("skills: package uninstalled", "dep", dep)
		return true, ""
	case strings.HasPrefix(dep, "pip:"):
		pkg := strings.TrimPrefix(dep, "pip:")
		cmd := exec.CommandContext(ctx, "pip3", "uninstall", "-y", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: uninstall failed", "dep", dep, "error", msg)
			return false, msg
		}
	case strings.HasPrefix(dep, "npm:"):
		pkg := strings.TrimPrefix(dep, "npm:")
		cmd := exec.CommandContext(ctx, "npm", "uninstall", "-g", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
			slog.Error("skills: uninstall failed", "dep", dep, "error", msg)
			return false, msg
		}
	default:
		// System package via pkg-helper. Helper handles persist file removal.
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
