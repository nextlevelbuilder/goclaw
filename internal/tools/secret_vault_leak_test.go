package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// captureLogs routes slog to a buffer for the duration of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	var mu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// echoEveryEncoding reflects the Authorization header the way a hostile or
// sloppy server might: raw, URL-encoded, base64, JSON-escaped, in a header
// and in the body.
func echoEveryEncoding(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	tok := strings.TrimPrefix(auth, "Bearer ")
	asJSON, _ := json.Marshal(map[string]string{"token": tok})
	w.Header().Set("X-Echo", tok)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, "raw=%s url=%s b64=%s json=%s",
		tok, url.QueryEscape(tok), base64.StdEncoding.EncodeToString([]byte(tok)), asJSON)
}

func tempEntries(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Skipf("cannot read temp dir: %v", err)
	}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

func fileContains(path, needle string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(b, []byte(needle))
}

// A full use of a secret must leave the value nowhere: not in anything the
// model sees, not in trace text, not in logs, and not in files on disk.
func TestSecretVaultLeak_NoSinkSeesTheValue(t *testing.T) {
	logs := captureLogs(t)
	before := tempEntries(t)

	e := newHTTPEnv(t, []string{"127.0.0.1"}, echoEveryEncoding)
	redact := tracing.RedactTextWith
	rt := secretvault.RuntimeFrom(e.ctx)
	traceText := rt.Redactor.Func()

	res := e.tool.Execute(e.ctx, e.args(map[string]any{
		"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"},
	}))
	if res.IsError {
		t.Fatalf("error: %s", res.ForLLM)
	}

	// Everything a sink could receive after a tool run: the three result
	// fields, and the argument map as it would be traced.
	traced, _ := json.Marshal(e.args(map[string]any{
		"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"},
	}))
	sinks := map[string]string{
		"ForLLM":      res.ForLLM,
		"ForUser":     res.ForUser,
		"Deliverable": res.Deliverable,
		"trace args":  redact(traceText, string(traced)),
		"logs":        logs.String(),
	}
	for name, v := range sinks {
		for _, variant := range secretvault.Variants(httpTestToken) {
			if strings.Contains(v, variant) {
				t.Errorf("%s contains a variant of the secret (%q)", name, variant)
			}
		}
	}
	if !strings.Contains(res.ForLLM, secretvault.Mask) {
		t.Errorf("expected the reflected value to be masked:\n%s", res.ForLLM)
	}
	if strings.Contains(string(traced), httpTestToken) {
		t.Error("the arguments must carry the placeholder, never the value")
	}

	for name := range tempEntries(t) {
		if before[name] {
			continue
		}
		p := filepath.Join(os.TempDir(), name)
		if fileContains(p, httpTestToken) {
			t.Errorf("new temp file %s contains the secret", p)
		}
	}
}

// Run 1 sends the secret to an allowed server that keeps it. Run 2 asks the
// server for it back: a fresh runtime must still redact what it reads.
func TestSecretVaultLeak_ExfiltrationReadBackIsRedacted(t *testing.T) {
	var stored string
	e := newHTTPEnv(t, []string{"127.0.0.1"}, nil)
	e.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			stored = r.Header.Get("Authorization")
			fmt.Fprint(w, "stored")
			return
		}
		fmt.Fprintf(w, "previous request carried: %s", stored)
	}

	if res := e.tool.Execute(e.ctx, e.args(map[string]any{
		"method":  "POST",
		"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"},
	})); res.IsError {
		t.Fatalf("run 1: %s", res.ForLLM)
	}
	if !strings.Contains(stored, httpTestToken) {
		t.Fatal("server did not receive the secret")
	}

	// Run 2: new runtime, no placeholder in the request.
	rt2 := secretvault.NewRuntime(e.st, uuid.New(), "user1")
	ctx2 := secretvault.WithRuntime(context.Background(), rt2)
	if err := rt2.Preload(ctx2); err != nil {
		t.Fatal(err)
	}
	res := e.tool.Execute(ctx2, e.args(nil))
	if res.IsError {
		t.Fatalf("run 2: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, httpTestToken) {
		t.Fatalf("read-back leaks the secret:\n%s", res.ForLLM)
	}
}

// Tools reached outside the agent loop (for example /tools/invoke) have no
// vault runtime: a placeholder must fail closed and never reach the network.
func TestSecretVaultLeak_NoRuntimeFailsClosedWithoutNetwork(t *testing.T) {
	e := newHTTPEnv(t, []string{"127.0.0.1"}, echoAuth)
	res := e.tool.Execute(context.Background(), e.args(map[string]any{
		"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"},
	}))
	if !res.IsError {
		t.Fatalf("expected an error, got:\n%s", res.ForLLM)
	}
	if e.hits.Load() != 0 {
		t.Fatal("a request without a vault runtime must not be sent")
	}
	if strings.Contains(res.ForLLM, httpTestToken) {
		t.Fatal("error text leaks the secret")
	}
}

// Credential kinds are not interchangeable: a login cannot be used as an API
// key and an API key cannot be used as a login.
func TestSecretVaultLeak_KindMismatchIsRefusedAndNeverSent(t *testing.T) {
	e := newHTTPEnv(t, []string{"127.0.0.1"}, echoAuth)
	rt := secretvault.RuntimeFrom(e.ctx)
	if _, err := rt.Use(e.ctx, "gh", "login", "127.0.0.1"); err == nil {
		t.Error("an api_key must not be usable as a login (fill_secret)")
	}
	if _, err := rt.Use(e.ctx, "bank", "api_key", "127.0.0.1"); err == nil {
		t.Error("a login must not be usable as an api_key (http_request)")
	}
	res := e.tool.Execute(e.ctx, e.args(map[string]any{
		"headers": map[string]any{"Authorization": "Bearer {{secret:bank}}"},
	}))
	if !res.IsError || e.hits.Load() != 0 {
		t.Fatalf("login in http_request must be refused before sending (hits=%d)", e.hits.Load())
	}
}
