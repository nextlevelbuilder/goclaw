//go:build pipnpm_e2e

// Package integration contains optional end-to-end tests for pip + npm update flow.
// These tests require real pip3 and npm on PATH. They are excluded from default CI
// and must be opted into via: go test -tags pipnpm_e2e ./tests/integration/...
//
// Typical pre-conditions in a test container:
//
//	pip3 install --break-system-packages "requests==2.25.0"
//	npm install -g "typescript@4.0.0"
package integration

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// TestPipUpdateChecker_E2E verifies that PipUpdateChecker detects a known-stale
// package and PipUpdateExecutor upgrades it successfully.
//
// Pre-condition: pip3 must be on PATH and "requests==2.25.0" must be installed.
// The test installs the old version itself if pip3 is available.
func TestPipUpdateChecker_E2E(t *testing.T) {
	if _, err := exec.LookPath("pip3"); err != nil {
		t.Skip("pip3 not on PATH — skipping pip e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Install a known-stale version of requests.
	installCmd := exec.CommandContext(ctx, "pip3", "install",
		"--break-system-packages", "--quiet", "requests==2.25.0")
	if out, err := installCmd.CombinedOutput(); err != nil {
		t.Fatalf("pre-condition: install requests==2.25.0 failed: %v\n%s", err, out)
	}

	// Check: PipUpdateChecker should detect requests as outdated.
	checker := skills.NewPipUpdateChecker()
	result := checker.Check(ctx, nil)

	if !result.Available {
		t.Fatal("PipUpdateChecker: Available=false with pip3 on PATH")
	}
	if result.Err != nil {
		t.Fatalf("PipUpdateChecker: unexpected error: %v", result.Err)
	}

	var requestsUpdate *skills.UpdateInfo
	for i := range result.Updates {
		if result.Updates[i].Name == "requests" {
			requestsUpdate = &result.Updates[i]
			break
		}
	}
	if requestsUpdate == nil {
		t.Fatal("PipUpdateChecker: 'requests' not listed as outdated (expected >=2.25.0 to have update)")
	}
	if requestsUpdate.CurrentVersion != "2.25.0" {
		t.Errorf("CurrentVersion = %q, want 2.25.0", requestsUpdate.CurrentVersion)
	}
	t.Logf("requests: %s → %s", requestsUpdate.CurrentVersion, requestsUpdate.LatestVersion)

	// Apply: PipUpdateExecutor should upgrade requests.
	executor := skills.NewPipUpdateExecutor()
	if err := executor.Update(ctx, "requests", requestsUpdate.LatestVersion, requestsUpdate.Meta); err != nil {
		t.Fatalf("PipUpdateExecutor: Update failed: %v", err)
	}

	// Re-check: requests should no longer be in the outdated list.
	result2 := checker.Check(ctx, nil)
	for _, u := range result2.Updates {
		if u.Name == "requests" {
			t.Errorf("requests still outdated after update: current=%s latest=%s",
				u.CurrentVersion, u.LatestVersion)
		}
	}
}

// TestNpmUpdateChecker_E2E verifies that NpmUpdateChecker detects a known-stale
// global npm package and NpmUpdateExecutor upgrades it.
//
// Pre-condition: npm must be on PATH and "typescript@4.0.0" must be globally installed.
func TestNpmUpdateChecker_E2E(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not on PATH — skipping npm e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Install a known-stale version of typescript globally.
	installCmd := exec.CommandContext(ctx, "npm", "install", "-g", "typescript@4.0.0")
	if out, err := installCmd.CombinedOutput(); err != nil {
		t.Fatalf("pre-condition: install typescript@4.0.0 failed: %v\n%s", err, out)
	}

	// Check: NpmUpdateChecker should detect typescript as outdated.
	checker := skills.NewNpmUpdateChecker()
	result := checker.Check(ctx, nil)

	if !result.Available {
		t.Fatal("NpmUpdateChecker: Available=false with npm on PATH")
	}
	if result.Err != nil {
		t.Fatalf("NpmUpdateChecker: unexpected error: %v", result.Err)
	}

	var tsUpdate *skills.UpdateInfo
	for i := range result.Updates {
		if result.Updates[i].Name == "typescript" {
			tsUpdate = &result.Updates[i]
			break
		}
	}
	if tsUpdate == nil {
		t.Fatal("NpmUpdateChecker: 'typescript' not listed as outdated (expected 4.0.0 to have update)")
	}
	t.Logf("typescript: %s → %s", tsUpdate.CurrentVersion, tsUpdate.LatestVersion)

	// Apply: NpmUpdateExecutor should upgrade typescript.
	executor := skills.NewNpmUpdateExecutor()
	if err := executor.Update(ctx, "typescript", tsUpdate.LatestVersion, tsUpdate.Meta); err != nil {
		t.Fatalf("NpmUpdateExecutor: Update failed: %v", err)
	}

	// Re-check: typescript should no longer be in the outdated list.
	result2 := checker.Check(ctx, nil)
	for _, u := range result2.Updates {
		if u.Name == "typescript" {
			t.Errorf("typescript still outdated after update: current=%s latest=%s",
				u.CurrentVersion, u.LatestVersion)
		}
	}
}
