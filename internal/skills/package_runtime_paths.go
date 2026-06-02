package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func packageRuntimeDir() string {
	if v := strings.TrimSpace(os.Getenv("RUNTIME_DIR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GOCLAW_DATA_DIR")); v != "" {
		return filepath.Join(v, ".runtime")
	}
	if runtime.GOOS != "windows" && !IsAlpineRuntime() {
		return "/var/lib/goclaw/data/.runtime"
	}
	return filepath.Join("/app/data", ".runtime")
}

func npmGlobalPrefix() string {
	if v := strings.TrimSpace(os.Getenv("NPM_CONFIG_PREFIX")); v != "" {
		return v
	}
	return filepath.Join(packageRuntimeDir(), "npm-global")
}

func npmGlobalBinDir() string {
	if runtime.GOOS == "windows" {
		return npmGlobalPrefix()
	}
	return filepath.Join(npmGlobalPrefix(), "bin")
}

func RuntimeExecutableDirs() []string {
	return uniqueNonEmptyPaths(
		filepath.Join(packageRuntimeDir(), "bin"),
		npmGlobalBinDir(),
		filepath.Join(packageRuntimeDir(), "pip", "bin"),
	)
}

func FindRuntimeExecutable(name string) (string, bool) {
	if strings.ContainsAny(name, `/\`) {
		return "", false
	}
	for _, dir := range RuntimeExecutableDirs() {
		path := filepath.Join(dir, name)
		if IsExecutableFile(path) {
			return path, true
		}
	}
	if path, ok := findNpmPackageExecutableAlias(name); ok {
		return path, true
	}
	return "", false
}

func IsExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0111 != 0
}

func npmGlobalNodePath() string {
	return filepath.Join(npmGlobalPrefix(), "lib", "node_modules")
}

func ensureNpmGlobalEnv() {
	prependProcessPath(npmGlobalBinDir())
}

func npmCommandEnv() []string {
	prefix := npmGlobalPrefix()
	binDir := npmGlobalBinDir()
	nodePath := npmGlobalNodePath()

	env := make([]string, 0, len(os.Environ())+3)
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "NPM_CONFIG_PREFIX="):
			continue
		case strings.HasPrefix(e, "PATH="):
			continue
		case strings.HasPrefix(e, "NODE_PATH="):
			continue
		}
		env = append(env, e)
	}

	pathValue := prependPathValue(os.Getenv("PATH"), binDir)
	nodePathValue := prependPathValue(os.Getenv("NODE_PATH"), nodePath)
	env = append(env,
		"NPM_CONFIG_PREFIX="+prefix,
		"PATH="+pathValue,
		"NODE_PATH="+nodePathValue,
	)
	return env
}

func prependProcessPath(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	_ = os.Setenv("PATH", prependPathValue(os.Getenv("PATH"), dir))
}

func prependPathValue(current, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return current
	}
	parts := filepath.SplitList(current)
	for _, p := range parts {
		if p == dir {
			return current
		}
	}
	if current == "" {
		return dir
	}
	return dir + string(os.PathListSeparator) + current
}

func uniqueNonEmptyPaths(paths ...string) []string {
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}
