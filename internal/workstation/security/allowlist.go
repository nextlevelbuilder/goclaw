package security

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// blockedEnvKeys is the set of environment variable names that are always rejected.
// These can be used for privilege escalation, path hijacking, or leaking GoClaw internals.
// Keys are checked after NFKC normalization to prevent Unicode bypass.
var blockedEnvKeys = map[string]bool{
	"LD_PRELOAD":            true,
	"LD_LIBRARY_PATH":       true,
	"PATH":                  true,
	"DYLD_INSERT_LIBRARIES": true,
}

// allowlistEntry is a cached allowlist for one workstation.
type allowlistEntry struct {
	patterns  []string // enabled binary name patterns
	fetchedAt time.Time
}

// AllowlistChecker validates exec requests against a per-workstation binary allowlist.
// Architecture:
//   - C1 fix: argv-exec model — cmd is the binary name (argv[0]), not a shell command string.
//     Shell injection is impossible because the SSH backend never invokes sh -c.
//   - C2 fix: NFKC normalization applied to cmd and each arg before any check.
//   - Default-deny: if no enabled pattern matches cmd's binary name → deny.
//   - Cache: allowlist loaded from DB with configurable TTL (default 30s).
//     Event-driven invalidation via Invalidate() called on permission changes.
type AllowlistChecker struct {
	permStore store.WorkstationPermissionStore
	cacheTTL  time.Duration

	mu    sync.Mutex
	cache map[uuid.UUID]*allowlistEntry // keyed by workstation ID
}

// NewAllowlistChecker creates an AllowlistChecker with the given store and cache TTL.
// Typical TTL: 30s (balances freshness vs. DB load).
func NewAllowlistChecker(permStore store.WorkstationPermissionStore, cacheTTL time.Duration) *AllowlistChecker {
	return &AllowlistChecker{
		permStore: permStore,
		cacheTTL:  cacheTTL,
		cache:     make(map[uuid.UUID]*allowlistEntry),
	}
}

// Invalidate evicts the cached allowlist for workstationID.
// Call this when permissions are added, removed, or toggled for that workstation.
func (c *AllowlistChecker) Invalidate(workstationID uuid.UUID) {
	c.mu.Lock()
	delete(c.cache, workstationID)
	c.mu.Unlock()
}

// Check validates cmd (argv[0]) + args against workstation policy.
//
// Pipeline:
//  1. NFKC normalize cmd and each arg (collapses Unicode lookalikes)
//  2. Reject NUL bytes and CRLF in cmd or any arg (unsafe in all contexts)
//  3. Allowlist match on binary name (default-deny)
//
// Env-key validation (LD_PRELOAD, PATH, GOCLAW_*, etc.) is handled
// separately by CheckEnv, called in the tool wiring layer.
func (c *AllowlistChecker) Check(
	ctx context.Context,
	ws *store.Workstation,
	cmd string,
	args []string,
) error {
	locale := store.LocaleFromContext(ctx)

	// ── Step 1: NFKC normalize ───────────────────────────────────────────────
	// C2 fix: must happen before ANY matching or byte-level validation.
	cmd = NormalizeCmd(cmd)
	for i, a := range args {
		args[i] = NormalizeCmd(a)
	}

	// ── Step 2: byte-level safety (NUL / CRLF) ──────────────────────────────
	if containsDangerousBytes(cmd) {
		c.auditDeny(ws, cmd, "dangerous_bytes_in_cmd")
		return fmt.Errorf("%s", i18n.T(locale, i18n.MsgWorkstationInputInvalid, "NUL or CRLF in command"))
	}
	for i, a := range args {
		if containsDangerousBytes(a) {
			c.auditDeny(ws, cmd, "dangerous_bytes_in_arg")
			return fmt.Errorf("%s", i18n.T(locale, i18n.MsgWorkstationInputInvalid,
				fmt.Sprintf("NUL or CRLF in arg[%d]", i)))
		}
	}

	// ── Step 3: binary allowlist (default-deny) ──────────────────────────────
	// Extract the binary name (basename of cmd, strip path).
	// e.g. "/usr/bin/git" → "git", "python3" → "python3"
	binaryName := filepath.Base(cmd)
	if binaryName == "" || binaryName == "." {
		c.auditDeny(ws, cmd, "empty_binary_name")
		return errors.New(i18n.T(locale, i18n.MsgWorkstationCmdDenied, "empty binary name"))
	}

	patterns, err := c.loadAllowlist(ctx, ws.ID)
	if err != nil {
		return fmt.Errorf("load allowlist: %w", err)
	}

	matched := false
	for _, pat := range patterns {
		if MatchAllowedBinary(pat, binaryName) {
			matched = true
			break
		}
	}
	if !matched {
		c.auditDeny(ws, cmd, "no_allowlist_match")
		return errors.New(i18n.T(locale, i18n.MsgWorkstationCmdDenied,
			"no allowlist match for: "+binaryName))
	}

	return nil
}

