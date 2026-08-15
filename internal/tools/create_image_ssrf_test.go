package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// downloadImageBytes fetches caller-supplied reference image URLs server-side, so
// it must reject SSRF targets and cap the response size. These tests lock that down.

func TestDownloadImageBytes_BlocksSSRFTargets(t *testing.T) {
	// Default: loopback/private/link-local are blocked by the SSRF guard.
	tool := NewCreateImageTool(nil)
	cases := []struct {
		name string
		url  string
	}{
		{"loopback", "http://127.0.0.1:9/x.png"},
		{"private", "http://10.0.0.5/x.png"},
		{"link_local_metadata", "http://169.254.169.254/latest/meta-data/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tool.downloadImageBytes(context.Background(), tc.url)
			if err == nil {
				t.Fatalf("expected SSRF guard to block %s, got nil error", tc.url)
			}
			if !strings.Contains(err.Error(), "invalid reference image URL") {
				t.Errorf("expected SSRF validation error, got: %v", err)
			}
		})
	}
}

func TestDownloadImageBytes_OversizedRejected(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Shrink the cap so we don't have to transfer 20 MB.
	orig := refImageMaxBytes
	refImageMaxBytes = 16
	defer func() { refImageMaxBytes = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("A", 1024))) // well over 16 bytes
	}))
	defer srv.Close()

	tool := NewCreateImageTool(nil)
	_, _, err := tool.downloadImageBytes(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected oversized reference image to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("expected size-limit error, got: %v", err)
	}
}

func TestDownloadImageBytes_HappyPath(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	want := []byte("\x89PNG\r\n\x1a\nfake-image-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(want)
	}))
	defer srv.Close()

	tool := NewCreateImageTool(nil)
	got, contentType, err := tool.downloadImageBytes(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("body = %q, want %q", got, want)
	}
	if contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", contentType)
	}
}

func TestDownloadImageBytes_RejectsRedirect(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// SafeClient never follows redirects: a 3xx is returned as-is and rejected
	// by the non-200 status check (prevents redirect-to-internal SSRF).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://169.254.169.254/", http.StatusFound)
	}))
	defer srv.Close()

	tool := NewCreateImageTool(nil)
	_, _, err := tool.downloadImageBytes(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected redirect to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "HTTP error") {
		t.Errorf("expected non-200 status error from unfollowed redirect, got: %v", err)
	}
}

func TestResolveReferenceImages_RejectsNonHTTPScheme(t *testing.T) {
	tool := NewCreateImageTool(nil)
	for _, bad := range []string{"file:///etc/passwd", "gopher://127.0.0.1/", "data:text/plain,hi"} {
		t.Run(bad, func(t *testing.T) {
			args := map[string]any{
				"ref_images": []any{
					map[string]any{"url": bad},
				},
			}
			_, err := tool.resolveReferenceImages(context.Background(), args)
			if err == nil {
				t.Fatalf("expected %q to be rejected as non-http(s), got nil error", bad)
			}
			if !strings.Contains(err.Error(), "must be http(s)") {
				t.Errorf("expected scheme error, got: %v", err)
			}
		})
	}
}
