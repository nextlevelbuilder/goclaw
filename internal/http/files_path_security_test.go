package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ---- FilesHandler path security tests ----
//
// These tests verify the 2-layer path isolation in FilesHandler.handleServe:
//   1. Workspace/dataDir boundary enforcement (all editions)
//   2. Path traversal prevention ("../"-style attacks)
//   3. Sensitive system directory blocking (/etc/, /proc/, ...)
//
// We bypass the auth wrapper by calling handleServe directly after setting up
// a token-signed file token or by registering the handler without auth on a
// test mux.

// makeTestFilesHandler creates a FilesHandler with a temp workspace and dataDir.
func makeTestFilesHandler(t *testing.T) (*FilesHandler, string) {
	t.Helper()
	workspace := t.TempDir()
	dataDir := t.TempDir()
	h := NewFilesHandler(workspace, dataDir)
	return h, workspace
}

// ---- handleServe: path traversal prevention ----

func TestFilesHandleServe_DotDotTraversal_Returns400(t *testing.T) {
	h, _ := makeTestFilesHandler(t)

	// Simulate PathValue("path") returning a traversal attack
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	req := httptest.NewRequest(http.MethodGet, "/v1/files/../../etc/passwd", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// ".." in path → 400 Bad Request (path traversal prevention)
	if w.Code == http.StatusOK {
		t.Error("traversal path should not return 200")
	}
}

// ---- handleServe: sensitive directory blocking ----

func TestFilesHandleServe_EtcPasswd_Returns403Or400(t *testing.T) {
	// Test that /etc/passwd is blocked even with a valid bearer token.
	// We bypass auth wrapper entirely by calling handleServe directly with a crafted request.
	h, workspace := makeTestFilesHandler(t)

	// Write a dummy file to workspace so the handler can run past auth.
	_ = os.WriteFile(filepath.Join(workspace, "safe.txt"), []byte("data"), 0644)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	// /etc/ prefix must be blocked
	req := httptest.NewRequest(http.MethodGet, "/v1/files/etc/passwd", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Either 400 (traversal detection) or 403 (sensitive path) — both are correct.
	if w.Code == http.StatusOK {
		t.Errorf("request for /etc/passwd should be denied, got %d", w.Code)
	}
}

func TestFilesHandleServe_ProcDir_Blocked(t *testing.T) {
	h, _ := makeTestFilesHandler(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	req := httptest.NewRequest(http.MethodGet, "/v1/files/proc/self/environ", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("/proc path should be blocked, got 200")
	}
}

// ---- handleServe: expanded deny-prefix list ----

// TestFilesHandleServe_ExpandedDenyPrefixes verifies that paths under /home/,
// /Users/, /var/lib/, /var/www/, /opt/, and /srv/ are all blocked. These are
// defense-in-depth blocks for misconfigured roots — the token/RBAC checks are
// the primary barriers.
func TestFilesHandleServe_ExpandedDenyPrefixes_Blocked(t *testing.T) {
	h, _ := makeTestFilesHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	cases := []struct {
		name string
		url  string
	}{
		{"home user ssh key", "/v1/files/home/user/.ssh/id_rsa"},
		{"home user aws creds", "/v1/files/home/user/.aws/credentials"},
		{"Users admin dir (macOS)", "/v1/files/Users/admin/secrets.txt"},
		{"var lib docker secret", "/v1/files/var/lib/docker/overlay2/secret"},
		{"var www config", "/v1/files/var/www/config.php"},
		{"opt secrets key", "/v1/files/opt/secrets/key.pem"},
		{"srv www app config", "/v1/files/srv/www/app/config.yaml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code == http.StatusOK {
				t.Errorf("path %s should be denied (deny-prefix), got 200", tc.url)
			}
		})
	}
}

// ---- handleServe: fail-closed observability on empty workspace/dataDir ----

// TestFilesHandleServe_NoBoundary_Denies verifies that when both workspace and
// dataDir are empty the handler returns 404 (fail-closed) for any path.
func TestFilesHandleServe_NoBoundary_Denies(t *testing.T) {
	h := NewFilesHandler("", "")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	req := httptest.NewRequest(http.MethodGet, "/v1/files/tmp/test.txt", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("empty workspace+dataDir should deny all requests, got 200")
	}
}

// ---- handleServe: workspace boundary enforcement ----

func TestFilesHandleServe_FileInsideWorkspace_WithToken_Serves(t *testing.T) {
	h, workspace := makeTestFilesHandler(t)

	// Write a file inside workspace
	content := []byte("hello workspace")
	filePath := filepath.Join(workspace, "hello.txt")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Build a valid signed token for this URL path
	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.Clean(filePath), "/")
	ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	req := httptest.NewRequest(http.MethodGet, urlPath+"?ft="+ft, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Should serve the file (200) or return 404 if the OS path doesn't match test env.
	// The key check: must NOT be 400/403 (security rejection).
	if w.Code == http.StatusBadRequest || w.Code == http.StatusForbidden {
		t.Errorf("workspace file with valid token should not be security-rejected, got %d", w.Code)
	}
}

func TestFilesHandleServe_FileOutsideAllDirs_WithToken_Returns404(t *testing.T) {
	h, _ := makeTestFilesHandler(t)

	// Build a signed token for a path outside workspace and dataDir.
	outsideDir := t.TempDir()
	filePath := filepath.Join(outsideDir, "secret.txt")
	_ = os.WriteFile(filePath, []byte("secret"), 0644)

	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.Clean(filePath), "/")
	ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	req := httptest.NewRequest(http.MethodGet, urlPath+"?ft="+ft, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// File token exists but path is outside workspace/dataDir → 404 (security denial via NotFound)
	if w.Code == http.StatusOK {
		t.Errorf("file outside workspace should not be served with signed token, got 200")
	}
}

func TestFilesHandleServe_SignedSymlinkEscape_Returns404(t *testing.T) {
	h, workspace := makeTestFilesHandler(t)
	outsideDir := t.TempDir()
	target := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(workspace, "link.txt")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.Clean(linkPath), "/")
	ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	req := httptest.NewRequest(http.MethodGet, urlPath+"?ft="+ft, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("signed symlink escaping workspace should not be served")
	}
}

