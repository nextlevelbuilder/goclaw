package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Quality Gate Tests ---

func TestIsQualityContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"short", "hello world", false},
		{"under_100_chars", strings.Repeat("x", 99), false},
		{"100_chars_few_words", strings.Repeat("x", 100), false}, // 1 word
		{"whitespace_heavy", strings.Repeat("  a  ", 30), true},  // 30 words, >100 chars
		{"valid_paragraph", "The quick brown fox jumps over the lazy dog. This is a sample paragraph with enough words and characters to pass the quality threshold for content extraction.", true},
		{"exactly_10_words_short", "one two three four five six seven eight nine ten", false}, // <100 chars
		{"10_words_long_enough", "word1_padding word2_padding word3_padding word4_padding word5_padding word6_padding word7_padding word8_padding word9_padding word10_padding", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isQualityContent(tt.content)
			if got != tt.want {
				t.Errorf("isQualityContent(%d chars, %d words) = %v, want %v",
					len(tt.content), len(strings.Fields(tt.content)), got, tt.want)
			}
		})
	}
}

// --- Mock Extractor ---

type mockExtractor struct {
	name    string
	content string
	err     error
}

func (m *mockExtractor) Name() string { return m.name }
func (m *mockExtractor) Extract(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.content, nil
}

// qualityContent returns a string that passes isQualityContent.
func qualityContent() string {
	return "This is a sufficiently long paragraph with more than ten words and well over one hundred characters to satisfy the quality content threshold check in the extractor chain."
}

// --- ExtractorChain Tests ---

func TestExtractorChain_FirstSuccess(t *testing.T) {
	chain := NewExtractorChain(
		&mockExtractor{name: "first", content: qualityContent()},
		&mockExtractor{name: "second", content: "should not reach"},
	)
	result, err := chain.Extract(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Extractor != "first" {
		t.Errorf("expected extractor 'first', got %q", result.Extractor)
	}
}

func TestExtractorChain_FirstFailsSecondSucceeds(t *testing.T) {
	chain := NewExtractorChain(
		&mockExtractor{name: "first", err: fmt.Errorf("network error")},
		&mockExtractor{name: "second", content: qualityContent()},
	)
	result, err := chain.Extract(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Extractor != "second" {
		t.Errorf("expected extractor 'second', got %q", result.Extractor)
	}
}

func TestExtractorChain_FirstLowQualitySecondSucceeds(t *testing.T) {
	chain := NewExtractorChain(
		&mockExtractor{name: "first", content: "too short"},
		&mockExtractor{name: "second", content: qualityContent()},
	)
	result, err := chain.Extract(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Extractor != "second" {
		t.Errorf("expected extractor 'second', got %q", result.Extractor)
	}
}

func TestExtractorChain_AllFail(t *testing.T) {
	chain := NewExtractorChain(
		&mockExtractor{name: "first", err: fmt.Errorf("fail1")},
		&mockExtractor{name: "second", err: fmt.Errorf("fail2")},
	)
	_, err := chain.Extract(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("expected error when all extractors fail")
	}
	if !strings.Contains(err.Error(), "all extractors failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExtractorChain_AllLowQuality(t *testing.T) {
	chain := NewExtractorChain(
		&mockExtractor{name: "first", content: "short"},
		&mockExtractor{name: "second", content: "also short"},
	)
	_, err := chain.Extract(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("expected error when all extractors return low quality")
	}
}

func TestExtractorChain_Empty(t *testing.T) {
	chain := NewExtractorChain()
	_, err := chain.Extract(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("expected error with empty chain")
	}
	if !strings.Contains(err.Error(), "no extractors configured") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractorChain_SingleSuccess(t *testing.T) {
	chain := NewExtractorChain(
		&mockExtractor{name: "only", content: qualityContent()},
	)
	result, err := chain.Extract(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Extractor != "only" {
		t.Errorf("expected 'only', got %q", result.Extractor)
	}
}

// --- DefuddleExtractor Tests ---

// newTestDefuddleExtractor creates a DefuddleExtractor pointing at a test server.
func newTestDefuddleExtractor(serverURL string) *DefuddleExtractor {
	return newDefuddleExtractor(serverURL+"/", defuddleTimeout)
}

func TestDefuddleExtractor_Success(t *testing.T) {
	expected := qualityContent()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example.com/page" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte(expected))
	}))
	defer server.Close()

	ext := newTestDefuddleExtractor(server.URL)
	result, err := ext.Extract(context.Background(), "https://example.com/page")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != expected {
		t.Errorf("content mismatch: got %d chars, want %d chars", len(result), len(expected))
	}
}

func TestDefuddleExtractor_URLConstruction(t *testing.T) {
	tests := []struct {
		input        string
		expectedPath string
	}{
		{"https://example.com/page", "/example.com/page"},
		{"http://example.com/page", "/example.com/page"},
		{"https://x.com/user/status/123", "/x.com/user/status/123"},
	}
	for _, tt := range tests {
		var gotPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.Write([]byte(qualityContent()))
		}))
		ext := newTestDefuddleExtractor(server.URL)
		_, _ = ext.Extract(context.Background(), tt.input)
		server.Close()

		if gotPath != tt.expectedPath {
			t.Errorf("input=%q: got path %q, want %q", tt.input, gotPath, tt.expectedPath)
		}
	}
}

