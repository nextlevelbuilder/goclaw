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
		// /api/show is POST-only and takes the model in a JSON body; a GET with a
		// query param is answered "405 method not allowed" by a real Ollama server.
		assert.Equal(t, http.MethodPost, r.Method)
		var body struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "llama3", body.Model)

		resp := response{ModelInfo: modelInfo{ContextLength: 8192}}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "llama3", "")
	assert.Equal(t, 8192, got)
}

// TestFetchOllamaModelContext_ArchNamespacedKey covers what a real Ollama server
// actually returns: the context length is namespaced by model architecture, never
// exposed under a bare "context_length" key.
func TestFetchOllamaModelContext_ArchNamespacedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"model_info":{"general.architecture":"gemma4","gemma4.context_length":131072,"gemma4.block_count":34}}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	got := FetchOllamaModelContext(context.Background(), srv.URL, "gemma-4-e4b", "")
	assert.Equal(t, 131072, got)
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

// TestOllamaProviderResolvesNumCtxPerModel proves the provider asks about the model
// the caller actually requested — not a hardcoded name — and caches the answer.
func TestOllamaProviderResolvesNumCtxPerModel(t *testing.T) {
	var calls int
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		asked = append(asked, body.Model)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"model_info":{"gemma4.context_length":65536}}`))
		require.NoError(t, err)
	}))
	defer srv.Close()

	provider := NewOllamaProvider("test", srv.URL, "unused-default", nil, nil)

	req := ChatRequest{Model: "gemma-4-e4b", Messages: []Message{{Role: "user", Content: "hi"}}}
	first := provider.buildRequest(context.Background(), req, false)
	assert.Equal(t, 65536, first.Options["num_ctx"])
	assert.Equal(t, []string{"gemma-4-e4b"}, asked)

	// Second call for the same model must be served from cache.
	second := provider.buildRequest(context.Background(), req, false)
	assert.Equal(t, 65536, second.Options["num_ctx"])
	assert.Equal(t, 1, calls, "second request for the same model should hit the cache")
}

// TestOllamaProviderExplicitNumCtxWins proves the operator's configured value takes
// priority and skips the lookup entirely — the lever for capping KV cache to fit VRAM.
func TestOllamaProviderExplicitNumCtxWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("/api/show must not be queried when num_ctx is configured explicitly")
	}))
	defer srv.Close()

	configured := 16384
	provider := NewOllamaProvider("test", srv.URL, "m", &configured, nil)

	req := ChatRequest{Model: "any-model", Messages: []Message{{Role: "user", Content: "hi"}}}
	built := provider.buildRequest(context.Background(), req, false)
	assert.Equal(t, 16384, built.Options["num_ctx"])
}
