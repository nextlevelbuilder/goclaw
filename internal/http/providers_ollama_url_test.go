package http

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestNormalizeOllamaAPIBase validates write-time normalization applied before DB persist.
// The function must append /v1, strip redundant trailing slashes, and leave non-Ollama
// provider types untouched.
func TestNormalizeOllamaAPIBase(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		input        string
		want         string
	}{
		// --- Ollama normalization cases ---
		{
			name:         "bare host:port gets /v1 appended",
			providerType: store.ProviderOllama,
			input:        "http://host:11434",
			want:         "http://host:11434/v1",
		},
		{
			name:         "trailing slash is stripped before /v1 is appended",
			providerType: store.ProviderOllama,
			input:        "http://host:11434/",
			want:         "http://host:11434/v1",
		},
		{
			name:         "already-normalized /v1 is not doubled",
			providerType: store.ProviderOllama,
			input:        "http://host:11434/v1",
			want:         "http://host:11434/v1",
		},
		{
			name:         "trailing slash after /v1 is stripped",
			providerType: store.ProviderOllama,
			input:        "http://host:11434/v1/",
			want:         "http://host:11434/v1",
		},
		{
			name:         "empty api_base is left unchanged",
			providerType: store.ProviderOllama,
			input:        "",
			want:         "",
		},
		// --- OllamaCloud also normalized ---
		{
			name:         "OllamaCloud bare URL gets /v1",
			providerType: store.ProviderOllamaCloud,
			input:        "https://ollama.com",
			want:         "https://ollama.com/v1",
		},
		{
			name:         "OllamaCloud already-normalized /v1 unchanged",
			providerType: store.ProviderOllamaCloud,
			input:        "https://ollama.com/v1",
			want:         "https://ollama.com/v1",
		},
		// --- Non-Ollama types must NOT be touched ---
		{
			name:         "OpenAI-compat is not modified",
			providerType: store.ProviderOpenAICompat,
			input:        "http://host:11434",
			want:         "http://host:11434",
		},
		{
			name:         "Anthropic native is not modified",
			providerType: store.ProviderAnthropicNative,
			input:        "https://api.anthropic.com",
			want:         "https://api.anthropic.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &store.LLMProviderData{
				ProviderType: tt.providerType,
				APIBase:      tt.input,
			}
			normalizeOllamaAPIBase(p)
			if p.APIBase != tt.want {
				t.Fatalf("normalizeOllamaAPIBase() APIBase = %q, want %q", p.APIBase, tt.want)
			}
		})
	}
}

