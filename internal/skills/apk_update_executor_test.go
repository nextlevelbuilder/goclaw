package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stubApkHelper returns a helper function that always returns the given values.
func stubApkHelper(ok bool, code, data, errMsg string) func(context.Context, string, string) (bool, string, string, string) {
	return func(_ context.Context, _, _ string) (bool, string, string, string) {
		return ok, code, data, errMsg
	}
}

// setApkHelperStub replaces apkHelperCallFunc for the duration of a test and
// restores the original in t.Cleanup.
func setApkHelperStub(t *testing.T, stub func(context.Context, string, string) (bool, string, string, string)) {
	t.Helper()
	orig := apkHelperCallFunc
	apkHelperCallFunc = stub
	t.Cleanup(func() { apkHelperCallFunc = orig })
}

func TestApkExecutor_Source(t *testing.T) {
	e := NewApkUpdateExecutor()
	if got := e.Source(); got != "apk" {
		t.Errorf("Source() = %q, want %q", got, "apk")
	}
}

func TestApkExecutor_InvalidName(t *testing.T) {
	e := NewApkUpdateExecutor()
	// helper must NOT be called — validation rejects before dial.
	called := false
	setApkHelperStub(t, func(_ context.Context, _, _ string) (bool, string, string, string) {
		called = true
		return true, "", "", ""
	})

	// Empty name returns a plain error (not wrapped with sentinel); non-empty
	// invalid names return ErrInvalidApkPackageName via fmt.Errorf("%w", ...).
	emptyErr := e.Update(context.Background(), "", "", nil)
	if emptyErr == nil {
		t.Error("name=\"\": expected error, got nil")
	}

	invalidNames := []string{
		"UPPERCASE",
		"curl;rm",
		"curl@edge",
		"-leading-hyphen",
		"has space",
	}
	for _, name := range invalidNames {
		err := e.Update(context.Background(), name, "", nil)
		if err == nil {
			t.Errorf("name=%q: expected error, got nil", name)
			continue
		}
		if !errors.Is(err, ErrInvalidApkPackageName) {
			t.Errorf("name=%q: errors.Is(err, ErrInvalidApkPackageName) = false; err = %v", name, err)
		}
	}
	if called {
		t.Error("helper was called despite invalid name — validation bypass")
	}
}

func TestApkExecutor_HelperUnavailable(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "helper_unavailable", "", "pkg-helper unavailable: connection refused"))

	err := e.Update(context.Background(), "curl", "8.0.0", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkHelperUnavail) {
		t.Errorf("errors.Is(err, ErrUpdateApkHelperUnavail) = false; err = %v", err)
	}
}

func TestApkExecutor_ConflictError(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "conflict", "", "unsatisfiable constraints"))

	err := e.Update(context.Background(), "libssl3", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkConflict) {
		t.Errorf("errors.Is(err, ErrUpdateApkConflict) = false; err = %v", err)
	}
}

func TestApkExecutor_NotFoundError(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "not_found", "", "ERROR: unable to select packages"))

	err := e.Update(context.Background(), "nonexistent-pkg", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkNotFound) {
		t.Errorf("errors.Is(err, ErrUpdateApkNotFound) = false; err = %v", err)
	}
}

func TestApkExecutor_NetworkError(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "network", "", "fetch failed: connection timed out"))

	err := e.Update(context.Background(), "curl", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkNetwork) {
		t.Errorf("errors.Is(err, ErrUpdateApkNetwork) = false; err = %v", err)
	}
}

func TestApkExecutor_LockedError(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "locked", "", "unable to lock database"))

	err := e.Update(context.Background(), "busybox", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkLocked) {
		t.Errorf("errors.Is(err, ErrUpdateApkLocked) = false; err = %v", err)
	}
}

func TestApkExecutor_PermissionError(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "permission", "", "write permission denied"))

	err := e.Update(context.Background(), "curl", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkPermission) {
		t.Errorf("errors.Is(err, ErrUpdateApkPermission) = false; err = %v", err)
	}
}

func TestApkExecutor_DiskFullError(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "disk_full", "", "no space left on device"))

	err := e.Update(context.Background(), "musl", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkDiskFull) {
		t.Errorf("errors.Is(err, ErrUpdateApkDiskFull) = false; err = %v", err)
	}
}

