// Package reliability provides a unified reliability layer for the agent
// runtime: a canonical error taxonomy, provider/model circuit breakers,
// health registry with runtime reliability scoring, a shared rate-limit
// coordinator (single-flight cooldown), and OTel-style reliability metrics.
//
// The package is intentionally self-contained: it does not import the
// provider implementations, so it can be layered on top of (or next to) the
// existing provider retry/failover machinery without creating import cycles.
package reliability

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// ErrorCode is a stable, machine-readable error classification.
// The format is "<layer>.<class>" so consumers can switch on the layer
// (provider / model / runtime / tool) and the class.
type ErrorCode string

const (
	// ---- Provider errors ----
	ErrProviderRateLimited     ErrorCode = "provider.rate_limited"
	ErrProviderOverloaded      ErrorCode = "provider.overloaded"
	ErrProviderServerError     ErrorCode = "provider.server_error"
	ErrProviderTimeout         ErrorCode = "provider.timeout"
	ErrProviderConnection      ErrorCode = "provider.connection"
	ErrProviderAuth            ErrorCode = "provider.auth"
	ErrProviderAuthPermanent   ErrorCode = "provider.auth_permanent"
	ErrProviderBadRequest      ErrorCode = "provider.bad_request"
	ErrProviderBilling         ErrorCode = "provider.billing"
	ErrProviderModelNotFound   ErrorCode = "provider.model_not_found"
	ErrProviderContentPolicy   ErrorCode = "provider.content_policy"
	ErrProviderInvalidResponse ErrorCode = "provider.invalid_response"
	ErrProviderContextOverflow ErrorCode = "provider.context_overflow"

	// ---- Model errors ----
	ErrModelEmptyOutput          ErrorCode = "model.empty_output"
	ErrModelMalformedToolCall    ErrorCode = "model.malformed_tool_call"
	ErrModelInvalidJSON          ErrorCode = "model.invalid_json"
	ErrModelUnsupportedToolCall  ErrorCode = "model.unsupported_tool_call"
	ErrModelRepeatedToolCall     ErrorCode = "model.repeated_tool_call"
	ErrModelPrematureCompletion  ErrorCode = "model.premature_completion"
	ErrModelLooping              ErrorCode = "model.looping"
	ErrModelLowSignal            ErrorCode = "model.low_signal"

	// ---- Runtime errors ----
	ErrRunCancelled       ErrorCode = "runtime.run_cancelled"
	ErrRunStalled         ErrorCode = "runtime.run_stalled"
	ErrRunDeadline        ErrorCode = "runtime.run_deadline"
	ErrRunRecoveryFailed  ErrorCode = "runtime.run_recovery_failed"

	// ---- Tool errors ----
	ErrToolTimeout         ErrorCode = "tool.timeout"
	ErrToolUnavailable     ErrorCode = "tool.unavailable"
	ErrToolInvalidArgs     ErrorCode = "tool.invalid_args"
	ErrToolPermissionDenied ErrorCode = "tool.permission_denied"
	ErrToolTransient       ErrorCode = "tool.transient"
	ErrToolPermanent       ErrorCode = "tool.permanent"
)

// Severity ranks how an error should surface to a user or operator.
type Severity int

const (
	SeverityDebug Severity = iota
	SeverityInfo
	SeverityWarning
	SeverityError
	SeverityFatal
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	case SeverityFatal:
		return "fatal"
	default:
		return "debug"
	}
}

// ReliabilityError is the canonical runtime error. It carries everything a
// consumer needs: a stable code, retryability, severity, optional Retry-After
// hint, and run context (runID/stage/attempt) populated as it travels up.
type ReliabilityError struct {
	Code       ErrorCode
	Message    string
	Retryable  bool
	Severity   Severity
	Cause      error
	RetryAfter time.Duration // >0 when Code is a rate_limit and the provider sent Retry-After

	RunID   string
	Stage   string
	Attempt int
}

func (e *ReliabilityError) Error() string {
	s := string(e.Code)
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.Cause != nil {
		if e.Message != "" {
			s += " (cause: " + e.Cause.Error() + ")"
		} else {
			s += ": " + e.Cause.Error()
		}
	}
	return s
}

// Unwrap exposes the underlying cause for errors.Is / errors.As chains.
func (e *ReliabilityError) Unwrap() error { return e.Cause }

// IsRetryable reports whether the error can be retried safely.
func (e *ReliabilityError) IsRetryable() bool { return e.Retryable }

// WithRunContext attaches run identity to the error and returns it for chaining.
func (e *ReliabilityError) WithRunContext(runID, stage string, attempt int) *ReliabilityError {
	e.RunID = runID
	e.Stage = stage
	e.Attempt = attempt
	return e
}