// TestResolveAPIBaseSafetyNet validates the read-time safety net in resolveAPIBase.
// Pre-existing DB records (written before write-time normalization) must receive /v1
// on read; already-normalized values and non-Ollama types must be returned as-is.
func TestResolveAPIBaseSafetyNet(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		apiBase      string
		want         string
	}{
		// Ollama missing /v1 — safety net fires
		{
			name:         "Ollama bare host:port gets /v1 via safety net",
			providerType: store.ProviderOllama,
			apiBase:      "http://host:11434",
			want:         "http://host:11434/v1",
		},
		{
			name:         "Ollama trailing slash gets /v1 via safety net",
			providerType: store.ProviderOllama,
			apiBase:      "http://host:11434/",
			want:         "http://host:11434/v1",
		},
		// Ollama already normalized — no double /v1
		{
			name:         "Ollama already has /v1 — no double append",
			providerType: store.ProviderOllama,
			apiBase:      "http://host:11434/v1",
			want:         "http://host:11434/v1",
		},
		{
			name:         "Ollama /v1 with trailing slash — trimmed, no double",
			providerType: store.ProviderOllama,
			apiBase:      "http://host:11434/v1/",
			want:         "http://host:11434/v1",
		},
		// Non-Ollama — must never have /v1 appended by safety net
		{
			name:         "OpenAI-compat is returned unchanged",
			providerType: store.ProviderOpenAICompat,
			apiBase:      "http://host:11434",
			want:         "http://host:11434",
		},
		{
			name:         "Anthropic native is returned unchanged",
			providerType: store.ProviderAnthropicNative,
			apiBase:      "https://api.anthropic.com",
			want:         "https://api.anthropic.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &ProvidersHandler{}
			p := &store.LLMProviderData{
				ProviderType: tt.providerType,
				APIBase:      tt.apiBase,
			}
			got := h.resolveAPIBase(p)
			if got != tt.want {
				t.Fatalf("resolveAPIBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResolveAPIBaseUsesAPIBaseFallback checks the config/env fallback path: when
// the DB value is empty and a fallback is configured, the fallback is used — and
// the Ollama safety-net normalization is applied to fallback values too.
func TestResolveAPIBaseUsesAPIBaseFallback(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		storedBase   string
		fallback     string
		want         string
	}{
		{
			name:         "empty DB, non-Ollama fallback returned as-is",
			providerType: store.ProviderOpenAICompat,
			storedBase:   "",
			fallback:     "https://api.example.com/v1",
			want:         "https://api.example.com/v1",
		},
		{
			name:         "empty DB, Ollama fallback missing /v1 gets safety net",
			providerType: store.ProviderOllama,
			storedBase:   "",
			fallback:     "http://localhost:11434",
			want:         "http://localhost:11434/v1",
		},
		{
			name:         "empty DB, Ollama fallback already has /v1 — no double",
			providerType: store.ProviderOllama,
			storedBase:   "",
			fallback:     "http://localhost:11434/v1",
			want:         "http://localhost:11434/v1",
		},
		{
			name:         "no fallback, empty DB, empty result",
			providerType: store.ProviderOllama,
			storedBase:   "",
			fallback:     "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &ProvidersHandler{
				apiBaseFallback: func(_ string) string { return tt.fallback },
			}
			p := &store.LLMProviderData{
				ProviderType: tt.providerType,
				APIBase:      tt.storedBase,
			}
			got := h.resolveAPIBase(p)
			if got != tt.want {
				t.Fatalf("resolveAPIBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRegisterInMemoryOllamaURLNormalization verifies that registerInMemory registers the
// Ollama provider with a correct base URL (already normalized with /v1 at write time)
// and does NOT produce a doubled /v1 in the registered provider.
func TestRegisterInMemoryOllamaURLNormalization(t *testing.T) {
	tests := []struct {
		name    string
		apiBase string // value as stored in DB (post-normalization)
		wantURL string // expected URLin the registered provider
	}{
		{
			name:    "normalized /v1 is registered unchanged",
			apiBase: "http://host:11434/v1",
			wantURL: "http://host:11434/v1",
		},
		{
			name:    "empty api_base falls back to default with /v1",
			apiBase: "",
			wantURL: "http://localhost:11434/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerReg := providers.NewRegistry(nil)
			handler := NewProvidersHandler(newMockProviderStore(), newMockSecretsStore(), providerReg, "")

			p := &store.LLMProviderData{
				BaseModel:    store.BaseModel{ID: uuid.New()},
				TenantID:     uuid.New(),
				Name:         "my-ollama",
				ProviderType: store.ProviderOllama,
				APIBase:      tt.apiBase,
				Enabled:      true,
			}

			handler.registerInMemory(p)

			runtimeProvider, err := providerReg.GetForTenant(p.TenantID, p.Name)
			if err != nil {
				t.Fatalf("GetForTenant() error = %v", err)
			}

			type apiBaseProvider interface {
				APIBase() string
			}
			abp, ok := runtimeProvider.(apiBaseProvider)
			if !ok {
				t.Fatalf("registered provider does not implement APIBase(), type = %T", runtimeProvider)
			}

			got := abp.APIBase()
			if got != tt.wantURL {
				t.Fatalf("registered APIBase = %q, want %q", got, tt.wantURL)
			}
		})
	}
}