func TestApkExecutor_Success(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(true, "", "", ""))

	err := e.Update(context.Background(), "curl", "8.5.0", nil)
	if err != nil {
		t.Errorf("expected nil error on success, got: %v", err)
	}
}

func TestApkExecutor_CtxCancel(t *testing.T) {
	e := NewApkUpdateExecutor()

	// Stub returns context.Canceled to simulate context cancellation propagated
	// from apkHelperCall when the connection deadline fires before response.
	setApkHelperStub(t, func(ctx context.Context, _, _ string) (bool, string, string, string) {
		// Respect the already-cancelled context.
		if err := ctx.Err(); err != nil {
			return false, "helper_error", "", err.Error()
		}
		return false, "helper_error", "", "context canceled"
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before Update is called

	err := e.Update(ctx, "curl", "", nil)
	if err == nil {
		t.Fatal("expected error on cancelled ctx, got nil")
	}
	// The error wraps a non-sentinel (generic "apk upgrade failed: ...") since
	// the stub returns code="helper_error" which maps to nil sentinel, and
	// the errMsg "context canceled" doesn't match any ClassifyApkStderr pattern.
	// We assert a non-nil error is returned (not a panic or silent success).
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected error mentioning context canceled, got: %v", err)
	}
}

// TestApkExecutor_EmptyCode_KnownStderr verifies fallback to ClassifyApkStderr
// when the helper returns an empty code but a recognizable stderr string.
func TestApkExecutor_EmptyCode_KnownStderr(t *testing.T) {
	e := NewApkUpdateExecutor()
	// Empty code + stderr that ClassifyApkStderr recognizes as ErrUpdateApkLocked.
	setApkHelperStub(t, stubApkHelper(false, "", "", "unable to lock database"))

	err := e.Update(context.Background(), "curl", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdateApkLocked) {
		t.Errorf("fallback classification: errors.Is(err, ErrUpdateApkLocked) = false; err = %v", err)
	}
}

// TestApkExecutor_EmptyCode_UnknownStderr verifies that an unrecognized code AND
// unrecognized stderr produce a generic (non-sentinel) error string.
func TestApkExecutor_EmptyCode_UnknownStderr(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(false, "", "", "weird cosmic ray error"))

	err := e.Update(context.Background(), "curl", "", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Must NOT be any sentinel — it's a generic wrapped error.
	sentinels := []error{
		ErrUpdateApkConflict, ErrUpdateApkNetwork, ErrUpdateApkLocked,
		ErrUpdateApkNotFound, ErrUpdateApkPermission, ErrUpdateApkDiskFull,
		ErrUpdateApkHelperUnavail, ErrInvalidApkPackageName,
	}
	for _, s := range sentinels {
		if errors.Is(err, s) {
			t.Errorf("unexpected sentinel %v matched for unrecognized stderr", s)
		}
	}
	if !strings.Contains(err.Error(), "apk upgrade failed") {
		t.Errorf("expected generic 'apk upgrade failed' message, got: %v", err)
	}
}

// TestApkExecutor_NoLockAcquire is a regression test for red-team finding C-1.
// It verifies that ApkUpdateExecutor.Update succeeds even without a pre-acquired
// PackageLocker — proving the executor does NOT attempt a second Acquire that
// would deadlock (PackageLocker is non-reentrant).
//
// If the executor ever adds a sharedPackageLocker().Acquire() call, this test
// will either deadlock (timeout) or return a lock-acquire error, causing failure.
func TestApkExecutor_NoLockAcquire(t *testing.T) {
	e := NewApkUpdateExecutor()
	setApkHelperStub(t, stubApkHelper(true, "", "", ""))

	// Intentionally do NOT set a shared PackageLocker — sharedLocker is nil.
	// If the executor calls sharedPackageLocker().Acquire(...), it will panic
	// (nil pointer dereference) or block forever, causing a test timeout.
	orig := sharedLocker.Load()
	sharedLocker.Store(nil)
	t.Cleanup(func() { sharedLocker.Store(orig) })

	err := e.Update(context.Background(), "curl", "8.5.0", nil)
	if err != nil {
		t.Errorf("expected nil error (no lock acquire), got: %v", err)
	}
}
