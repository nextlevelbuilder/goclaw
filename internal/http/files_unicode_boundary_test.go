package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func serveSigned(t *testing.T, h *FilesHandler, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}

// A signed media link must serve files whose directory or file name contains a
// space and non-ASCII (Vietnamese) characters, including a link that has been
// re-encoded several times on its way through stored session history.
func TestSignMediaPathServesUnicodeAndSpaces(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Nguyễn Văn A", "teams", "0193a5b0-7000-7000-8000-000000000001", "system")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	full := filepath.Join(dir, "báo cáo a b.md")
	if err := os.WriteFile(full, []byte("nội dung"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h := NewFilesHandler(root, root)

	first := SignMediaPath(full, FileSigningKey())
	if first == "" {
		t.Fatal("SignMediaPath returned empty URL")
	}
	// Simulate history that was persisted after extra percent-encoding layers.
	reencoded := first
	for range 3 {
		reencoded = escapePathOnce(reencoded)
	}
	reencoded = SignMediaPath(reencoded, FileSigningKey())

	for name, target := range map[string]string{"signed once": first, "signed again from encoded ref": reencoded} {
		rec := serveSigned(t, h, target)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d body=%q url=%s", name, rec.Code, rec.Body.String(), target)
		}
		if rec.Body.String() != "nội dung" {
			t.Fatalf("%s: unexpected body %q", name, rec.Body.String())
		}
	}
}

// escapePathOnce percent-encodes the whole signed URL one more time, which is
// what a client round trip used to do to stored media refs.
func escapePathOnce(signed string) string {
	var b []byte
	for i := 0; i < len(signed); i++ {
		if signed[i] == '%' {
			b = append(b, '%', '2', '5')
			continue
		}
		b = append(b, signed[i])
	}
	return string(b)
}

// A correctly signed link to a file outside the configured workspace and data
// directory is still refused: the boundary is enforced independently of the
// signature, so agents writing elsewhere on disk cannot be downloaded.
func TestSignedURLOutsideAllowedRootsIsRefused(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	full := filepath.Join(outside, "Bruce Le", "system", "brief.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h := NewFilesHandler(allowed, allowed)

	rec := serveSigned(t, h, SignMediaPath(full, FileSigningKey()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a path outside the allowed roots", rec.Code)
	}
}
