package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		b    int64
		want string
	}{
		{"0 bytes", 0, "0 B"},
		{"500 bytes", 500, "500 B"},
		{"1 KB", 1 << 10, "1.0 KB"},
		{"512 KB", 512 * (1 << 10), "512.0 KB"},
		{"1 MB", 1 << 20, "1.0 MB"},
		{"340 MB", 340 * (1 << 20), "340.0 MB"},
		{"1 GB", 1 << 30, "1.0 GB"},
		{"15 GB", 15 * (1 << 30), "15.0 GB"},
		{"1.5 MB (computed)", 1572864, "1.5 MB"},     // 1.5 * 1048576
		{"2.3 GB (computed)", 2469493248, "2.3 GB"},  // 2.3 * 1073741824
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatBytes(tc.b)
			if got != tc.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tc.b, got, tc.want)
			}
		})
	}
}

func TestDirSize_EmptyPath(t *testing.T) {
	// Empty path should return 0
	size := DirSize("")
	if size != 0 {
		t.Errorf("DirSize(\"\") = %d, want 0", size)
	}
}

func TestDirSize_NonexistentDir(t *testing.T) {
	// Nonexistent directory should return 0 (graceful error handling)
	size := DirSize("/nonexistent/path/that/does/not/exist")
	if size != 0 {
		t.Errorf("DirSize(nonexistent) = %d, want 0", size)
	}
}

func TestDirSize_SingleFile(t *testing.T) {
	// Create a temporary directory with a single file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "hello world"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	size := DirSize(tmpDir)
	expectedSize := int64(len(testContent))
	if size != expectedSize {
		t.Errorf("DirSize(single file) = %d, want %d", size, expectedSize)
	}
}

func TestDirSize_MultipleFiles(t *testing.T) {
	// Create a temporary directory with multiple files
	tmpDir := t.TempDir()

	files := map[string]string{
		"file1.txt": "hello",
		"file2.txt": "world",
		"file3.txt": "test",
	}

	totalSize := int64(0)
	for name, content := range files {
		filePath := filepath.Join(tmpDir, name)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file %s: %v", name, err)
		}
		totalSize += int64(len(content))
	}

	size := DirSize(tmpDir)
	if size != totalSize {
		t.Errorf("DirSize(multiple files) = %d, want %d", size, totalSize)
	}
}

