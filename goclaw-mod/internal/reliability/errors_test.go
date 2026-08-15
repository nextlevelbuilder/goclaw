package reliability

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNewCodeProperties(t *testing.T) {
	cases := []struct {
		code      ErrorCode
		retryable bool
		severity  Severity
	}{
		{ErrProviderRateLimited, true, SeverityWarning},
		{ErrProviderTimeout, true, SeverityError},
		{ErrProviderAuth, false, SeverityError},
		{ErrProviderBilling, false, SeverityError},
		{ErrModelMalformedToolCall, true, SeverityWarning},
		{ErrRunDeadline, false, SeverityWarning},
		{ErrToolPermissionDenied, false, SeverityError},
	}
	for _, c := range cases {
		e := New(c.code, "msg")
		if e.Code != c.code {
			t.Errorf("code mismatch: got %s want %s", e.Code, c.code)
		}
		if e.IsRetryable() != c.retryable {
			t.Errorf("%s: retryable=%v want %v", c.code, e.IsRetryable(), c.retryable)
		}
		if e.Severity != c.severity {
			t.Errorf("%s: severity=%v want %v", c.code, e.Severity, c.severity)
		}
	}
}

func TestClassifyHTTP(t *testing.T) {
	cl := Classer{}
	cases := []struct {
		status int
		body   string
		want   ErrorCode
	}{
		{429, "rate limited", ErrProviderRateLimited},
		{429, "", ErrProviderRateLimited},
		{401, "", ErrProviderAuth},
		{403, "token revoked", ErrProviderAuthPermanent},
		{402, "insufficient balance", ErrProviderBilling},
		{404, "model does not exist", ErrProviderModelNotFound},
		{404, "some other resource", ErrProviderBadRequest},
		{529, "overloaded", ErrProviderOverloaded},
		{503, "server overloaded", ErrProviderOverloaded},
		{500, "internal", ErrProviderServerError},
		{408, "", ErrProviderTimeout},
		{400, "context length exceeded", ErrProviderContextOverflow},
		{400, "invalid_request_error", ErrProviderInvalidResponse},
	}
	for _, tc := range cases {
		if got := cl.ClassifyHTTP(tc.status, tc.body); got != tc.want {
			t.Errorf("ClassifyHTTP(%d,%q)=%s want %s", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestClassifyErrorWrapsAndPreserves(t *testing.T) {
	base := errors.New("things broke")
	re, err := ClassifyError(base)
	if re == nil || err == nil {
		t.Fatalf("expected wrapped reliability error, got nil")
	}
	if !strings.Contains(re.Error(), "things broke") {
		t.Errorf("error string should include cause: %q", re.Error())
	}
	if !re.IsRetryable() {
		t.Errorf("network-ish error should be retryable: %s", re.Code)
	}
}

func TestClassifyErrorRecognizesContext(t *testing.T) {
	re, _ := ClassifyError(context.Canceled)
	if re.Code != ErrRunCancelled {
		t.Errorf("context.Canceled → %s, want %s", re.Code, ErrRunCancelled)
	}
	re2, _ := ClassifyError(context.DeadlineExceeded)
	if re2.Code != ErrProviderTimeout {
		t.Errorf("context.DeadlineExceeded → %s, want %s", re2.Code, ErrProviderTimeout)
	}
}

type fakeTimeoutError struct{ err error }

func (e *fakeTimeoutError) Error() string   { return e.err.Error() }
func (e *fakeTimeoutError) Timeout() bool   { return true }
func (e *fakeTimeoutError) Temporary() bool { return true }

func TestClassifyErrorNetworkTimeout(t *testing.T) {
	var netErr net.Error = &fakeTimeoutError{err: errors.New("i/o timeout")}
	re, _ := ClassifyError(netErr)
	if re.Code != ErrProviderTimeout {
		t.Errorf("net timeout → %s, want %s", re.Code, ErrProviderTimeout)
	}
	if !re.IsRetryable() {
		t.Errorf("timeout should be retryable")
	}
}

func TestClassifyErrorNetworkConnection(t *testing.T) {
	var netErr net.Error = &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	re, _ := ClassifyError(netErr)
	if re.Code != ErrProviderConnection {
		t.Errorf("connection refused → %s, want %s", re.Code, ErrProviderConnection)
	}
}

// canceledAndTimeoutError behaves like a real transport error: it wraps
// context.Canceled (so errors.Is(err, context.Canceled) matches) yet also
// reports Timeout()==true. This is the shape many HTTP clients produce for a
// timed-out request.
type canceledAndTimeoutError struct {
	cause error
}

func (e *canceledAndTimeoutError) Error() string   { return "flaky transport: " + e.cause.Error() }
func (e *canceledAndTimeoutError) Unwrap() error   { return e.cause }
func (e *canceledAndTimeoutError) Timeout() bool   { return true }
func (e *canceledAndTimeoutError) Temporary() bool { return true }

// TestClassifyErrorTimeoutBeatsCancellation is the regression test for the
// net.Error-before-context.Canceled precedence fix: a transport timeout that
// also wraps context.Canceled must classify as a retryable provider timeout,
// NOT a non-retryable run cancellation.
func TestClassifyErrorTimeoutBeatsCancellation(t *testing.T) {
	err := &canceledAndTimeoutError{cause: context.Canceled}
	re, _ := ClassifyError(err)
	if re.Code != ErrProviderTimeout {
		t.Errorf("timeout wrapping context.Canceled → %s, want %s", re.Code, ErrProviderTimeout)
	}
	if !re.IsRetryable() {
		t.Errorf("a transport timeout should be retryable, got non-retryable")
	}
}

func TestIsRetryableRespectsWrapping(t *testing.T) {
	e := New(ErrToolPermissionDenied, "denied")
	if IsRetryable(e) {
		t.Errorf("permission denied should not be retryable")
	}
	cancel := New(ErrRunCancelled, "cancelled")
	wrapped := errors.Join(cancel, e)
	if IsRetryable(wrapped) {
		t.Errorf("joined error should surface non-retryable")
	}
}

func TestReliabilityErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := Wrap(ErrToolTimeout, cause)
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is should match wrapped cause")
	}
	var got *ReliabilityError
	if !errors.As(e, &got) {
		t.Errorf("errors.As should surface ReliabilityError")
	}
}

func TestWithRunContext(t *testing.T) {
	e := New(ErrProviderOverloaded, "busy")
	e.WithRunContext("r-123", "think", 2)
	if e.RunID != "r-123" || e.Stage != "think" || e.Attempt != 2 {
		t.Errorf("run context not preserved: %+v", e)
	}
}

func TestRetryAfter(t *testing.T) {
	e := New(ErrProviderRateLimited, "slow down").WithRetryAfter(42 * time.Second)
	if e.RetryAfter != 42*time.Second {
		t.Errorf("retry after not stored: %v", e.RetryAfter)
	}
}