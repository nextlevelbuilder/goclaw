package feishu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestResolveLarkDocs_FetchAndInject runs the full pipeline: URL extraction,
// fetch via mock Lark server, and injection into the returned text. Also
// verifies the cache dedupes the second call to the same doc.
func TestResolveLarkDocs_FetchAndInject(t *testing.T) {
	var fetchCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		if strings.Contains(r.URL.Path, "/documents/") && strings.HasSuffix(r.URL.Path, "/raw_content") {
			atomic.AddInt32(&fetchCount, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"content":"Specification: version 1.0\n\nOverview..."}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ch := &Channel{
		client:   NewLarkClient("app", "secret", srv.URL),
		docCache: newDocCache(larkDocCacheSize, larkDocCacheTTL),
	}

	input := "Please check https://test.larksuite.com/docx/DOC_ABC for the spec."
	out := ch.resolveLarkDocs(context.Background(), input)

	// Block should be appended after the original text.
	if !strings.HasPrefix(out, input) {
		t.Errorf("output should start with original text, got: %q", out)
	}
	if !strings.Contains(out, "[Lark Doc:") {
		t.Errorf("output missing doc block header, got: %q", out)
	}
	if !strings.Contains(out, "Specification: version 1.0") {
		t.Errorf("output missing doc content, got: %q", out)
	}
	if !strings.Contains(out, "[End of Lark Doc]") {
		t.Errorf("output missing doc block footer, got: %q", out)
	}

	// Second call — same URL, same doc ID — should hit cache, no new HTTP fetch.
	_ = ch.resolveLarkDocs(context.Background(), input)
	if got := atomic.LoadInt32(&fetchCount); got != 1 {
		t.Errorf("cache miss on second call: fetch count got %d, want 1", got)
	}
}

// TestResolveLarkDocs_AccessDenied_SoftFailure asserts that access-denied
// errors are rendered inline as a soft marker instead of crashing the
// pipeline. Also verifies soft-failure markers are NOT cached — otherwise a
// transient denial would be locked in for the TTL.
func TestResolveLarkDocs_AccessDenied_SoftFailure(t *testing.T) {
	var fetchCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		atomic.AddInt32(&fetchCount, 1)
		// Real docx v1 permission-denied code per Lark official docs.
		_, _ = w.Write([]byte(`{"code":1770032,"msg":"forbidden","data":{}}`))
	}))
	defer srv.Close()

	ch := &Channel{
		client:   NewLarkClient("app", "secret", srv.URL),
		docCache: newDocCache(4, time.Minute),
	}

	input := "check https://test.larksuite.com/docx/FORBIDDEN"
	out := ch.resolveLarkDocs(context.Background(), input)

	if !strings.Contains(out, "access denied") {
		t.Errorf("expected access-denied marker in output, got: %q", out)
	}
	// Second call should re-try (soft failure not cached so a permission grant becomes visible immediately).
	_ = ch.resolveLarkDocs(context.Background(), input)
	if got := atomic.LoadInt32(&fetchCount); got != 2 {
		t.Errorf("soft failure should not be cached: fetch count got %d, want 2", got)
	}
}

