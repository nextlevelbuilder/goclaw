package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Helper: create a valid 1536-dim embedding vector
func makeEmbedding(dim int) []float64 {
	vec := make([]float64, dim)
	for i := range vec {
		vec[i] = float64(i) * 0.001 // Small variation per index
	}
	return vec
}

// TestOpenAIEmbedding_Success tests successful embedding request
func TestOpenAIEmbedding_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Bearer") {
			t.Fatal("missing or invalid Authorization header")
		}

		// Parse request body
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Get input texts
		inputs, ok := reqBody["input"].([]any)
		if !ok {
			http.Error(w, "invalid input format", http.StatusBadRequest)
			return
		}

		// Build response with embeddings for each input
		data := make([]embeddingData, len(inputs))
		for i := range inputs {
			data[i] = embeddingData{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     i,
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	texts := []string{"hello", "world"}
	embeddings, err := provider.Embed(context.Background(), texts)

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(embeddings))
	}
	for i, emb := range embeddings {
		if len(emb) != ExpectedEmbeddingDim {
			t.Fatalf("embedding %d: expected dim %d, got %d", i, ExpectedEmbeddingDim, len(emb))
		}
	}
}

// TestOpenAIEmbedding_BatchSplitting tests that large input is split into batches
func TestOpenAIEmbedding_BatchSplitting(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)

		inputs, _ := reqBody["input"].([]any)

		// Verify batch size constraints
		if callCount == 1 {
			if len(inputs) != embeddingBatchSize {
				t.Fatalf("batch 1: expected %d inputs, got %d", embeddingBatchSize, len(inputs))
			}
		} else if callCount == 2 {
			expectedLen := 3000 - embeddingBatchSize // remainder
			if len(inputs) != expectedLen {
				t.Fatalf("batch 2: expected %d inputs, got %d", expectedLen, len(inputs))
			}
		}

		// Build response
		data := make([]embeddingData, len(inputs))
		for i := range inputs {
			data[i] = embeddingData{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     i,
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	// Create 3000 texts (should split into 2 batches: 2048 + 952)
	texts := make([]string, 3000)
	for i := range texts {
		texts[i] = fmt.Sprintf("text %d", i)
	}

	embeddings, err := provider.Embed(context.Background(), texts)

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 API calls, got %d", callCount)
	}
	if len(embeddings) != 3000 {
		t.Fatalf("expected 3000 embeddings, got %d", len(embeddings))
	}
}