// errorClass holds the static classification of one ErrorCode.
type errorClass struct {
	retryable bool
	severity  Severity
}

// classes defines the canonical behavior of every code in the taxonomy.
// When uncertain, prefer NOT retryable so callers fail safe rather than
// amplifying a permanent failure.
var classes = map[ErrorCode]errorClass{
	// Provider
	ErrProviderRateLimited:     {retryable: true, severity: SeverityWarning},
	ErrProviderOverloaded:      {retryable: true, severity: SeverityWarning},
	ErrProviderServerError:     {retryable: true, severity: SeverityError},
	ErrProviderTimeout:         {retryable: true, severity: SeverityError},
	ErrProviderConnection:      {retryable: true, severity: SeverityWarning},
	ErrProviderAuth:            {retryable: false, severity: SeverityError},
	ErrProviderAuthPermanent:   {retryable: false, severity: SeverityError},
	ErrProviderBadRequest:      {retryable: false, severity: SeverityError},
	ErrProviderBilling:         {retryable: false, severity: SeverityError},
	ErrProviderModelNotFound:   {retryable: false, severity: SeverityError},
	ErrProviderContentPolicy:   {retryable: false, severity: SeverityWarning},
	ErrProviderInvalidResponse: {retryable: true, severity: SeverityError},
	ErrProviderContextOverflow: {retryable: true, severity: SeverityWarning},

	// Model
	ErrModelEmptyOutput:         {retryable: true, severity: SeverityWarning},
	ErrModelMalformedToolCall:   {retryable: true, severity: SeverityWarning},
	ErrModelInvalidJSON:         {retryable: true, severity: SeverityWarning},
	ErrModelUnsupportedToolCall: {retryable: true, severity: SeverityWarning},
	ErrModelRepeatedToolCall:    {retryable: true, severity: SeverityWarning},
	ErrModelPrematureCompletion: {retryable: true, severity: SeverityWarning},
	ErrModelLooping:             {retryable: false, severity: SeverityError},
	ErrModelLowSignal:           {retryable: true, severity: SeverityWarning},

	// Runtime
	ErrRunCancelled:      {retryable: false, severity: SeverityInfo},
	ErrRunStalled:        {retryable: true, severity: SeverityWarning},
	ErrRunDeadline:       {retryable: false, severity: SeverityWarning},
	ErrRunRecoveryFailed: {retryable: false, severity: SeverityError},

	// Tool
	ErrToolTimeout:         {retryable: true, severity: SeverityWarning},
	ErrToolUnavailable:     {retryable: true, severity: SeverityWarning},
	ErrToolInvalidArgs:     {retryable: false, severity: SeverityWarning},
	ErrToolPermissionDenied: {retryable: false, severity: SeverityError},
	ErrToolTransient:       {retryable: true, severity: SeverityWarning},
	ErrToolPermanent:       {retryable: false, severity: SeverityError},
}

// New builds a ReliabilityError from a code and message, applying the
// canonical retryability and severity for that code.
func New(code ErrorCode, message string) *ReliabilityError {
	c := classes[code]
	return &ReliabilityError{
		Code:      code,
		Message:   message,
		Retryable: c.retryable,
		Severity:  c.severity,
	}
}

// Wrap builds a ReliabilityError that wraps a cause, keeping retryability and
// severity from the code table.
func Wrap(code ErrorCode, cause error) *ReliabilityError {
	c := classes[code]
	return &ReliabilityError{
		Code:      code,
		Message:   codeMessage(code, cause),
		Cause:     cause,
		Retryable: c.retryable,
		Severity:  c.severity,
	}
}

// WithRetryAfter marks a rate-limit error with the provider's Retry-After hint.
func (e *ReliabilityError) WithRetryAfter(d time.Duration) *ReliabilityError {
	e.RetryAfter = d
	return e
}

func codeMessage(code ErrorCode, cause error) string {
	if cause != nil {
		return cause.Error()
	}
	return "reliability error " + string(code)
}

// ---------------------------------------------------------------------------
// Classification
// ---------------------------------------------------------------------------

// Classer converts a transported HTTP status + body into a canonical code.
// It mirrors the existing providers.DefaultClassifier semantics while adding
// the reliability taxonomy.
type Classer struct{}

