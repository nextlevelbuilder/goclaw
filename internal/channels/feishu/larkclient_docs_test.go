package feishu

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetDocRawContent_Success verifies happy-path fetch of a Lark doc.
func TestGetDocRawContent_Success(t *testing.T) {
	const docID = "DOCABC123"
	var gotPath, gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"content":"Hello from Lark doc\n\nSecond paragraph."}}`))
	}))
	defer srv.Close()

	client := NewLarkClient("app", "secret", srv.URL)
	content, err := client.GetDocRawContent(context.Background(), docID)
	if err != nil {
		t.Fatalf("GetDocRawContent error: %v", err)
	}

	wantPath := "/open-apis/docx/v1/documents/" + docID + "/raw_content"
	if gotPath != wantPath {
		t.Errorf("path: got %q, want %q", gotPath, wantPath)
	}
	if gotAuth == "" {
		t.Errorf("missing Authorization header")
	}
	if content != "Hello from Lark doc\n\nSecond paragraph." {
		t.Errorf("content: got %q, want full doc text", content)
	}
}

// TestGetDocRawContent_AccessDenied asserts that permission failures surface as
// ErrDocAccessDenied so the caller can render a soft failure marker rather
// than crashing the pipeline. Uses Lark docx v1 error code 1770032 (HTTP 403).
func TestGetDocRawContent_AccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1770032,"msg":"forbidden","data":{}}`))
	}))
	defer srv.Close()

	client := NewLarkClient("app", "secret", srv.URL)
	_, err := client.GetDocRawContent(context.Background(), "DOC_NO_PERM")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDocAccessDenied) {
		t.Errorf("expected ErrDocAccessDenied, got %v", err)
	}
}

// TestGetDocRawContent_NotFound maps the docx 404 code (1770002) to
// ErrDocAccessDenied for uniform soft-failure handling (either way the bot
// has no content to inject).
func TestGetDocRawContent_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1770002,"msg":"document not found","data":{}}`))
	}))
	defer srv.Close()

	client := NewLarkClient("app", "secret", srv.URL)
	_, err := client.GetDocRawContent(context.Background(), "DOC_MISSING")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDocAccessDenied) {
		t.Errorf("expected ErrDocAccessDenied, got %v", err)
	}
}

// TestGetDocRawContent_LegacyPermCode preserves coverage for the legacy
// generic permission denied code (99991672) still kept in the whitelist as
// a belt-and-suspenders fallback for tenants that surface it.
func TestGetDocRawContent_LegacyPermCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":99991672,"msg":"no permission","data":{}}`))
	}))
	defer srv.Close()

	client := NewLarkClient("app", "secret", srv.URL)
	_, err := client.GetDocRawContent(context.Background(), "DOC_LEGACY")
	if !errors.Is(err, ErrDocAccessDenied) {
		t.Errorf("expected ErrDocAccessDenied, got %v", err)
	}
}

// TestGetDocRawContent_GenericError verifies that unknown error codes surface
// as a normal error (not the sentinel) so callers log them differently.
func TestGetDocRawContent_GenericError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":500001,"msg":"internal error","data":{}}`))
	}))
	defer srv.Close()

	client := NewLarkClient("app", "secret", srv.URL)
	_, err := client.GetDocRawContent(context.Background(), "DOC")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrDocAccessDenied) {
		t.Errorf("unknown codes should NOT map to ErrDocAccessDenied, got %v", err)
	}
}

// TestGetDocRawContent_EmptyDocID guards against accidental empty-ID requests.
func TestGetDocRawContent_EmptyDocID(t *testing.T) {
	client := NewLarkClient("a", "s", "http://unused")
	if _, err := client.GetDocRawContent(context.Background(), ""); err == nil {
		t.Fatal("expected error on empty doc ID")
	}
}