// CheckEnv validates environment variable keys against the blocklist.
// Called separately so the tool layer can report specific key names.
// Keys are NFKC-normalized before comparison.
func (c *AllowlistChecker) CheckEnv(ctx context.Context, ws *store.Workstation, env map[string]string) error {
	locale := store.LocaleFromContext(ctx)
	for k := range env {
		normalized := NormalizeCmd(k)
		if isBlockedEnvKey(normalized) {
			c.auditDeny(ws, normalized, "blocked_env_key")
			return errors.New(i18n.T(locale, i18n.MsgWorkstationEnvDenied, k))
		}
	}
	return nil
}

// MatchAllowedBinary returns true if pattern matches the binary name.
//
// Matching rules (argv[0] binary name, NOT full command string):
//   - Exact match:   "git"     matches "git"
//   - Prefix glob:   "python*" matches "python3", "python3.11", "python"
//   - No catch-all:  "*" alone is rejected as too permissive — returns false
//
// This is intentionally simple. Matching only the binary name is safe because:
//   - Shell injection requires a shell; the SSH backend uses argv exec (no sh -c).
//   - Argument validation is the remote shell's / OS's responsibility once the
//     binary is allowed.
func MatchAllowedBinary(pattern, binaryName string) bool {
	// Reject the lone wildcard — it would allow everything including shells.
	if pattern == "*" {
		return false
	}
	// Exact match (most common case).
	if pattern == binaryName {
		return true
	}
	// Prefix glob: "python*" matches "python3", "python3.11".
	if before, ok := strings.CutSuffix(pattern, "*"); ok {
		prefix := before
		return prefix != "" && strings.HasPrefix(binaryName, prefix)
	}
	return false
}

// isBlockedEnvKey returns true if the (NFKC-normalized) key should be rejected.
func isBlockedEnvKey(k string) bool {
	if blockedEnvKeys[k] {
		return true
	}
	// Block all GOCLAW_* keys to prevent leaking gateway internals.
	return strings.HasPrefix(k, "GOCLAW_")
}

// loadAllowlist returns the enabled binary name patterns for workstationID.
// Results are cached for cacheTTL; evicted by Invalidate().
func (c *AllowlistChecker) loadAllowlist(ctx context.Context, workstationID uuid.UUID) ([]string, error) {
	c.mu.Lock()
	entry, ok := c.cache[workstationID]
	if ok && time.Since(entry.fetchedAt) < c.cacheTTL {
		patterns := entry.patterns
		c.mu.Unlock()
		return patterns, nil
	}
	c.mu.Unlock()

	// Fetch from DB (outside lock to avoid holding lock during I/O).
	perms, err := c.permStore.ListForWorkstation(ctx, workstationID)
	if err != nil {
		return nil, err
	}

	var patterns []string
	for _, p := range perms {
		if p.Enabled {
			patterns = append(patterns, p.Pattern)
		}
	}

	c.mu.Lock()
	c.cache[workstationID] = &allowlistEntry{
		patterns:  patterns,
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	return patterns, nil
}

// auditDeny emits a structured security log entry on every deny.
// cmd_hash (not plaintext) is logged for PII/secret hygiene.
func (c *AllowlistChecker) auditDeny(ws *store.Workstation, cmd, reason string) {
	hash := sha256.Sum256([]byte(cmd))
	slog.Warn("security.workstation_cmd_denied",
		"workstation_id", ws.ID,
		"tenant_id", ws.TenantID,
		"cmd_hash", fmt.Sprintf("%x", hash[:6]),
		"reason", reason,
	)
}
