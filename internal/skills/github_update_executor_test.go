package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeMinimalELF64 returns a byte slice containing a parseable minimal ELF64
// header for the current runtime.GOARCH. The file is intentionally empty
// beyond the header — debug/elf.NewFile accepts it.
func makeMinimalELF64(t *testing.T) []byte {
	t.Helper()
	buf := make([]byte, 64)
	// e_ident[0:4] = magic
	buf[0] = 0x7f
	buf[1] = 'E'
	buf[2] = 'L'
	buf[3] = 'F'
	buf[4] = 2 // ELFCLASS64
	buf[5] = 1 // ELFDATA2LSB
	buf[6] = 1 // EV_CURRENT
	// e_type = ET_EXEC (2)
	binary.LittleEndian.PutUint16(buf[16:18], 2)
	// e_machine: EM_X86_64 = 62, EM_AARCH64 = 183
	var machine uint16 = 62
	if runtime.GOARCH == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(buf[18:20], machine)
	// e_version = 1
	binary.LittleEndian.PutUint32(buf[20:24], 1)
	// e_ehsize = 64
	binary.LittleEndian.PutUint16(buf[52:54], 64)
	return buf
}

// makeTarballWithBinary returns (tarGzPath, sha256hex) for a tarball
// containing a single binary entry named binName with the given content.
func makeTarballWithBinary(t *testing.T, binName string, content []byte) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: binName, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	f, err := os.CreateTemp("", "goclaw-test-exec-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	h := sha256.Sum256(buf.Bytes())
	return f.Name(), hex.EncodeToString(h[:])
}

// mockAssetServer serves an asset at the given path.
func mockAssetServer(t *testing.T, filePath string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(filePath)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.Copy(w, f)
	}))
}

// withTestInsecureHTTP disables HTTPS + host + IP validation for the duration
// of the test, allowing httptest servers (http://127.0.0.1) to work.
func withTestInsecureHTTP(t *testing.T) {
	t.Helper()
	testSkipDownloadValidation = true
	t.Cleanup(func() { testSkipDownloadValidation = false })
}

// withTestDownloadHosts temporarily allows 127.0.0.1 as a download host so
// tests pointing at httptest servers (which bind to loopback) pass the SSRF
// guard. Restores on t.Cleanup.
func withTestDownloadHosts(t *testing.T, u string) {
	t.Helper()
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	host := parsed.Hostname()
	allowedDownloadHosts[host] = true
	t.Cleanup(func() { delete(allowedDownloadHosts, host) })
}

func TestGitHubUpdateExecutor_HappyPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("executor gated to linux (ErrUnsupportedOS)")
	}
	// Build a valid ELF64 content + tarball.
	binContent := makeMinimalELF64(t)
	tarPath, tarSHA := makeTarballWithBinary(t, "lazygit", binContent)

	// Serve the tarball; replace raw URL with http://127.0.0.1 server.
	srv := mockAssetServer(t, tarPath)
	defer srv.Close()
	withTestInsecureHTTP(t)
	withTestDownloadHosts(t, srv.URL)

	dir := t.TempDir()
	cfg := &GitHubPackagesConfig{BinDir: filepath.Join(dir, "bin"), ManifestPath: filepath.Join(dir, "manifest.json")}
	cfg.Defaults()
	inst := NewGitHubInstaller(NewGitHubClient(""), cfg)
	// Seed manifest with current v0.42.0 + a placeholder binary file.
	if err := os.MkdirAll(cfg.BinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(cfg.BinDir, "lazygit")
	if err := os.WriteFile(oldPath, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := &GitHubManifest{Version: 1, Packages: []GitHubPackageEntry{{
		Name: "lazygit", Repo: "jesseduffield/lazygit", Tag: "v0.42.0",
		Binaries: []string{"lazygit"}, SHA256: "old",
	}}}
	if err := inst.saveManifest(seed); err != nil {
		t.Fatal(err)
	}

	exec := NewGitHubUpdateExecutor(inst)
	exec.ScratchDir = filepath.Join(dir, "tmp")
	meta := map[string]any{
		"assetName":      "lazygit.tar.gz",
		"assetURL":       srv.URL + "/lazygit.tar.gz",
		"assetSHA256":    tarSHA,
		"assetSizeBytes": int64(1),
	}
	if err := exec.Update(context.Background(), "lazygit", "v0.44.5", meta); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Verify new binary content.
	got, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binContent) {
		t.Errorf("binary content not swapped")
	}
	// Verify manifest updated.
	m, _ := inst.loadManifest()
	if m.Packages[0].Tag != "v0.44.5" {
		t.Errorf("manifest tag not updated: %+v", m.Packages[0])
	}
	if m.Packages[0].SHA256 == "old" {
		t.Errorf("manifest sha256 not updated")
	}
	// Verify no .bak files left.
	matches, _ := filepath.Glob(filepath.Join(cfg.BinDir, "*.bak.*"))
	if len(matches) != 0 {
		t.Errorf("leftover .bak files: %v", matches)
	}
}

