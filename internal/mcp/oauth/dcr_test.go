package mcpoauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

func TestRegisterClientSuccess201(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req DCRRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req.ClientName != "GoClaw Gateway" {
			http.Error(w, "wrong client_name", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(DCRResponse{ClientID: "registered-client-id"})
	}))
	defer srv.Close()

	client := security.NewSafeClient(5 * time.Second)
	resp, err := RegisterClient(context.Background(), client, srv.URL, "https://goclaw.example.com/callback")
	if err != nil {
		t.Fatalf("RegisterClient() error: %v", err)
	}
	if resp.ClientID != "registered-client-id" {
		t.Errorf("ClientID = %q, want %q", resp.ClientID, "registered-client-id")
	}
}

func TestRegisterClientAccepts200(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // 200, not 201
		_ = json.NewEncoder(w).Encode(DCRResponse{ClientID: "client-200"})
	}))
	defer srv.Close()

	client := security.NewSafeClient(5 * time.Second)
	resp, err := RegisterClient(context.Background(), client, srv.URL, "https://example.com/cb")
	if err != nil {
		t.Fatalf("RegisterClient() with HTTP 200 error: %v", err)
	}
	if resp.ClientID != "client-200" {
		t.Errorf("ClientID = %q, want %q", resp.ClientID, "client-200")
	}
}

func TestRegisterClientMissingClientID(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// Response has no client_id.
		_ = json.NewEncoder(w).Encode(DCRResponse{})
	}))
	defer srv.Close()

	client := security.NewSafeClient(5 * time.Second)
	_, err := RegisterClient(context.Background(), client, srv.URL, "https://example.com/cb")
	if err == nil {
		t.Fatal("expected error when client_id missing, got nil")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Errorf("error should mention client_id, got: %v", err)
	}
}

func TestRegisterClientHTTPError(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := security.NewSafeClient(5 * time.Second)
	_, err := RegisterClient(context.Background(), client, srv.URL, "https://example.com/cb")
	if err == nil {
		t.Fatal("expected error for HTTP 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention HTTP status, got: %v", err)
	}
}

func TestRegisterClientResponseTooLarge(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// Write > 32KB of data.
		large := make([]byte, 33*1024)
		for i := range large {
			large[i] = 'x'
		}
		// Wrap in valid JSON to pass JSON decode.
		_, _ = w.Write([]byte(`{"client_id":"`))
		_, _ = w.Write(large)
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	client := security.NewSafeClient(5 * time.Second)
	// The response is truncated to 32KB before JSON decode; the JSON will be
	// malformed, so we expect a parse error.
	_, err := RegisterClient(context.Background(), client, srv.URL, "https://example.com/cb")
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}
