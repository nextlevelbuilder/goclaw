package http

import (
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
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

func TestStorageReadRejectsSymlinkedTenantParentForMaster(t *testing.T) {
	baseDir := t.TempDir()
	tenantSecret := filepath.Join(baseDir, "tenants", "tenant-a", "secret.txt")
	writeStorageTestFile(t, tenantSecret, "tenant-secret")
	if err := os.Symlink(filepath.Join(baseDir, "tenants"), filepath.Join(baseDir, "tenant-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest("GET", "/v1/storage/files/tenant-link/tenant-a/secret.txt", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	req.SetPathValue("path", "tenant-link/tenant-a/secret.txt")
	w := httptest.NewRecorder()

	handler.handleRead(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if strings.Contains(w.Body.String(), "tenant-secret") {
		t.Fatal("response leaked tenant secret through symlinked parent")
	}
}

func TestStorageDeleteRejectsSymlinkedTenantParentForMaster(t *testing.T) {
	baseDir := t.TempDir()
	tenantSecret := filepath.Join(baseDir, "tenants", "tenant-a", "secret.txt")
	writeStorageTestFile(t, tenantSecret, "tenant-secret")
	if err := os.Symlink(filepath.Join(baseDir, "tenants"), filepath.Join(baseDir, "tenant-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest(http.MethodDelete, "/v1/storage/files/tenant-link/tenant-a/secret.txt", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	req.SetPathValue("path", "tenant-link/tenant-a/secret.txt")
	w := httptest.NewRecorder()

	handler.handleDelete(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if _, err := os.Stat(tenantSecret); err != nil {
		t.Fatalf("tenant secret should not be deleted through symlinked parent: %v", err)
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

func TestStorageDeleteInvalidatesSizeCache(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "tmp.txt"), "abc")

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest(http.MethodDelete, "/v1/storage/files/tmp.txt", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	req.SetPathValue("path", "tmp.txt")

	sizeBase := handler.tenantBaseDir(req)
	handler.sizeCache.Store(sizeBase, &sizeCacheEntry{total: 3, files: 1})

	w := httptest.NewRecorder()
	handler.handleDelete(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if _, ok := handler.sizeCache.Load(sizeBase); ok {
		t.Fatal("expected size cache entry to be invalidated after delete")
	}
}

func TestStorageMoveInvalidatesSizeCache(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "from.txt"), "abc")

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest(http.MethodPut, "/v1/storage/move?from=from.txt&to=to.txt", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))

	sizeBase := handler.tenantBaseDir(req)
	handler.sizeCache.Store(sizeBase, &sizeCacheEntry{total: 3, files: 1})

	w := httptest.NewRecorder()
	handler.handleMove(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if _, ok := handler.sizeCache.Load(sizeBase); ok {
		t.Fatal("expected size cache entry to be invalidated after move")
	}
}

func TestStorageMoveRejectsSymlinkedTenantDestinationParent(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "from.txt"), "abc")
	writeStorageTestFile(t, filepath.Join(baseDir, "tenants", "tenant-a", ".keep"), "")
	if err := os.Symlink(filepath.Join(baseDir, "tenants", "tenant-a"), filepath.Join(baseDir, "tenant-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	handler := NewStorageHandler(baseDir)
	req := httptest.NewRequest(http.MethodPut, "/v1/storage/move?from=from.txt&to=tenant-link/moved.txt", nil)
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	w := httptest.NewRecorder()

	handler.handleMove(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "from.txt")); err != nil {
		t.Fatalf("source should remain after rejected move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "tenants", "tenant-a", "moved.txt")); !os.IsNotExist(err) {
		t.Fatalf("destination should not be created through symlinked parent, err=%v", err)
	}
}

func TestStorageUploadRejectsSymlinkedTenantDestinationParent(t *testing.T) {
	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "tenants", "tenant-a", ".keep"), "")
	if err := os.Symlink(filepath.Join(baseDir, "tenants", "tenant-a"), filepath.Join(baseDir, "tenant-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	handler := NewStorageHandler(baseDir)
	req := newStorageUploadRequest(t, "/v1/storage/files?path=tenant-link", "file", "x.txt", "data")
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	w := httptest.NewRecorder()

	handler.handleUpload(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "tenants", "tenant-a", "x.txt")); !os.IsNotExist(err) {
		t.Fatalf("upload should not write through symlinked parent, err=%v", err)
	}
}

func TestStorageUploadReplacesLeafSymlinkWithoutFollowingTarget(t *testing.T) {
	baseDir := t.TempDir()
	tenantSecret := filepath.Join(baseDir, "tenants", "tenant-a", "secret.txt")
	writeStorageTestFile(t, tenantSecret, "tenant-secret")
	leaf := filepath.Join(baseDir, "x.txt")
	if err := os.Symlink(tenantSecret, leaf); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	handler := NewStorageHandler(baseDir)
	req := newStorageUploadRequest(t, "/v1/storage/files", "file", "x.txt", "replacement")
	req = req.WithContext(store.WithTenantID(context.Background(), store.MasterTenantID))
	w := httptest.NewRecorder()

	handler.handleUpload(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	tenantData, err := os.ReadFile(tenantSecret)
	if err != nil {
		t.Fatalf("read tenant secret: %v", err)
	}
	if string(tenantData) != "tenant-secret" {
		t.Fatalf("tenant secret overwritten through leaf symlink: %q", tenantData)
	}
	info, err := os.Lstat(leaf)
	if err != nil {
		t.Fatalf("lstat uploaded leaf: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("upload should replace the leaf symlink itself")
	}
	uploaded, err := os.ReadFile(leaf)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(uploaded) != "replacement" {
		t.Fatalf("uploaded content = %q, want replacement", uploaded)
	}
}

func TestStorageMutationsRequireTenantAdmin(t *testing.T) {
	setupTestToken(t, "gateway-token")
	setupTestNoAuthFallback(t, false)
	ts := newMockTenantStore()
	tenantID := uuid.New()
	ts.addTenant(tenantID, "acme")
	ts.setUserRole(tenantID, "viewer-user", store.TenantRoleViewer)
	ts.setUserRole(tenantID, "admin-user", store.TenantRoleAdmin)
	setupTestTenantStore(t, ts)

	baseDir := t.TempDir()
	writeStorageTestFile(t, filepath.Join(baseDir, "tenants", "acme", "from.txt"), "abc")

	handler := NewStorageHandler(baseDir, ts)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	viewerUpload := newStorageUploadRequest(t, "/v1/storage/files", "file", "x.txt", "data")
	viewerUpload.Header.Set("Authorization", "Bearer gateway-token")
	viewerUpload.Header.Set("X-GoClaw-User-Id", "viewer-user")
	viewerUpload.Header.Set("X-GoClaw-Tenant-Id", "acme")
	viewerUploadRR := httptest.NewRecorder()
	mux.ServeHTTP(viewerUploadRR, viewerUpload)
	if viewerUploadRR.Code != http.StatusForbidden {
		t.Fatalf("viewer upload status = %d, want 403", viewerUploadRR.Code)
	}

	viewerMove := httptest.NewRequest(http.MethodPut, "/v1/storage/move?from=from.txt&to=to.txt", nil)
	viewerMove.Header.Set("Authorization", "Bearer gateway-token")
	viewerMove.Header.Set("X-GoClaw-User-Id", "viewer-user")
	viewerMove.Header.Set("X-GoClaw-Tenant-Id", "acme")
	viewerMoveRR := httptest.NewRecorder()
	mux.ServeHTTP(viewerMoveRR, viewerMove)
	if viewerMoveRR.Code != http.StatusForbidden {
		t.Fatalf("viewer move status = %d, want 403", viewerMoveRR.Code)
	}

	viewerDelete := httptest.NewRequest(http.MethodDelete, "/v1/storage/files/from.txt", nil)
	viewerDelete.Header.Set("Authorization", "Bearer gateway-token")
	viewerDelete.Header.Set("X-GoClaw-User-Id", "viewer-user")
	viewerDelete.Header.Set("X-GoClaw-Tenant-Id", "acme")
	viewerDeleteRR := httptest.NewRecorder()
	mux.ServeHTTP(viewerDeleteRR, viewerDelete)
	if viewerDeleteRR.Code != http.StatusForbidden {
		t.Fatalf("viewer delete status = %d, want 403", viewerDeleteRR.Code)
	}

	adminUpload := newStorageUploadRequest(t, "/v1/storage/files", "file", "admin.txt", "data")
	adminUpload.Header.Set("Authorization", "Bearer gateway-token")
	adminUpload.Header.Set("X-GoClaw-User-Id", "admin-user")
	adminUpload.Header.Set("X-GoClaw-Tenant-Id", "acme")
	adminUploadRR := httptest.NewRecorder()
	mux.ServeHTTP(adminUploadRR, adminUpload)
	if adminUploadRR.Code != http.StatusOK {
		t.Fatalf("tenant admin upload status = %d, want 200: %s", adminUploadRR.Code, adminUploadRR.Body.String())
	}
}

func newStorageUploadRequest(t *testing.T, target, field, filename, content string) *http.Request {
	t.Helper()
	var body strings.Builder
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write multipart content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body.String()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
