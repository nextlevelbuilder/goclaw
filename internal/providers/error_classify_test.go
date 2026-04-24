package providers

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestClassifyHTTP429RateLimit(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(errors.New("wrapped"), 429, "Rate limit exceeded")
	if result.Reason != FailoverRateLimit {
		t.Errorf("expected FailoverRateLimit, got %s", result.Reason)
	}
}

func TestClassifyHTTP402Billing(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 402, "Payment required")
	if result.Reason != FailoverBilling {
		t.Errorf("expected FailoverBilling, got %s", result.Reason)
	}
}

func TestClassifyHTTP401RevokedAuthPermanent(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 401, "API key has been revoked")
	if result.Reason != FailoverAuthPermanent {
		t.Errorf("expected FailoverAuthPermanent, got %s", result.Reason)
	}
}

func TestClassifyHTTP401WithoutRevokedAuth(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 401, "Invalid API key")
	if result.Reason != FailoverAuth {
		t.Errorf("expected FailoverAuth, got %s", result.Reason)
	}
}

func TestClassifyHTTP403DeletedAuthPermanent(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 403, "Account has been deleted")
	if result.Reason != FailoverAuthPermanent {
		t.Errorf("expected FailoverAuthPermanent, got %s", result.Reason)
	}
}

func TestClassifyHTTP403DisabledAuthPermanent(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 403, "API key is disabled")
	if result.Reason != FailoverAuthPermanent {
		t.Errorf("expected FailoverAuthPermanent, got %s", result.Reason)
	}
}

func TestClassifyHTTP404ModelNotFound(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 404, "Model not found")
	if result.Reason != FailoverModelNotFound {
		t.Errorf("expected FailoverModelNotFound, got %s", result.Reason)
	}
}

func TestClassifyHTTP404WithModelKeyword(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 404, "Requested model does not exist")
	if result.Reason != FailoverModelNotFound {
		t.Errorf("expected FailoverModelNotFound, got %s", result.Reason)
	}
}

func TestClassifyHTTP529Overloaded(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 529, "Service overloaded")
	if result.Reason != FailoverOverloaded {
		t.Errorf("expected FailoverOverloaded, got %s", result.Reason)
	}
}

func TestClassifyHTTP500WithOverload(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 500, "Server overload detected")
	if result.Reason != FailoverOverloaded {
		t.Errorf("expected FailoverOverloaded, got %s", result.Reason)
	}
}

func TestClassifyHTTP500WithCapacity(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 500, "Insufficient capacity")
	if result.Reason != FailoverOverloaded {
		t.Errorf("expected FailoverOverloaded, got %s", result.Reason)
	}
}

func TestClassifyHTTP502BadGateway(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 502, "Bad gateway")
	if result.Reason != FailoverUnknown {
		t.Errorf("expected FailoverUnknown for 502, got %s", result.Reason)
	}
}

func TestClassifyContextWindowExceeded(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "Context length exceeded")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow kind, got %s", result.Kind)
	}
}

func TestClassifyContextWindowEnglish(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "error: maximum context length reached")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s", result.Kind)
	}
}

func TestClassifyContextWindowChinese(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "错误: 超出最大长度限制")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s", result.Kind)
	}
}

func TestClassifyPromptTooLong(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "Prompt is too long")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s", result.Kind)
	}
}

func TestClassifyTooManyTokens(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "Too many tokens in request")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s", result.Kind)
	}
}

func TestClassifyNetworkTimeoutError(t *testing.T) {
	classifier := NewDefaultClassifier()
	timeoutErr := &net.DNSError{
		Err:       "timeout",
		Name:      "example.com",
		IsTimeout: true,
	}
	result := classifier.Classify(timeoutErr, 0, "")
	if result.Reason != FailoverTimeout {
		t.Errorf("expected FailoverTimeout, got %s", result.Reason)
	}
}

func TestClassifyNetworkConnectionReset(t *testing.T) {
	classifier := NewDefaultClassifier()
	err := errors.New("connection reset by peer")
	result := classifier.Classify(err, 0, "")
	if result.Reason != FailoverTimeout {
		t.Errorf("expected FailoverTimeout, got %s", result.Reason)
	}
}

func TestClassifyNetworkBrokenPipe(t *testing.T) {
	classifier := NewDefaultClassifier()
	err := errors.New("broken pipe")
	result := classifier.Classify(err, 0, "")
	if result.Reason != FailoverTimeout {
		t.Errorf("expected FailoverTimeout, got %s", result.Reason)
	}
}

func TestClassifyNetworkEOF(t *testing.T) {
	classifier := NewDefaultClassifier()
	err := errors.New("EOF")
	result := classifier.Classify(err, 0, "")
	if result.Reason != FailoverTimeout {
		t.Errorf("expected FailoverTimeout, got %s", result.Reason)
	}
}

func TestRegisterOpenAIPatterns(t *testing.T) {
	classifier := NewDefaultClassifier()
	RegisterOpenAIPatterns(classifier)

	result := classifier.Classify(nil, 0, "model_is_deactivated")
	if result.Reason != FailoverModelNotFound {
		t.Errorf("expected FailoverModelNotFound for deactivated model, got %s", result.Reason)
	}
}

