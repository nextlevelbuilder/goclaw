package http

import (
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: file paths containing spaces (e.g. C:\Users\Bruce Le\...) must be
// fully signed and downloadable. Previously fileURLRe stopped at whitespace, so
// only "/v1/files/C:\Users\Bruce" got signed and the rest of the path was left
// without any token -> download returned {"error":"unauthorized"}.
func TestSignFileURLsPathWithSpaces(t *testing.T) {
	secret := "test-secret"
	for _, ext := range []string{".md", ".txt"} {
		raw := `C:\Users\Bruce Le\system\brief file` + ext
		content := "[brief](http://127.0.0.1:18800/v1/files/" + raw + ")"

		signed := SignFileURLs(content, secret)

		if strings.Contains(signed, " ") {
			t.Fatalf("%s: signed content still contains raw space: %q", ext, signed)
		}
		if !strings.Contains(signed, "%20Le") {
			t.Fatalf("%s: expected percent-encoded space, got %q", ext, signed)
		}
		ft := extractFT(t, signed)
		if ft == "" {
			t.Fatalf("%s: no ft token in %q", ext, signed)
		}
		expected := expectedSignedPath(t, raw)
		if !VerifyFileToken(ft, expected, secret) {
			t.Fatalf("%s: token does not verify against %q", ext, expected)
		}
	}
}

// A signed URL must actually serve a file whose absolute path contains a space.
func TestSignedURLServesFileWithSpaces(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "Bruce Le", "system")
	if err := os.MkdirAll(spaced, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, ext := range []string{".md", ".txt"} {
		name := "brief" + ext
		full := filepath.Join(spaced, name)
		if err := os.WriteFile(full, []byte("hello "+ext), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		h := NewFilesHandler(dir, dir)
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)

		urlPath := fileURLPath(full)
		ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)
		target := escapedFileURL(urlPath) + "?ft=" + ft

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", target, nil)
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d body=%q (url=%s)", ext, rec.Code, rec.Body.String(), target)
		}
		if got := rec.Body.String(); got != "hello "+ext {
			t.Fatalf("%s: unexpected body %q", ext, got)
		}
	}
}

func extractFT(t *testing.T, signed string) string {
	t.Helper()
	loc := strings.Index(signed, "?ft=")
	if loc < 0 {
		return ""
	}
	fields := strings.FieldsFunc(signed[loc+4:], func(r rune) bool {
		return r == ')' || r == '"' || r == ' ' || r == '\n'
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// expectedSignedPath mirrors how handleServe rebuilds the signed path:
// "/v1/files/" + decoded PathValue, with OS separators normalized to "/".
func expectedSignedPath(t *testing.T, rawWindowsPath string) string {
	t.Helper()
	slashed := strings.ReplaceAll(rawWindowsPath, `\`, "/")
	p := "/v1/files/" + slashed
	decoded, err := url.PathUnescape(p)
	if err != nil {
		t.Fatalf("unescape: %v", err)
	}
	return decoded
}

// Chat links carry Windows backslashes (raw or percent-encoded as %5C) while
// SignFileURLs signs the '/'-separated form; the handler must accept both.
func TestSignedURLBackslashEncodedPathServes(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "Bruce Le", "system", "brief.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	h := NewFilesHandler(dir, dir)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	signed := SignFileURLs("[b](http://127.0.0.1:18800/v1/files/"+full+")", FileSigningKey())
	rel := signed[strings.Index(signed, "/v1/files/"):strings.LastIndex(signed, ")")]
	q := strings.Index(rel, "?")
	decoded, err := url.PathUnescape(strings.TrimPrefix(rel[:q], "/v1/files/"))
	if err != nil {
		t.Fatalf("unescape: %v", err)
	}
	for name, target := range map[string]string{
		"as-signed":         rel,
		"fully-encoded-seg": "/v1/files/" + url.PathEscape(decoded) + rel[q:],
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "hi" {
			t.Fatalf("%s: got %d %q (url=%s)", name, rec.Code, rec.Body.String(), target)
		}
	}
}

// Media attachments (screenshots, write_file deliver) are signed via
// SignMediaPath from an OS-native absolute path; the resulting URL must serve.
func TestSignMediaPathWithSpacesServes(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "Bruce Le", "shot.txt")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mux := http.NewServeMux()
	NewFilesHandler(dir, dir).RegisterRoutes(mux)

	u := SignMediaPath(full, FileSigningKey())
	if strings.ContainsAny(strings.SplitN(u, "?", 2)[0], " \\") {
		t.Fatalf("url not escaped: %s", u)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q (url=%s)", rec.Code, rec.Body.String(), u)
	}
}

// Re-signing a media path must be idempotent: a path that already carries
// percent-encoding (including stacked layers from earlier bugs) must resolve to
// the same URL and token-signed path as the raw path.
func TestSignMediaPathIdempotentEncoding(t *testing.T) {
	secret := "test-secret"
	raw := `C:\Users\Bruce Le\system\screenshots\shot.png`
	want := SignMediaPath(raw, secret)
	if want == "" || strings.Contains(want, "%25") {
		t.Fatalf("unexpected base URL %q", want)
	}
	wantPath, _, _ := strings.Cut(want, "?ft=")

	inputs := []string{
		wantPath,
		`C:\Users\Bruce%20Le\system\screenshots\shot.png`,
		`C:\Users\Bruce%2520Le\system\screenshots\shot.png`,
		`C:\Users\Bruce%2525252525252520Le\system\screenshots\shot.png`,
	}
	for _, in := range inputs {
		got := SignMediaPath(in, secret)
		gotPath, ft, _ := strings.Cut(got, "?ft=")
		if gotPath != wantPath {
			t.Fatalf("input %q: got %q want %q", in, gotPath, wantPath)
		}
		if !VerifyFileToken(ft, "/v1/files/C:/Users/Bruce Le/system/screenshots/shot.png", secret) {
			t.Fatalf("input %q: token does not verify against decoded path", in)
		}
		// Feeding the output back in must not change it.
		if again, _, _ := strings.Cut(SignMediaPath(gotPath, secret), "?ft="); again != wantPath {
			t.Fatalf("not idempotent for %q: %q", in, again)
		}
	}
}

// Signing a history message must not write through to the MediaRefs backing
// array shared with the session cache (store histories are shallow copies).
func TestSignMessageFileURLsDoesNotMutateSharedRefs(t *testing.T) {
	secret := "test-secret"
	rawPath := `C:\Users\Bruce Le\ws\shot.png`
	cached := []providers.Message{{
		Role:      "assistant",
		MediaRefs: []providers.MediaRef{{ID: "a", Kind: "image", Path: rawPath}},
	}}
	history := make([]providers.Message, len(cached))
	copy(history, cached) // what GetHistory returns

	for range 3 {
		SignMessageFileURLs(&history[0], secret)
	}

	if got := cached[0].MediaRefs[0].Path; got != rawPath {
		t.Fatalf("cached ref was mutated: %q", got)
	}
	if got := history[0].MediaRefs[0].Path; !strings.HasPrefix(got, "/v1/files/") || !strings.Contains(got, "?ft=") {
		t.Fatalf("delivered ref not signed: %q", got)
	}
}
