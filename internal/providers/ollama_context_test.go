package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchOllamaModelContext_Success(t *testing.T) {
	type modelInfo struct {
		ContextLength int `json:"context_length"`
	}
	type response struct {
		ModelInfo modelInfo `json:"model_info"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/show", r.URL.Path)
		assert.Equal(t, "llama3", r.URL.Query().Get("model"))

		resp := response{ModelInfo: modelInfo{ContextLength: 8192}}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "llama3", "")
	assert.Equal(t, 8192, got)
}

func TestFetchOllamaModelContext_SuccessWithV1Suffix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/show", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"model_info":{"context_length":32768}}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL+"/v1", "mistral", "")
	assert.Equal(t, 32768, got)
}

func TestFetchOllamaModelContext_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "llama3", "")
	assert.Equal(t, OllamaDefaultNumCtx, got)
}

func TestFetchOllamaModelContext_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{this is not valid json`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "llama3", "")
	assert.Equal(t, OllamaDefaultNumCtx, got)
}

func TestFetchOllamaModelContext_ZeroContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"model_info":{"context_length":0}}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "llama3", "")
	assert.Equal(t, OllamaDefaultNumCtx, got)
}

func TestFetchOllamaModelContext_MissingModelInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"name":"llama3","size":4000000000}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "llama3", "")
	assert.Equal(t, OllamaDefaultNumCtx, got)
}

func TestFetchOllamaModelContext_APIKeyHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer secret-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"model_info":{"context_length":16384}}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "llama3", "secret-token")
	assert.Equal(t, 16384, got)
}

func TestFetchOllamaModelContext_ConnectionRefused(t *testing.T) {
	got := FetchOllamaModelContext(context.Background(), "http://127.0.0.1:19999", "llama3", "")
	assert.Equal(t, OllamaDefaultNumCtx, got)
}
