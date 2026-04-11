// Package permissions — Dangerous patterns detection (CP-06).
// Safety net that blocks known-dangerous command patterns even when
// other rules would allow them.
package permissions

import "regexp"

// DangerousPattern defines a regex + reason for blocking.
type DangerousPattern struct {
	Pattern *regexp.Regexp
	Reason  string
}

// DangerousPatterns are checked AFTER rule matching.
// A match here overrides any allow rule — this is the last-resort safety net.
var DangerousPatterns = []DangerousPattern{
	// Shell injection via download
	{regexp.MustCompile(`curl\s.*\|\s*(ba)?sh`), "pipe download to shell execution"},
	{regexp.MustCompile(`wget\s.*\|\s*(ba)?sh`), "pipe download to shell execution"},
	{regexp.MustCompile(`curl\s.*\|\s*python`), "pipe download to Python execution"},

	// Eval injection
	{regexp.MustCompile(`eval\s*\(`), "eval with dynamic input"},
	{regexp.MustCompile(`eval\s+"\$`), "eval with variable expansion"},

	// Filesystem destruction
	{regexp.MustCompile(`rm\s+-rf\s+/[^.]`), "recursive delete from root"},
	{regexp.MustCompile(`rm\s+-rf\s+~`), "recursive delete of home directory"},
	{regexp.MustCompile(`>\s*/dev/sd`), "direct disk write"},
	{regexp.MustCompile(`dd\s+if=.*of=/dev/`), "raw disk write"},
	{regexp.MustCompile(`mkfs\.`), "filesystem format"},

	// Permission escalation
	{regexp.MustCompile(`chmod\s+777`), "world-writable permissions"},
	{regexp.MustCompile(`chmod\s+.*\+s`), "setuid/setgid bit"},

	// Git destruction
	{regexp.MustCompile(`git\s+push\s+.*--force`), "force push (potentially destructive)"},
	{regexp.MustCompile(`git\s+reset\s+--hard`), "hard reset (discards changes)"},
	{regexp.MustCompile(`git\s+clean\s+-fd`), "clean untracked files"},

	// Database destruction
	{regexp.MustCompile(`(?i)DROP\s+DATABASE`), "drop database"},
	{regexp.MustCompile(`(?i)DROP\s+TABLE`), "drop table"},
	{regexp.MustCompile(`(?i)TRUNCATE\s+TABLE`), "truncate table"},
	{regexp.MustCompile(`(?i)DELETE\s+FROM\s+\w+\s*;`), "delete all rows (no WHERE)"},

	// System config overwrite
	{regexp.MustCompile(`>\s*/etc/`), "overwrite system config"},
	{regexp.MustCompile(`>\s*/usr/`), "overwrite system binaries"},

	// Fork bomb
	{regexp.MustCompile(`:\(\)\s*\{\s*:\|:\s*&\s*\}`), "fork bomb"},
	{regexp.MustCompile(`\.\s*\(\)\s*\{\s*\.\|\.\s*&\s*\}`), "fork bomb variant"},
}

// CheckDangerousPatterns tests a command against all known dangerous patterns.
// Returns (blocked, reason). If blocked is true, the command MUST NOT execute.
func CheckDangerousPatterns(command string) (bool, string) {
	for _, dp := range DangerousPatterns {
		if dp.Pattern.MatchString(command) {
			return true, dp.Reason
		}
	}
	return false, ""
}
