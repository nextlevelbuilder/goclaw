package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// writeStorageTestFile creates a file with the given content for testing.
func writeStorageTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestStorageListHidesTenantRootForMaster(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "master.txt"), "master")
	writeStorageTestFile(t, filepath.Join(baseDir, "tenants", "tenant-a", "secret.txt"), "tenant-secret")

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest("GET", "/v1/storage/files", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	w := httptest.NewRecorder()

	handler.handleList(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Files []struct {
			Path string `json:"path"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	for _, f := range resp.Files {
		if f.Path == "tenants" || strings.HasPrefix(f.Path, "tenants/") {
			t.Fatalf("master storage unexpectedly exposed tenant path %q", f.Path)
		}
	}
}

func TestStorageListSubpathTenantReturnsNotFound(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "tenants", "tenant-a", "secret.txt"), "tenant-secret")

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest("GET", "/v1/storage/files?path=tenants", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	w := httptest.NewRecorder()

	handler.handleList(w, req)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestStorageReadTenantRootReturnsNotFoundForMaster(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "tenants", "tenant-a", "secret.txt"), "tenant-secret")

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest("GET", "/v1/storage/files/tenants/tenant-a/secret.txt", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	req.SetPathValue("path", "tenants/tenant-a/secret.txt")
	w := httptest.NewRecorder()

	handler.handleRead(w, req)
	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestStorageSizeExcludesTenantRootForMaster(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "master.txt"), "12345")
	writeStorageTestFile(t, filepath.Join(baseDir, "tenants", "tenant-a", "secret.txt"), "1234567890")

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest("GET", "/v1/storage/size", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	w := httptest.NewRecorder()

	handler.handleSize(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) == 0 {
		t.Fatal("expected SSE response body")
	}

	// Find the final "done" event.
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "data: ") {
		t.Fatalf("unexpected SSE line %q", last)
	}

	var payload struct {
		Total int64 `json:"total"`
		Files int   `json:"files"`
		Done  bool  `json:"done"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(last, "data: ")), &payload); err != nil {
		t.Fatalf("unmarshal SSE payload: %v", err)
	}
	if !payload.Done {
		t.Fatalf("expected final SSE payload, got %#v", payload)
	}
	if payload.Total != 5 {
		t.Fatalf("total = %d, want 5 (tenant files should be excluded)", payload.Total)
	}
	if payload.Files != 1 {
		t.Fatalf("files = %d, want 1 (tenant files should be excluded)", payload.Files)
	}
}

// TestIsHiddenPathOnlyAffectsMaster verifies that isHiddenPath only blocks
// the master tenant and leaves non-master tenants unaffected.
func TestIsHiddenPathOnlyAffectsMaster(t *testing.T) {
	handler := NewStorageHandler(t.TempDir())
	nonMasterID := uuid.MustParse("0193a5b0-7000-7000-8000-000000000099")

	masterReq := httptest.NewRequest("GET", "/", nil)
	masterReq = masterReq.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))

	otherReq := httptest.NewRequest("GET", "/", nil)
	otherReq = otherReq.WithContext(store.WithTenantID(context.Background(), nonMasterID))

	// Master tenant: tenants paths are hidden.
	if !handler.isHiddenPath(masterReq, "tenants") {
		t.Fatal("expected 'tenants' to be hidden for master")
	}
	if !handler.isHiddenPath(masterReq, "tenants/foo/bar") {
		t.Fatal("expected 'tenants/foo/bar' to be hidden for master")
	}
	if !handler.isHiddenPath(masterReq, "Tenants") {
		t.Fatal("expected case-insensitive match for master")
	}

	// Non-master tenant: tenants paths are NOT hidden.
	if handler.isHiddenPath(otherReq, "tenants") {
		t.Fatal("tenants should not be hidden for non-master tenant")
	}
	if handler.isHiddenPath(otherReq, "tenants/foo") {
		t.Fatal("tenants/foo should not be hidden for non-master tenant")
	}

	// Empty path is never hidden.
	if handler.isHiddenPath(masterReq, "") {
		t.Fatal("empty path should never be hidden")
	}

	// Non-tenants paths are never hidden.
	if handler.isHiddenPath(masterReq, "skills") {
		t.Fatal("non-tenants path should not be hidden")
	}
	if handler.isHiddenPath(masterReq, "my-tenants") {
		t.Fatal("partial match should not be hidden")
	}
}