// TestOpenAIEmbedding_DimensionMismatch tests error when API returns wrong dimension
func TestOpenAIEmbedding_DimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return wrong dimension (512 instead of 1536)
		data := []embeddingData{
			{
				Embedding: makeEmbedding(512), // Wrong!
				Index:     0,
			},
		}
		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"test"})

	if err == nil {
		t.Fatal("expected dimension mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "dimension mismatch") {
		t.Fatalf("expected 'dimension mismatch' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "returned 512") {
		t.Fatalf("expected '512' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "expected 1536") {
		t.Fatalf("expected '1536' in error, got: %v", err)
	}
}

// TestOpenAIEmbedding_EmptyInput tests that empty input returns nil
func TestOpenAIEmbedding_EmptyInput(t *testing.T) {
	provider := NewOpenAIEmbeddingProvider("test-key", "https://api.openai.com/v1", "text-embedding-3-small")

	embeddings, err := provider.Embed(context.Background(), []string{})

	if err != nil {
		t.Fatalf("Embed with empty input should not error, got: %v", err)
	}
	if embeddings != nil {
		t.Fatalf("expected nil for empty input, got %v", embeddings)
	}
}

// TestOpenAIEmbedding_HTTPError tests handling of HTTP 429 (rate limit)
func TestOpenAIEmbedding_HTTPError_429(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		// Always return 429 to trigger retry exhaustion
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"error": "rate limited"}`)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"test"})

	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	// With default config (Attempts: 3), should retry 3 times total
	if attemptCount != 3 {
		t.Fatalf("expected 3 attempts, got %d", attemptCount)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("expected '429' in error, got: %v", err)
	}
}

// TestOpenAIEmbedding_InvalidJSON tests error handling for malformed response
func TestOpenAIEmbedding_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "not valid json")
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"test"})

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "Unmarshal") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

// TestOpenAIEmbedding_ProviderName tests Name() returns expected provider name
func TestOpenAIEmbedding_ProviderName(t *testing.T) {
	provider := NewOpenAIEmbeddingProvider("test-key", "https://api.openai.com/v1", "text-embedding-3-small")

	if provider.Name() != "openai" {
		t.Fatalf("expected Name() = 'openai', got %s", provider.Name())
	}
}

// TestOpenAIEmbedding_Model tests Model() returns expected model
func TestOpenAIEmbedding_Model(t *testing.T) {
	provider := NewOpenAIEmbeddingProvider("test-key", "https://api.openai.com/v1", "text-embedding-3-small")

	if provider.Model() != "text-embedding-3-small" {
		t.Fatalf("expected Model() = 'text-embedding-3-small', got %s", provider.Model())
	}
}

// TestOpenAIEmbedding_DefaultBaseURL tests that default base URL is set
func TestOpenAIEmbedding_DefaultBaseURL(t *testing.T) {
	provider := NewOpenAIEmbeddingProvider("test-key", "", "text-embedding-3-small")

	if !strings.Contains(provider.apiBase, "api.openai.com") {
		t.Fatalf("expected default base URL to contain 'api.openai.com', got %s", provider.apiBase)
	}
}

// TestOpenAIEmbedding_DefaultModel tests that default model is set
func TestOpenAIEmbedding_DefaultModel(t *testing.T) {
	provider := NewOpenAIEmbeddingProvider("test-key", "https://api.openai.com/v1", "")

	if provider.Model() != "text-embedding-3-small" {
		t.Fatalf("expected default model 'text-embedding-3-small', got %s", provider.Model())
	}
}

// TestOpenAIEmbedding_HeaderTrimming tests that base URL is trimmed
func TestOpenAIEmbedding_HeaderTrimming(t *testing.T) {
	provider := NewOpenAIEmbeddingProvider("test-key", "https://api.openai.com/v1/", "text-embedding-3-small")

	// Should trim trailing slash
	if strings.HasSuffix(provider.apiBase, "/") {
		t.Fatalf("base URL should not have trailing slash, got %s", provider.apiBase)
	}
}

// TestOpenAIEmbedding_OutputOrdering tests that embeddings are returned in order
func TestOpenAIEmbedding_OutputOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return embeddings in reverse order to test ordering logic
		data := []embeddingData{
			{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     2,
			},
			{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     0,
			},
			{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     1,
			},
		}
		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	embeddings, err := provider.Embed(context.Background(), []string{"a", "b", "c"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	// Verify all positions are filled (no nil)
	for i, emb := range embeddings {
		if emb == nil {
			t.Fatalf("embedding at index %d is nil (not filled by response)", i)
		}
	}
}

// TestVoyageEmbeddingProvider_Name tests VoyageEmbeddingProvider name
func TestVoyageEmbeddingProvider_Name(t *testing.T) {
	provider := NewVoyageEmbeddingProvider("test-key", "voyage-3-large")

	if provider.Name() != "voyage" {
		t.Fatalf("expected Name() = 'voyage', got %s", provider.Name())
	}
}

// TestVoyageEmbeddingProvider_BaseURL tests VoyageEmbeddingProvider uses correct base URL
func TestVoyageEmbeddingProvider_BaseURL(t *testing.T) {
	provider := NewVoyageEmbeddingProvider("test-key", "voyage-3-large")

	if !strings.Contains(provider.apiBase, "voyageai.com") {
		t.Fatalf("expected base URL to contain 'voyageai.com', got %s", provider.apiBase)
	}
}

// TestVoyageEmbeddingProvider_DefaultModel tests VoyageEmbeddingProvider default model
func TestVoyageEmbeddingProvider_DefaultModel(t *testing.T) {
	provider := NewVoyageEmbeddingProvider("test-key", "")

	if provider.Model() != "voyage-3-large" {
		t.Fatalf("expected default model 'voyage-3-large', got %s", provider.Model())
	}
}

// TestVoyageEmbeddingProvider_FunctionalEmbed tests VoyageEmbeddingProvider with actual embedding call
func TestVoyageEmbeddingProvider_FunctionalEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		inputs, _ := reqBody["input"].([]any)

		data := make([]embeddingData, len(inputs))
		for i := range inputs {
			data[i] = embeddingData{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     i,
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider with custom server URL (bypassing actual voyageai.com)
	provider := NewVoyageEmbeddingProvider("test-key", "voyage-3-large")
	provider.apiBase = server.URL // Override for testing

	embeddings, err := provider.Embed(context.Background(), []string{"test"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(embeddings))
	}
}

// TestOpenAIEmbedding_MultipleTexts tests embedding multiple different texts
func TestOpenAIEmbedding_MultipleTexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		inputs, _ := reqBody["input"].([]any)

		data := make([]embeddingData, len(inputs))
		for i := range inputs {
			data[i] = embeddingData{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     i,
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	texts := []string{"hello world", "how are you", "this is a test", "embeddings are great"}
	embeddings, err := provider.Embed(context.Background(), texts)

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embeddings) != len(texts) {
		t.Fatalf("expected %d embeddings, got %d", len(texts), len(embeddings))
	}
}

// TestOpenAIEmbedding_ContextCancellation tests that cancelled context stops embedding
func TestOpenAIEmbedding_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just hang to simulate slow server
		select {}
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := provider.Embed(ctx, []string{"test"})

	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	// Error should be wrapped with context.Canceled somewhere in the chain
	if err != context.Canceled && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

// TestOpenAIEmbedding_LargeVectorConversion tests float64 to float32 conversion
func TestOpenAIEmbedding_LargeVectorConversion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a vector with specific values to verify conversion
		vec := make([]float64, ExpectedEmbeddingDim)
		for i := range vec {
			vec[i] = float64(i) * 0.5 // Known values
		}

		data := []embeddingData{
			{
				Embedding: vec,
				Index:     0,
			},
		}
		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	embeddings, err := provider.Embed(context.Background(), []string{"test"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embeddings[0]) != ExpectedEmbeddingDim {
		t.Fatalf("expected %d dimensions, got %d", ExpectedEmbeddingDim, len(embeddings[0]))
	}
	// Verify conversion happened (check a specific value)
	if embeddings[0][0] != 0.0 {
		t.Fatalf("expected first value to be 0.0, got %f", embeddings[0][0])
	}
}

// TestOpenAIEmbedding_RequestHeaders tests that correct headers are sent
func TestOpenAIEmbedding_RequestHeaders(t *testing.T) {
	headerCheck := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Fatal("invalid Authorization header format")
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Fatalf("expected Content-Type application/json, got %s", contentType)
		}

		headerCheck = true

		data := []embeddingData{
			{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     0,
			},
		}
		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"test"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if !headerCheck {
		t.Fatal("header verification callback was not called")
	}
}

// TestOpenAIEmbedding_RequestBody tests that request body is formatted correctly
func TestOpenAIEmbedding_RequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Verify model is sent
		model, ok := reqBody["model"].(string)
		if !ok || model != "text-embedding-3-small" {
			t.Fatal("invalid or missing model in request")
		}

		// Verify input is array
		inputs, ok := reqBody["input"].([]any)
		if !ok {
			t.Fatal("invalid input format in request")
		}
		if len(inputs) != 2 {
			t.Fatalf("expected 2 inputs, got %d", len(inputs))
		}

		data := make([]embeddingData, len(inputs))
		for i := range inputs {
			data[i] = embeddingData{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     i,
			}
		}

		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"hello", "world"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
}

// TestOpenAIEmbedding_BatchBoundary tests batch boundary conditions
func TestOpenAIEmbedding_BatchBoundary(t *testing.T) {
	tests := []struct {
		name         string
		inputCount   int
		expectedCalls int
	}{
		{"single", 1, 1},
		{"batch size", embeddingBatchSize, 1},
		{"batch size + 1", embeddingBatchSize + 1, 2},
		{"double batch", embeddingBatchSize * 2, 2},
		{"double batch + 1", embeddingBatchSize*2 + 1, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				var reqBody map[string]any
				json.NewDecoder(r.Body).Decode(&reqBody)
				inputs, _ := reqBody["input"].([]any)

				data := make([]embeddingData, len(inputs))
				for i := range inputs {
					data[i] = embeddingData{
						Embedding: makeEmbedding(ExpectedEmbeddingDim),
						Index:     i,
					}
				}

				resp := embeddingResponse{Data: data}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

			texts := make([]string, tt.inputCount)
			for i := range texts {
				texts[i] = fmt.Sprintf("text %d", i)
			}

			embeddings, err := provider.Embed(context.Background(), texts)

			if err != nil {
				t.Fatalf("Embed failed: %v", err)
			}
			if callCount != tt.expectedCalls {
				t.Fatalf("expected %d API calls, got %d", tt.expectedCalls, callCount)
			}
			if len(embeddings) != tt.inputCount {
				t.Fatalf("expected %d embeddings, got %d", tt.inputCount, len(embeddings))
			}
		})
	}
}

// TestOpenAIEmbedding_PartialResponse tests handling of partial responses
func TestOpenAIEmbedding_PartialResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return fewer embeddings than requested
		data := []embeddingData{
			{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     0,
			},
		}
		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	embeddings, err := provider.Embed(context.Background(), []string{"a", "b", "c"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	// Should return 3 embeddings even though only 1 was in response
	if len(embeddings) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(embeddings))
	}
	// First embedding should be present, others nil
	if embeddings[0] == nil {
		t.Fatal("first embedding should not be nil")
	}
	if embeddings[1] != nil {
		t.Fatal("second embedding should be nil (not in response)")
	}
}

// TestOpenAIEmbedding_HTTPError_500 tests handling of HTTP 500 with retries
func TestOpenAIEmbedding_HTTPError_500(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"error": "internal server error"}`)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"test"})

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if attemptCount != 3 {
		t.Fatalf("expected 3 attempts, got %d", attemptCount)
	}
}

