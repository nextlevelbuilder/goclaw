package http

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func brandingMultipartBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, w.FormDataContentType()
}

func masterOwnerContext() context.Context {
	ctx := store.WithRole(context.Background(), store.RoleOwner)
	return store.WithTenantID(ctx, store.MasterTenantID)
}

func TestBrandingAssetsUploadStoresAllowedImage(t *testing.T) {
	dataDir := t.TempDir()
	h := NewBrandingAssetsHandler(dataDir)
	body, contentType := brandingMultipartBody(t, "../My Logo.PNG", []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/branding/assets", body)
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(masterOwnerContext())
	rec := httptest.NewRecorder()

	h.handleUpload(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		URL         string `json:"url"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(resp.URL, "/branding-assets/") {
		t.Fatalf("url = %q, want /branding-assets/ prefix", resp.URL)
	}
	if !strings.HasSuffix(resp.Filename, ".png") {
		t.Fatalf("filename = %q, want .png suffix", resp.Filename)
	}
	if strings.Contains(resp.Filename, "..") || strings.ContainsAny(resp.Filename, `/\`) {
		t.Fatalf("filename was not sanitized: %q", resp.Filename)
	}
	if resp.ContentType != "image/png" {
		t.Fatalf("content_type = %q, want image/png", resp.ContentType)
	}
	stored := filepath.Join(dataDir, "branding-assets", resp.Filename)
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("uploaded file not stored at %s: %v", stored, err)
	}

	serveReq := httptest.NewRequest(http.MethodGet, resp.URL, nil)
	serveReq.SetPathValue("name", resp.Filename)
	serveRec := httptest.NewRecorder()
	h.handleServe(serveRec, serveReq)
	if serveRec.Code != http.StatusOK {
		t.Fatalf("serve status = %d, body = %s", serveRec.Code, serveRec.Body.String())
	}
	if got := serveRec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestBrandingAssetsUploadRejectsOversizedFile(t *testing.T) {
	h := NewBrandingAssetsHandler(t.TempDir())
	body, contentType := brandingMultipartBody(t, "logo.png", bytes.Repeat([]byte{'a'}, MaxBrandingAssetUploadBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/v1/branding/assets", body)
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(masterOwnerContext())
	rec := httptest.NewRecorder()

	h.handleUpload(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestBrandingAssetsUploadRejectsUnsafeSVG(t *testing.T) {
	h := NewBrandingAssetsHandler(t.TempDir())
	body, contentType := brandingMultipartBody(t, "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	req := httptest.NewRequest(http.MethodPost, "/v1/branding/assets", body)
	req.Header.Set("Content-Type", contentType)
	req = req.WithContext(masterOwnerContext())
	rec := httptest.NewRecorder()

	h.handleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