// ClassifyHTTP maps an HTTP status and response body to an ErrorCode.
// statusCode 0 means "not an HTTP error" and falls through to body checks.
func (Classer) ClassifyHTTP(statusCode int, body string) ErrorCode {
	lower := strings.ToLower(body)
	switch {
	case isContextOverflowStr(lower):
		return ErrProviderContextOverflow
	case statusCode == 429:
		return ErrProviderRateLimited
	case statusCode == 402:
		return ErrProviderBilling
	case statusCode == 401 || statusCode == 403:
		if containsAnyStr(lower, "revoked", "deleted", "deactivated", "disabled", "expired") {
			return ErrProviderAuthPermanent
		}
		return ErrProviderAuth
	case statusCode == 404:
		if containsAnyStr(lower, "model", "not found", "does not exist") {
			return ErrProviderModelNotFound
		}
		return ErrProviderBadRequest
	case statusCode == 529:
		return ErrProviderOverloaded
	case statusCode == 408:
		return ErrProviderTimeout
	case statusCode >= 500:
		if containsAnyStr(lower, "overload", "capacity", "too many") {
			return ErrProviderOverloaded
		}
		return ErrProviderServerError
	}

	// Body-only hints (status 0 or non-matching status)
	if containsAnyStr(lower, "credit balance", "insufficient_quota", "billing") {
		return ErrProviderBilling
	}
	if isContentPolicyStr(lower, statusCode) {
		return ErrProviderContentPolicy
	}
	return ErrProviderInvalidResponse
}

// ClassifyError inspects an arbitrary error and returns the best-matching
// canonical code and the error itself wrapped. It recognizes common sentinel
// patterns (network, timeout, cancellation, context deadline).
func ClassifyError(err error) (*ReliabilityError, error) {
	if err == nil {
		return nil, nil
	}

	// Already a reliability error → pass through.
	var re *ReliabilityError
	if errors.As(err, &re) {
		return re, err
	}

	var code ErrorCode
	var retryAfter time.Duration
	var cause error = err

	// isNetTimeout reports whether err (or anything in its chain) reports a
	// transport timeout, independent of whether it also satisfies
	// context.Canceled.
	var netErr net.Error
	switch {
	case errors.As(err, &netErr) && netErr.Timeout():
		// Check the network-timeout case BEFORE context.Canceled: many HTTP
		// clients wrap transport timeouts in an error that both satisfies
		// net.Error (Timeout()==true) and wraps context.Canceled (e.g.
		// *url.Error). If we matched context.Canceled first, such a transient
		// timeout would be classified as a non-retryable run cancellation and
		// the reliability layer would give up on a retryable condition.
		code = ErrProviderTimeout
	case errors.Is(err, context.Canceled):
		code = ErrRunCancelled
	case errors.Is(err, context.DeadlineExceeded):
		code = ErrProviderTimeout
	case errors.As(err, &netErr):
		code = ErrProviderConnection
	default:
		s := strings.ToLower(err.Error())
		switch {
		case isContextOverflowStr(s):
			code = ErrProviderContextOverflow
		case containsAnyStr(s, "connection reset", "broken pipe", "eof", "connection refused"):
			code = ErrProviderConnection
		case containsAnyStr(s, "timeout", "deadline exceeded"):
			code = ErrProviderTimeout
		case containsAnyStr(s, "rate limit", "too many requests", "429"):
			code = ErrProviderRateLimited
		default:
			code = ErrProviderServerError
		}
	}

	re = Wrap(code, cause)
	if retryAfter > 0 {
		re.WithRetryAfter(retryAfter)
	}
	return re, re
}

// IsRetryable reports whether err (possibly wrapped) is retryable per the
// taxonomy. It returns false for nil errors.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var re *ReliabilityError
	if errors.As(err, &re) {
		return re.IsRetryable()
	}
	// Fall back to a conservative check: network errors are retryable.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	s := strings.ToLower(err.Error())
	return containsAnyStr(s, "connection reset", "broken pipe", "eof", "timeout", "429", "rate limit")
}

// ---------------------------------------------------------------------------
// Small helpers (kept local to avoid importing provider internals)
// ---------------------------------------------------------------------------

func containsAnyStr(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func isContextOverflowStr(lower string) bool {
	return containsAnyStr(lower,
		"context length exceeded",
		"context window",
		"maximum context length",
		"token limit",
		"too many tokens",
		"prompt is too long",
		"超出最大长度限制",
		"上下文长度",
		"prompt exceeds max length",
		"request_too_large",
		"input is too long",
		"exceed_context_size",
		"请求输入过长",
	)
}

func isContentPolicyStr(lower string, statusCode int) bool {
	if statusCode != 0 && containsAnyStr(lower, "data_inspection_failed", "inappropriate content", "content_policy_violation") {
		return true
	}
	if containsAnyStr(lower, "limited access to this content for safety reasons", "content for safety reasons") {
		return true
	}
	return strings.Contains(lower, "invalid prompt") && strings.Contains(lower, "safety")
}