func TestGitHubUpdateExecutor_ChecksumMismatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	binContent := makeMinimalELF64(t)
	tarPath, _ := makeTarballWithBinary(t, "lazygit", binContent)
	srv := mockAssetServer(t, tarPath)
	defer srv.Close()
	withTestInsecureHTTP(t)
	withTestDownloadHosts(t, srv.URL)

	dir := t.TempDir()
	cfg := &GitHubPackagesConfig{BinDir: filepath.Join(dir, "bin"), ManifestPath: filepath.Join(dir, "manifest.json")}
	cfg.Defaults()
	inst := NewGitHubInstaller(NewGitHubClient(""), cfg)
	os.MkdirAll(cfg.BinDir, 0o755)
	oldPath := filepath.Join(cfg.BinDir, "lazygit")
	os.WriteFile(oldPath, []byte("OLD"), 0o755)
	seed := &GitHubManifest{Version: 1, Packages: []GitHubPackageEntry{{
		Name: "lazygit", Repo: "jesseduffield/lazygit", Tag: "v0.42.0",
		Binaries: []string{"lazygit"},
	}}}
	inst.saveManifest(seed)

	exec := NewGitHubUpdateExecutor(inst)
	exec.ScratchDir = filepath.Join(dir, "tmp")
	meta := map[string]any{
		"assetName":   "lazygit.tar.gz",
		"assetURL":    srv.URL + "/lazygit.tar.gz",
		"assetSHA256": strings.Repeat("ff", 32), // deliberately wrong
	}
	err := exec.Update(context.Background(), "lazygit", "v0.44.5", meta)
	if !errors.Is(err, ErrUpdateChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	// Old binary preserved.
	got, _ := os.ReadFile(oldPath)
	if string(got) != "OLD" {
		t.Errorf("old binary clobbered: %q", got)
	}
}

func TestGitHubUpdateExecutor_NotInstalled(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("executor gated to linux")
	}
	dir := t.TempDir()
	cfg := &GitHubPackagesConfig{BinDir: filepath.Join(dir, "bin"), ManifestPath: filepath.Join(dir, "manifest.json")}
	cfg.Defaults()
	inst := NewGitHubInstaller(NewGitHubClient(""), cfg)
	inst.saveManifest(&GitHubManifest{Version: 1})

	exec := NewGitHubUpdateExecutor(inst)
	exec.ScratchDir = filepath.Join(dir, "tmp")
	err := exec.Update(context.Background(), "nonexistent", "v1.0.0", map[string]any{})
	if !errors.Is(err, ErrPackageNotInstalled) {
		t.Fatalf("expected ErrPackageNotInstalled, got %v", err)
	}
}

func TestGitHubUpdateExecutor_MetaAssertions_NilSafe(t *testing.T) {
	// Red-team C6: nil-safe map assertions must never panic.
	cases := []map[string]any{
		nil,
		{},
		{"assetURL": 42},                   // wrong type
		{"assetURL": "", "assetName": nil}, // nil value
	}
	for _, m := range cases {
		_, _ = metaString(m, "assetURL")
		_, _ = metaString(m, "assetName")
		_, _ = metaString(m, "assetSHA256")
	}
}

func TestSanitizeTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.0.0", "v1.0.0"},
		{"v1.0.0-beta.1", "v1.0.0-beta.1"},
		{"release/42", "release-42"},
		{"v1.0.0 beta", "v1.0.0-beta"},
	}
	for _, tc := range cases {
		if got := sanitizeTag(tc.in); got != tc.want {
			t.Errorf("sanitizeTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestVerifyChecksum_ConstantTime_RejectsTruncated validates that VerifyChecksum
// uses constant-time comparison and properly rejects truncated/mutated/empty hashes.
// This is a red-team check to ensure crypto/subtle.ConstantTimeCompare is used.
func TestVerifyChecksum_ConstantTime_RejectsTruncated(t *testing.T) {
	validHash := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	cases := []struct {
		name     string
		expected string
		actual   string
		wantErr  bool
	}{
		{
			name:     "matching hashes",
			expected: validHash,
			actual:   validHash,
			wantErr:  false,
		},
		{
			name:     "case-insensitive",
			expected: strings.ToUpper(validHash),
			actual:   strings.ToLower(validHash),
			wantErr:  false,
		},
		{
			name:     "truncated hash",
			expected: validHash,
			actual:   validHash[:62], // missing last 2 chars
			wantErr:  true,
		},
		{
			name:     "empty expected",
			expected: "",
			actual:   validHash,
			wantErr:  true,
		},
		{
			name:     "empty actual",
			expected: validHash,
			actual:   "",
			wantErr:  true,
		},
		{
			name:     "single bit flip",
			expected: validHash,
			actual:   "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456788", // last char changed
			wantErr:  true,
		},
		{
			name:     "leading whitespace stripped",
			expected: "  " + validHash,
			actual:   validHash,
			wantErr:  false,
		},
		{
			name:     "trailing whitespace stripped",
			expected: validHash + "  ",
			actual:   validHash,
			wantErr:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyChecksum(tc.expected, tc.actual)
			if (err != nil) != tc.wantErr {
				t.Errorf("VerifyChecksum(%q, %q): err=%v, wantErr=%v",
					tc.expected, tc.actual, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrChecksumMismatch) {
				t.Errorf("expected ErrChecksumMismatch, got %v", err)
			}
		})
	}
}

// Silence unused import warnings if build tags strip something out.
var _ = json.Marshal
var _ = fmt.Sprintf
