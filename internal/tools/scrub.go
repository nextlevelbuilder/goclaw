package tools

import (
	"regexp"
	"strings"
	"sync"
)

// Credential patterns to scrub from tool output before returning to the LLM.
// Inspired by zeroclaw's credential scrubbing system.
var credentialPatterns = []*regexp.Regexp{
	// OpenAI
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	// Anthropic
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9-]{20,}`),
	// GitHub personal access tokens
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
	regexp.MustCompile(`gho_[a-zA-Z0-9]{36}`),
	regexp.MustCompile(`ghu_[a-zA-Z0-9]{36}`),
	regexp.MustCompile(`ghs_[a-zA-Z0-9]{36}`),
	regexp.MustCompile(`ghr_[a-zA-Z0-9]{36}`),
	// AWS
	regexp.MustCompile(`AKIA[A-Z0-9]{16}`),
	// Generic key=value patterns (case-insensitive)
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|bearer|authorization)\s*[:=]\s*["']?\S{8,}["']?`),

	// Connection strings (PostgreSQL, MySQL, MongoDB, Redis, AMQP)
	regexp.MustCompile(`(?i)(postgres|postgresql|mysql|mongodb|redis|amqp)://[^\s"']+`),
	// Generic KEY=/SECRET=/CREDENTIAL= env-var patterns (skip already-redacted [REDACTED] values)
	regexp.MustCompile(`(?i)[A-Z_]*(KEY|SECRET|CREDENTIAL|PRIVATE)[A-Z_]*\s*=\s*[^\[\s]{8,}`),
	// DSN/DATABASE_URL env vars (skip already-redacted values)
	regexp.MustCompile(`(?i)(DSN|DATABASE_URL|REDIS_URL|MONGO_URI)\s*=\s*[^\[\s]{8,}`),
	// VIRTUAL_* env vars (internal runtime config, should not leak)
	regexp.MustCompile(`(?i)VIRTUAL_[A-Z_]+\s*=\s*[^\[\s]{4,}`),
	// Long hex strings (64+ chars) — likely encryption keys, hashes, or secrets
	regexp.MustCompile(`[a-fA-F0-9]{64,}`),
}

const redactedPlaceholder = "[REDACTED]"
const serverIPPlaceholder = "[SERVER_IP]"

// dynamicScrubValues holds runtime-discovered values to scrub (e.g., server IPs).
var (
	dynamicScrubMu     sync.RWMutex
	dynamicScrubValues []string
)

// credentialScrubValues holds credential values to scrub (replaced with [REDACTED]).
// Separate from dynamicScrubValues which use [SERVER_IP] placeholder.
var (
	credentialScrubMu     sync.RWMutex
	credentialScrubValues []string
)

// AddDynamicScrubValues adds exact string values to the dynamic scrub list.
// Thread-safe. Deduplicates. Empty strings are ignored.
func AddDynamicScrubValues(values ...string) {
	dynamicScrubMu.Lock()
	defer dynamicScrubMu.Unlock()

	existing := make(map[string]bool, len(dynamicScrubValues))
	for _, v := range dynamicScrubValues {
		existing[v] = true
	}
	for _, v := range values {
		if v != "" && !existing[v] {
			dynamicScrubValues = append(dynamicScrubValues, v)
			existing[v] = true
		}
	}
}

// DynamicScrubCount returns the number of dynamic scrub values registered.
func DynamicScrubCount() int {
	dynamicScrubMu.RLock()
	defer dynamicScrubMu.RUnlock()
	return len(dynamicScrubValues)
}

// ResetDynamicScrubValues clears all dynamic scrub values. For testing only.
func ResetDynamicScrubValues() {
	dynamicScrubMu.Lock()
	defer dynamicScrubMu.Unlock()
	dynamicScrubValues = nil
}

// ResetCredentialScrubValues clears all credential scrub values.
// Called when credentials are updated to prevent stale values from accumulating.
func ResetCredentialScrubValues() {
	credentialScrubMu.Lock()
	defer credentialScrubMu.Unlock()
	credentialScrubValues = nil
}

// AddCredentialScrubValues adds credential values to the scrub list.
// These are replaced with [REDACTED] (not [SERVER_IP]).
// Thread-safe. Deduplicates. Empty/short strings ignored.
func AddCredentialScrubValues(values ...string) {
	credentialScrubMu.Lock()
	defer credentialScrubMu.Unlock()

	existing := make(map[string]bool, len(credentialScrubValues))
	for _, v := range credentialScrubValues {
		existing[v] = true
	}
	for _, v := range values {
		// Skip short values to avoid false-positive scrubbing
		if len(v) >= 6 && !existing[v] {
			credentialScrubValues = append(credentialScrubValues, v)
			existing[v] = true
		}
	}
}

// ScrubCredentials replaces known credential patterns and dynamic values in text.
func ScrubCredentials(text string) string {
	for _, pat := range credentialPatterns {
		text = pat.ReplaceAllString(text, redactedPlaceholder)
	}

	// Credential values (from credentialed exec)
	credentialScrubMu.RLock()
	credVals := credentialScrubValues
	credentialScrubMu.RUnlock()

	for _, v := range credVals {
		text = strings.ReplaceAll(text, v, redactedPlaceholder)
	}

	// Dynamic values (server IPs, etc.)
	dynamicScrubMu.RLock()
	vals := dynamicScrubValues
	dynamicScrubMu.RUnlock()

	for _, v := range vals {
		text = strings.ReplaceAll(text, v, serverIPPlaceholder)
	}

	return text
}
