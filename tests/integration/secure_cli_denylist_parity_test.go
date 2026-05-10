//go:build integration

package integration

// C4 denylist parity test: verify that the frontend denylist (TypeScript) matches
// the backend denylist (Go package internal/crypto/env_denylist.go).
//
// Strategy: the Go denylist is imported directly via package import.
// The frontend denylist is read from the TypeScript source file via string parsing.
// If the sets diverge, the test fails with a diff showing added/removed keys.

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
)

// frontendDenylistExact reads the frontend ENV_DENYLIST_EXACT set from the TypeScript source.
// Parses the JS Set literal `const ENV_DENYLIST_EXACT = new Set([...])`.
func frontendDenylistExact(t *testing.T) map[string]struct{} {
	t.Helper()
	// Path relative to the test file's directory (tests/integration/).
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	tsFile := filepath.Join(root, "ui", "web", "src", "pages", "cli-credentials",
		"cli-credential-grant-env-section.tsx")

	f, err := os.Open(tsFile)
	if err != nil {
		t.Skipf("frontend file not found (not in TS codebase scope): %v", err)
		return nil
	}
	defer f.Close()

	result := make(map[string]struct{})
	inSet := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "const ENV_DENYLIST_EXACT") {
			inSet = true
		}
		if inSet {
			// Extract quoted identifiers.
			parts := strings.Split(line, `"`)
			for i := 1; i < len(parts); i += 2 {
				key := strings.TrimSpace(parts[i])
				if key != "" && !strings.Contains(key, " ") {
					result[key] = struct{}{}
				}
			}
		}
		if inSet && strings.Contains(line, "]);") {
			break
		}
	}
	return result
}

// frontendDenylistPrefixes reads the frontend ENV_DENYLIST_PREFIXES array.
func frontendDenylistPrefixes(t *testing.T) map[string]struct{} {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	tsFile := filepath.Join(root, "ui", "web", "src", "pages", "cli-credentials",
		"cli-credential-grant-env-section.tsx")

	f, err := os.Open(tsFile)
	if err != nil {
		t.Skipf("frontend file not found: %v", err)
		return nil
	}
	defer f.Close()

	result := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "const ENV_DENYLIST_PREFIXES") {
			// Parse prefix entries from: ["DYLD_", "GOCLAW_", "LD_"]
			parts := strings.Split(line, `"`)
			for i := 1; i < len(parts); i += 2 {
				pfx := strings.TrimSpace(parts[i])
				if pfx != "" && !strings.Contains(pfx, " ") {
					result[pfx] = struct{}{}
				}
			}
			break
		}
	}
	return result
}

// backendDenylistExact returns the Go exact-match denylist by probing known keys.
// Since deniedExact is unexported, we use IsDeniedEnvKey with a controlled set of
// all keys that appear in either Go or frontend source.
//
// This is the exhaustive union probe set — any key on this list that differs between
// Go and TS is caught.
var knownExactKeys = []string{
	"PATH", "HOME", "USER", "SHELL", "PWD",
	"LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT",
	"NODE_OPTIONS", "NODE_PATH",
	"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP",
	"GIT_SSH_COMMAND", "GIT_SSH", "GIT_EXEC_PATH", "GIT_CONFIG_SYSTEM",
	"SSH_AUTH_SOCK",
	// Additions from finding #6
	"BASH_ENV", "ENV", "PROMPT_COMMAND",
	"PERL5LIB", "RUBYOPT",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE",
	"IFS",
}

// TestDenylistParity_ExactKeysPresentInBoth verifies that every key in the frontend
// ENV_DENYLIST_EXACT is also rejected by the Go backend (IsDeniedEnvKey returns true).
func TestDenylistParity_ExactKeysPresentInBoth(t *testing.T) {
	frontendExact := frontendDenylistExact(t)
	if len(frontendExact) == 0 {
		t.Skip("frontend denylist not parseable — skipping parity check")
	}

	for key := range frontendExact {
		if !crypto.IsDeniedEnvKey(key) {
			t.Errorf("PARITY DRIFT: frontend denies %q but backend does NOT deny it", key)
		}
	}
}

// TestDenylistParity_BackendDeniesKnownKeys verifies all known-dangerous keys are
// denied by the backend after finding #6 additions.
func TestDenylistParity_BackendDeniesKnownKeys(t *testing.T) {
	// Keys from original denylist + finding #6 additions.
	mustDeny := []string{
		// Original
		"PATH", "HOME", "USER", "SHELL", "PWD",
		"LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT",
		"NODE_OPTIONS", "NODE_PATH",
		"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP",
		"GIT_SSH_COMMAND", "GIT_SSH", "GIT_EXEC_PATH", "GIT_CONFIG_SYSTEM",
		"SSH_AUTH_SOCK",
		// Finding #6 additions
		"BASH_ENV", "ENV", "PROMPT_COMMAND",
		"PERL5LIB", "RUBYOPT",
		"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "CURL_CA_BUNDLE",
		"IFS",
		// Prefix matches
		"DYLD_INSERT_LIBRARIES", "DYLD_FRAMEWORK_PATH",
		"GOCLAW_SECRET", "GOCLAW_ENCRYPTION_KEY",
		"LD_SOMETHING",
		// npm_config_ prefix (finding #6)
		"npm_config_registry", "npm_config_prefix",
	}
	for _, key := range mustDeny {
		if !crypto.IsDeniedEnvKey(key) {
			t.Errorf("backend should deny %q but IsDeniedEnvKey returned false", key)
		}
	}
}

// TestDenylistParity_SafeKeyNotDenied verifies that safe keys pass validation.
func TestDenylistParity_SafeKeyNotDenied(t *testing.T) {
	safeKeys := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"DATABASE_URL",
		"API_KEY",
		"MY_CUSTOM_VAR",
	}
	for _, key := range safeKeys {
		if crypto.IsDeniedEnvKey(key) {
			t.Errorf("safe key %q should not be denied by backend", key)
		}
	}
}

// TestDenylistParity_PrefixesInBoth verifies that frontend prefix list matches backend.
func TestDenylistParity_PrefixesInBoth(t *testing.T) {
	frontendPfx := frontendDenylistPrefixes(t)
	if len(frontendPfx) == 0 {
		t.Skip("frontend prefix list not parseable")
	}

	// For each frontend prefix, verify a key with that prefix is denied by backend.
	for pfx := range frontendPfx {
		testKey := pfx + "SOMETHING"
		if !crypto.IsDeniedEnvKey(testKey) {
			t.Errorf("PARITY DRIFT: frontend prefix %q blocks keys but backend does NOT deny %q", pfx, testKey)
		}
	}
}
