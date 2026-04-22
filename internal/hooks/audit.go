package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
)

// MaxErrorLen caps the visible `error` column in hook_executions (M2 mitigation).
// The full payload is preserved — encrypted — in `error_detail`.
const MaxErrorLen = 256

const redactedMarker = "[REDACTED]"

// piiPatterns covers the three shapes the audit path needs to strip before
// the error message is persisted in plaintext. Scoped locally rather than
// reusing tools.ScrubCredentials to keep the audit writer dependency-free.
var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`),
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]{8,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{8,}`),
}

// TruncateError shortens s to at most MaxErrorLen bytes, but never cuts a
// multi-byte rune mid-sequence. Return is guaranteed to be valid UTF-8.
func TruncateError(s string) string {
	if len(s) <= MaxErrorLen {
		return s
	}
	cut := MaxErrorLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// RedactPII replaces emails, Bearer tokens, and sk-* API keys with a fixed
// marker. The replacement is idempotent: the marker itself contains no PII
// patterns, so RedactPII(RedactPII(x)) == RedactPII(x).
func RedactPII(s string) string {
	for _, p := range piiPatterns {
		s = p.ReplaceAllString(s, redactedMarker)
	}
	return s
}

// AuditWriter wraps a HookStore and enforces the audit-hygiene rules for
// hook_executions writes: truncate+redact the visible error, encrypt the
// full error_detail before it hits the DB.
type AuditWriter struct {
	store  HookStore
	encKey string
}

// NewAuditWriter binds a HookStore to an AES-256-GCM key. When encKey is
// empty the writer runs in degraded mode (Lite/dev) and passes error_detail
// through as plaintext — operators trade PII exposure for post-mortem
// debuggability.
func NewAuditWriter(store HookStore, encKey string) *AuditWriter {
	return &AuditWriter{store: store, encKey: encKey}
}

// Log applies PII scrubbing + error truncation + error_detail encryption,
// then delegates the write to the underlying store.
func (w *AuditWriter) Log(ctx context.Context, exec HookExecution) error {
	if exec.Error != "" {
		exec.Error = TruncateError(RedactPII(exec.Error))
	}
	if len(exec.ErrorDetail) > 0 && w.encKey != "" {
		ct, err := crypto.Encrypt(string(exec.ErrorDetail), w.encKey)
		if err != nil {
			return err
		}
		exec.ErrorDetail = []byte(ct)
	}
	return w.store.WriteExecution(ctx, exec)
}

// CanonicalInputHash returns sha256 of (tool_name + canonical JSON of args)
// as a 64-char hex string. Map keys are sorted at every nesting level so the
// hash is stable across Go map iteration orders — this feeds hook_executions.input_hash
// and the dedup key upstream.
func CanonicalInputHash(toolName string, args map[string]any) (string, error) {
	h := sha256.New()
	h.Write([]byte(toolName))
	h.Write([]byte{0}) // delimiter: avoids "Read"+"{...}" colliding with "Rea"+"d{...}"
	if args == nil {
		// Hash the literal null so "no args" is distinct from "{}".
		h.Write([]byte("null"))
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	if err := writeCanonicalJSON(h, args); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeCanonicalJSON emits v with object keys sorted recursively. Only the
// shapes CEL/tool_input can produce are handled; anything else falls through
// to encoding/json's default ordering.
func writeCanonicalJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if _, err := w.Write([]byte{'{'}); err != nil {
			return err
		}
		for i, k := range keys {
			if i > 0 {
				if _, err := w.Write([]byte{','}); err != nil {
					return err
				}
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			if _, err := w.Write(kb); err != nil {
				return err
			}
			if _, err := w.Write([]byte{':'}); err != nil {
				return err
			}
			if err := writeCanonicalJSON(w, t[k]); err != nil {
				return err
			}
		}
		_, err := w.Write([]byte{'}'})
		return err
	case []any:
		if _, err := w.Write([]byte{'['}); err != nil {
			return err
		}
		for i, item := range t {
			if i > 0 {
				if _, err := w.Write([]byte{','}); err != nil {
					return err
				}
			}
			if err := writeCanonicalJSON(w, item); err != nil {
				return err
			}
		}
		_, err := w.Write([]byte{']'})
		return err
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
}
