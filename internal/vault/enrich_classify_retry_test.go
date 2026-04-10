package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ============================================================================
// Mock Provider for Testing
// ============================================================================

// mockClassifyProvider implements providers.Provider for testing retry logic.
type mockClassifyProvider struct {
	responses []string
	errors    []error
	calls     int
}

func (m *mockClassifyProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	resp := ""
	if idx < len(m.responses) {
		resp = m.responses[idx]
	}
	return &providers.ChatResponse{Content: resp}, nil
}

func (m *mockClassifyProvider) ChatStream(ctx context.Context, req providers.ChatRequest, onChunk func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return m.Chat(ctx, req)
}

func (m *mockClassifyProvider) DefaultModel() string { return "test" }

func (m *mockClassifyProvider) Name() string { return "mock" }

// ============================================================================
// Retry Logic Tests
// ============================================================================

// TestCallClassifyWithRetry_Success succeeds on first attempt.
func TestCallClassifyWithRetry_Success(t *testing.T) {
	provider := &mockClassifyProvider{
		responses: []string{`[{"idx":1,"type":"reference","ctx":"test"}]`},
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

	if !strings.Contains(resp, "reference") {
		t.Errorf("Response doesn't contain expected content: %q", resp)
	}

	if provider.calls != 1 {
		t.Errorf("Expected 1 call, got %d", provider.calls)
	}
}

// TestCallClassifyWithRetry_RetryThenSuccess fails twice, succeeds on third attempt.
func TestCallClassifyWithRetry_RetryThenSuccess(t *testing.T) {
	provider := &mockClassifyProvider{
		responses: []string{
			"", // attempt 0: error
			"", // attempt 1: error
			`[{"idx":1,"type":"reference","ctx":"success"}]`, // attempt 2: success
		},
		errors: []error{
			fmt.Errorf("network error"),
			fmt.Errorf("timeout"),
			nil, // no error on third attempt
		},
	}

	worker := &enrichWorker{
		provider: provider,
		model:    "test",
	}

	ctx := context.Background()
	resp, err := worker.callClassifyWithRetry(ctx, "system", "user")

	if err != nil {
		t.Fatalf("callClassifyWithRetry should succeed after retries, got error: %v", err)
	}

	if !strings.Contains(resp, "success") {
		t.Errorf("Response doesn't contain expected content: %q", resp)
	}

	if provider.calls != 3 {
		t.Errorf("Expected 3 calls, got %d", provider.calls)
	}
}

// TestCallClassifyWithRetry_AllFail exhausts retries and returns error.
func TestCallClassifyWithRetry_AllFail(t *testing.T) {
	provider := &mockClassifyProvider{
		responses: []string{"", "", ""},
		errors: []error{
			fmt.Errorf("error1"),
			fmt.Errorf("error2"),
			fmt.Errorf("error3"),
		},
	}

	worker := &enrichWorker{
		provider: provider,
		model:    "test",
	}

	ctx := context.Background()
	_, err := worker.callClassifyWithRetry(ctx, "system", "user")

	if err == nil {
		t.Fatalf("callClassifyWithRetry should return error after exhausting retries")
	}

	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("Error should mention exhausted retries: %v", err)
	}

	if provider.calls != enrichMaxRetries {
		t.Errorf("Expected %d calls, got %d", enrichMaxRetries, provider.calls)
	}
}

// TestCallClassifyWithRetry_ContextCancellation respects context cancellation.
func TestCallClassifyWithRetry_ContextCancellation(t *testing.T) {
	provider := &mockClassifyProvider{
		responses: []string{"", "", ""},
		errors: []error{
			fmt.Errorf("error1"),
			fmt.Errorf("error2"),
			fmt.Errorf("error3"),
		},
	}

	worker := &enrichWorker{
		provider: provider,
		model:    "test",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := worker.callClassifyWithRetry(ctx, "system", "user")

	if err == nil {
		t.Fatalf("callClassifyWithRetry should return error for cancelled context")
	}

	if !strings.Contains(err.Error(), "context") {
		t.Errorf("Error should mention context: %v", err)
	}
}

// TestCallClassifyWithRetry_RetriesAndBackoffs verifies retry timeouts and backoffs escalate.
func TestCallClassifyWithRetry_RetriesAndBackoffs(t *testing.T) {
	// Verify first backoff is 0 (no backoff on first attempt)
	if enrichRetryBackoffs[0] != 0 {
		t.Errorf("First backoff should be 0, got %v", enrichRetryBackoffs[0])
	}

	// Verify backoffs escalate for retry attempts
	if enrichRetryBackoffs[1] == 0 || enrichRetryBackoffs[2] == 0 {
		t.Errorf("Backoffs should escalate: %v", enrichRetryBackoffs)
	}

	if enrichRetryBackoffs[1] >= enrichRetryBackoffs[2] {
		t.Errorf("Backoffs should increase: %v", enrichRetryBackoffs)
	}

	// Verify timeouts escalate
	if enrichRetryTimeouts[0] >= enrichRetryTimeouts[1] || enrichRetryTimeouts[1] >= enrichRetryTimeouts[2] {
		t.Errorf("Timeouts should escalate: %v", enrichRetryTimeouts)
	}
}

