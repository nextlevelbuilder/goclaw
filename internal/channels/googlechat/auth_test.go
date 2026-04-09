package googlechat

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testServiceAccountJSON(t *testing.T, dir string) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	sa := map[string]string{
		"type":         "service_account",
		"client_email": "test@test.iam.gserviceaccount.com",
		"private_key":  string(pemBlock),
		"token_uri":    "https://oauth2.googleapis.com/token",
	}
	data, _ := json.Marshal(sa)
	path := filepath.Join(dir, "sa.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path, key
}

func TestNewServiceAccountAuth_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path, _ := testServiceAccountJSON(t, dir)
	auth, err := NewServiceAccountAuth(path, []string{scopeChat})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.email != "test@test.iam.gserviceaccount.com" {
		t.Errorf("email = %q, want test@test.iam.gserviceaccount.com", auth.email)
	}
}

func TestNewServiceAccountAuth_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("{bad json"), 0600)
	_, err := NewServiceAccountAuth(path, []string{scopeChat})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewServiceAccountAuth_MissingFile(t *testing.T) {
	_, err := NewServiceAccountAuth("/nonexistent/sa.json", []string{scopeChat})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestServiceAccountAuth_Token_CachesWithinTTL(t *testing.T) {
	dir := t.TempDir()
	path, _ := testServiceAccountJSON(t, dir)
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-123",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer ts.Close()

	auth, err := NewServiceAccountAuth(path, []string{scopeChat})
	if err != nil {
		t.Fatal(err)
	}
	auth.tokenEndpoint = ts.URL

	ctx := context.Background()
	tok1, _ := auth.Token(ctx)
	tok2, _ := auth.Token(ctx)
	if tok1 != tok2 {
		t.Errorf("tokens differ")
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

func TestServiceAccountAuth_Token_RefreshesExpired(t *testing.T) {
	dir := t.TempDir()
	path, _ := testServiceAccountJSON(t, dir)
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"expires_in":   1,
			"token_type":   "Bearer",
		})
	}))
	defer ts.Close()

	auth, err := NewServiceAccountAuth(path, []string{scopeChat})
	if err != nil {
		t.Fatal(err)
	}
	auth.tokenEndpoint = ts.URL
	auth.Token(context.Background())
	auth.expiresAt = time.Now().Add(-1 * time.Minute)
	auth.Token(context.Background())
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
}

func TestServiceAccountAuth_Token_RefreshFailure(t *testing.T) {
	dir := t.TempDir()
	path, _ := testServiceAccountJSON(t, dir)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	auth, _ := NewServiceAccountAuth(path, []string{scopeChat})
	auth.tokenEndpoint = ts.URL
	_, err := auth.Token(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
}