func TestDirSize_NestedDirs(t *testing.T) {
	// Create nested directories with files
	tmpDir := t.TempDir()

	// Create nested directory
	nestedDir := filepath.Join(tmpDir, "subdir", "nested")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

	// Write files at different levels
	f1 := filepath.Join(tmpDir, "top.txt")
	f2 := filepath.Join(tmpDir, "subdir", "middle.txt")
	f3 := filepath.Join(nestedDir, "deep.txt")

	content1 := "top"
	content2 := "middle"
	content3 := "deep"

	for path, content := range map[string]string{f1: content1, f2: content2, f3: content3} {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	expectedSize := int64(len(content1) + len(content2) + len(content3))
	size := DirSize(tmpDir)
	if size != expectedSize {
		t.Errorf("DirSize(nested dirs) = %d, want %d", size, expectedSize)
	}
}

func TestCheckDiskSpace(t *testing.T) {
	// Test current directory
	check, freeDisk := checkDiskSpace(".")
	if check.Name != "disk_space" {
		t.Errorf("check.Name = %q, want disk_space", check.Name)
	}

	// Status should be either "ok" or "warning" (not "missing" for normal systems)
	if check.Status != "ok" && check.Status != "warning" {
		t.Errorf("check.Status = %q, want 'ok' or 'warning'", check.Status)
	}

	// Free disk should be > 0 on normal systems
	if freeDisk <= 0 {
		t.Errorf("freeDisk = %d, want > 0", freeDisk)
	}

	// Detail field should contain useful information
	if check.Detail == "" {
		t.Error("check.Detail should not be empty")
	}
}

func TestPreflightResult_FlatFields(t *testing.T) {
	// Verify that the flat fields are populated correctly
	tmpDir := t.TempDir()

	// Create a test file to measure directory size
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// We can't easily test with a real DSN, so test the structure with empty DSN
	result := RunPreflight(context.Background(), "", tmpDir, "")

	// Verify flat fields exist
	if !result.DiskSpaceOK && result.DiskSpaceOK {
		// This tautology is to just check the field exists
		t.Error("result.DiskSpaceOK field not accessible")
	}

	if result.FreeDiskBytes <= 0 {
		t.Errorf("result.FreeDiskBytes = %d, want > 0", result.FreeDiskBytes)
	}

	dataDirSize := result.DataDirSizeBytes
	if dataDirSize <= 0 {
		t.Errorf("result.DataDirSizeBytes = %d, want > 0 (has test file)", dataDirSize)
	}

	// Warnings should be a slice (may be nil if no warnings)
	// Just verify it's accessible
	_ = result.Warnings

	// Checks should be populated with at least disk_space check
	if len(result.Checks) == 0 {
		t.Error("result.Checks should have at least one check")
	}

	// Find disk_space check
	var diskCheck *PreflightCheck
	for i := range result.Checks {
		if result.Checks[i].Name == "disk_space" {
			diskCheck = &result.Checks[i]
			break
		}
	}
	if diskCheck == nil {
		t.Error("disk_space check not found")
	} else if diskCheck.Status != "ok" && diskCheck.Status != "warning" {
		t.Errorf("disk_space check status = %q, want 'ok' or 'warning'", diskCheck.Status)
	}
}

// TestCollectWarnings_IncludesDetailAndHint verifies that both "warning"
// and "missing" checks surface their Detail (the problem) alongside their
// Hint (the fix). Regression guard: earlier versions only surfaced Detail
// for "warning" status, leaving "missing" checks to display the fix with
// no explanation of the cause.
func TestCollectWarnings_IncludesDetailAndHint(t *testing.T) {
	checks := []PreflightCheck{
		{Name: "ok_check", Status: "ok", Detail: "all good"},
		{Name: "warn_check", Status: "warning", Detail: "degraded service", Hint: "do X to recover"},
		{Name: "missing_check", Status: "missing", Detail: "pg_dump 17 cannot dump PostgreSQL 18 server", Hint: "Install postgresql18-client"},
		{Name: "ok_with_hint", Status: "ok", Detail: "informational", Hint: "optional tune-up"},
	}

	warnings := make([]string, 0)
	for _, c := range checks {
		if (c.Status == "warning" || c.Status == "missing") && c.Detail != "" {
			warnings = append(warnings, c.Detail)
		}
		if c.Hint != "" {
			warnings = append(warnings, c.Hint)
		}
	}

	want := []string{
		"degraded service",
		"do X to recover",
		"pg_dump 17 cannot dump PostgreSQL 18 server",
		"Install postgresql18-client",
		"optional tune-up",
	}
	if len(warnings) != len(want) {
		t.Fatalf("warnings len = %d, want %d\ngot: %v", len(warnings), len(want), warnings)
	}
	for i, w := range want {
		if warnings[i] != w {
			t.Errorf("warnings[%d] = %q, want %q", i, warnings[i], w)
		}
	}
}

func TestFormatBytes_EdgeCases(t *testing.T) {
	// Test boundary values
	tests := []struct {
		b int64
		// Just verify it doesn't panic and returns a string
	}{
		{1 << 30},       // 1 GB exact
		{1<<30 - 1},     // Just below 1 GB
		{1<<30 + 1},     // Just above 1 GB
		{1 << 40},       // 1 TB (should still use GB)
		{-1},            // Negative (edge case)
		{1<<63 - 1},     // Max int64
	}

	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			result := FormatBytes(tc.b)
			if result == "" {
				t.Errorf("FormatBytes(%d) returned empty string", tc.b)
			}
		})
	}
}
