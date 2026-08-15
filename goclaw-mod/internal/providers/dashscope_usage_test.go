package providers

import (
	"encoding/json"
	"testing"
)

func TestOpenAIUsage_DashScopeCacheHit_Unmarshal(t *testing.T) {
	raw := `{
		"prompt_tokens": 2318,
		"completion_tokens": 195,
		"total_tokens": 2513,
		"prompt_tokens_details": {
			"text_tokens": 2318,
			"cache_creation_input_tokens": 0,
			"cached_tokens": 2304
		}
	}`
	var u openAIUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.PromptTokensDetails.CachedTokens != 2304 {
		t.Errorf("cached_tokens: got %d want 2304", u.PromptTokensDetails.CachedTokens)
	}
	if u.PromptTokensDetails.CacheCreationInputTokens != 0 {
		t.Errorf("cache_creation: got %d want 0", u.PromptTokensDetails.CacheCreationInputTokens)
	}
}

func TestOpenAIUsage_DashScopeCacheCreate_Unmarshal(t *testing.T) {
	raw := `{
		"prompt_tokens": 2318,
		"prompt_tokens_details": {
			"cache_creation_input_tokens": 2304,
			"cached_tokens": 0
		}
	}`
	var u openAIUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.PromptTokensDetails.CacheCreationInputTokens != 2304 {
		t.Errorf("got %d want 2304", u.PromptTokensDetails.CacheCreationInputTokens)
	}
}

func TestOpenAIUsage_NoDetails_OK(t *testing.T) {
	raw := `{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}`
	var u openAIUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.PromptTokensDetails != nil {
		t.Errorf("expected nil PromptTokensDetails, got %+v", u.PromptTokensDetails)
	}
}