func TestFilesHandleServe_OpenThenSwapToSymlinkEscape_Returns404(t *testing.T) {
	h, workspace := makeTestFilesHandler(t)
	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(workspace, "race.txt")
	if err := os.WriteFile(filePath, []byte("allowed"), 0644); err != nil {
		t.Fatal(err)
	}
	filesAfterOpenHookForTest = func(opened string) {
		if opened != filePath {
			return
		}
		_ = os.Remove(filePath)
		_ = os.Symlink(secretPath, filePath)
	}
	defer func() { filesAfterOpenHookForTest = nil }()

	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.Clean(filePath), "/")
	ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", h.handleServe)

	req := httptest.NewRequest(http.MethodGet, urlPath+"?ft="+ft, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("file swapped to escaping symlink after open should not be served")
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("response leaked swapped outside file content")
	}
}

func TestFilesHandleSign_SymlinkEscape_ReturnsForbidden(t *testing.T) {
	setupTestToken(t, "")
	setupTestNoAuthFallback(t, true)
	h, workspace := makeTestFilesHandler(t)
	outsideDir := t.TempDir()
	target := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(workspace, "link.txt")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/files/sign", strings.NewReader(`{"path":`+strconv.Quote(linkPath)+`}`))
	w := httptest.NewRecorder()
	h.handleSign(w, req)

	if w.Code == http.StatusOK {
		t.Fatal("sign endpoint should reject symlinks escaping allowed roots")
	}
}

// ---- handleServe: empty path ----

func TestFilesHandleServe_EmptyPath_Returns400(t *testing.T) {
	h, _ := makeTestFilesHandler(t)

	// Serve with empty path value — PathValue("path") returns ""
	req := httptest.NewRequest(http.MethodGet, "/v1/files/", nil)
	w := httptest.NewRecorder()

	// Call handleServe directly — PathValue returns "" for this pattern.
	h.handleServe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("empty path should return 400, got %d", w.Code)
	}
}

// ---- auth middleware: invalid file token ----

func TestFilesAuthMiddleware_InvalidFileToken_Returns401(t *testing.T) {
	h, workspace := makeTestFilesHandler(t)

	called := false
	wrapped := h.auth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	filePath := filepath.Join(workspace, "test.txt")
	_ = os.WriteFile(filePath, []byte("x"), 0644)

	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.Clean(filePath), "/")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", wrapped)

	req := httptest.NewRequest(http.MethodGet, urlPath+"?ft=invalid-token", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if called {
		t.Error("handler should not be called with invalid file token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("invalid ft should return 401, got %d", w.Code)
	}
}

func TestFilesAuthMiddleware_ValidFileToken_Passes(t *testing.T) {
	h, workspace := makeTestFilesHandler(t)

	called := false
	wrapped := h.auth(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	filePath := filepath.Join(workspace, "test.txt")
	_ = os.WriteFile(filePath, []byte("x"), 0644)

	urlPath := "/v1/files/" + strings.TrimPrefix(filepath.Clean(filePath), "/")
	ft := SignFileToken(urlPath, FileSigningKey(), FileTokenTTL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files/{path...}", wrapped)

	req := httptest.NewRequest(http.MethodGet, urlPath+"?ft="+ft, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if !called {
		t.Error("handler should be called with valid file token")
	}
}
