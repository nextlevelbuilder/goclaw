package protocol

import (
	"errors"
	"fmt"
)

// APIError is a non-zero Zalo error_code from either the outer response
// envelope or the decrypted inner data object.
type APIError struct {
	Code int
	Op   string
}

func (e *APIError) Error() string {
	if e == nil {
		return "zalo_personal: api error"
	}
	if e.Op == "" {
		return fmt.Sprintf("zalo_personal: api error code %d", e.Code)
	}
	return fmt.Sprintf("zalo_personal: %s error code %d", e.Op, e.Code)
}

// Unwrap exposes the classified sentinel so callers can use errors.Is.
func (e *APIError) Unwrap() error {
	return classifyCode(e.Code)
}

// Kind returns the classified sentinel for this error code.
func (e *APIError) Kind() error {
	if e == nil {
		return ErrTransient
	}
	return classifyCode(e.Code)
}

// Retryable reports whether the caller may retry the same operation.
func (e *APIError) Retryable() bool {
	return errors.Is(e.Kind(), ErrTransient)
}

var (
	// ErrAuthExpired means the session/cookies are no longer accepted.
	// Re-authentication is required; blind restarts cannot recover it.
	ErrAuthExpired = errors.New("zalo_personal: auth expired")
	// ErrTransient means a network/5xx/rate-limit failure that may succeed on retry.
	ErrTransient = errors.New("zalo_personal: transient failure")
	// ErrPolicy means Zalo rejected the request or content; retrying unchanged
	// input will fail the same way.
	ErrPolicy = errors.New("zalo_personal: policy rejection")
)

// classifyCode buckets observed Zalo error codes. The upstream library exposes
// only the numeric code, and the protocol documentation has no classification
// table, so unknown codes deliberately fall back to transient rather than
// hard-failing a new code forever.
func classifyCode(code int) error {
	switch code {
	case 401, 3000:
		// 3000 is the session-conflict / logged-out code seen when another
		// client takes over the account.
		return ErrAuthExpired
	case 400, 403, 404, 405, 1000, -201:
		return ErrPolicy
	case 408, 429:
		return ErrTransient
	default:
		// Includes 5xx and any new/unobserved code. A new code must not turn
		// every send into a permanent failure.
		return ErrTransient
	}
}

// NewAPIError builds a classified API error.
func NewAPIError(op string, code int) *APIError {
	return &APIError{Code: code, Op: op}
}

// IsAuthError reports whether err is an expired/unauthorized session error.
func IsAuthError(err error) bool {
	return errors.Is(err, ErrAuthExpired)
}

// IsPolicyError reports whether err is a non-retryable policy/content rejection.
func IsPolicyError(err error) bool {
	return errors.Is(err, ErrPolicy)
}

// IsTransient reports whether err is retryable. Non-API errors (network,
// timeout, EOF) are treated as transient.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	return true
}
