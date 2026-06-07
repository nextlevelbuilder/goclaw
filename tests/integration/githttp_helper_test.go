//go:build integration

package integration

// Local git http-backend test server used by Phase 3 PAT integration tests.
//
// Why this exists:
//   - The git adapter promises PAT injection via GIT_CONFIG_COUNT/KEY_0/VALUE_0
//     so the token never lands on argv or .git/config.
//   - To prove that end-to-end we need a real git server that demands an
//     Authorization header and rejects anonymous clones.
//   - We wrap `git-http-backend` (ships with git) behind httptest so the test
//     is self-contained — no network, no docker.
//
// Auth model:
//   - Server checks `Authorization: Bearer <expectedToken>` on every request.
//   - Missing/wrong → 401, which makes git fail immediately.
//   - Matching → defer to git-http-backend CGI.
//
// macOS note: git-http-backend lives under `git --exec-path`. We probe that
// and skip if not present (some minimal CI images strip it).

import (
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// startGitHTTPServer creates an httptest server backed by git-http-backend
// serving repos under projectRoot. Requests must carry the bearer token.
func startGitHTTPServer(t *testing.T, projectRoot, expectedToken string) *httptest.Server {
	t.Helper()

	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skipf("git --exec-path unavailable: %v", err)
	}
	backend := filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("git-http-backend not installed at %s: %v", backend, err)
	}

	handler := &cgi.Handler{
		Path: backend,
		Env: []string{
			"GIT_PROJECT_ROOT=" + projectRoot,
			"GIT_HTTP_EXPORT_ALL=1",
			// http-backend uses REMOTE_USER for ident; harmless placeholder.
			"REMOTE_USER=pat-test",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if got != "Bearer "+expectedToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})

	// TLS required: the git adapter writes `http.https://<host>/.extraheader`
	// which only matches HTTPS URLs. Production PATs must travel over TLS, so
	// the test mirrors that constraint.
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// makeBareRepo seeds projectRoot/<name>.git with a single commit so clone has
// something to fetch. Returns the on-disk path (useful for assertions).
func makeBareRepo(t *testing.T, projectRoot, name string) string {
	t.Helper()

	// Working tree to seed history.
	work := t.TempDir()
	mustRun(t, work, "git", "init", "-q", "-b", "main")
	mustRun(t, work, "git", "config", "user.email", "ci@test.local")
	mustRun(t, work, "git", "config", "user.name", "CI")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	mustRun(t, work, "git", "add", ".")
	mustRun(t, work, "git", "commit", "-q", "-m", "init")

	// Bare repo under project root.
	bare := filepath.Join(projectRoot, name+".git")
	mustRun(t, "", "git", "clone", "-q", "--bare", work, bare)
	// http-backend requires this to serve smart HTTP without auth bypass.
	mustRun(t, bare, "git", "config", "http.receivepack", "true")
	mustRun(t, bare, "git", "update-server-info")
	return bare
}

func mustRun(t *testing.T, cwd string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
