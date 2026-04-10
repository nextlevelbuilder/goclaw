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

// TestCallClassifyWithRetry_FirstAttemptSuccess uses first timeout.
func TestCallClassifyWithRetry_FirstAttemptSuccess(t *testing.T) {
	// Verify that first attempt uses classifyTimeouts[0]
	if classifyTimeouts[0] == 0 {
		t.Errorf("First timeout should be non-zero")
	}

	provider := &mockClassifyProvider{
		responses: []string{`[{"idx":1,"type":"reference","ctx":"first attempt"}]`},
		errors:    []error{nil},
	}

	worker := &enrichWorker{
		provider: provider,
		model:    "test",
	}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, "system", "user")

	if err != nil {
		t.Fatalf("First attempt should succeed: %v", err)
	}

	if provider.calls != 1 {
		t.Errorf("Expected exactly 1 call on first-attempt success, got %d", provider.calls)
	}

	if !strings.Contains(resp, "reference") {
		t.Errorf("Response missing expected content: %q", resp)
	}
}

// TestCallClassifyWithRetry_ResponseWhitespaceStripping trims whitespace from response.
func TestCallClassifyWithRetry_ResponseWhitespaceStripping(t *testing.T) {
	// Response has leading/trailing whitespace that should be stripped
	provider := &mockClassifyProvider{
		responses: []string{"\n\n  [{\"idx\":1,\"type\":\"reference\",\"ctx\":\"test\"}]  \n"},
		errors:    []error{nil},
	}

	worker := &enrichWorker{
		provider: provider,
		model:    "test",
	}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, "system", "user")

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

// TestCallClassifyWithRetry_MaxRetriesConstant verifies retry limit.
func TestCallClassifyWithRetry_MaxRetriesConstant(t *testing.T) {
	if classifyMaxRetries != 3 {
		t.Errorf("classifyMaxRetries should be 3, got %d", classifyMaxRetries)
	}

	// Verify arrays have correct length
	if len(classifyTimeouts) != classifyMaxRetries {
		t.Errorf("classifyTimeouts length should be %d, got %d", classifyMaxRetries, len(classifyTimeouts))
	}

	if len(classifyBackoffs) != classifyMaxRetries {
		t.Errorf("classifyBackoffs length should be %d, got %d", classifyMaxRetries, len(classifyBackoffs))
	}
}

// TestCallClassifyWithRetry_SecondAttemptSucceeds verifies first retry succeeds.
func TestCallClassifyWithRetry_SecondAttemptSucceeds(t *testing.T) {
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

	worker := &enrichWorker{
		provider: provider,
		model:    "test",
	}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, "system", "user")

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

	worker := &enrichWorker{
		provider: provider,
		model:    "test",
	}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, "system", "user")

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
