package gc

import "errors"

// Failure taxonomy sentinels (plan §101). These are the canonical error codes
// surfaced through the /gc: command execution paths. Callers test with
// errors.Is, never ==.
var (
	ErrProviderRateLimit    = errors.New("provider rate limit")
	ErrProviderUnavailable  = errors.New("provider unavailable")
	ErrProviderTimeout      = errors.New("provider timeout")
	ErrToolTimeout          = errors.New("tool timeout")
	ErrToolPermission       = errors.New("tool permission denied")
	ErrContextOverflow      = errors.New("context overflow")
	ErrResourceLimit        = errors.New("resource limit")
	ErrPolicyDenied         = errors.New("policy denied")
	ErrVerificationFailed   = errors.New("verification failed")
	ErrUserCancelled        = errors.New("user cancelled")
)

// UserError is the user-facing error contract (plan §102). It maps a failure
// taxonomy code to a human message and a retryability flag so the UI/agent can
// decide whether to retry, fall back, or stop.
type UserError struct {
	Status     string // e.g. "recovering", "failed"
	Code       string // taxonomy code, e.g. "PROVIDER_RATE_LIMIT"
	Retryable  bool
	Message    string // human-readable message
	NextAction string // suggested recovery step, e.g. "fallback"
}

// BuildUserError maps a known sentinel to a UserError. Unknown errors are
// mapped to a generic non-retryable failure. The sentinel's taxonomy code is
// derived from the sentinel name; unknown codes are reported as "UNKNOWN".
func BuildUserError(code error, message string) UserError {
	switch {
	case errors.Is(code, ErrProviderRateLimit):
		return UserError{Status: "recovering", Code: "PROVIDER_RATE_LIMIT", Retryable: true, Message: message, NextAction: "fallback"}
	case errors.Is(code, ErrProviderUnavailable):
		return UserError{Status: "recovering", Code: "PROVIDER_UNAVAILABLE", Retryable: true, Message: message, NextAction: "fallback"}
	case errors.Is(code, ErrProviderTimeout):
		return UserError{Status: "recovering", Code: "PROVIDER_TIMEOUT", Retryable: true, Message: message, NextAction: "retry"}
	case errors.Is(code, ErrToolTimeout):
		return UserError{Status: "recovering", Code: "TOOL_TIMEOUT", Retryable: true, Message: message, NextAction: "retry"}
	case errors.Is(code, ErrToolPermission):
		return UserError{Status: "failed", Code: "TOOL_PERMISSION", Retryable: false, Message: message, NextAction: "request_permission"}
	case errors.Is(code, ErrContextOverflow):
		return UserError{Status: "recovering", Code: "CONTEXT_OVERFLOW", Retryable: true, Message: message, NextAction: "summarize"}
	case errors.Is(code, ErrResourceLimit):
		return UserError{Status: "failed", Code: "RESOURCE_LIMIT", Retryable: false, Message: message, NextAction: "reduce_scope"}
	case errors.Is(code, ErrPolicyDenied):
		return UserError{Status: "failed", Code: "POLICY_DENIED", Retryable: false, Message: message, NextAction: "none"}
	case errors.Is(code, ErrVerificationFailed):
		return UserError{Status: "failed", Code: "VERIFICATION_FAILED", Retryable: true, Message: message, NextAction: "repair"}
	case errors.Is(code, ErrUserCancelled):
		return UserError{Status: "failed", Code: "USER_CANCELLED", Retryable: false, Message: message, NextAction: "none"}
	default:
		return UserError{Status: "failed", Code: "UNKNOWN", Retryable: false, Message: message, NextAction: "none"}
	}
}