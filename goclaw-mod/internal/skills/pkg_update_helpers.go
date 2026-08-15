package skills

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Sentinel errors for pip update failures.
var (
	ErrUpdatePipConflict          = errors.New("pip update: dependency conflict")
	ErrUpdatePipNetwork           = errors.New("pip update: network error")
	ErrUpdatePipPermission        = errors.New("pip update: permission denied")
	ErrUpdatePipNotFound          = errors.New("pip update: package not found")
	ErrUpdatePipExternallyManaged = errors.New("pip update: externally-managed environment")
)

// Sentinel errors for npm update failures.
var (
	ErrUpdateNpmConflict      = errors.New("npm update: peer dependency conflict")
	ErrUpdateNpmNetwork       = errors.New("npm update: network error")
	ErrUpdateNpmPermission    = errors.New("npm update: permission denied")
	ErrUpdateNpmNotFound      = errors.New("npm update: package not found")
	ErrUpdateNpmTargetMissing = errors.New("npm update: version/target missing")
)

// Sentinel errors for apk update failures.
var (
	ErrUpdateApkConflict      = errors.New("apk update: dependency conflict")
	ErrUpdateApkNetwork       = errors.New("apk update: network error")
	ErrUpdateApkLocked        = errors.New("apk update: database locked")
	ErrUpdateApkNotFound      = errors.New("apk update: package not found")
	ErrUpdateApkPermission    = errors.New("apk update: permission denied")
	ErrUpdateApkDiskFull      = errors.New("apk update: disk full")
	ErrUpdateApkHelperUnavail = errors.New("apk update: pkg-helper unavailable")
	ErrInvalidApkPackageName  = errors.New("apk update: invalid package name")
)

