package tools

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// completeCall captures one recorded request to the check-run completion
// endpoint so tests can assert on the full wire contract.
type completeCall struct {
	Method string
	Path   string
	Header string
	Body   []byte
}

func newCheckRunServer(t *testing.T, secret string, status int, beforeRespond func(c completeCall)) (*httptest.Server, *atomic.Pointer[completeCall]) {
	t.Helper()
	latest := &atomic.Pointer[completeCall]{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c := completeCall{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: r.Header.Get("X-Cartridge-Signature-256"),
			Body:   b,
		}
		latest.Store(&c)
		if secret != "" {
			want := canonicalSig(b, secret)
			if c.Header != want {
				http.Error(w, "signature mismatch", http.StatusUnauthorized)
				return
			}
		}
		if beforeRespond != nil {
			beforeRespond(c)
		}
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, latest
}

func canonicalSig(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newTool(t *testing.T, srvURL, secret string) *GitHubCompleteCheckTool {
	t.Helper()
	tool := NewGitHubCompleteCheckTool()
	tool.SetCompletionEndpoint(srvURL, secret)
	// Use the server's client so httptest's TLS/Close plumbing is wired up.
	tool.httpClient = &http.Client{}
	return tool
}

func withCheckRunMetadata(ctx context.Context, id any, repoSlug string) context.Context {
	md := map[string]any{
		"check_run_id": id,
		"repo_slug":    repoSlug,
	}
	return WithWakeMetadata(ctx, md)
}

func TestGitHubCompleteCheck_HappyPath(t *testing.T) {
	secret := "shared-secret"
	srv, latest := newCheckRunServer(t, secret, http.StatusOK, nil)
	tool := newTool(t, srv.URL, secret)

	ctx := withCheckRunMetadata(context.Background(), float64(42), "cartridge-gg/internal")
	res := tool.Execute(ctx, map[string]any{
		"conclusion": "success",
		"title":      "All clean",
		"summary":    "Zero findings.",
	})

	if res == nil || res.IsError {
		t.Fatalf("expected success, got error: %+v", res)
	}
	call := latest.Load()
	if call == nil {
		t.Fatal("no request recorded")
	}
	if call.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", call.Method)
	}
	if call.Path != "/agent/github/checks/42/complete" {
		t.Errorf("path = %q, want /agent/github/checks/42/complete", call.Path)
	}
	if call.Header == "" {
		t.Error("missing X-Cartridge-Signature-256 header")
	}

	var body completeRequest
	if err := json.Unmarshal(call.Body, &body); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if body.Owner != "cartridge-gg" || body.Repo != "internal" {
		t.Errorf("owner/repo wrong: %+v", body)
	}
	if body.Conclusion != "success" || body.Title != "All clean" || body.Summary != "Zero findings." {
		t.Errorf("top-level fields wrong: %+v", body)
	}
}

func TestGitHubCompleteCheck_MissingCheckRunID(t *testing.T) {
	srv, latest := newCheckRunServer(t, "s", http.StatusOK, nil)
	tool := newTool(t, srv.URL, "s")

	// Wake metadata present but check_run_id missing — simulates a non-GitHub wake.
	ctx := WithWakeMetadata(context.Background(), map[string]any{"event_type": "something_else"})
	res := tool.Execute(ctx, map[string]any{"conclusion": "success"})

	if res == nil || !res.IsError {
		t.Fatalf("expected error result, got: %+v", res)
	}
	if !strings.Contains(res.ForLLM, "check_run_id") {
		t.Errorf("error message should mention check_run_id, got: %q", res.ForLLM)
	}
	if latest.Load() != nil {
		t.Error("no HTTP call should have been made")
	}
}

func TestGitHubCompleteCheck_NoWakeMetadata(t *testing.T) {
	srv, latest := newCheckRunServer(t, "s", http.StatusOK, nil)
	tool := newTool(t, srv.URL, "s")

	// No WithWakeMetadata at all — tool is being invoked outside a wake.
	res := tool.Execute(context.Background(), map[string]any{"conclusion": "success"})

	if res == nil || !res.IsError {
		t.Fatalf("expected error, got: %+v", res)
	}
	if !strings.Contains(res.ForLLM, "wake metadata") {
		t.Errorf("error should reference wake metadata, got: %q", res.ForLLM)
	}
	if latest.Load() != nil {
		t.Error("no HTTP call should have been made")
	}
}

func TestGitHubCompleteCheck_InvalidConclusion(t *testing.T) {
	srv, latest := newCheckRunServer(t, "s", http.StatusOK, nil)
	tool := newTool(t, srv.URL, "s")

	ctx := withCheckRunMetadata(context.Background(), float64(1), "cartridge-gg/internal")
	res := tool.Execute(ctx, map[string]any{"conclusion": "made_up"})

	if res == nil || !res.IsError {
		t.Fatalf("expected error, got: %+v", res)
	}
	if !strings.Contains(res.ForLLM, "conclusion") {
		t.Errorf("error should reference conclusion, got: %q", res.ForLLM)
	}
	if latest.Load() != nil {
		t.Error("validation must happen before HTTP")
	}
}

func TestGitHubCompleteCheck_MissingConfig(t *testing.T) {
	// No SetCompletionEndpoint call.
	tool := NewGitHubCompleteCheckTool()
	ctx := withCheckRunMetadata(context.Background(), float64(1), "cartridge-gg/internal")
	res := tool.Execute(ctx, map[string]any{"conclusion": "success"})
	if res == nil || !res.IsError {
		t.Fatalf("expected error, got: %+v", res)
	}
	if !strings.Contains(res.ForLLM, "not configured") {
		t.Errorf("error should mention missing config, got: %q", res.ForLLM)
	}
}

func TestGitHubCompleteCheck_ServerError(t *testing.T) {
	// Always 500 — retry exhausts, tool surfaces the upstream error.
	srv, _ := newCheckRunServer(t, "s", http.StatusInternalServerError, nil)
	tool := newTool(t, srv.URL, "s")

	ctx := withCheckRunMetadata(context.Background(), float64(99), "cartridge-gg/internal")
	res := tool.Execute(ctx, map[string]any{"conclusion": "failure"})

	if res == nil || !res.IsError {
		t.Fatalf("expected error, got: %+v", res)
	}
	if !strings.Contains(res.ForLLM, "500") {
		t.Errorf("error should surface upstream status, got: %q", res.ForLLM)
	}
}

func TestGitHubCompleteCheck_SignatureContract(t *testing.T) {
	secret := "pin-this-secret"
	srv, latest := newCheckRunServer(t, secret, http.StatusOK, nil)
	tool := newTool(t, srv.URL, secret)

	ctx := withCheckRunMetadata(context.Background(), float64(7), "cartridge-gg/internal")
	res := tool.Execute(ctx, map[string]any{"conclusion": "neutral"})

	if res == nil || res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	call := latest.Load()
	if call == nil {
		t.Fatal("no request recorded")
	}
	// Independent recomputation — any drift in the signing algorithm trips this.
	want := canonicalSig(call.Body, secret)
	if call.Header != want {
		t.Errorf("signature mismatch:\n got  %s\n want %s", call.Header, want)
	}
}

// TestGitHubCompleteCheck_CheckRunIDFloat64Coercion pins the JSON decoder
// quirk: json.Unmarshal into `map[string]any` delivers numbers as float64,
// so a large check_run_id must not render as `1.234e+07` in the URL.
func TestGitHubCompleteCheck_CheckRunIDFloat64Coercion(t *testing.T) {
	secret := "s"
	srv, latest := newCheckRunServer(t, secret, http.StatusOK, nil)
	tool := newTool(t, srv.URL, secret)

	// 72,551,876,077 is a realistic GitHub check_run_id (the live one from PR #4381).
	const id int64 = 72551876077
	ctx := withCheckRunMetadata(context.Background(), float64(id), "cartridge-gg/internal")

	res := tool.Execute(ctx, map[string]any{"conclusion": "success"})
	if res == nil || res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	call := latest.Load()
	if call == nil {
		t.Fatal("no request recorded")
	}
	wantPath := "/agent/github/checks/72551876077/complete"
	if call.Path != wantPath {
		t.Errorf("path = %q, want %q (float64 coercion broken — URL has scientific notation or decimals)", call.Path, wantPath)
	}
}

// TestGitHubCompleteCheck_ParentCtxCanceled confirms the callback fires
// even when the agent's run ctx is already cancelled — prevents the
// "agent crash strands check run in_progress" failure mode.
func TestGitHubCompleteCheck_ParentCtxCanceled(t *testing.T) {
	secret := "s"
	srv, latest := newCheckRunServer(t, secret, http.StatusOK, nil)
	tool := newTool(t, srv.URL, secret)

	parent, cancel := context.WithCancel(withCheckRunMetadata(context.Background(), float64(5), "cartridge-gg/internal"))
	cancel() // cancel BEFORE calling Execute

	res := tool.Execute(parent, map[string]any{"conclusion": "success"})
	if res == nil || res.IsError {
		t.Fatalf("expected success under detached ctx, got: %+v", res)
	}
	if latest.Load() == nil {
		t.Fatal("POST was not made — tool incorrectly honoured the parent cancellation")
	}
}

func TestGitHubCompleteCheck_InvalidRepoSlug(t *testing.T) {
	srv, latest := newCheckRunServer(t, "s", http.StatusOK, nil)
	tool := newTool(t, srv.URL, "s")

	// Metadata has check_run_id but missing/malformed repo_slug.
	ctx := WithWakeMetadata(context.Background(), map[string]any{
		"check_run_id": float64(1),
		"repo_slug":    "just-one-segment",
	})
	res := tool.Execute(ctx, map[string]any{"conclusion": "success"})
	if res == nil || !res.IsError {
		t.Fatalf("expected error on malformed repo_slug, got: %+v", res)
	}
	if latest.Load() != nil {
		t.Error("no HTTP call should have been made")
	}
}

func TestGitHubCompleteCheck_ForwardsAnnotations(t *testing.T) {
	secret := "s"
	srv, latest := newCheckRunServer(t, secret, http.StatusOK, nil)
	tool := newTool(t, srv.URL, secret)

	ctx := withCheckRunMetadata(context.Background(), float64(11), "cartridge-gg/internal")
	anns := []any{
		map[string]any{
			"path":             "cmd/main.go",
			"start_line":       float64(42),
			"end_line":         float64(42),
			"annotation_level": "failure",
			"title":            "Nil dereference",
			"message":          "could panic if req is nil",
		},
		map[string]any{
			"path":             "internal/util.go",
			"start_line":       float64(10),
			"end_line":         float64(15),
			"annotation_level": "warning",
			"message":          "consider extracting helper",
		},
	}
	res := tool.Execute(ctx, map[string]any{
		"conclusion":  "action_required",
		"annotations": anns,
	})
	if res == nil || res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	call := latest.Load()
	if call == nil {
		t.Fatal("no request recorded")
	}
	var body completeRequest
	if err := json.Unmarshal(call.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Annotations) != 2 {
		t.Fatalf("annotations = %d, want 2: %+v", len(body.Annotations), body.Annotations)
	}
	if body.Annotations[0].Path != "cmd/main.go" || body.Annotations[0].StartLine != 42 || body.Annotations[0].EndLine != 42 {
		t.Errorf("first annotation wrong: %+v", body.Annotations[0])
	}
	if body.Annotations[0].AnnotationLevel != "failure" {
		t.Errorf("annotation level not forwarded: %q", body.Annotations[0].AnnotationLevel)
	}
	if body.Annotations[1].Title != "" {
		t.Errorf("optional title should be empty when not provided: %q", body.Annotations[1].Title)
	}
}

func TestReadCheckRunID(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  int64
		ok    bool
	}{
		{"float64 positive", float64(42), 42, true},
		{"float64 large", float64(72551876077), 72551876077, true},
		{"float64 zero", float64(0), 0, false},
		{"float64 negative", float64(-1), 0, false},
		{"float64 fractional", float64(1.5), 0, false},
		{"int", int(99), 99, true},
		{"int64", int64(10000), 10000, true},
		{"json.Number", json.Number("123"), 123, true},
		{"string numeric", "456", 456, true},
		{"string nonsense", "abc", 0, false},
		{"nil", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			md := map[string]any{"check_run_id": tc.input}
			got, ok := readCheckRunID(md)
			if ok != tc.ok || got != tc.want {
				t.Errorf("got (%d, %v), want (%d, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}

	if _, ok := readCheckRunID(map[string]any{}); ok {
		t.Error("missing key should return ok=false")
	}
}

func TestSplitRepoSlug(t *testing.T) {
	cases := []struct {
		slug        string
		owner, repo string
		ok          bool
	}{
		{"cartridge-gg/internal", "cartridge-gg", "internal", true},
		{"owner/repo", "owner", "repo", true},
		{"", "", "", false},
		{"no-slash", "", "", false},
		{"/leading", "", "", false},
		{"trailing/", "", "", false},
		{"a/b/c", "", "", false}, // multi-segment — reject
	}
	for _, tc := range cases {
		owner, repo, ok := splitRepoSlug(tc.slug)
		if owner != tc.owner || repo != tc.repo || ok != tc.ok {
			t.Errorf("splitRepoSlug(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.slug, owner, repo, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}
