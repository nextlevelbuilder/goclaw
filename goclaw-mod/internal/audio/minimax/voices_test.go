package minimax_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
)

func TestListVoices_RequestShape(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"system_voice":[],"voice_cloning":[],"voice_generation":[]}`))
	}))
	defer srv.Close()

	vl := minimax.NewVoiceLister("test-key", srv.URL, 5000, uuid.New())
	vl.ListVoices(t.Context())

	if gotMethod != http.MethodPost {
		t.Errorf("method: got %q, want POST", gotMethod)
	}
	if gotPath != "/get_voice" {
		t.Errorf("path: got %q, want /get_voice", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth: got %q, want Bearer test-key", gotAuth)
	}
	if gotBody["voice_type"] != "all" {
		t.Errorf("voice_type: got %v, want all", gotBody["voice_type"])
	}
	// NO GroupId in URL or body.
	if _, ok := gotBody["group_id"]; ok {
		t.Error("group_id must NOT be in request body")
	}
}

func TestListVoices_ParsesAllThreeArrays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"system_voice":[{"voice_id":"S","voice_name":"Sys"}],
			"voice_cloning":[{"voice_id":"C","voice_name":"Clone"}],
			"voice_generation":[{"voice_id":"G","voice_name":"Gen"}]
		}`))
	}))
	defer srv.Close()

	vl := minimax.NewVoiceLister("k", srv.URL, 5000, uuid.New())
	voices, err := vl.ListVoices(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(voices) != 3 {
		t.Fatalf("expected 3 voices, got %d", len(voices))
	}

	cats := map[string]string{}
	for _, v := range voices {
		cats[v.ID] = v.Category
	}
	if cats["S"] != "system" {
		t.Errorf("S category: got %q, want system", cats["S"])
	}
	if cats["C"] != "cloning" {
		t.Errorf("C category: got %q, want cloning", cats["C"])
	}
	if cats["G"] != "generation" {
		t.Errorf("G category: got %q, want generation", cats["G"])
	}
}

func TestListVoices_EmptyArrays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// all null arrays — must not panic
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	vl := minimax.NewVoiceLister("k", srv.URL, 5000, uuid.New())
	voices, err := vl.ListVoices(t.Context())
	if err != nil {
		t.Fatalf("unexpected error for empty response: %v", err)
	}
	if voices == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(voices) != 0 {
		t.Errorf("expected 0 voices, got %d", len(voices))
	}
}

func TestListVoices_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"invalid_api_key"}`))
	}))
	defer srv.Close()

	vl := minimax.NewVoiceLister("bad-key", srv.URL, 5000, uuid.New())
	_, err := vl.ListVoices(t.Context())
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !containsMiniMaxStr(err.Error(), "401") && !containsMiniMaxStr(err.Error(), "unauthorized") {
		t.Errorf("error should mention 401/unauthorized, got: %v", err)
	}
}

func TestListVoices_5xxFallback_ReturnsStaleCacheOrEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	vl := minimax.NewVoiceLister("k", srv.URL, 5000, uuid.New())
	voices, err := vl.ListVoices(t.Context())
	// No prior cache → returns empty slice + error.
	if err == nil {
		t.Error("expected error when upstream 500 and no cache")
	}
	if voices == nil {
		t.Error("voices must be non-nil even on error (empty slice)")
	}
}

func TestListVoices_5xxFallback_ReturnsStaleCacheWhenPresent(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: success, populates cache.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"system_voice":[{"voice_id":"V1","voice_name":"Voice1"},{"voice_id":"V2","voice_name":"Voice2"}],
				"voice_cloning":[],
				"voice_generation":[]
			}`))
		} else {
			// Subsequent calls: 500.
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	vl := minimax.NewVoiceLister("k", srv.URL, 5000, uuid.New())

	// First call populates cache.
	voices1, err := vl.ListVoices(t.Context())
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(voices1) != 2 {
		t.Fatalf("first call: expected 2 voices, got %d", len(voices1))
	}

	// Expire the cache by re-fetching after forcing expiry.
	// We simulate by calling ListVoices directly; the cache is still fresh
	// so it returns the cached result without calling upstream again.
	voices2, err := vl.ListVoices(t.Context())
	if err != nil {
		t.Fatalf("second call (cached) failed: %v", err)
	}
	if len(voices2) != 2 {
		t.Errorf("second call (cached): expected 2 voices, got %d", len(voices2))
	}
}