func TestDefuddleExtractor_HTTP404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ext := newTestDefuddleExtractor(server.URL)
	_, err := ext.Extract(context.Background(), "https://example.com/missing")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefuddleExtractor_HTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ext := newTestDefuddleExtractor(server.URL)
	_, err := ext.Extract(context.Background(), "https://example.com/error")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefuddleExtractor_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ext := newTestDefuddleExtractor(server.URL)
	result, err := ext.Extract(context.Background(), "https://example.com/empty")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestDefuddleExtractorFromEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(qualityContent()))
	}))
	defer server.Close()

	ext := NewDefuddleExtractorFromEntry(ExtractorEntry{
		Name:    "defuddle",
		Enabled: true,
		Timeout: 5,
		BaseURL: server.URL + "/",
	})
	result, err := ext.Extract(context.Background(), "https://example.com/page")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != qualityContent() {
		t.Errorf("content mismatch")
	}
	// Verify timeout was applied
	if ext.client.Timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", ext.client.Timeout)
	}
}

func TestDefuddleExtractorFromEntry_Defaults(t *testing.T) {
	ext := NewDefuddleExtractorFromEntry(ExtractorEntry{Name: "defuddle", Enabled: true})
	if ext.baseURL != defuddleBaseURL {
		t.Errorf("expected default base URL %q, got %q", defuddleBaseURL, ext.baseURL)
	}
	if ext.client.Timeout != defuddleTimeout {
		t.Errorf("expected default timeout %v, got %v", defuddleTimeout, ext.client.Timeout)
	}
}

// --- InProcessExtractor Tests (delegate to fetchRawContent) ---

