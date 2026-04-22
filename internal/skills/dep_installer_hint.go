package skills

import "strings"

// pipBuildFailHint returns a human-readable hint when a pip install fails
// due to a build-from-source error (missing system lib or compiler).
// Returns "" when no known pattern matches the combined stdout+stderr.
//
// Hints are log-side only — never returned to callers / HTTP responses —
// so they can safely suggest alternatives without altering install semantics.
// There is no auto-retry: failing packages typically have no wheel at all,
// and a retry would burn the 5-minute install timeout uselessly.
func pipBuildFailHint(pkg, combinedOutput string) string {
	out := combinedOutput
	if !strings.Contains(out, "Failed building wheel") &&
		!strings.Contains(out, "Could not build wheels") &&
		!strings.Contains(out, "pg_config") &&
		!strings.Contains(out, "mysql_config") {
		return ""
	}
	switch {
	case strings.Contains(out, "pg_config"):
		return "install 'pip:psycopg2-binary' (prebuilt wheel, no pg_config needed) or declare deps: in SKILL.md"
	case strings.Contains(out, "mysql_config"):
		return "install 'pip:PyMySQL' (pure-Python) or add 'system:mysql-dev' to deps: in SKILL.md"
	case strings.Contains(pkg, "psycopg") && !strings.Contains(pkg, "binary"):
		return "try 'pip:psycopg[binary]' (v3) or 'pip:psycopg2-binary' (v2) and declare explicitly in SKILL.md deps:"
	case strings.Contains(pkg, "crypto") || strings.Contains(pkg, "Crypto"):
		return "use 'pip:pycryptodome' (modern successor) and declare in SKILL.md deps:"
	}
	return "pip failed to build from source — try a '-binary' variant if available, or declare explicit deps: in SKILL.md"
}