// TestOpenAIEmbedding_HTTPError_400 tests that non-retryable errors fail immediately
func TestOpenAIEmbedding_HTTPError_400(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"error": "bad request"}`)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"test"})

	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	// Should not retry for 400 — only one attempt
	if attemptCount != 1 {
		t.Fatalf("expected 1 attempt (no retry for 400), got %d", attemptCount)
	}
}

// TestOpenAIEmbedding_Float64Float32Conversion tests type conversion correctness
func TestOpenAIEmbedding_Float64Float32Conversion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vec := make([]float64, ExpectedEmbeddingDim)
		// Use specific values that test precision
		vec[0] = 1.0
		vec[1] = 0.5
		vec[2] = 0.25
		vec[3] = -0.125

		data := []embeddingData{
			{
				Embedding: vec,
				Index:     0,
			},
		}
		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	embeddings, _ := provider.Embed(context.Background(), []string{"test"})

	vec := embeddings[0]
	if vec[0] != 1.0 || vec[1] != 0.5 || vec[2] != 0.25 || vec[3] != -0.125 {
		t.Fatalf("conversion lost precision: got %v", []float32{vec[0], vec[1], vec[2], vec[3]})
	}
}

// TestOpenAIEmbedding_RawBytes tests that request body is properly encoded
func TestOpenAIEmbedding_RawBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read raw body and verify it's valid JSON
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Fatal("request body is empty")
		}

		var reqBody map[string]any
		if err := json.Unmarshal(body, &reqBody); err != nil {
			t.Fatalf("request body is not valid JSON: %v", err)
		}

		data := []embeddingData{
			{
				Embedding: makeEmbedding(ExpectedEmbeddingDim),
				Index:     0,
			},
		}
		resp := embeddingResponse{Data: data}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOpenAIEmbeddingProvider("test-key", server.URL, "text-embedding-3-small")

	_, err := provider.Embed(context.Background(), []string{"test"})

	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
}