func TestRegisterOpenAIPatternsModelNotFound(t *testing.T) {
	classifier := NewDefaultClassifier()
	RegisterOpenAIPatterns(classifier)

	result := classifier.Classify(nil, 0, "The model 'gpt-999' does not exist")
	if result.Reason != FailoverModelNotFound {
		t.Errorf("expected FailoverModelNotFound, got %s", result.Reason)
	}
}

func TestRegisterAnthropicPatterns(t *testing.T) {
	classifier := NewDefaultClassifier()
	RegisterAnthropicPatterns(classifier)

	result := classifier.Classify(nil, 0, "API is overloaded")
	if result.Reason != FailoverOverloaded {
		t.Errorf("expected FailoverOverloaded, got %s", result.Reason)
	}
}

func TestRegisterAnthropicPatternsBilling(t *testing.T) {
	classifier := NewDefaultClassifier()
	RegisterAnthropicPatterns(classifier)

	result := classifier.Classify(nil, 0, "Insufficient credit balance")
	if result.Reason != FailoverBilling {
		t.Errorf("expected FailoverBilling, got %s", result.Reason)
	}
}

func TestClassifyHTTPErrorConvenience(t *testing.T) {
	classifier := NewDefaultClassifier()
	httpErr := &HTTPError{
		Status:     429,
		Body:       "Rate limit exceeded",
		RetryAfter: 60 * time.Second,
	}
	result := ClassifyHTTPError(classifier, httpErr)
	if result.Reason != FailoverRateLimit {
		t.Errorf("expected FailoverRateLimit, got %s", result.Reason)
	}
}

func TestClassifyHTTPErrorNonHTTPError(t *testing.T) {
	classifier := NewDefaultClassifier()
	err := errors.New("connection reset by peer")
	result := ClassifyHTTPError(classifier, err)
	if result.Reason != FailoverTimeout {
		t.Errorf("expected FailoverTimeout, got %s", result.Reason)
	}
}

func TestClassifyHTTPErrorNil(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := ClassifyHTTPError(classifier, nil)
	if result.Reason != FailoverUnknown {
		t.Errorf("expected FailoverUnknown for nil error, got %s", result.Reason)
	}
}

func TestClassifyBillingInsufficientQuota(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "insufficient_quota")
	if result.Reason != FailoverBilling {
		t.Errorf("expected FailoverBilling, got %s", result.Reason)
	}
}

func TestClassifyFormatToolCall(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "Invalid tool_call format")
	if result.Reason != FailoverFormat {
		t.Errorf("expected FailoverFormat, got %s", result.Reason)
	}
}

func TestClassifyFormatFunctionCall(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "Invalid function_call in request")
	if result.Reason != FailoverFormat {
		t.Errorf("expected FailoverFormat, got %s", result.Reason)
	}
}

func TestClassifyFormatInvalidRequest(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "invalid_request_error")
	if result.Reason != FailoverFormat {
		t.Errorf("expected FailoverFormat, got %s", result.Reason)
	}
}

func TestClassifyHTTP401WithExpired(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 401, "Token has expired")
	if result.Reason != FailoverAuthPermanent {
		t.Errorf("expected FailoverAuthPermanent for expired token, got %s", result.Reason)
	}
}

func TestClassifyContextOverflowTokenLimit(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "token limit exceeded")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s", result.Kind)
	}
}

func TestClassifyContextOverflowChineseContext(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "请求的上下文长度超出限制")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s", result.Kind)
	}
}

func TestClassifyLowercaseInsensitive(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 401, "API KEY HAS BEEN REVOKED")
	if result.Reason != FailoverAuthPermanent {
		t.Errorf("expected FailoverAuthPermanent (case-insensitive), got %s", result.Reason)
	}
}

func TestClassifyUnknownError(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 500, "Unknown server error")
	if result.Reason != FailoverUnknown {
		t.Errorf("expected FailoverUnknown, got %s", result.Reason)
	}
}

// Issue 958: New context overflow patterns for ZAI/GLM, DashScope, generic

func TestClassifyPromptExceedsMaxLength(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, `{"error":{"code":"1261","message":"Prompt exceeds max length"}}`)
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s (reason: %s)", result.Kind, result.Reason)
	}
}

func TestClassifyInputTooLong(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "Input is too long for this model")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s (reason: %s)", result.Kind, result.Reason)
	}
}

func TestClassifyRequestTooLarge(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "request_too_large: payload exceeds limit")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s (reason: %s)", result.Kind, result.Reason)
	}
}

func TestClassifyChineseInputTooLong(t *testing.T) {
	classifier := NewDefaultClassifier()
	result := classifier.Classify(nil, 400, "请求输入过长")
	if result.Kind != "context_overflow" {
		t.Errorf("expected context_overflow, got %s (reason: %s)", result.Kind, result.Reason)
	}
}
