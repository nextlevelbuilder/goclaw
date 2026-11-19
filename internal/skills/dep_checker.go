package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// defaultDepCheckTimeout bounds a single python3/node import probe. Importing
// heavy packages (e.g. anthropic) can take several seconds on a cold start.
const defaultDepCheckTimeout = 30 * time.Second

// DepCheckStatus describes the confidence of a dependency check.
type DepCheckStatus int

const (
	// DepCheckPresent means every declared dependency was verified present.
	DepCheckPresent DepCheckStatus = iota
	// DepCheckMissing means at least one dependency was verified missing.
	DepCheckMissing
	// DepCheckUnknown means the check was cancelled or timed out before it
	// could decide. Unknown is not evidence that a dependency is missing.
	DepCheckUnknown
)

// DepCheckResult is the richer dependency-check result for callers that need
// to tell "verified present" from "not verified".
type DepCheckResult struct {
	Status  DepCheckStatus
	Missing []string
	// Incomplete means at least one probe had no verdict, even if another dependency is known missing.
	Incomplete bool
}

// depCheckTimeout returns the probe timeout, overridable via
// GOCLAW_DEP_CHECK_TIMEOUT (Go duration, e.g. "60s"). Empty, invalid or
// non-positive values fall back to the default. Read at call time.
func depCheckTimeout() time.Duration {
	v := os.Getenv("GOCLAW_DEP_CHECK_TIMEOUT")
	if v == "" {
		return defaultDepCheckTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		slog.Warn("skills.dep_check.invalid_timeout", "value", v, "default", defaultDepCheckTimeout)
		return defaultDepCheckTimeout
	}
	return d
}

// probeResult is the outcome of one runtime probe.
type probeResult struct {
	missing []string
	unknown bool
}

// CheckSkillDeps verifies all dependencies in a manifest are available.
// Returns (ok, missing) where missing lists unavailable dependencies.
// For Python packages, sets PYTHONPATH=ScriptsDir so local modules and stdlib
// are resolved natively — only truly missing pip packages are reported.
//
// A cancelled or timed-out probe is reported as ok with no missing packages,
// preserving the historic fail-open behavior. Callers that must distinguish
// "not verified" from "verified present" should use CheckSkillDepsContext.
func CheckSkillDeps(m *SkillManifest) (bool, []string) {
	res := CheckSkillDepsContext(context.Background(), m)
	return res.Status != DepCheckMissing, res.Missing
}

// CheckSkillDepsContext verifies all dependencies in a manifest are available
// under ctx. Probes stop when ctx is cancelled or when their own probe timeout
// elapses; both cases return DepCheckUnknown unless another dependency was
// already verified missing.
func CheckSkillDepsContext(ctx context.Context, m *SkillManifest) DepCheckResult {
	if err := ctx.Err(); err != nil {
		slog.Warn("skills.dep_check.cancelled", "error", err)
		return DepCheckResult{Status: DepCheckUnknown, Incomplete: true}
	}
	if m == nil || m.IsEmpty() {
		return DepCheckResult{Status: DepCheckPresent}
	}
	ensureNpmGlobalEnv()

	var missing []string
	unknown := false

	// Check system binaries
	for _, bin := range m.Requires {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}

	// Check Python packages via import with PYTHONPATH set.
	// If python3 binary is absent, skip per-package listing — the "python3" binary
	// is already reported via m.Requires and is the root cause.
	if len(m.RequiresPython) > 0 {
		if _, err := exec.LookPath("python3"); err == nil {
			res := checkPythonPackages(ctx, m.RequiresPython, m.ScriptsDir)
			missing = append(missing, res.missing...)
			unknown = unknown || res.unknown
		}
	}

	// Check Node packages.
	// If node binary is absent, skip per-package listing for the same reason.
	if len(m.RequiresNode) > 0 {
		if _, err := exec.LookPath("node"); err == nil {
			res := checkNodePackages(ctx, m.RequiresNode, m.ScriptsDir)
			missing = append(missing, res.missing...)
			unknown = unknown || res.unknown
		}
	}

	switch {
	case len(missing) > 0:
		return DepCheckResult{Status: DepCheckMissing, Missing: missing, Incomplete: unknown}
	case unknown:
		return DepCheckResult{Status: DepCheckUnknown, Incomplete: true}
	default:
		return DepCheckResult{Status: DepCheckPresent}
	}
}

// probeDeadline derives the per-probe deadline from the caller context.
func probeDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, depCheckTimeout())
}

// probeInterrupted reports whether a failed probe produced no verdict because
// the caller context or the per-probe deadline ended it.
func probeInterrupted(ctx, probeCtx context.Context, runtime string, subjects []string) bool {
	parentErr := ctx.Err()
	if parentErr != nil {
		slog.Warn("skills.dep_check.cancelled", "runtime", runtime, "error", parentErr, "packages", subjects)
		return true
	}
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
		slog.Warn("skills.dep_check.timeout", "runtime", runtime, "timeout", depCheckTimeout(), "packages", subjects)
		return true
	}
	return false
}

