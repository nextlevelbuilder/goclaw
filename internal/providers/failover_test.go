package providers

import (
	"context"
	"errors"
	"testing"
)

func TestRunWithFailoverFirstCandidateSucceeds(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
		},
		Classifier: NewDefaultClassifier(),
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		return "success", nil
	}

	result, attempts, err := RunWithFailover(ctx, cfg, runFn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
	if len(attempts) != 0 {
		t.Errorf("expected 0 attempts recorded, got %d", len(attempts))
	}
}

func TestRunWithFailoverRateLimitRotatesProfile(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
		},
		Classifier: NewDefaultClassifier(),
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		if callCount == 1 {
			return "", &HTTPError{Status: 429, Body: "Rate limit exceeded"}
		}
		return "success", nil
	}

	result, attempts, err := RunWithFailover(ctx, cfg, runFn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if len(attempts) != 1 {
		t.Errorf("expected 1 attempt recorded, got %d", len(attempts))
	}
	if attempts[0].Classification.Reason != FailoverRateLimit {
		t.Errorf("expected FailoverRateLimit, got %s", attempts[0].Classification.Reason)
	}
}

func TestRunWithFailoverAuthPermanentSkipsModel(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key1"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key2"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key3"},
		},
		Classifier: NewDefaultClassifier(),
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		// First candidate fails with auth_permanent, should skip all claude models
		if candidate.Model == "claude-opus-4-6" {
			return "", &HTTPError{Status: 401, Body: "API key has been revoked"}
		}
		return "success", nil
	}

	result, attempts, err := RunWithFailover(ctx, cfg, runFn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
	// Should only try: first claude (fail), then skip to gpt (succeed)
	// Total: 2 calls
	if callCount != 2 {
		t.Errorf("expected 2 calls (first claude fails, skip rest, try gpt), got %d", callCount)
	}
	if len(attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Classification.Reason != FailoverAuthPermanent {
		t.Errorf("expected FailoverAuthPermanent, got %s", attempts[0].Classification.Reason)
	}
}

func TestRunWithFailoverOverloadCapReached(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key3"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key4"},
		},
		Classifier:            NewDefaultClassifier(),
		OverloadRotationLimit: 2, // Only allow 2 overload rotations before model fallback
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		if candidate.Model == "gpt-4o" {
			return "", &HTTPError{Status: 529, Body: "Service overloaded"}
		}
		return "success", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
	// Should try: key1 (fail), key2 (fail), then skip to next model, try key4 (succeed)
	// Total: 3 calls
	if callCount != 3 {
		t.Errorf("expected 3 calls (2 overload rotations then model fallback), got %d", callCount)
	}
}

func TestRunWithFailoverAllExhausted(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key2"},
		},
		Classifier: NewDefaultClassifier(),
	}

	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		return "", &HTTPError{Status: 500, Body: "Internal server error"}
	}

	result, attempts, err := RunWithFailover(ctx, cfg, runFn)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != "" {
		t.Errorf("expected empty result, got %s", result)
	}

	var summaryErr *FailoverSummaryError
	if !errors.As(err, &summaryErr) {
		t.Fatalf("expected FailoverSummaryError, got %T", err)
	}

	if len(attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", len(attempts))
	}
}

func TestRunWithFailoverContextOverflowReturnsImmediately(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
		},
		Classifier: NewDefaultClassifier(),
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		return "", &HTTPError{Status: 400, Body: "Context length exceeded"}
	}

	result, attempts, err := RunWithFailover(ctx, cfg, runFn)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != "" {
		t.Errorf("expected empty result, got %s", result)
	}
	// Should stop after first context overflow, not try profile rotation
	if callCount != 1 {
		t.Errorf("expected 1 call (context overflow stops immediately), got %d", callCount)
	}
	if len(attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Classification.Kind != "context_overflow" {
		t.Errorf("expected context_overflow kind, got %s", attempts[0].Classification.Kind)
	}
}

func TestRunWithFailoverContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
		},
		Classifier: NewDefaultClassifier(),
	}

	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		t.Error("runFn should not be called when context is cancelled")
		return "", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %s", result)
	}
}

func TestRunWithFailoverNoCandidates(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{},
		Classifier: NewDefaultClassifier(),
	}

	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		t.Error("runFn should not be called with no candidates")
		return "", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != "" {
		t.Errorf("expected empty result, got %s", result)
	}
}

func TestRunWithFailoverDefaultClassifier(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
		},
		Classifier: nil, // Should use default
	}

	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		return "success", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
}

