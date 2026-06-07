package skills

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ApkUpdateExecutor implements UpdateExecutor for the "apk" source.
// It upgrades a single Alpine package by calling the pkg-helper v2
// `upgrade` action over the privileged Unix socket.
//
// Thread-safe: no mutable state; concurrent package serialization is
// handled upstream by PackageLocker (injected via UpdateRegistry.Apply).
// Process-level apk serialization is handled downstream by apkMutex
// inside pkg-helper. The executor itself acquires NO locks. A second
// PackageLocker.Acquire from this goroutine would deadlock (non-reentrant
// chan struct{} — see update_registry.go:284 and package_lock.go:49-73).
type ApkUpdateExecutor struct{}

// NewApkUpdateExecutor returns an ApkUpdateExecutor ready for use.
func NewApkUpdateExecutor() *ApkUpdateExecutor { return &ApkUpdateExecutor{} }

// Source returns "apk".
func (e *ApkUpdateExecutor) Source() string { return "apk" }

// Update upgrades `name` to the latest available version using the pkg-helper v2
// `upgrade` action over the Unix socket at /tmp/pkg.sock.
//
// Argument ordering matches UpdateExecutor interface: (ctx, name, toVersion, meta).
// `name` is validated via ValidateApkPackageName before any socket dial.
// `toVersion` is used for logging only — apk always upgrades to the latest
// available version from repositories (no pinned-version upgrade in Phase 2b).
// `meta` is accepted for interface symmetry; apk has no pre-release concept.
// On success, cleanCaches is called for disk symmetry with dep_installer.go.
// On failure, resp.Code is mapped via mapApkHelperCodeToSentinel; if the code
// is unrecognized or empty, ClassifyApkStderr is tried; finally a generic error.
//
// IMPORTANT: This method acquires NO PackageLocker. UpdateRegistry.Apply
// (update_registry.go:284) already holds the lock on ("apk", name) before
// invoking Update. PackageLocker is non-reentrant — a second Acquire from
// this goroutine deadlocks until the 5-minute context timeout fires.
func (e *ApkUpdateExecutor) Update(ctx context.Context, name, toVersion string, meta map[string]any) error {
	// Defense-in-depth validation; pkg-helper also validates on its side.
	if err := ValidateApkPackageName(name); err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	start := time.Now()

	// DO NOT acquire sharedPackageLocker() here. See docstring above.
	ok, code, _, errMsg := apkHelperCallFunc(cctx, "upgrade", name)

	durationMs := time.Since(start).Milliseconds()

	if ok {
		// Success: purge caches for disk symmetry with dep_installer.go.
		cleanCaches(cctx)
		slog.Info("package.update.apk.outcome",
			"name", name,
			"to", toVersion,
			"status", "success",
			"duration_ms", durationMs)
		return nil
	}

	// Failure: classify the error code into a sentinel, falling back to stderr.
	sentinel := mapApkHelperCodeToSentinel(code)
	if sentinel == nil {
		sentinel, _ = ClassifyApkStderr(errMsg)
	}
	if sentinel == nil {
		sentinel = fmt.Errorf("apk upgrade failed: %s", errMsg)
	}

	slog.Warn("package.update.apk.outcome",
		"name", name,
		"status", "failed",
		"code", code,
		"err_class", fmt.Sprintf("%T:%v", sentinel, sentinel),
		"reason", truncateStderr(errMsg, 500),
		"duration_ms", durationMs)

	return fmt.Errorf("%w: %s", sentinel, truncateStderr(errMsg, 500))
}

// mapApkHelperCodeToSentinel maps pkg-helper v2 `code` field values to
// Phase 1 apk update sentinels. Returns nil when code is empty or
// unrecognized, delegating to ClassifyApkStderr as the next fallback.
func mapApkHelperCodeToSentinel(code string) error {
	switch code {
	case "validation":
		return ErrInvalidApkPackageName
	case "not_found":
		return ErrUpdateApkNotFound
	case "conflict", "constraint":
		return ErrUpdateApkConflict
	case "locked":
		return ErrUpdateApkLocked
	case "network":
		return ErrUpdateApkNetwork
	case "permission":
		return ErrUpdateApkPermission
	case "disk_full":
		return ErrUpdateApkDiskFull
	case "helper_unavailable":
		return ErrUpdateApkHelperUnavail
	case "helper_error", "system_error", "":
		return nil // fall through to ClassifyApkStderr
	}
	return nil // unrecognized code — fall through
}
