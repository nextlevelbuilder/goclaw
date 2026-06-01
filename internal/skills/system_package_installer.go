package skills

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
)

var (
	debPackageNameRE               = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)
	systemLookPath                 = exec.LookPath
	systemCommandCombinedOutput    = runSystemCommandCombinedOutput
	aptSystemPackageAliases        = map[string]string{"pip3": "python3-pip", "github-cli": "gh", "go": "golang-go", "golang": "golang-go"}
	errSystemPackageMgrUnavailable = "system package manager unavailable on this runtime"
)

func runSystemCommandCombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func installSystemPackage(ctx context.Context, requested string) (bool, string) {
	if IsAlpineRuntime() {
		return apkViaHelper(ctx, "install", requested)
	}
	pkg, err := resolveDebianPackageName(requested)
	if err != nil {
		return false, err.Error()
	}
	if _, err := systemLookPath("apt-get"); err != nil {
		return false, errSystemPackageMgrUnavailable
	}
	if ok, msg := runAptCommand(ctx, "install", pkg); !ok {
		return false, msg
	}
	if err := addSystemPackageRecord(requested, pkg, "apt"); err != nil {
		slog.Warn("skills: system package record add failed", "package", requested, "resolved", pkg, "error", err)
	}
	return true, ""
}

func uninstallSystemPackage(ctx context.Context, requested string) (bool, string) {
	if IsAlpineRuntime() {
		return apkViaHelper(ctx, "uninstall", requested)
	}
	pkg, err := resolveDebianPackageName(requested)
	if err != nil {
		return false, err.Error()
	}
	if _, err := systemLookPath("apt-get"); err != nil {
		return false, errSystemPackageMgrUnavailable
	}
	if ok, msg := runAptCommand(ctx, "remove", pkg); !ok {
		return false, msg
	}
	if err := removeSystemPackageRecord(requested, pkg, "apt"); err != nil {
		slog.Warn("skills: system package record remove failed", "package", requested, "resolved", pkg, "error", err)
	}
	return true, ""
}

func resolveDebianPackageName(requested string) (string, error) {
	pkg := strings.ToLower(strings.TrimSpace(requested))
	if alias, ok := aptSystemPackageAliases[pkg]; ok {
		pkg = alias
	}
	if !debPackageNameRE.MatchString(pkg) {
		return "", fmt.Errorf("invalid Debian package name: %s", requested)
	}
	return pkg, nil
}

func runAptCommand(ctx context.Context, action, pkg string) (bool, string) {
	args := []string{"-n", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get"}
	switch action {
	case "install":
		args = append(args, "install", "-y", "--no-install-recommends", pkg)
	case "remove":
		args = append(args, "remove", "-y", pkg)
	default:
		return false, "unsupported apt action"
	}
	out, err := systemCommandCombinedOutput(ctx, "sudo", args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		} else {
			msg = fmt.Sprintf("%s: %v", msg, err)
		}
		return false, msg
	}
	return true, ""
}