// Compiled regexes — all allocated once at package init.
var (
	// pipPreReleaseRE matches PEP 440 pre-release identifiers.
	// Digits are optional (e.g. bare "rc", "a", "b" are valid per PEP 440).
	// Also matches .pre/.preview suffixes.
	pipPreReleaseRE = regexp.MustCompile(`(?i)(a|b|rc|dev)\d*|\.pre(?:view)?`)

	// npmPreReleaseRE matches SemVer pre-release labels used by npm.
	npmPreReleaseRE = regexp.MustCompile(`(?i)-(alpha|beta|rc|pre|preview|dev|nightly|snapshot)`)

	// validPipName enforces PyPI normalized name rules:
	// must start with alphanumeric, then alphanumeric plus dots, hyphens, underscores.
	validPipName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

	// validNpmName enforces npm package name rules:
	// optional @scope/ prefix (lowercase), then lowercase alphanumeric + dots/hyphens.
	validNpmName = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)

	// validApkName enforces Alpine package name rules:
	// lowercase alphanumeric start, plus dots, underscores, plus, hyphens.
	// Rejects uppercase, slashes, @, shell metacharacters.
	// Example valid: curl, libstdc++, gtk+3.0, ca-certificates, py3-pip.
	validApkName = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*$`)

	// ansiRE strips ANSI escape sequences from stderr.
	ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
)

// IsPipPreRelease returns true when version looks like a PEP 440 pre-release.
// Covers: alpha (a), beta (b), release candidate (rc), dev, and .pre/.preview suffixes.
func IsPipPreRelease(version string) bool {
	return pipPreReleaseRE.MatchString(version)
}

// IsNpmPreRelease returns true when version contains a SemVer pre-release label
// (alpha, beta, rc, pre, preview, dev, nightly, snapshot preceded by a dash).
func IsNpmPreRelease(version string) bool {
	return npmPreReleaseRE.MatchString(version)
}

// ValidatePipPackageName rejects names that would bypass pip's package
// resolution or inject shell metacharacters. Rules: must match PyPI normalized
// name (^[a-zA-Z0-9][a-zA-Z0-9._-]*$). Rejects @version suffixes, spaces,
// shell metachars, empty strings.
func ValidatePipPackageName(name string) error {
	if name == "" {
		return errors.New("pip package name must not be empty")
	}
	if !validPipName.MatchString(name) {
		return fmt.Errorf("invalid pip package name: %q", name)
	}
	return nil
}

// ValidateNpmPackageName rejects names that npm would reject or that could
// be used to inject shell metacharacters. Rules: optional @scope/ prefix
// (lowercase), then lowercase alphanumeric with dots/hyphens. Uppercase is
// rejected (npm policy). Empty names are rejected.
func ValidateNpmPackageName(name string) error {
	if name == "" {
		return errors.New("npm package name must not be empty")
	}
	if !validNpmName.MatchString(name) {
		return fmt.Errorf("invalid npm package name: %q", name)
	}
	return nil
}

// ValidateApkPackageName rejects names that Alpine apk would reject or that could
// inject shell metacharacters. Defence-in-depth with pkg-helper's own regex.
//
// Valid: curl, libstdc++, gtk+3.0, ca-certificates, py3-pip.
// Invalid: CURL (uppercase), curl;rm (metachar), curl@edge (@), -pkg (leading hyphen), empty.
//
// Note: intentional divergence from helper's legacy validPkgName regex. The strict
// validApkName applies only to the upgrade action; install/uninstall keep the legacy
// regex for pip/npm cross-runtime compatibility. See plan.md §Security Considerations.
func ValidateApkPackageName(name string) error {
	if name == "" {
		return errors.New("apk package name must not be empty")
	}
	if !validApkName.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidApkPackageName, name)
	}
	return nil
}

// ClassifyPipStderr inspects stderr output from pip and returns a sentinel
// error identifying the failure category, plus a truncated reason string
// (≤500 chars after ANSI stripping and whitespace normalization).
//
// Pattern priority: most-specific first. The default path returns (nil, reason)
// so callers can wrap generically.
func ClassifyPipStderr(stderr string) (error, string) {
	reason := truncateStderr(stderr, 500)
	switch {
	case strings.Contains(stderr, "externally-managed-environment") ||
		strings.Contains(stderr, "EXTERNALLY-MANAGED"):
		return ErrUpdatePipExternallyManaged, reason
	case strings.Contains(stderr, "Permission denied") ||
		strings.Contains(stderr, "EACCES"):
		return ErrUpdatePipPermission, reason
	case strings.Contains(stderr, "No matching distribution") ||
		strings.Contains(stderr, "Could not find a version"):
		return ErrUpdatePipNotFound, reason
	case strings.Contains(stderr, "Read timed out") ||
		strings.Contains(stderr, "ConnectionError") ||
		strings.Contains(strings.ToLower(stderr), "network"):
		return ErrUpdatePipNetwork, reason
	case strings.Contains(stderr, "incompatible") ||
		strings.Contains(stderr, "dependency resolver") ||
		strings.Contains(stderr, "Shallow backtracking"):
		return ErrUpdatePipConflict, reason
	default:
		return nil, reason // unclassified — caller wraps generically
	}
}

// ClassifyNpmStderr inspects stderr from npm and returns a sentinel error
// plus a truncated reason string (≤500 chars).
//
// Pattern priority: most-specific first. Default path returns (nil, reason).
func ClassifyNpmStderr(stderr string) (error, string) {
	reason := truncateStderr(stderr, 500)
	switch {
	case strings.Contains(stderr, "EACCES"):
		return ErrUpdateNpmPermission, reason
	case strings.Contains(stderr, "ERESOLVE"):
		return ErrUpdateNpmConflict, reason
	case strings.Contains(stderr, "ETIMEDOUT") ||
		strings.Contains(stderr, "ENOTFOUND") ||
		strings.Contains(stderr, "getaddrinfo"):
		return ErrUpdateNpmNetwork, reason
	case strings.Contains(stderr, "ETARGET"):
		return ErrUpdateNpmTargetMissing, reason
	case strings.Contains(stderr, "E404") ||
		strings.Contains(stderr, "404") ||
		strings.Contains(stderr, "not in this registry"):
		return ErrUpdateNpmNotFound, reason
	default:
		return nil, reason
	}
}

// ClassifyApkStderr inspects stderr from apk and returns a sentinel error plus
// a truncated reason string (≤500 chars). Pattern priority: most-specific first.
//
// Pattern ordering rationale:
//   - "unable to lock" checked before "Permission denied" — a locked database error
//     often includes "Permission denied" in the same message; locked is more actionable.
//   - "unsatisfiable constraints" split by "breaks: world" / "required by" into
//     conflict vs not-found — missing package and dependency conflict share same prefix.
//   - Default path returns (nil, reason) so callers can wrap generically.
func ClassifyApkStderr(stderr string) (error, string) {
	reason := truncateStderr(stderr, 500)
	switch {
	case strings.Contains(stderr, "unable to lock"):
		return ErrUpdateApkLocked, reason
	case strings.Contains(stderr, "Permission denied"):
		return ErrUpdateApkPermission, reason
	case strings.Contains(stderr, "No space left on device") ||
		strings.Contains(stderr, "disk full"):
		return ErrUpdateApkDiskFull, reason
	case strings.Contains(stderr, "unsatisfiable constraints"):
		// "breaks: world" or "required by" indicates a dependency conflict with an
		// existing package; otherwise the package itself is simply not found.
		if strings.Contains(stderr, "breaks: world") ||
			strings.Contains(stderr, "required by") {
			return ErrUpdateApkConflict, reason
		}
		return ErrUpdateApkNotFound, reason
	case strings.Contains(stderr, "breaks: world"):
		return ErrUpdateApkConflict, reason
	case strings.Contains(strings.ToLower(stderr), "network") ||
		strings.Contains(stderr, "unable to fetch") ||
		strings.Contains(stderr, "connection") ||
		strings.Contains(stderr, "timed out") ||
		strings.Contains(stderr, "hostname resolution failed"):
		return ErrUpdateApkNetwork, reason
	default:
		return nil, reason
	}
}

// truncateStderr normalizes and caps a stderr string for safe logging.
// Steps: (1) strip ANSI escape codes, (2) normalize CRLF → LF,
// (3) collapse whitespace runs to single space, (4) cap at n bytes with ellipsis.
func truncateStderr(s string, n int) string {
	s = ansiRE.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	// Collapse consecutive whitespace (tabs, newlines, spaces) to single space.
	fields := strings.Fields(s)
	s = strings.Join(fields, " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