// checkPythonPackages checks which Python import names are importable.
// Sets PYTHONPATH=scriptsDir so local modules resolve natively (no false positives).
// Returns missing deps as "pip:<pip-package-name>".
func checkPythonPackages(ctx context.Context, importNames []string, scriptsDir string) probeResult {
	if len(importNames) == 0 {
		return probeResult{}
	}
	if err := ctx.Err(); err != nil {
		slog.Warn("skills.dep_check.cancelled", "runtime", "python3", "error", err, "packages", importNames)
		return probeResult{unknown: true}
	}

	// Build a script that tries each import and prints failures
	var sb strings.Builder
	for _, name := range importNames {
		sb.WriteString(fmt.Sprintf("try:\n import %s\nexcept ImportError:\n print(%q)\n", name, name))
	}

	probeCtx, cancel := probeDeadline(ctx)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, "python3", "-c", sb.String())
	// PYTHONPATH lets Python find local modules in scriptsDir — stdlib and local dirs
	// resolve natively, so only truly missing pip packages produce ImportError.
	// We filter the existing PYTHONPATH from os.Environ() to avoid duplicate-key issues
	// (on Linux, getenv() returns the first match, so appending would be silently ignored).
	if scriptsDir != "" {
		pythonpath := scriptsDir
		if existing := os.Getenv("PYTHONPATH"); existing != "" {
			pythonpath = existing + ":" + scriptsDir
		}
		baseEnv := make([]string, 0, len(os.Environ()))
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "PYTHONPATH=") {
				baseEnv = append(baseEnv, e)
			}
		}
		cmd.Env = append(baseEnv, "PYTHONPATH="+pythonpath)
	}

	out, err := cmd.Output()
	if err != nil {
		// A timeout or cancellation says nothing about whether the packages are
		// installed: report them as unknown (not missing) instead of archiving
		// the skill. Any stdout printed before the kill is discarded — the
		// probe is all-or-nothing by design, keeping the fail-open behavior.
		if probeInterrupted(ctx, probeCtx, "python3", importNames) {
			return probeResult{unknown: true}
		}
		// Python itself failed — all packages are missing
		var missing []string
		for _, name := range importNames {
			missing = append(missing, "pip:"+importToPipName(name))
		}
		return probeResult{missing: missing}
	}

	var missing []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			missing = append(missing, "pip:"+importToPipName(line))
		}
	}
	return probeResult{missing: missing}
}

// checkNodePackages checks which Node packages are resolvable.
// Sets CWD to scriptsDir so local require() calls resolve correctly.
func checkNodePackages(ctx context.Context, packages []string, scriptsDir string) probeResult {
	if len(packages) == 0 {
		return probeResult{}
	}
	if err := ctx.Err(); err != nil {
		slog.Warn("skills.dep_check.cancelled", "runtime", "node", "error", err, "packages", packages)
		return probeResult{unknown: true}
	}

	var sb strings.Builder
	for _, pkg := range packages {
		sb.WriteString(fmt.Sprintf("try{require.resolve('%s')}catch(e){console.log(%q)}\n", pkg, pkg))
	}

	probeCtx, cancel := probeDeadline(ctx)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, "node", "-e", sb.String())
	cmd.Env = npmCommandEnv()
	if scriptsDir != "" {
		cmd.Dir = scriptsDir
	}

	out, err := cmd.Output()
	if err != nil {
		if probeInterrupted(ctx, probeCtx, "node", packages) {
			return probeResult{unknown: true}
		}
		var missing []string
		for _, pkg := range packages {
			missing = append(missing, "npm:"+pkg)
		}
		return probeResult{missing: missing}
	}

	var missing []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			missing = append(missing, "npm:"+line)
		}
	}
	return probeResult{missing: missing}
}

// importToPipName maps Python import names to their pip package names when they differ.
var importToPipName = func(importName string) string {
	m := map[string]string{
		"cv2":         "opencv-python",
		"PIL":         "Pillow",
		"yaml":        "pyyaml",
		"sklearn":     "scikit-learn",
		"bs4":         "beautifulsoup4",
		"dateutil":    "python-dateutil",
		"dotenv":      "python-dotenv",
		"pptx":        "python-pptx",
		"docx":        "python-docx",
		"attr":        "attrs",
		"gi":          "PyGObject",
		"psycopg2":    "psycopg2-binary",
		"psycopg":     "psycopg[binary]",
		"MySQLdb":     "mysqlclient",
		"Crypto":      "pycryptodome",
		"serial":      "pyserial",
		"skimage":     "scikit-image",
		"Levenshtein": "python-Levenshtein",
	}
	if pip, ok := m[importName]; ok {
		return pip
	}
	return importName
}

// FormatMissing formats a missing deps list into a human-readable string.
func FormatMissing(missing []string) string {
	return strings.Join(missing, ", ")
}
