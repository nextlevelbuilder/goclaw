package http

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
)

const MaxBrandingAssetUploadBytes = 2 * 1024 * 1024

var brandingAssetNameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// BrandingAssetsHandler handles global branding media uploads and public serving.
type BrandingAssetsHandler struct {
	dir string
}

func NewBrandingAssetsHandler(dataDir string) *BrandingAssetsHandler {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "."
	}
	return &BrandingAssetsHandler{dir: filepath.Join(dataDir, "branding-assets")}
}

func (h *BrandingAssetsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/branding/assets", requireAuth(permissions.RoleAdmin, h.handleUpload))
	mux.HandleFunc("GET /branding-assets/{name}", h.handleServe)
}

func (h *BrandingAssetsHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	locale := extractLocale(r)
	r.Body = http.MaxBytesReader(w, r.Body, int64(MaxBrandingAssetUploadBytes+(1<<20)))
	if err := r.ParseMultipartForm(int64(MaxBrandingAssetUploadBytes + (1 << 20))); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMissingFileField)})
		return
	}
	defer file.Close()

	filename := sanitizeBrandingAssetFilename(header.Filename)
	if filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidFilename)})
		return
	}

	data, err := io.ReadAll(io.LimitReader(file, int64(MaxBrandingAssetUploadBytes)+1))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to read upload")})
		return
	}
	if len(data) > MaxBrandingAssetUploadBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}

	contentType, err := classifyBrandingAsset(filename, data)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	storedName := uniqueBrandingAssetName(filename)
	if err := os.MkdirAll(h.dir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create asset directory")})
		return
	}
	path := filepath.Join(h.dir, storedName)
	if err := os.WriteFile(path, data, 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save asset")})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"url":          "/branding-assets/" + storedName,
		"filename":     storedName,
		"content_type": contentType,
		"size":         len(data),
	})
}

func (h *BrandingAssetsHandler) handleServe(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(h.dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType, err := classifyBrandingAsset(name, data)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func sanitizeBrandingAssetFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	ext := strings.ToLower(filepath.Ext(base))
	if !isAllowedBrandingAssetExt(ext) {
		return ""
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.ToLower(brandingAssetNameRe.ReplaceAllString(stem, "-"))
	stem = strings.Trim(stem, ".-_")
	if stem == "" {
		stem = "asset"
	}
	if len(stem) > 64 {
		stem = strings.Trim(stem[:64], ".-_")
		if stem == "" {
			stem = "asset"
		}
	}
	return stem + ext
}

func uniqueBrandingAssetName(name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%s%s", stem, randomHex(6), ext)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func classifyBrandingAsset(name string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty file")
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png":
		if http.DetectContentType(data) == "image/png" {
			return "image/png", nil
		}
	case ".jpg", ".jpeg":
		if http.DetectContentType(data) == "image/jpeg" {
			return "image/jpeg", nil
		}
	case ".webp":
		if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
			return "image/webp", nil
		}
	case ".ico":
		if len(data) >= 4 && data[0] == 0 && data[1] == 0 && data[2] == 1 && data[3] == 0 {
			return "image/x-icon", nil
		}
	case ".svg":
		if isSafeSVG(data) {
			return "image/svg+xml", nil
		}
		return "", fmt.Errorf("unsafe svg content")
	}
	return "", fmt.Errorf("unsupported or invalid branding asset")
}

func isAllowedBrandingAssetExt(ext string) bool {
	switch ext {
	case ".svg", ".png", ".jpg", ".jpeg", ".webp", ".ico":
		return true
	default:
		return false
	}
}

func isSafeSVG(data []byte) bool {
	lower := strings.ToLower(string(data))
	if !strings.Contains(lower, "<svg") {
		return false
	}
	blocked := []string{"<script", "javascript:", " onload=", " onerror=", " onclick=", " onmouseover="}
	for _, needle := range blocked {
		if strings.Contains(lower, needle) {
			return false
		}
	}
	return true
}