func TestInProcessExtractor_HTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Hello World</h1><p>This is a test paragraph with enough content to be meaningful and pass quality checks.</p></body></html>`))
	}))
	defer server.Close()

	tool := NewWebFetchTool(WebFetchConfig{})
	ext := &InProcessExtractor{tool: tool}
	result, err := ext.Extract(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Hello World") {
		t.Errorf("expected heading content, got: %s", result)
	}
	if !strings.Contains(result, "test paragraph") {
		t.Errorf("expected paragraph content, got: %s", result)
	}
}

func TestInProcessExtractor_JSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"key":"value","nested":{"a":1}}`))
	}))
	defer server.Close()

	tool := NewWebFetchTool(WebFetchConfig{})
	ext := &InProcessExtractor{tool: tool}
	result, err := ext.Extract(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"key": "value"`) {
		t.Errorf("expected pretty-printed JSON, got: %s", result)
	}
}

// --- ResolveExtractorChain Tests ---

func TestResolveExtractorChain_FromSettings(t *testing.T) {
	settings := `{"extractors":[{"name":"defuddle","enabled":true,"timeout":5,"base_url":"https://test.example.com/"},{"name":"html-to-markdown","enabled":true}]}`
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
		"web_fetch": json.RawMessage(settings),
	})
	tool := NewWebFetchTool(WebFetchConfig{})
	chain := ResolveExtractorChain(ctx, tool)
	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if len(chain.extractors) != 2 {
		t.Fatalf("expected 2 extractors, got %d", len(chain.extractors))
	}
	if chain.extractors[0].Name() != "defuddle" {
		t.Errorf("first extractor should be 'defuddle', got %q", chain.extractors[0].Name())
	}
	if chain.extractors[1].Name() != "html-to-markdown" {
		t.Errorf("second extractor should be 'html-to-markdown', got %q", chain.extractors[1].Name())
	}
}

func TestResolveExtractorChain_DisabledExtractor(t *testing.T) {
	settings := `{"extractors":[{"name":"defuddle","enabled":false},{"name":"html-to-markdown","enabled":true}]}`
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
		"web_fetch": json.RawMessage(settings),
	})
	tool := NewWebFetchTool(WebFetchConfig{})
	chain := ResolveExtractorChain(ctx, tool)
	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
	if len(chain.extractors) != 1 {
		t.Fatalf("expected 1 extractor (defuddle disabled), got %d", len(chain.extractors))
	}
	if chain.extractors[0].Name() != "html-to-markdown" {
		t.Errorf("expected 'html-to-markdown', got %q", chain.extractors[0].Name())
	}
}

func TestResolveExtractorChain_NoSettings(t *testing.T) {
	tool := NewWebFetchTool(WebFetchConfig{})
	chain := ResolveExtractorChain(context.Background(), tool)
	if chain == nil {
		t.Fatal("expected default chain")
	}
	if len(chain.extractors) != 1 {
		t.Fatalf("expected 1 default extractor, got %d", len(chain.extractors))
	}
	if chain.extractors[0].Name() != "html-to-markdown" {
		t.Errorf("expected 'html-to-markdown' default, got %q", chain.extractors[0].Name())
	}
}

func TestResolveExtractorChain_UnknownExtractor(t *testing.T) {
	settings := `{"extractors":[{"name":"unknown-thing","enabled":true},{"name":"html-to-markdown","enabled":true}]}`
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
		"web_fetch": json.RawMessage(settings),
	})
	tool := NewWebFetchTool(WebFetchConfig{})
	chain := ResolveExtractorChain(ctx, tool)
	if len(chain.extractors) != 1 {
		t.Fatalf("expected 1 extractor (unknown skipped), got %d", len(chain.extractors))
	}
}

func TestResolveExtractorChain_MalformedJSON(t *testing.T) {
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
		"web_fetch": []byte(`not valid json`),
	})
	tool := NewWebFetchTool(WebFetchConfig{})
	chain := ResolveExtractorChain(ctx, tool)
	// Malformed JSON falls through to default InProcess chain.
	if chain == nil {
		t.Fatal("expected default chain on malformed JSON")
	}
	if len(chain.extractors) != 1 || chain.extractors[0].Name() != "html-to-markdown" {
		t.Errorf("expected single html-to-markdown fallback, got %d extractors", len(chain.extractors))
	}
}

// --- formatFetchResult Tests ---

func TestFormatFetchResult_Basic(t *testing.T) {
	result := formatFetchResult("hello world", "defuddle", "https://example.com", 60000, context.Background())
	if !strings.Contains(result, "URL: https://example.com") {
		t.Error("missing URL in result")
	}
	if !strings.Contains(result, "Extractor: defuddle") {
		t.Error("missing extractor name in result")
	}
	if !strings.Contains(result, "hello world") {
		t.Error("missing content in result")
	}
}

func TestFormatFetchResult_Truncation(t *testing.T) {
	longContent := strings.Repeat("x", 200)
	result := formatFetchResult(longContent, "test", "https://example.com", 100, context.Background())
	if !strings.Contains(result, "Truncated: true") && !strings.Contains(result, "Content-Length:") {
		if !strings.Contains(result, "Content truncated") {
			t.Error("expected truncation indicator in result")
		}
	}
}
