package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVertexDefaultAPIBase(t *testing.T) {
	cases := []struct {
		name, project, region, want string
	}{
		{"basic", "my-proj", "us-central1", "https://us-central1-aiplatform.googleapis.com/v1/projects/my-proj/locations/us-central1/endpoints/openapi"},
		{"asia", "acme", "asia-southeast1", "https://asia-southeast1-aiplatform.googleapis.com/v1/projects/acme/locations/asia-southeast1/endpoints/openapi"},
		{"empty_project", "", "us-central1", ""},
		{"empty_region", "my-proj", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VertexDefaultAPIBase(tc.project, tc.region); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewVertexProviderMissingFields(t *testing.T) {
	cases := []struct {
		name    string
		cfg     VertexConfig
		wantSub string
	}{
		{"no_project", VertexConfig{Region: "us-central1"}, "project_id"},
		{"no_region", VertexConfig{ProjectID: "x"}, "region"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewVertexProvider(context.Background(), tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q missing %q", err, tc.wantSub)
			}
		})
	}
}

func TestNewVertexProviderInvalidInlineJSON(t *testing.T) {
	_, err := NewVertexProvider(context.Background(), VertexConfig{
		CredentialsJSON: "not json",
		ProjectID:       "my-proj",
		Region:          "us-central1",
	})
	if err == nil {
		t.Fatal("expected error parsing bad JSON")
	}
	if !strings.Contains(err.Error(), "credentials") {
		t.Errorf("error %q does not mention credentials", err)
	}
}

func TestNewVertexProviderCredentialsFileMissing(t *testing.T) {
	_, err := NewVertexProvider(context.Background(), VertexConfig{
		CredentialsFile: filepath.Join(t.TempDir(), "does-not-exist.json"),
		ProjectID:       "my-proj",
		Region:          "us-central1",
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read credentials file") {
		t.Errorf("error %q missing expected prefix", err)
	}
}

func TestNewVertexProviderCredentialsFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewVertexProvider(context.Background(), VertexConfig{
		CredentialsFile: path,
		ProjectID:       "my-proj",
		Region:          "us-central1",
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "credentials file") {
		t.Errorf("error %q missing expected phrase", err)
	}
}

// TestOpenAIProviderWithoutAuthHeaderSkipsAuthorization verifies the skip-auth path
// added for Vertex — doRequest() must NOT set an Authorization header when skipAuthHeader is true.
// This is the sole non-trivial code change in openai.go needed for Vertex to work.
func TestOpenAIProviderWithoutAuthHeaderSkipsAuthorization(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// Minimal successful openai response
		_, _ = io.WriteString(w, `{"id":"1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	prov := NewOpenAIProvider("test", "sk-should-not-appear", server.URL, "x").
		WithoutAuthHeader()

	resp, err := prov.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content=%q, want %q", resp.Content, "ok")
	}
	if gotAuth != "" {
		t.Errorf("unexpected Authorization header %q — WithoutAuthHeader() should skip it", gotAuth)
	}
}

// TestOpenAIProviderWithHTTPClientUsesCustomClient verifies WithHTTPClient() replaces the default.
// A transport that tags outgoing requests with a sentinel header lets us confirm the custom client
// is the one used for Vertex AI (so oauth2.Transport actually runs).
func TestOpenAIProviderWithHTTPClientUsesCustomClient(t *testing.T) {
	var sawSentinel bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSentinel = r.Header.Get("X-Test-Transport") == "custom"
		_, _ = io.WriteString(w, `{"id":"1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	customClient := &http.Client{Transport: &taggingTransport{Base: http.DefaultTransport, Header: "X-Test-Transport", Value: "custom"}}
	prov := NewOpenAIProvider("test", "ignored", server.URL, "x").
		WithHTTPClient(customClient).
		WithoutAuthHeader()

	if _, err := prov.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if !sawSentinel {
		t.Error("custom transport did not run — WithHTTPClient() may not have replaced the client")
	}
}

// taggingTransport is a test-only RoundTripper that sets a fixed header on every outbound request.
type taggingTransport struct {
	Base   http.RoundTripper
	Header string
	Value  string
}

func (t *taggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(t.Header, t.Value)
	return t.Base.RoundTrip(req)
}

// Sanity check: ensure Vertex provider wires default model and endpoint correctly.
// We cannot exercise real token refresh without a real SA — skipAuthHeader + endpoint
// assertions cover the provider-specific wiring.
func TestNewVertexProviderWiresEndpointAndModel(t *testing.T) {
	// Valid (but fake) SA JSON — CredentialsFromJSON parses structure without fetching tokens.
	fakeSA := map[string]any{
		"type":         "service_account",
		"project_id":   "my-proj",
		"private_key":  fakePEM,
		"client_email": "test@my-proj.iam.gserviceaccount.com",
		"token_uri":    "https://oauth2.googleapis.com/token",
	}
	data, _ := json.Marshal(fakeSA)

	prov, err := NewVertexProvider(context.Background(), VertexConfig{
		CredentialsJSON: string(data),
		ProjectID:       "my-proj",
		Region:          "us-central1",
	})
	if err != nil {
		t.Fatalf("NewVertexProvider: %v", err)
	}
	wantBase := "https://us-central1-aiplatform.googleapis.com/v1/projects/my-proj/locations/us-central1/endpoints/openapi"
	if prov.APIBase() != wantBase {
		t.Errorf("APIBase=%q, want %q", prov.APIBase(), wantBase)
	}
	if prov.DefaultModel() != VertexDefaultModel {
		t.Errorf("DefaultModel=%q, want %q", prov.DefaultModel(), VertexDefaultModel)
	}
	if prov.Name() != "vertex" {
		t.Errorf("Name=%q, want %q", prov.Name(), "vertex")
	}
	if prov.ProviderType() != ProviderTypeVertex {
		t.Errorf("ProviderType=%q, want %q", prov.ProviderType(), ProviderTypeVertex)
	}
}

// Minimal valid-looking PKCS#8 PEM body — google.CredentialsFromJSON parses lazily
// so it does NOT attempt real key validation; test just needs structurally-valid JSON.
// The private_key field can be any non-empty string.
const fakePEM = "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"

// Regression test for H1 from code review: thought_signature detection must recognize
// providers whose providerType is "vertex" (or apiBase contains "aiplatform"),
// even when the model string does NOT contain "gemini". Without this fix, tool-call
// rounds against a fine-tuned Vertex endpoint ID would drop the signature on passback
// and trigger HTTP 400 from the Vertex API.
func TestVertexProviderForwardsThoughtSignatureOnToolCalls(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		// Return a tool call with a thought_signature so the next round would echo it.
		_, _ = io.WriteString(w, `{"id":"1","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"t1","type":"function","function":{"name":"noop","arguments":"{}","thought_signature":"sig-xyz"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()

	// Build a Vertex-style OpenAIProvider manually (avoids oauth2 in tests).
	prov := NewOpenAIProvider("vertex", "", server.URL, "some-tuned-endpoint-id").
		WithProviderType(ProviderTypeVertex).
		WithoutAuthHeader()

	// Round 1: assistant responds with tool_calls carrying thought_signature.
	r1, err := prov.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "go"}},
		Tools:    []ToolDefinition{{Type: "function", Function: ToolFunctionSchema{Name: "noop", Parameters: map[string]any{"type": "object"}}}},
	})
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if len(r1.ToolCalls) != 1 {
		t.Fatalf("round 1 tool_calls = %d, want 1", len(r1.ToolCalls))
	}
	if r1.ToolCalls[0].Metadata["thought_signature"] != "sig-xyz" {
		t.Fatalf("thought_signature metadata missing on round 1 tool call")
	}

	// Round 2: pass the assistant's tool call + a tool-result message. Expect the
	// outbound request to INCLUDE thought_signature on the tool_calls entry.
	toolCall := r1.ToolCalls[0]
	toolCall.Arguments = map[string]any{}
	_, err = prov.Chat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "go"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{toolCall}},
			{Role: "tool", Content: "ok", ToolCallID: "t1"},
			{Role: "user", Content: "next"},
		},
		Tools: []ToolDefinition{{Type: "function", Function: ToolFunctionSchema{Name: "noop", Parameters: map[string]any{"type": "object"}}}},
	})
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if len(bodies) < 2 {
		t.Fatalf("expected 2 round-trips, got %d", len(bodies))
	}
	if !strings.Contains(bodies[1], `"thought_signature":"sig-xyz"`) {
		t.Errorf("round 2 body missing thought_signature (H1 regression): %s", bodies[1])
	}
}

// Sanity check the validation helpers surface clear errors on bad input (M1 / M2).
func TestVertexValidationRejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name, project, region, apiBase, wantSub string
	}{
		{"region_host_escape", "my-proj", "evil.com/a?", "", "invalid region"},
		{"region_with_slash", "my-proj", "us/central1", "", "invalid region"},
		{"project_uppercase", "MY-PROJ", "us-central1", "", "invalid project_id"},
		{"project_starts_with_digit", "1badproj", "us-central1", "", "invalid project_id"},
		{"project_too_short", "abc", "us-central1", "", "invalid project_id"},
		{"override_http", "my-proj", "us-central1", "http://evil.com", "https scheme"},
		{"override_non_google", "my-proj", "us-central1", "https://evil.com/vertex", "googleapis.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewVertexProvider(context.Background(), VertexConfig{
				ProjectID:       tc.project,
				Region:          tc.region,
				APIBaseOverride: tc.apiBase,
			})
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q missing %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// Confirm well-formed projects+regions plus a valid override URL still work.
func TestVertexValidationAcceptsWellFormedInput(t *testing.T) {
	fakeSA := map[string]any{
		"type":         "service_account",
		"project_id":   "my-proj",
		"private_key":  fakePEM,
		"client_email": "test@my-proj.iam.gserviceaccount.com",
		"token_uri":    "https://oauth2.googleapis.com/token",
	}
	data, _ := json.Marshal(fakeSA)

	_, err := NewVertexProvider(context.Background(), VertexConfig{
		CredentialsJSON: string(data),
		ProjectID:       "my-proj",
		Region:          "asia-southeast1",
		APIBaseOverride: "https://asia-southeast1-aiplatform.googleapis.com/v1/projects/my-proj/locations/asia-southeast1/endpoints/openapi",
	})
	if err != nil {
		t.Fatalf("well-formed input rejected: %v", err)
	}
}
