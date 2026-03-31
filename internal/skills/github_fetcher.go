package skills

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxDownloadSize   = 50 << 20 // 50 MB tarball download limit
	maxExtractedSize  = 20 << 20 // 20 MB extracted content limit
	maxExtractedFiles = 500      // tar bomb prevention
	fetchTimeout      = 30 * time.Second
)

// SkillRef represents a parsed skill reference — either a GitHub repo or a registry slug.
type SkillRef struct {
	Owner      string
	Repo       string
	Ref        string // tag, branch, or "" for default
	IsRegistry bool   // true if input was a plain slug needing registry lookup
}

// ParseSkillRef parses a skill input string into a SkillRef.
//
// Accepted formats:
//   - "github.com/owner/repo"        → GitHub repo, default branch
//   - "github.com/owner/repo@v1.0"   → GitHub repo, specific ref
//   - "owner/repo"                   → GitHub repo shorthand
//   - "owner/repo@v1.0"             → GitHub repo shorthand with ref
//   - "my-skill"                     → registry slug (needs Resolve)
func ParseSkillRef(input string) (SkillRef, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return SkillRef{}, fmt.Errorf("empty skill reference")
	}

	// Strip "https://" or "http://" prefix if present.
	cleaned := input
	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.TrimPrefix(cleaned, "http://")

	// Split off @ref suffix.
	var ref string
	if idx := strings.LastIndex(cleaned, "@"); idx > 0 {
		ref = cleaned[idx+1:]
		cleaned = cleaned[:idx]
	}

	// Strip "github.com/" prefix.
	cleaned = strings.TrimPrefix(cleaned, "github.com/")

	// Check if it looks like "owner/repo".
	parts := strings.SplitN(cleaned, "/", 3)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		if !isValidGitHubName(parts[0]) || !isValidGitHubName(parts[1]) {
			return SkillRef{}, fmt.Errorf("invalid GitHub owner/repo: %q (only alphanumeric, hyphen, dot, underscore allowed)", cleaned)
		}
		return SkillRef{Owner: parts[0], Repo: parts[1], Ref: ref}, nil
	}

	// Single token = registry slug.
	if len(parts) == 1 && !strings.Contains(cleaned, "/") {
		return SkillRef{IsRegistry: true, Ref: ref}, nil
	}

	return SkillRef{}, fmt.Errorf("invalid skill reference: %q (expected owner/repo, github.com/owner/repo, or a slug)", input)
}

// FetchFromGitHub downloads a repository tarball from GitHub and extracts it
// to a temporary directory. The caller owns the returned directory and must
// clean it up. On error, any partial temp dir is removed automatically.
func FetchFromGitHub(ctx context.Context, owner, repo, ref string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	// Build tarball URL.
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tarball", owner, repo)
	if ref != "" {
		url += "/" + ref
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "goclaw/skill-hub")
	req.Header.Set("Accept", "application/vnd.github+json")

	// Auto-detect GitHub token for higher rate limits.
	if token := resolveGitHubToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch tarball: %w", err)
	}
	defer resp.Body.Close()

	// Log rate limit status.
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, _ := strconv.Atoi(remaining); n < 10 {
			slog.Warn("github: rate limit low", "remaining", n)
		}
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// OK — continue
	case http.StatusNotFound:
		return "", fmt.Errorf("repository %s/%s not found (or private without token)", owner, repo)
	case http.StatusForbidden:
		return "", fmt.Errorf("GitHub API rate limited — set GITHUB_TOKEN env or install gh CLI")
	default:
		return "", fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	// Check Content-Length if available.
	if resp.ContentLength > maxDownloadSize {
		return "", fmt.Errorf("tarball too large: %d bytes (max %d)", resp.ContentLength, maxDownloadSize)
	}

	// Limit reader to prevent unbounded download.
	limited := io.LimitReader(resp.Body, maxDownloadSize+1)

	return extractTarGz(limited, owner, repo)
}

// extractTarGz streams a gzipped tar archive into a temp directory,
// stripping the single root directory prefix that GitHub adds.
func extractTarGz(r io.Reader, owner, repo string) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("gzip decode: %w", err)
	}
	defer gz.Close()

	tmpDir, err := os.MkdirTemp("", "goclaw-skill-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	tr := tar.NewReader(gz)
	var totalSize int64
	var fileCount int

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("tar read: %w", err)
		}

		// Strip the root directory (e.g. "owner-repo-abc123/").
		name := stripRootDir(hdr.Name)
		if name == "" || name == "." {
			continue
		}

		// Skip system artifacts.
		if IsSystemArtifact(name) {
			continue
		}

		// Security: reject symlinks unconditionally.
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			continue
		}

		// Security: validate path — no traversal, no absolute paths.
		dest, err := safePath(tmpDir, name)
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("path security: %w", err)
		}

		// File count cap.
		fileCount++
		if fileCount > maxExtractedFiles {
			os.RemoveAll(tmpDir)
			return "", fmt.Errorf("too many files in archive (max %d)", maxExtractedFiles)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0755); err != nil {
				os.RemoveAll(tmpDir)
				return "", err
			}
		case tar.TypeReg:
			// Size check.
			totalSize += hdr.Size
			if totalSize > maxExtractedSize {
				os.RemoveAll(tmpDir)
				return "", fmt.Errorf("extracted content too large (max %d MB)", maxExtractedSize>>20)
			}

			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				os.RemoveAll(tmpDir)
				return "", err
			}
			f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				os.RemoveAll(tmpDir)
				return "", err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, hdr.Size+1)); err != nil {
				f.Close()
				os.RemoveAll(tmpDir)
				return "", err
			}
			f.Close()
		}
	}

	return tmpDir, nil
}

// stripRootDir removes the first path component from a tar entry name.
// GitHub tarballs wrap everything in "owner-repo-sha/" which we need to strip.
func stripRootDir(name string) string {
	// Normalize separators.
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "/")
	idx := strings.Index(name, "/")
	if idx < 0 {
		return "" // root dir entry itself
	}
	return name[idx+1:]
}

// safePath validates and returns the cleaned destination path for a tar entry.
// Rejects absolute paths, ".." components, and paths that escape the base dir.
func safePath(base, name string) (string, error) {
	// Reject absolute paths.
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("absolute path in archive: %q", name)
	}
	// Reject any ".." component.
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == ".." {
			return "", fmt.Errorf("path traversal in archive: %q", name)
		}
	}
	dest := filepath.Join(base, name)
	// Final check: must be under base.
	if !strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator), filepath.Clean(base)+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes destination: %q", name)
	}
	return dest, nil
}

// validGitHubName matches GitHub usernames and repo names.
var validGitHubName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func isValidGitHubName(name string) bool {
	return validGitHubName.MatchString(name)
}

// resolveGitHubToken returns a GitHub token for API authentication.
// Checks GITHUB_TOKEN env var first, then falls back to `gh auth token`.
func resolveGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	// Fallback: gh CLI token (if installed).
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		if token := strings.TrimSpace(string(out)); token != "" {
			return token
		}
	}
	return ""
}
