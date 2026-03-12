package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const installTimeout = 5 * time.Minute

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

	var cmd *exec.Cmd
	switch {
	case strings.HasPrefix(dep, "pip:"):
		pkg := strings.TrimPrefix(dep, "pip:")
		cmd = exec.CommandContext(ctx, "pip3", "install", "--no-cache-dir", "--break-system-packages", pkg)
	case strings.HasPrefix(dep, "npm:"):
		pkg := strings.TrimPrefix(dep, "npm:")
		cmd = exec.CommandContext(ctx, "npm", "install", "-g", pkg)
	default:
		// System binary via apk
		cmd = exec.CommandContext(ctx, "doas", "apk", "add", "--no-cache", dep)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("%s: %v", strings.TrimSpace(string(out)), err)
		slog.Error("skills: dep install failed", "dep", dep, "error", msg)
		return false, msg
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
			pipPkgs = append(pipPkgs, strings.TrimPrefix(dep, "pip:"))
		case strings.HasPrefix(dep, "npm:"):
			npmPkgs = append(npmPkgs, strings.TrimPrefix(dep, "npm:"))
		default:
			sysPkgs = append(sysPkgs, dep)
		}
	}

	if len(sysPkgs) > 0 {
		slog.Info("skills: installing system packages", "pkgs", sysPkgs)
		args := append([]string{"apk", "add", "--no-cache"}, sysPkgs...)
		cmd := exec.CommandContext(ctx, "doas", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("apk: %s (%v)", strings.TrimSpace(string(out)), err))
		} else {
			result.System = sysPkgs
		}
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

// cleanCaches removes pip and npm caches to save disk space.
func cleanCaches(ctx context.Context) {
	exec.CommandContext(ctx, "pip3", "cache", "purge").Run()           //nolint:errcheck
	exec.CommandContext(ctx, "rm", "-rf", "/tmp/npm-*").Run()          //nolint:errcheck
}