// TestResolveLarkDocs_NilCache verifies the nil-guard path so a future
// refactor that breaks the guard would fail a test rather than panic in
// production. Channels constructed outside the production New() factory
// (e.g., in tests) may have a nil docCache.
func TestResolveLarkDocs_NilCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"content":"unchanged"}}`))
	}))
	defer srv.Close()

	ch := &Channel{
		client:   NewLarkClient("app", "secret", srv.URL),
		docCache: nil,
	}
	input := "https://x.larksuite.com/docx/NOCACHE"
	out := ch.resolveLarkDocs(context.Background(), input)
	if !strings.Contains(out, "unchanged") {
		t.Errorf("nil cache path should still fetch content, got: %q", out)
	}
}

// TestResolveLarkDocs_NoURLs verifies early-exit on plain text.
func TestResolveLarkDocs_NoURLs(t *testing.T) {
	ch := &Channel{
		client:   NewLarkClient("a", "s", "http://unused"),
		docCache: newDocCache(4, time.Minute),
	}
	input := "just a normal message without any links"
	out := ch.resolveLarkDocs(context.Background(), input)
	if out != input {
		t.Errorf("no-op case should return input unchanged, got: %q", out)
	}
}

// TestResolveLarkDocs_MultipleDocs verifies concurrent fetches are all
// represented in the output in the order they appear in the input.
func TestResolveLarkDocs_MultipleDocs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		// Return content that echoes the doc ID so we can assert ordering.
		docID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/open-apis/docx/v1/documents/"), "/raw_content")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"code":0,"msg":"","data":{"content":"content_of_%s"}}`, docID)
	}))
	defer srv.Close()

	ch := &Channel{
		client:   NewLarkClient("app", "secret", srv.URL),
		docCache: newDocCache(4, time.Minute),
	}

	input := "first https://a.larksuite.com/docx/ONE and second https://b.feishu.cn/docx/TWO"
	out := ch.resolveLarkDocs(context.Background(), input)

	idxOne := strings.Index(out, "content_of_ONE")
	idxTwo := strings.Index(out, "content_of_TWO")
	if idxOne < 0 || idxTwo < 0 {
		t.Fatalf("missing doc content in output: %q", out)
	}
	if idxOne > idxTwo {
		t.Errorf("docs should appear in input order: ONE at %d, TWO at %d", idxOne, idxTwo)
	}
}

// TestResolveLarkDocs_PerMessageCap verifies the spam guard: when a message
// contains more doc URLs than larkDocMaxPerMessage, the extras are dropped
// with a visible "skipped" notice and only the first N are fetched.
func TestResolveLarkDocs_PerMessageCap(t *testing.T) {
	var fetchCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		atomic.AddInt32(&fetchCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"content":"x"}}`))
	}))
	defer srv.Close()

	ch := &Channel{
		client:   NewLarkClient("app", "secret", srv.URL),
		docCache: newDocCache(larkDocCacheSize, larkDocCacheTTL),
	}

	// Build an input with N+5 distinct URLs (exceeds the cap).
	extra := 5
	var parts []string
	for i := 0; i < larkDocMaxPerMessage+extra; i++ {
		parts = append(parts, fmt.Sprintf("https://x.larksuite.com/docx/DOC%d", i))
	}
	input := strings.Join(parts, " ")

	out := ch.resolveLarkDocs(context.Background(), input)

	if got := atomic.LoadInt32(&fetchCount); got != int32(larkDocMaxPerMessage) {
		t.Errorf("fetch count: got %d, want %d (capped)", got, larkDocMaxPerMessage)
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("expected skipped notice in output")
	}
	if !strings.Contains(out, fmt.Sprintf("%d more Lark doc URLs", extra)) {
		t.Errorf("expected skipped count %d in notice, got: %q", extra, out)
	}
}

// TestResolveLarkDocs_ContentTruncation verifies large docs are capped at
// larkDocMaxContentLen with a truncation notice.
func TestResolveLarkDocs_ContentTruncation(t *testing.T) {
	bigBody := strings.Repeat("A", larkDocMaxContentLen+500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"code":0,"msg":"","data":{"content":%q}}`, bigBody)
	}))
	defer srv.Close()

	ch := &Channel{
		client:   NewLarkClient("app", "secret", srv.URL),
		docCache: newDocCache(4, time.Minute),
	}

	input := "big doc: https://test.larksuite.com/docx/BIGDOC"
	out := ch.resolveLarkDocs(context.Background(), input)

	if !strings.Contains(out, "[... truncated, original size") {
		preview := out
		if len(preview) > 500 {
			preview = preview[:500]
		}
		t.Errorf("expected truncation notice, got: %q", preview)
	}
	// Full body must NOT appear in full.
	if strings.Contains(out, bigBody) {
		t.Errorf("untruncated body should not appear in output")
	}
}
