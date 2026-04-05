// Package updater checks GitHub Releases for newer desktop (lite) versions
// and applies updates by swapping the .app bundle (macOS) or .exe (Windows).
package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/version"
)

const (
	githubRepo = "nextlevelbuilder/goclaw"
	tagPrefix  = "lite-v"
	// maxFileSize limits individual extracted files to 500 MB (decompression bomb guard).
	maxFileSize = 500 << 20
)

// httpClient with timeouts — never use http.DefaultClient for external calls.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// UpdateInfo describes an available update.
type UpdateInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version"`       // e.g. "0.2.0"
	DownloadURL  string `json:"download_url"`  // asset URL for current OS/arch
	ReleaseURL   string `json:"release_url"`   // GitHub release page
	ReleaseNotes string `json:"release_notes"` // release body markdown
}

// githubRelease is a minimal GitHub Release API response.
type githubRelease struct {
	TagName    string        `json:"tag_name"`
	HTMLURL    string        `json:"html_url"`
	Body       string        `json:"body"`
	Assets     []githubAsset `json:"assets"`
	Prerelease bool          `json:"prerelease"`
	Draft      bool          `json:"draft"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckForUpdate queries GitHub Releases for a newer lite-v* release.
// currentVersion should be a semver string like "0.1.0" (no "v" prefix).
func CheckForUpdate(currentVersion string) (*UpdateInfo, error) {
	if currentVersion == "" || currentVersion == "dev" {
		return &UpdateInfo{Available: false}, nil
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases", githubRepo)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github api: %s", resp.Status)
	}

	var releases []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}

	for _, rel := range releases {
		if rel.Draft || rel.Prerelease {
			continue
		}
		if !strings.HasPrefix(rel.TagName, tagPrefix) {
			continue
		}
		relVersion := strings.TrimPrefix(rel.TagName, tagPrefix)
		if !isNewer(relVersion, currentVersion) {
			continue
		}

		assetURL := findAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
		if assetURL == "" {
			continue
		}

		return &UpdateInfo{
			Available:    true,
			Version:      relVersion,
			DownloadURL:  assetURL,
			ReleaseURL:   rel.HTMLURL,
			ReleaseNotes: rel.Body,
		}, nil
	}

	return &UpdateInfo{Available: false}, nil
}

// findAsset returns the download URL for the .tar.gz (macOS) or .zip (Windows) asset.
func findAsset(assets []githubAsset, goos, goarch string) string {
	var ext string
	switch goos {
	case "darwin":
		ext = ".tar.gz"
	case "windows":
		ext = ".zip"
	default:
		return ""
	}

	suffix := fmt.Sprintf("-%s-%s%s", goos, goarch, ext)
	for _, a := range assets {
		if strings.HasSuffix(a.Name, suffix) {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// DownloadAndApply downloads the update asset and replaces the current app.
// appPath is the path to the current .app bundle (macOS) or .exe (Windows).
func DownloadAndApply(info *UpdateInfo, appPath string) error {
	if info == nil || info.DownloadURL == "" {
		return fmt.Errorf("no download URL")
	}

	// Enforce HTTPS for download URLs.
	if !strings.HasPrefix(info.DownloadURL, "https://") {
		return fmt.Errorf("download URL must use HTTPS")
	}

	slog.Info("updater: downloading", "url", info.DownloadURL)
	dlClient := &http.Client{Timeout: 10 * time.Minute} // large file download
	resp, err := dlClient.Get(info.DownloadURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download: %s", resp.Status)
	}

	tmpDir, err := os.MkdirTemp("", "goclaw-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	switch runtime.GOOS {
	case "darwin":
		return applyMacOS(resp.Body, tmpDir, appPath)
	case "windows":
		return applyWindows(resp.Body, tmpDir, appPath)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// applyMacOS extracts .tar.gz and atomically swaps the .app bundle.
func applyMacOS(r io.Reader, tmpDir, appPath string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	cleanTmpDir := filepath.Clean(tmpDir) + string(filepath.Separator)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		target := filepath.Join(tmpDir, hdr.Name)
		// Path traversal guard
		if !strings.HasPrefix(filepath.Clean(target)+string(filepath.Separator), cleanTmpDir) &&
			filepath.Clean(target) != filepath.Clean(tmpDir) {
			slog.Warn("updater: skipping path-traversal entry", "name", hdr.Name)
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("mkdir %s: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", hdr.Name, err)
			}
			if err := extractFile(target, tr, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("extract %s: %w", hdr.Name, err)
			}
		case tar.TypeSymlink:
			// Validate symlink target doesn't escape tmpDir
			linkTarget := filepath.Join(filepath.Dir(target), hdr.Linkname)
			if !strings.HasPrefix(filepath.Clean(linkTarget), filepath.Clean(tmpDir)) {
				slog.Warn("updater: skipping symlink escaping tmpDir", "name", hdr.Name, "target", hdr.Linkname)
				continue
			}
			os.Remove(target) // remove existing if any
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("symlink %s: %w", hdr.Name, err)
			}
		}
	}

	// Find extracted .app
	newApp := filepath.Join(tmpDir, "goclaw-lite.app")
	if _, err := os.Stat(newApp); err != nil {
		return fmt.Errorf("extracted app not found: %w", err)
	}

	// Atomic swap: rename current → .bak, rename new → current, remove .bak
	bakPath := appPath + ".bak"
	os.RemoveAll(bakPath)
	if err := os.Rename(appPath, bakPath); err != nil {
		return fmt.Errorf("backup current app: %w", err)
	}
	if err := os.Rename(newApp, appPath); err != nil {
		// Rollback
		os.Rename(bakPath, appPath)
		return fmt.Errorf("install new app: %w", err)
	}
	os.RemoveAll(bakPath)

	// Remove quarantine attribute (unsigned app on macOS)
	removeQuarantine(appPath)

	slog.Info("updater: macOS app updated", "path", appPath)
	return nil
}

// applyWindows extracts .zip and replaces the .exe via temp rename.
func applyWindows(r io.Reader, tmpDir, exePath string) error {
	// Write zip to temp file (zip needs random access)
	tmpZip := filepath.Join(tmpDir, "update.zip")
	f, err := os.Create(tmpZip)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(r, maxFileSize)); err != nil {
		f.Close()
		return fmt.Errorf("write zip: %w", err)
	}
	f.Close()

	zr, err := zip.OpenReader(tmpZip)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	// Find the .exe in the zip (validate no path traversal)
	var exeFile *zip.File
	for _, zf := range zr.File {
		name := filepath.Base(zf.Name) // use only basename to prevent traversal
		if strings.HasSuffix(name, ".exe") && !strings.Contains(zf.Name, "..") {
			exeFile = zf
			break
		}
	}
	if exeFile == nil {
		return fmt.Errorf("no .exe found in zip")
	}

	// Extract to temp
	newExe := filepath.Join(tmpDir, "goclaw-lite.exe")
	src, err := exeFile.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	if err := extractFile(newExe, src, 0o755); err != nil {
		return fmt.Errorf("extract exe: %w", err)
	}

	// Windows: can't delete running exe, but can rename it
	bakPath := exePath + ".bak"
	os.Remove(bakPath)
	if err := os.Rename(exePath, bakPath); err != nil {
		return fmt.Errorf("backup current exe: %w", err)
	}
	if err := os.Rename(newExe, exePath); err != nil {
		os.Rename(bakPath, exePath)
		return fmt.Errorf("install new exe: %w", err)
	}
	// .bak will be cleaned up on next launch
	slog.Info("updater: windows exe updated", "path", exePath)
	return nil
}

// extractFile writes src to a file at path with size limit and proper error handling.
func extractFile(path string, src io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(src, maxFileSize))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// removeQuarantine strips the macOS quarantine xattr from an app bundle.
func removeQuarantine(appPath string) {
	// xattr -rd com.apple.quarantine /path/to/app
	if runtime.GOOS == "darwin" {
		exec := filepath.Join("/usr/bin", "xattr")
		if _, err := os.Stat(exec); err == nil {
			cmd := &os.ProcAttr{Files: []*os.File{nil, nil, nil}}
			p, err := os.StartProcess(exec, []string{"xattr", "-rd", "com.apple.quarantine", appPath}, cmd)
			if err == nil {
				p.Wait()
			}
		}
	}
}

// isNewer returns true if version a is newer than b.
func isNewer(a, b string) bool {
	return version.IsNewer(a, b)
}

// ResolveAppPath returns the path to the current .app bundle (macOS) or .exe (Windows)
// by walking up from the running executable path.
func ResolveAppPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "darwin":
		// exe: /path/to/GoClaw Lite.app/Contents/MacOS/goclaw-lite
		// app: /path/to/GoClaw Lite.app
		dir := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if strings.HasSuffix(dir, ".app") {
			return dir, nil
		}
		return "", fmt.Errorf("not running from .app bundle: %s", exe)
	case "windows":
		return exe, nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
