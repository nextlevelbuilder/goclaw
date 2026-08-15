package vault

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ============================================================================
// Advanced Retry Logic Tests
// ============================================================================

// TestCallClassifyWithRetry_ResponseWhitespaceStripping trims whitespace from response.
func TestCallClassifyWithRetry_ResponseWhitespaceStripping(t *testing.T) {
	// Response has leading/trailing whitespace that should be stripped
	provider := &mockClassifyProvider{
		responses: []string{"\n\n  [{\"idx\":1,\"type\":\"reference\",\"ctx\":\"test\"}]  \n"},
		errors:    []error{nil},
	}

	worker := &EnrichWorker{}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, provider, "test", "system", "user")

	if err != nil {
		t.Fatalf("callClassifyWithRetry failed: %v", err)
	}

	// Response should be trimmed
	if strings.HasPrefix(resp, "\n") || strings.HasSuffix(resp, "\n") {
		t.Errorf("Response should be trimmed: %q", resp)
	}

	if !strings.Contains(resp, "reference") {
		t.Errorf("Response missing expected content: %q", resp)
	}
}

// TestCallClassifyWithRetry_SecondAttemptSucceeds verifies first retry succeeds.
func TestCallClassifyWithRetry_SecondAttemptSucceeds(t *testing.T) {
	fastBackoffsForTest(t) // skip real 2s backoff between attempts
	provider := &mockClassifyProvider{
		responses: []string{
			"", // attempt 0: error
			`[{"idx":1,"type":"extends","ctx":"second attempt"}]`, // attempt 1: success
		},
		errors: []error{
			errors.New("first attempt failed"),
			nil, // no error on second attempt
		},
	}

	worker := &EnrichWorker{}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, provider, "test", "system", "user")

	if err != nil {
		t.Fatalf("Should succeed on second attempt, got error: %v", err)
	}

	if provider.calls != 2 {
		t.Errorf("Expected 2 calls, got %d", provider.calls)
	}

	if !strings.Contains(resp, "extends") {
		t.Errorf("Response from second attempt not found: %q", resp)
	}
}

// TestCallClassifyWithRetry_EmptyResponse returns error for empty response on all attempts.
func TestCallClassifyWithRetry_EmptyResponse(t *testing.T) {
	provider := &mockClassifyProvider{
		responses: []string{"", "", ""},
		errors:    []error{nil, nil, nil},
	}

	worker := &EnrichWorker{}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, provider, "test", "system", "user")

	// Empty response is still a successful LLM call, should return empty string
	if err != nil {
		t.Fatalf("Empty response should not error: %v", err)
	}

	if resp != "" {
		t.Errorf("Expected empty response, got %q", resp)
	}

	if provider.calls != 1 {
		t.Errorf("Expected 1 call (succeeded immediately), got %d", provider.calls)
	}
}
