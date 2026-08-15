// Package crypto — env_denylist.go provides env-key validation for grant env overrides.
// Reusable across HTTP handlers and any future validation layer.
package crypto

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// validEnvKeyShape is the regex for accepted env key shapes.
// Accepts uppercase letters, digits, and underscores only, starting with a letter or underscore.
// Rejects: lowercase, spaces, parentheses (Shellshock-class), empty.
var validEnvKeyShape = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// deniedExact is the exhaustive set of env keys that are rejected (case-insensitive, stored uppercase).
// Keep in sync with ENV_DENYLIST_EXACT in ui/web/src/pages/cli-credentials/cli-credential-grant-env-section.tsx.
var deniedExact = map[string]struct{}{
	"PATH":              {},
	"HOME":              {},
	"USER":              {},
	"SHELL":             {},
	"PWD":               {},
	"LD_PRELOAD":        {},
	"LD_LIBRARY_PATH":   {},
	"LD_AUDIT":          {},
	"NODE_OPTIONS":      {},
	"NODE_PATH":         {},
	"PYTHONPATH":        {},
	"PYTHONHOME":        {},
	"PYTHONSTARTUP":     {},
	"GIT_SSH_COMMAND":   {},
	"GIT_SSH":           {},
	"GIT_EXEC_PATH":     {},
	"GIT_CONFIG_SYSTEM": {},
	"SSH_AUTH_SOCK":     {},
	// Finding #6: additional dangerous vars for shell injection / TLS bypass / exfil
	"BASH_ENV":         {}, // sourced by non-interactive bash
	"ENV":              {}, // sourced by sh (non-interactive)
	"PROMPT_COMMAND":   {}, // executed before each shell prompt
	"PERL5LIB":         {}, // Perl library path override
	"RUBYOPT":          {}, // Ruby interpreter options
	"HTTPS_PROXY":      {}, // HTTPS exfiltration channel
	"HTTP_PROXY":       {}, // HTTP exfiltration channel
	"NO_PROXY":         {}, // disables proxy bypass
	"SSL_CERT_FILE":    {}, // TLS CA cert override — MitM
	"SSL_CERT_DIR":     {}, // TLS CA cert dir override — MitM
	"CURL_CA_BUNDLE":   {}, // curl TLS CA bundle override — MitM
	"IFS":              {}, // Internal Field Separator — shell injection
}

// deniedPrefixes is the set of uppercase key prefixes that are rejected.
// Keep in sync with ENV_DENYLIST_PREFIXES in ui/web/src/pages/cli-credentials/cli-credential-grant-env-section.tsx.
var deniedPrefixes = []string{
	"DYLD_",
	"GOCLAW_",
	"LD_",
	"NPM_CONFIG_", // npm lifecycle overrides (rc-style, loads modules); case-insensitive match via ToUpper
}

// maxGrantEnvKeys is the maximum number of env keys allowed per grant.
const maxGrantEnvKeys = 50

// maxGrantEnvValueBytes is the maximum byte length for a single env value.
const maxGrantEnvValueBytes = 4096

// IsDeniedEnvKey reports whether key is on the grant env denylist.
// Comparison is case-insensitive.
func IsDeniedEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	if _, ok := deniedExact[upper]; ok {
		return true
	}
	for _, pfx := range deniedPrefixes {
		if strings.HasPrefix(upper, pfx) {
			return true
		}
	}
	return false
}

// ValidateGrantEnvVars checks all keys and values in envVars against the denylist
// and value constraints.
//
// Returns rejectedKeys (non-nil when any key is denied) and valueErr (first value violation).
// Callers should check rejectedKeys before valueErr.
//
// Rules:
//   - Key count ≤ maxGrantEnvKeys
//   - Key not on denylist (case-insensitive)
//   - Value: no NUL byte, no newline, max maxGrantEnvValueBytes bytes
func ValidateGrantEnvVars(envVars map[string]string) (rejectedKeys []string, valueErr error) {
	if len(envVars) > maxGrantEnvKeys {
		return nil, fmt.Errorf("too many env keys: max %d, got %d", maxGrantEnvKeys, len(envVars))
	}

	// Finding #6: reject keys that don't match the valid key shape.
	// This catches Shellshock-class injections (keys with `()`, whitespace, lowercase).
	// Also catches empty key "".

	// Finding #7: sort keys before iterating to produce deterministic error messages.
	// Map iteration in Go is non-deterministic — without sorting, the same input can
	// produce different error output on repeated calls, which is confusing for users.
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var denied []string
	for _, k := range keys {
		v := envVars[k]
		// Key-shape validation: must match ^[A-Z_][A-Z0-9_]*$ (uppercase, no special chars).
		if !validEnvKeyShape.MatchString(strings.ToUpper(k)) || k == "" {
			return nil, fmt.Errorf("env key %q has invalid shape: must match ^[A-Z_][A-Z0-9_]*$ (uppercase, no spaces or special chars)", k)
		}
		if IsDeniedEnvKey(k) {
			denied = append(denied, k)
		}
		if err := validateGrantEnvValue(v); err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
	}
	return denied, nil
}

func validateGrantEnvValue(v string) error {
	if len(v) > maxGrantEnvValueBytes {
		return fmt.Errorf("env value exceeds %d bytes", maxGrantEnvValueBytes)
	}
	for _, c := range v {
		if c == 0 {
			return fmt.Errorf("env value must not contain NUL bytes")
		}
		if c == '\n' || c == '\r' {
			return fmt.Errorf("env value must not contain newlines")
		}
	}
	return nil
}
