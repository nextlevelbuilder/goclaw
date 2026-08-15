package providers

import (
	"testing"
)

func TestServiceTierMiddleware(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		cfg      MiddlewareConfig
		expected map[string]any
	}{
		{
			name: "anthropic with valid tier 'auto'",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "api_key",
				Options: map[string]any{
					OptServiceTier: "auto",
				},
			},
			expected: map[string]any{
				"model":        "claude-3-opus",
				"service_tier": "auto",
			},
		},
		{
			name: "anthropic with valid tier 'standard_only'",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "api_key",
				Options: map[string]any{
					OptServiceTier: "standard_only",
				},
			},
			expected: map[string]any{
				"model":        "claude-3-opus",
				"service_tier": "standard_only",
			},
		},
		{
			name: "anthropic with OAuth auth - tier not injected",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "oauth",
				Options: map[string]any{
					OptServiceTier: "auto",
				},
			},
			expected: map[string]any{"model": "claude-3-opus"},
		},
		{
			name: "anthropic with invalid tier - not injected",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "api_key",
				Options: map[string]any{
					OptServiceTier: "invalid_tier",
				},
			},
			expected: map[string]any{"model": "claude-3-opus"},
		},
		{
			name: "openai with valid tier 'auto'",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: "auto",
				},
			},
			expected: map[string]any{
				"model":        "gpt-4",
				"service_tier": "auto",
			},
		},
		{
			name: "openai with valid tier 'default'",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: "default",
				},
			},
			expected: map[string]any{
				"model":        "gpt-4",
				"service_tier": "default",
			},
		},
		{
			name: "openai with valid tier 'flex'",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: "flex",
				},
			},
			expected: map[string]any{
				"model":        "gpt-4",
				"service_tier": "flex",
			},
		},
		{
			name: "openai with valid tier 'priority'",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: "priority",
				},
			},
			expected: map[string]any{
				"model":        "gpt-4",
				"service_tier": "priority",
			},
		},
		{
			name: "openai with invalid tier - not injected",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: "invalid",
				},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "pre-existing service_tier not overridden",
			body: map[string]any{
				"model":        "gpt-4",
				"service_tier": "default",
			},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: "priority",
				},
			},
			expected: map[string]any{
				"model":        "gpt-4",
				"service_tier": "default", // original unchanged
			},
		},
		{
			name: "no service_tier option - body unchanged",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options:  map[string]any{},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "service_tier not a string - ignored",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: 123, // invalid type
				},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "service_tier empty string - ignored",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptServiceTier: "",
				},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "nil options map - no injection",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options:  nil,
			},
			expected: map[string]any{"model": "gpt-4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ServiceTierMiddleware(tt.body, tt.cfg)

			// Check all expected keys exist with correct values
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

func TestFastModeMiddleware(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		cfg      MiddlewareConfig
		expected map[string]any
	}{
		{
			name: "anthropic fast=true maps to tier auto",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "api_key",
				Options: map[string]any{
					OptFastMode: true,
				},
			},
			expected: map[string]any{
				"model":        "claude-3-opus",
				"service_tier": "auto",
			},
		},
		{
			name: "anthropic fast=false maps to tier standard_only",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "api_key",
				Options: map[string]any{
					OptFastMode: false,
				},
			},
			expected: map[string]any{
				"model":        "claude-3-opus",
				"service_tier": "standard_only",
			},
		},
		{
			name: "anthropic OAuth fast=true - tier not injected",
			body: map[string]any{"model": "claude-3-opus"},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "oauth",
				Options: map[string]any{
					OptFastMode: true,
				},
			},
			expected: map[string]any{"model": "claude-3-opus"},
		},
		{
			name: "openai fast=true maps to tier priority",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptFastMode: true,
				},
			},
			expected: map[string]any{
				"model":        "gpt-4",
				"service_tier": "priority",
			},
		},
		{
			name: "openai fast=false does not set tier",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptFastMode: false,
				},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "pre-existing service_tier not overridden",
			body: map[string]any{
				"model":        "gpt-4",
				"service_tier": "default",
			},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptFastMode: true,
				},
			},
			expected: map[string]any{
				"model":        "gpt-4",
				"service_tier": "default", // original unchanged
			},
		},
		{
			name: "no fast_mode option - body unchanged",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options:  map[string]any{},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "fast_mode not a bool - ignored",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options: map[string]any{
					OptFastMode: "true", // string, not bool
				},
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "nil options map - no injection",
			body: map[string]any{"model": "gpt-4"},
			cfg: MiddlewareConfig{
				Provider: "openai",
				Options:  nil,
			},
			expected: map[string]any{"model": "gpt-4"},
		},
		{
			name: "anthropic with both fast and service_tier (service_tier wins)",
			body: map[string]any{
				"model":        "claude-3-opus",
				"service_tier": "standard_only",
			},
			cfg: MiddlewareConfig{
				Provider: "anthropic",
				AuthType: "api_key",
				Options: map[string]any{
					OptFastMode: true, // would set to "auto"
				},
			},
			expected: map[string]any{
				"model":        "claude-3-opus",
				"service_tier": "standard_only", // not overridden
			},
		},
		{
			name: "groq (openai-compatible) fast=true",
			body: map[string]any{"model": "mixtral-8x7b"},
			cfg: MiddlewareConfig{
				Provider: "openai", // Groq uses openai provider type
				Options: map[string]any{
					OptFastMode: true,
				},
			},
			expected: map[string]any{
				"model":        "mixtral-8x7b",
				"service_tier": "priority",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FastModeMiddleware(tt.body, tt.cfg)

			// Check all expected keys exist with correct values
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
