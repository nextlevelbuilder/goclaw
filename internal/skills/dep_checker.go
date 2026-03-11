package skills

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const depCheckTimeout = 5 * time.Second

// CheckSkillDeps verifies all dependencies in a manifest are available.
// Returns (ok, missing) where missing lists unavailable dependencies.
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

	// Check Python packages (batch check)
	if len(m.RequiresPython) > 0 {
		if _, err := exec.LookPath("python3"); err != nil {
			// python3 not available — all python deps are missing
			for _, pkg := range m.RequiresPython {
				missing = append(missing, "pip:"+pkg)
			}
		} else {
			missing = append(missing, checkPythonPackages(m.RequiresPython)...)
		}
	}

	// Check Node packages (batch check)
	if len(m.RequiresNode) > 0 {
		if _, err := exec.LookPath("node"); err != nil {
			for _, pkg := range m.RequiresNode {
				missing = append(missing, "npm:"+pkg)
			}
		} else {
			missing = append(missing, checkNodePackages(m.RequiresNode)...)
		}
	}

	return len(missing) == 0, missing
}

// checkPythonPackages checks which Python packages are importable.
func checkPythonPackages(packages []string) []string {
	var missing []string
	for _, pkg := range packages {
		// Map pip package name back to import name for checking
		importName := pipToImport(pkg)
		ctx, cancel := context.WithTimeout(context.Background(), depCheckTimeout)
		cmd := exec.CommandContext(ctx, "python3", "-c", fmt.Sprintf("import %s", importName))
		err := cmd.Run()
		cancel()
		if err != nil {
			missing = append(missing, "pip:"+pkg)
		}
	}
	return missing
}

// checkNodePackages checks which Node packages are resolvable.
func checkNodePackages(packages []string) []string {
	var missing []string
	for _, pkg := range packages {
		ctx, cancel := context.WithTimeout(context.Background(), depCheckTimeout)
		cmd := exec.CommandContext(ctx, "node", "-e", fmt.Sprintf("require.resolve('%s')", pkg))
		err := cmd.Run()
		cancel()
		if err != nil {
			missing = append(missing, "npm:"+pkg)
		}
	}
	return missing
}

// pipToImport maps pip package names to their Python import names.
var pipToImport = func(pkg string) string {
	m := map[string]string{
		"opencv-python":  "cv2",
		"Pillow":         "PIL",
		"pyyaml":         "yaml",
		"scikit-learn":   "sklearn",
		"beautifulsoup4": "bs4",
		"python-dateutil": "dateutil",
		"python-dotenv":  "dotenv",
		"python-pptx":    "pptx",
		"python-docx":    "docx",
		"attrs":          "attr",
		"PyGObject":      "gi",
	}
	if imp, ok := m[pkg]; ok {
		return imp
	}
	// Default: replace hyphens with underscores
	return strings.ReplaceAll(pkg, "-", "_")
}

// FormatMissing formats a missing deps list into a human-readable string.
func FormatMissing(missing []string) string {
	return strings.Join(missing, ", ")
}
