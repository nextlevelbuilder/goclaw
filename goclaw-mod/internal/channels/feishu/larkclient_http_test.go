package feishu

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- clearToken ---

func TestClearToken_ResetsFields(t *testing.T) {
	c := NewLarkClient("app", "secret", "http://localhost")
	c.token = "some-token"
	c.clearToken()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		t.Errorf("token: got %q, want empty", c.token)
	}
	if !c.tokenExp.IsZero() {
		t.Errorf("tokenExp: got %v, want zero", c.tokenExp)
	}
}

// --- isTokenError ---

func TestIsTokenError(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{99991663, true},
		{99991664, true},
		{99991671, true},
		{0, false},
		{230002, false},
		{10001, false},
	}
	for _, tc := range cases {
		if got := isTokenError(tc.code); got != tc.want {
			t.Errorf("isTokenError(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// --- doJSON token refresh ---

func TestDoJSON_TokenRefreshOnExpiry(t *testing.T) {
	tokenCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"fresh-tok","expire":7200}`)
			return
		}
		// First real call returns token error code → triggers refresh
		w.Header().Set("Content-Type", "application/json")
		if tokenCalls == 1 {
			io.WriteString(w, `{"code":99991663,"msg":"token expired","data":{}}`)
		} else {
			io.WriteString(w, `{"code":0,"msg":"ok","data":{}}`)
		}
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	resp, err := c.doJSON(context.Background(), "GET", "/some/api", nil)
	if err != nil {
		t.Fatalf("doJSON returned error: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("final response code: got %d, want 0", resp.Code)
	}
	if tokenCalls < 2 {
		t.Errorf("expected at least 2 token calls (initial + refresh), got %d", tokenCalls)
	}
}

// --- doDownload ---

func TestDoDownload_Success(t *testing.T) {
	fileBytes := []byte("fake-image-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		// Verify auth header is set
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="photo.jpg"`)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(fileBytes)
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	data, fileName, err := c.doDownload(context.Background(), "/download/img_123")
	if err != nil {
		t.Fatalf("doDownload error: %v", err)
	}
	if !bytes.Equal(data, fileBytes) {
		t.Errorf("data mismatch: got %q, want %q", data, fileBytes)
	}
	if fileName != "photo.jpg" {
		t.Errorf("fileName: got %q, want %q", fileName, "photo.jpg")
	}
}

func TestDoDownload_JSONErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		// Server responds with JSON error instead of binary
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":1234567,"msg":"resource not found","data":{}}`)
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	_, _, err := c.doDownload(context.Background(), "/download/missing")
	if err == nil {
		t.Fatal("expected error for JSON error response")
	}
}

func TestDoDownload_NoContentDisposition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47}) // PNG magic bytes
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	data, fileName, err := c.doDownload(context.Background(), "/download/no_cd")
	if err != nil {
		t.Fatalf("doDownload error: %v", err)
	}
	if fileName != "" {
		t.Errorf("fileName: got %q, want empty when no Content-Disposition", fileName)
	}
	if len(data) == 0 {
		t.Error("data should not be empty")
	}
}

// --- doMultipart ---

func TestDoMultipart_Success(t *testing.T) {
	var gotContentType string
	var gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		gotContentType = r.Header.Get("Content-Type")
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"msg":"ok","data":{"image_key":"img_uploaded_123"}}`)
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	resp, err := c.doMultipart(
		context.Background(),
		"/upload/image",
		map[string]string{"image_type": "message"},
		"image",
		bytes.NewReader([]byte{0xFF, 0xD8, 0xFF}), // fake JPEG
		"test.jpg",
	)
	if err != nil {
		t.Fatalf("doMultipart error: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("response code: got %d, want 0", resp.Code)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("content-type: got %q, want multipart/form-data", gotContentType)
	}
	if !strings.HasPrefix(gotAuthHeader, "Bearer tok") {
		t.Errorf("auth header: got %q", gotAuthHeader)
	}
}

func TestDoMultipart_NoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenEndpoint {
			io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"tok","expire":7200}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"msg":"ok","data":{}}`)
	}))
	defer srv.Close()

	c := NewLarkClient("app", "secret", srv.URL)
	// fileField="" and fileData=nil → fields-only upload
	resp, err := c.doMultipart(
		context.Background(),
		"/upload/fields-only",
		map[string]string{"key": "value"},
		"",
		nil,
		"",
	)
	if err != nil {
		t.Fatalf("doMultipart (no file) error: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("response code: got %d, want 0", resp.Code)
	}
}

func TestDoMultipart_NetworkError(t *testing.T) {
	c := NewLarkClient("app", "secret", "http://localhost:1")
	_, err := c.doMultipart(
		context.Background(),
		"/upload",
		nil, "", nil, "",
	)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}
