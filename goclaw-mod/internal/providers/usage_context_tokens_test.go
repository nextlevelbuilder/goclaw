package providers

import "testing"

// TestUsage_ContextTokens guards the "current context size" computation used by
// the sessions context-usage display and compaction calibration. Anthropic-style
// accounting reports input_tokens EXCLUDING cached segments, so the real context
// of a call is prompt + cache_read + cache_creation. OpenAI-style accounting
// (PromptTokensIncludeCachedSegments=true) already includes cached tokens.
func TestUsage_ContextTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		usage Usage
		want  int
	}{
		{
			name: "anthropic-style excludes cached segments — add them back",
			usage: Usage{
				PromptTokens:        1000,
				CacheReadTokens:     80000,
				CacheCreationTokens: 5000,
			},
			want: 86000,
		},
		{
			name: "openai-style already includes cached segments",
			usage: Usage{
				PromptTokens:                      86000,
				CacheReadTokens:                   80000,
				PromptTokensIncludeCachedSegments: true,
			},
			want: 86000,
		},
		{
			name:  "zero value",
			usage: Usage{},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.usage.ContextTokens(); got != tt.want {
				t.Errorf("ContextTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}
