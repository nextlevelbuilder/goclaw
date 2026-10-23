package store

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseThinkingEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		settings json.RawMessage
		want     *bool
	}{
		{
			name:     "unset settings",
			settings: nil,
			want:     nil,
		},
		{
			name:     "empty object",
			settings: json.RawMessage(`{}`),
			want:     nil,
		},
		{
			name:     "explicit true",
			settings: json.RawMessage(`{"thinking_enabled":true}`),
			want:     &trueVal,
		},
		{
			name:     "explicit false",
			settings: json.RawMessage(`{"thinking_enabled":false}`),
			want:     &falseVal,
		},
		{
			name:     "coexists with other settings keys",
			settings: json.RawMessage(`{"num_ctx":8192,"thinking_enabled":true}`),
			want:     &trueVal,
		},
		{
			name:     "malformed json",
			settings: json.RawMessage(`{not valid json`),
			want:     nil,
		},
		{
			name:     "wrong type is ignored",
			settings: json.RawMessage(`{"thinking_enabled":"yes"}`),
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseThinkingEnabled(tc.settings)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}

func TestNormalizeProviderType(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"custom", ProviderOpenAICompat},
		{"Custom", ProviderOpenAICompat},
		{" custom ", ProviderOpenAICompat},
		{ProviderOpenAICompat, ProviderOpenAICompat},
		{ProviderOpenRouter, ProviderOpenRouter},
		{"not-a-provider", "not-a-provider"},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, NormalizeProviderType(tt.in), "input %q", tt.in)
	}
	// The alias must resolve to a type the create/update handlers accept.
	assert.True(t, ValidProviderTypes[NormalizeProviderType(ProviderTypeCustomAlias)])
	// The alias itself is not a stored provider type.
	assert.False(t, ValidProviderTypes[ProviderTypeCustomAlias])
}
