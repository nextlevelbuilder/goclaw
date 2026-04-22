package skills

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const depCheckTimeout = 5 * time.Second

// CheckSkillDeps verifies all dependencies in a manifest are available.
// Returns (ok, missing) where missing lists unavailable dependencies.
// For Python packages, sets PYTHONPATH=ScriptsDir so local modules and stdlib
// are resolved natively — only truly missing pip packages are reported.
func CheckSkillDeps(m *SkillManifest) (bool, []string) {
	if m == nil || m.IsEmpty() {
		return true, nil
	}

	var missing []string

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
			missing = append(missing, checkPythonPackages(m.RequiresPython, m.ScriptsDir)...)
		}
	}

	// Check Node packages.
	// If node binary is absent, skip per-package listing for the same reason.
	if len(m.RequiresNode) > 0 {
		if _, err := exec.LookPath("node"); err == nil {
			missing = append(missing, checkNodePackages(m.RequiresNode, m.ScriptsDir)...)
		}
	}

	return len(missing) == 0, missing
}

// checkPythonPackages checks which Python import names are importable.
// Sets PYTHONPATH=scriptsDir so local modules resolve natively (no false positives).
// Returns missing deps as "pip:<pip-package-name>".
func checkPythonPackages(importNames []string, scriptsDir string) []string {
	if len(importNames) == 0 {
		return nil
	}

	// Build a script that tries each import and prints failures
	var sb strings.Builder
	for _, name := range importNames {
		sb.WriteString(fmt.Sprintf("try:\n import %s\nexcept ImportError:\n print(%q)\n", name, name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), depCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "-c", sb.String())
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
		// Python itself failed — all packages are missing
		var missing []string
		for _, name := range importNames {
			missing = append(missing, "pip:"+importToPipName(name))
		}
		return missing
	}

	var missing []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			missing = append(missing, "pip:"+importToPipName(line))
		}
	}
	return missing
}

// checkNodePackages checks which Node packages are resolvable.
// Sets CWD to scriptsDir so local require() calls resolve correctly.
func checkNodePackages(packages []string, scriptsDir string) []string {
	if len(packages) == 0 {
		return nil
	}

	var sb strings.Builder
	for _, pkg := range packages {
		sb.WriteString(fmt.Sprintf("try{require.resolve('%s')}catch(e){console.log(%q)}\n", pkg, pkg))
	}

	ctx, cancel := context.WithTimeout(context.Background(), depCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", "-e", sb.String())
	if scriptsDir != "" {
		cmd.Dir = scriptsDir
	}

	out, err := cmd.Output()
	if err != nil {
		var missing []string
		for _, pkg := range packages {
			missing = append(missing, "npm:"+pkg)
		}
		return missing
	}

	var missing []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			missing = append(missing, "npm:"+line)
		}
	}
	return missing
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
