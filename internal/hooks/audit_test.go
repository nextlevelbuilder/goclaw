package hooks_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// fakeExecStore captures HookExecution rows passed to WriteExecution.
// Other HookStore methods panic if called — tests must not rely on them.
type fakeExecStore struct {
	hooks.HookStore // embed nil interface; only WriteExecution is overridden
	mu              sync.Mutex
	execs           []hooks.HookExecution
}

func (s *fakeExecStore) WriteExecution(_ context.Context, e hooks.HookExecution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execs = append(s.execs, e)
	return nil
}

// testEncKey is 64 hex chars (32 raw bytes) — valid for crypto.DeriveKey.
const testEncKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestTruncateError_NoChangeUnderLimit(t *testing.T) {
	got := hooks.TruncateError("short message")
	if got != "short message" {
		t.Errorf("TruncateError returned %q, want no change", got)
	}
}

func TestTruncateError_CutsAtMaxLen(t *testing.T) {
	long := strings.Repeat("a", hooks.MaxErrorLen+200)
	got := hooks.TruncateError(long)
	if len(got) != hooks.MaxErrorLen {
		t.Errorf("len=%d, want %d", len(got), hooks.MaxErrorLen)
	}
}

func TestTruncateError_TreatsUnicodeByRune(t *testing.T) {
	// Each emoji is multi-byte; must not corrupt mid-codepoint.
	long := strings.Repeat("🔒", hooks.MaxErrorLen+10)
	got := hooks.TruncateError(long)
	// Result must decode as valid UTF-8 (no mid-codepoint cut).
	if !isValidUTF8(got) {
		t.Errorf("TruncateError produced invalid UTF-8 (mid-codepoint cut)")
	}
}

func TestRedactPII_MasksEmail(t *testing.T) {
	out := hooks.RedactPII("contact user@example.com for details")
	if strings.Contains(out, "user@example.com") {
		t.Errorf("email not redacted: %q", out)
	}
	if !strings.Contains(strings.ToUpper(out), "REDACTED") {
		t.Errorf("redaction marker missing: %q", out)
	}
}

func TestRedactPII_MasksBearerTokens(t *testing.T) {
	// Include two common shapes: Bearer <jwt> and sk-… API keys.
	raw := "auth: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig key=sk-abc123def456GHIJ7890xyzmore"
	out := hooks.RedactPII(raw)
	if strings.Contains(out, "eyJhbGciOiJIUzI1NiJ9.payload.sig") {
		t.Errorf("JWT not redacted: %q", out)
	}
	if strings.Contains(out, "sk-abc123def456GHIJ7890xyzmore") {
		t.Errorf("api key not redacted: %q", out)
	}
}

func TestRedactPII_IsIdempotent(t *testing.T) {
	once := hooks.RedactPII("error for user@example.com")
	twice := hooks.RedactPII(once)
	if once != twice {
		t.Errorf("RedactPII not idempotent: once=%q twice=%q", once, twice)
	}
}

// WriteExecution must truncate + redact the visible error column and encrypt
// the full error_detail before delegating to the underlying store.
func TestAuditWriter_WriteExecution_TruncatesAndEncrypts(t *testing.T) {
	fs := &fakeExecStore{}
	w := hooks.NewAuditWriter(fs, testEncKey)

	longError := strings.Repeat("x", hooks.MaxErrorLen+50) + " user@example.com"
	full := "stack trace:\nuser=user@example.com\ntoken=sk-abc123"
	err := w.Log(context.Background(), hooks.HookExecution{
		ID:          uuid.New(),
		Event:       hooks.EventPreToolUse,
		Decision:    hooks.DecisionBlock,
		Error:       longError,
		ErrorDetail: []byte(full),
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(fs.execs) != 1 {
		t.Fatalf("expected 1 row written, got %d", len(fs.execs))
	}
	row := fs.execs[0]

	// error col: truncated AND redacted.
	if len(row.Error) > hooks.MaxErrorLen {
		t.Errorf("error len %d > max %d", len(row.Error), hooks.MaxErrorLen)
	}
	if strings.Contains(row.Error, "user@example.com") {
		t.Errorf("error col still contains raw email: %q", row.Error)
	}

	// error_detail: must be AES-GCM encrypted (not plaintext).
	if !crypto.IsEncrypted(string(row.ErrorDetail)) {
		t.Errorf("error_detail not encrypted: %s", string(row.ErrorDetail))
	}
	// Round-trip decrypt returns the original plaintext.
	got, err := crypto.Decrypt(string(row.ErrorDetail), testEncKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != full {
		t.Errorf("decrypted = %q, want %q", got, full)
	}
}

// When encKey is empty, the writer skips encryption (degraded mode for Lite/dev).
// Plaintext passes through so operators can still debug crashes, but they must
// be aware PII will land in the audit log.
func TestAuditWriter_EmptyKey_PassesThroughPlaintext(t *testing.T) {
	fs := &fakeExecStore{}
	w := hooks.NewAuditWriter(fs, "")
	detail := "plain detail"
	if err := w.Log(context.Background(), hooks.HookExecution{
		ID:          uuid.New(),
		Event:       hooks.EventPreToolUse,
		Decision:    hooks.DecisionAllow,
		ErrorDetail: []byte(detail),
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if string(fs.execs[0].ErrorDetail) != detail {
		t.Errorf("empty-key path should pass through; got %q", fs.execs[0].ErrorDetail)
	}
}

func TestCanonicalInputHash_Deterministic(t *testing.T) {
	// Map insertion order must not affect hash.
	a := map[string]any{"path": "/etc/passwd", "mode": "r"}
	b := map[string]any{"mode": "r", "path": "/etc/passwd"}
	h1, err := hooks.CanonicalInputHash("Read", a)
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	h2, err := hooks.CanonicalInputHash("Read", b)
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash differs across key orderings: %s vs %s", h1, h2)
	}
	if len(h1) != sha256.Size*2 {
		t.Errorf("hash len=%d, want %d hex chars", len(h1), sha256.Size*2)
	}
	// Hex format sanity.
	if _, err := hex.DecodeString(h1); err != nil {
		t.Errorf("hash is not hex: %q", h1)
	}
}

func TestCanonicalInputHash_DifferentInputsDiffer(t *testing.T) {
	h1, _ := hooks.CanonicalInputHash("Read", map[string]any{"path": "/a"})
	h2, _ := hooks.CanonicalInputHash("Read", map[string]any{"path": "/b"})
	h3, _ := hooks.CanonicalInputHash("Write", map[string]any{"path": "/a"})
	if h1 == h2 {
		t.Error("hash collision across distinct paths")
	}
	if h1 == h3 {
		t.Error("hash collision across distinct tool names")
	}
}

// isValidUTF8 wraps the stdlib check; kept as a named helper so intent reads
// clearly at the call site inside the truncation test.
func isValidUTF8(s string) bool {
	return utf8.ValidString(s)
}