func TestRunWithFailoverDefaultConfigDefaults(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key3"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key4"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key5"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key6"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key7"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key8"},
		},
		Classifier:            NewDefaultClassifier(),
		OverloadRotationLimit: 0, // Should be set to default 3
		MaxProfileRotations:   0, // Should be set to default 5
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		// Fail with overload for first 3 gpt models, then succeed
		if callCount <= 3 && candidate.Model == "gpt-4o" {
			return "", &HTTPError{Status: 529, Body: "Overloaded"}
		}
		// If more calls than expected (would happen if defaults weren't applied), fail obviously
		if callCount > 100 {
			t.Error("too many calls - defaults not applied properly")
		}
		return "success", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
	// With default OverloadRotationLimit=3, after 3 overload rotations should escalate to next model
	// callCount should be: 3 failed (gpt models) + 1 success (anthropic) = 4
	if callCount != 4 {
		t.Errorf("expected 4 calls (3 overload rotations + escalate), got %d", callCount)
	}
}

func TestIsProfileRotatable(t *testing.T) {
	tests := []struct {
		reason   FailoverReason
		expected bool
	}{
		{FailoverRateLimit, true},
		{FailoverOverloaded, true},
		{FailoverTimeout, true},
		{FailoverAuth, true},
		{FailoverAuthPermanent, false},
		{FailoverBilling, false},
		{FailoverFormat, false},
		{FailoverModelNotFound, false},
		{FailoverUnknown, false},
	}

	for _, test := range tests {
		result := isProfileRotatable(test.reason)
		if result != test.expected {
			t.Errorf("isProfileRotatable(%s): expected %v, got %v", test.reason, test.expected, result)
		}
	}
}

func TestIsModelFallbackRequired(t *testing.T) {
	tests := []struct {
		reason   FailoverReason
		expected bool
	}{
		{FailoverAuthPermanent, true},
		{FailoverBilling, true},
		{FailoverFormat, true},
		{FailoverModelNotFound, true},
		{FailoverRateLimit, false},
		{FailoverOverloaded, false},
		{FailoverTimeout, false},
		{FailoverAuth, false},
		{FailoverUnknown, false},
	}

	for _, test := range tests {
		result := isModelFallbackRequired(test.reason)
		if result != test.expected {
			t.Errorf("isModelFallbackRequired(%s): expected %v, got %v", test.reason, test.expected, result)
		}
	}
}

func TestRunWithFailoverMaxProfileRotationsLimit(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key3"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key4"},
		},
		Classifier:            NewDefaultClassifier(),
		OverloadRotationLimit: 10, // High limit so only max profile rotations matters
		MaxProfileRotations:   2,
	}

	callCount := 0
	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		if candidate.Model == "gpt-4o" {
			return "", &HTTPError{Status: 429, Body: "Rate limit"}
		}
		return "success", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
	// Should try: key1 (fail), key2 (fail), then skip to next model, try key4 (succeed)
	// Total: 3 calls
	if callCount != 3 {
		t.Errorf("expected 3 calls (2 rotations then model fallback), got %d", callCount)
	}
}

func TestRunWithFailoverFailoverSummaryErrorFormat(t *testing.T) {
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key2"},
		},
	}

	summaryErr := &FailoverSummaryError{
		Attempts: []FailoverAttempt{
			{
				Candidate:      cfg.Candidates[0],
				Classification: FailoverClassification{Kind: "reason", Reason: FailoverRateLimit},
				Err:            errors.New("rate limit"),
			},
			{
				Candidate:      cfg.Candidates[1],
				Classification: FailoverClassification{Kind: "reason", Reason: FailoverBilling},
				Err:            errors.New("billing error"),
			},
		},
	}

	errMsg := summaryErr.Error()
	if errMsg == "" {
		t.Fatal("expected non-empty error message")
	}
	// Should contain info about both attempts
	if !contains(errMsg, "openai") || !contains(errMsg, "anthropic") {
		t.Errorf("error message should contain provider names: %s", errMsg)
	}
}

func TestRunWithFailoverMultipleModelsMultipleProfiles(t *testing.T) {
	ctx := context.Background()
	cfg := FailoverConfig{
		Candidates: []ModelCandidate{
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
			{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key3"},
			{Provider: "anthropic", Model: "claude-opus-4-6", ProfileID: "key4"},
		},
		Classifier: NewDefaultClassifier(),
	}

	callCount := 0
	attemptedProfiles := []string{}

	runFn := func(ctx context.Context, candidate ModelCandidate) (string, error) {
		callCount++
		attemptedProfiles = append(attemptedProfiles, candidate.ProfileID)
		if callCount < 4 {
			return "", &HTTPError{Status: 500, Body: "error"}
		}
		return "success", nil
	}

	result, _, err := RunWithFailover(ctx, cfg, runFn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected success, got %s", result)
	}
	if callCount != 4 {
		t.Errorf("expected 4 calls, got %d", callCount)
	}
	if len(attemptedProfiles) != 4 {
		t.Errorf("expected 4 profile attempts, got %d", len(attemptedProfiles))
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
