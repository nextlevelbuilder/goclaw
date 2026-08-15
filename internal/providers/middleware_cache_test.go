package providers

import (
	"testing"
)

func TestCacheMiddleware(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		cfg      MiddlewareConfig
		expected map[string]any
	}{
		{
			name: "native OpenAI with both cache params",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://api.openai.com/v1",
				Options: map[string]any{
					OptPromptCacheKey:       "test-key-123",
					OptPromptCacheRetention: "5m",
				},
			},
			expected: map[string]any{
				"model":                  "gpt-4",
				"prompt_cache_key":       "test-key-123",
				"prompt_cache_retention": "5m",
			},
		},
		{
			name: "native OpenAI with only cache key",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://api.openai.com/v1",
				Options: map[string]any{
					OptPromptCacheKey: "test-key-456",
				},
			},
			expected: map[string]any{
				"model":            "gpt-4",
				"prompt_cache_key": "test-key-456",
			},
		},
		{
			name: "native OpenAI with only retention",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://api.openai.com/v1",
				Options: map[string]any{
					OptPromptCacheRetention: "10m",
				},
			},
			expected: map[string]any{
				"model":                  "gpt-4",
				"prompt_cache_retention": "10m",
			},
		},
		{
			name: "proxy endpoint - cache params ignored",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://proxy.example.com/v1",
				Options: map[string]any{
					OptPromptCacheKey:       "test-key",
					OptPromptCacheRetention: "5m",
				},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "azure endpoint - cache params ignored",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://myinstance.openai.azure.com/openai/deployments/gpt-4/chat/completions",
				Options: map[string]any{
					OptPromptCacheKey: "test-key",
				},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "non-OpenAI provider - cache params ignored",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				APIBase:  "https://api.anthropic.com/v1",
				Options: map[string]any{
					OptPromptCacheKey:       "test-key",
					OptPromptCacheRetention: "5m",
				},
			},
			expected: map[string]any{"model": "claude-3-opus"},
		},
		{
			name: "no cache options - body unchanged",
			body: map[string]any{"model": "gpt-4", "temperature": 0.7},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://api.openai.com/v1",
				Options:  map[string]any{},
			},
			expected: map[string]any{"model": "gpt-4", "temperature": 0.7},
		},
		{
			name: "native OpenAI with nil options map",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://api.openai.com/v1",
				Options:  nil,
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "empty body",
			body: map[string]any{},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://api.openai.com/v1",
				Options: map[string]any{
					OptPromptCacheKey: "test-key",
				},
			},
			expected: map[string]any{
				"prompt_cache_key": "test-key",
			},
		},
		{
			name: "native OpenAI case insensitive URL match",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				APIBase:  "https://API.OPENAI.COM/v1",
				Options: map[string]any{
					OptPromptCacheKey: "test-key",
				},
			},
			expected: map[string]any{
				"model":            "gpt-4",
				"prompt_cache_key": "test-key",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CacheMiddleware(tt.body, tt.cfg)

			// Check all expected keys exist
			for key, val := range tt.expected {
				if result[key] != val {
					t.Errorf("key %q: got %v, want %v", key, result[key], val)
				}
			}

			// Check no unexpected keys were added
			for key := range result {
				if _, found := tt.expected[key]; !found {
					t.Errorf("unexpected key in result: %q with value %v", key, result[key])
				}
			}
		})
	}
}
