package providers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOllamaBuildRequest_ThinkDefaultsToFalse verifies that when no
// provider-level thinking override is configured, buildRequest disables
// thinking by default (matches the OpenAI-compat Ollama path).
func TestOllamaBuildRequest_ThinkDefaultsToFalse(t *testing.T) {
	p := NewOllamaProvider("ollama", "http://localhost:11434", "llama3.3", nil, nil)
	req := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	ollamaReq := p.buildRequest(context.Background(), req, false)

	require.NotNil(t, ollamaReq.Think)
	value, ok := ollamaReq.Think.Value.(bool)
	require.True(t, ok, "expected Think.Value to be a bool")
	assert.False(t, value)
}

// TestOllamaBuildRequest_ThinkOverride verifies that WithThinkingEnabled
// correctly sets ollamaReq.Think for both true and false overrides.
func TestOllamaBuildRequest_ThinkOverride(t *testing.T) {
	trueVal := true
	falseVal := false

	cases := []struct {
		name     string
		override *bool
		want     bool
	}{
		{name: "override true enables thinking", override: &trueVal, want: true},
		{name: "override false disables thinking", override: &falseVal, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewOllamaProvider("ollama", "http://localhost:11434", "llama3.3", nil, nil).
				WithThinkingEnabled(tc.override)
			req := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
			ollamaReq := p.buildRequest(context.Background(), req, false)

			require.NotNil(t, ollamaReq.Think)
			value, ok := ollamaReq.Think.Value.(bool)
			require.True(t, ok, "expected Think.Value to be a bool")
			assert.Equal(t, tc.want, value)
		})
	}
}

// TestOllamaThinkingEnabledAccessor verifies the WithThinkingEnabled /
// ThinkingEnabled getter/setter round-trip.
func TestOllamaThinkingEnabledAccessor(t *testing.T) {
	p := NewOllamaProvider("ollama", "http://localhost:11434", "llama3.3", nil, nil)
	assert.Nil(t, p.ThinkingEnabled())

	trueVal := true
	p.WithThinkingEnabled(&trueVal)
	require.NotNil(t, p.ThinkingEnabled())
	assert.True(t, *p.ThinkingEnabled())
}
