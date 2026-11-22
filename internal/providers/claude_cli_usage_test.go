package providers

import "testing"

// Claude CLI result usage includes cache_read_input_tokens and
// cache_creation_input_tokens. input_tokens excludes those segments, and
// ContextTokens adds them back. Dropping the cache fields under-reports context.
func TestParseJSONResponse_IncludesCacheUsage(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":12,"cache_creation_input_tokens":30,"cache_read_input_tokens":800,"output_tokens":7}}`)
	resp, err := parseJSONResponse(raw)
	if err != nil {
		t.Fatalf("parseJSONResponse: %v", err)
	}
	assertCLICacheUsage(t, resp, 12, 7, 30, 800, 842)
}

func TestParseJSONArray_IncludesCacheUsage(t *testing.T) {
	raw := []byte(`[{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}},{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":4,"cache_creation_input_tokens":8,"cache_read_input_tokens":100,"output_tokens":2}}]`)
	assertCLICacheUsage(t, parseJSONArray(raw), 4, 2, 8, 100, 112)
}

func TestParseJSONResponse_OmitsAbsentCacheUsage(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":5,"output_tokens":3}}`)
	resp, err := parseJSONResponse(raw)
	if err != nil {
		t.Fatalf("parseJSONResponse: %v", err)
	}
	assertCLICacheUsage(t, resp, 5, 3, 0, 0, 5)
}

func assertCLICacheUsage(t *testing.T, resp *ChatResponse, prompt, completion, cacheCreate, cacheRead, context int) {
	t.Helper()
	if resp == nil || resp.Usage == nil {
		t.Fatal("expected usage")
	}
	u := resp.Usage
	if u.PromptTokens != prompt || u.CompletionTokens != completion || u.CacheCreationTokens != cacheCreate || u.CacheReadTokens != cacheRead {
		t.Errorf("usage = %+v, want prompt=%d completion=%d cacheCreate=%d cacheRead=%d", u, prompt, completion, cacheCreate, cacheRead)
	}
	if got := u.ContextTokens(); got != context {
		t.Errorf("ContextTokens() = %d, want %d", got, context)
	}
	if u.TotalTokens != prompt+completion {
		t.Errorf("TotalTokens = %d, want %d", u.TotalTokens, prompt+completion)
	}
}
