// Phase 6 — audit log shape tests for `security.system_env_injection`.
//
// Pins the JSON field schema operators grep for in their SIEM. A change here
// breaks log-search dashboards in production, so the test asserts:
//   - exact event name (`msg=security.system_env_injection`)
//   - presence of every documented field
//   - host scope hashed (not plaintext) — PII safety
//   - env keys present, env values NOT present
//   - PAT/SSH key bytes NOT present anywhere in the captured buffer
package tools

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

// capturedAudit runs fn with a JSON slog handler capturing into buf, then
// restores the previous default logger. Returns parsed JSON records.
func capturedAudit(t *testing.T, fn func()) ([]map[string]any, string) {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	fn()

	raw := buf.String()
	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimRight(raw, "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line not JSON: %q (%v)", line, err)
		}
		records = append(records, rec)
	}
	return records, raw
}

// 1. Audit log shape — PAT path emits the documented field schema.
func TestEmitSystemEnvInjectionAudit_PAT(t *testing.T) {
	scope := "github.com"
	// Use sentinel values long enough that substring match in the captured
	// log buffer is meaningful (short values like "1" would false-match digits
	// in the timestamp).
	patToken := "ghp_SENTINEL_PAT_VALUE_4242424242424242"
	inj := &Injection{
		Env: map[string]string{
			"GIT_CONFIG_COUNT":   "SENTINEL_COUNT_VALUE",
			"GIT_CONFIG_KEY_0":   "http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0": "AUTHORIZATION: basic " + patToken,
		},
		// PAT path uses env only, no argv mutation.
		ArgvPrefix: nil,
	}

	records, raw := capturedAudit(t, func() {
		emitSystemEnvInjectionAudit("git", "git", "user-42", inj, &scope)
	})

	if len(records) != 1 {
		t.Fatalf("expected exactly 1 audit record, got %d (raw=%q)", len(records), raw)
	}
	rec := records[0]
	if rec["msg"] != "security.system_env_injection" {
		t.Fatalf("msg=%v, want security.system_env_injection", rec["msg"])
	}
	if rec["adapter"] != "git" {
		t.Errorf("adapter=%v, want git", rec["adapter"])
	}
	if rec["binary"] != "git" {
		t.Errorf("binary=%v, want git", rec["binary"])
	}
	if rec["user_id"] != "user-42" {
		t.Errorf("user_id=%v, want user-42", rec["user_id"])
	}
	if rec["argv_prefix_len"] != float64(0) {
		t.Errorf("argv_prefix_len=%v, want 0", rec["argv_prefix_len"])
	}

	keys, _ := rec["env_keys"].([]any)
	want := []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"}
	if len(keys) != len(want) {
		t.Fatalf("env_keys=%v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Errorf("env_keys[%d]=%v, want %s (must be sorted)", i, keys[i], k)
		}
	}

	hash, _ := rec["host_scope_hash"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(hash) {
		t.Errorf("host_scope_hash=%q, want 8 hex chars", hash)
	}
	if hash == scope {
		t.Errorf("host_scope_hash leaked plaintext hostname")
	}

	// Plaintext hostname must NOT appear anywhere in the raw log output.
	if strings.Contains(raw, scope) {
		t.Errorf("plaintext hostname %q leaked into audit log: %s", scope, raw)
	}
	// Env VALUES must NOT appear in the audit log (only NAMES go in env_keys).
	// Especially the PAT-like content: AC6 redaction guarantee.
	for _, v := range inj.Env {
		if strings.Contains(raw, v) {
			t.Errorf("env value %q leaked into audit log: %s", v, raw)
		}
	}
	if strings.Contains(raw, patToken) {
		t.Errorf("PAT token leaked into audit log: %s", raw)
	}
}

// 2. Audit log shape — SSH path: env_keys=[GIT_SSH_COMMAND], hash present,
// no PEM bytes in the log.
func TestEmitSystemEnvInjectionAudit_SSH(t *testing.T) {
	scope := "gitlab.example.com:2222"
	pemBody := "-----BEGIN OPENSSH PRIVATE KEY-----\nfakebody\n-----END OPENSSH PRIVATE KEY-----"
	inj := &Injection{
		Env: map[string]string{
			"GIT_SSH_COMMAND": "ssh -i /tmp/goclaw-gitkey-xyz -o StrictHostKeyChecking=accept-new",
		},
	}

	records, raw := capturedAudit(t, func() {
		emitSystemEnvInjectionAudit("git", "git", "u1", inj, &scope)
	})

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	rec := records[0]
	keys, _ := rec["env_keys"].([]any)
	if len(keys) != 1 || keys[0] != "GIT_SSH_COMMAND" {
		t.Errorf("env_keys=%v, want [GIT_SSH_COMMAND]", keys)
	}

	hash, _ := rec["host_scope_hash"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(hash) {
		t.Errorf("host_scope_hash=%q, want 8 hex chars", hash)
	}

	// PEM contents and plaintext hostname must NOT leak.
	if strings.Contains(raw, "BEGIN OPENSSH") || strings.Contains(raw, pemBody) {
		t.Errorf("PEM content leaked into audit log: %s", raw)
	}
	if strings.Contains(raw, "gitlab.example.com") {
		t.Errorf("plaintext hostname leaked into audit log: %s", raw)
	}
}

// 3. Nil injection: emitter is a no-op (audit only fires on actual injection).
func TestEmitSystemEnvInjectionAudit_NilInjection(t *testing.T) {
	records, _ := capturedAudit(t, func() {
		emitSystemEnvInjectionAudit("git", "git", "u1", nil, nil)
	})
	if len(records) != 0 {
		t.Fatalf("expected 0 records for nil injection, got %d", len(records))
	}
}

// 4. Nil host scope (passthrough adapter): hash = "none", not crashy.
func TestEmitSystemEnvInjectionAudit_NilHostScope(t *testing.T) {
	inj := &Injection{Env: map[string]string{"FOO": "bar"}}
	records, _ := capturedAudit(t, func() {
		emitSystemEnvInjectionAudit("passthrough", "gh", "u1", inj, nil)
	})
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0]["host_scope_hash"] != "none" {
		t.Errorf("host_scope_hash=%v, want none", records[0]["host_scope_hash"])
	}
}
